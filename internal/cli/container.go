package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

func newContainerCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	group := &cobra.Command{
		Use:   "container",
		Short: "Show the containers of the host",
		Long: "The container commands read every container of the host from the container engine: the stacks of this\n" +
			"host (own), other Compose projects (foreign) and containers that belong to no project (single). They\n" +
			"change nothing. Only root may use the service channel, so run them with sudo. See 'qdeploy help containers'.",
	}
	group.AddCommand(newContainerList(program, state), newContainerShow(program, state))
	return group
}

func newContainerList(program *cobracli.Program, state *notRunning) *cobra.Command {
	var metrics bool
	cmd := &cobra.Command{
		Use:   "list [--metrics]",
		Short: "List all containers of the host, grouped into own stacks, foreign projects and single containers",
		Long: "list shows each group with its kind, name, state, uptime and restart count, and below a stack or project\n" +
			"its containers with their images. The state is the worst of the group: unhealthy, restarting, stopped,\n" +
			"starting or ok. A foreign project shows its working directory and compose files; a foreign project that\n" +
			"has the name of a stack of this host but was started from another folder is marked as a conflict.\n\n" +
			"--metrics adds CPU and memory of the running containers. Reading them takes about two seconds per\n" +
			"running container, so the command may wait up to two minutes; if a reading fails or time runs out, the\n" +
			"result says metrics are incomplete. With --output json the result of the service is printed as it is.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			limit := client.CallTimeout
			if metrics {
				limit = control.ContainerListTimeout
			}
			var res control.ContainerListResult
			if err := containerCall(c, state, limit, control.ContainerListCommand, control.ContainerListArgs{Metrics: metrics}, &res); err != nil {
				return err
			}
			if res.MetricsIncomplete {
				_, _ = fmt.Fprintln(c.ErrOrStderr(), "note: metrics are incomplete; a running container has no reading")
			}
			return encodeContainerList(c.OutOrStdout(), program.Format(), res, metrics)
		},
	}
	cmd.Flags().BoolVar(&metrics, "metrics", false, "add CPU and memory of the running containers (slow)")
	return cmd
}

func newContainerShow(program *cobracli.Program, state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show the details of one container: ports, mounts, networks, labels and variable names",
		Long: "show takes the name or the ID of a container and shows its group, state, ports, mounts, networks and\n" +
			"labels, and the names of its environment variables. The values of the variables are never shown.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var d control.ContainerDetail
			if err := containerCall(c, state, client.CallTimeout, control.ContainerGetCommand, control.ContainerGetArgs{Name: args[0]}, &d); err != nil {
				return err
			}
			return encodeContainerDetail(c.OutOrStdout(), program.Format(), d)
		},
	}
}

// containerCall adds the codes of the container engine to what failureOf knows.
func containerCall(c *cobra.Command, state *notRunning, limit time.Duration, command string, args, result any) error {
	err := callFor(c.Context(), limit, serviceEndpoint(), command, args, result)
	if err == nil {
		return nil
	}
	if errors.Is(err, client.ErrNotRunning) {
		state.seen = true
	}
	var remote *control.Error
	if errors.As(err, &remote) {
		switch remote.Code {
		case control.CodeDockerUnreachable:
			return &cli.Failure{Code: output.CodeUnreachable, Message: "the container engine is not reachable",
				NextStep: "Start the container engine, or point the service at its socket with 'qdeploy run --docker-socket'.", Cause: err}
		case control.CodeDockerPermission:
			return &cli.Failure{Code: output.CodePermission, Message: "the service may not use the container engine",
				NextStep: "Give the service user access to the engine's socket, then run the command again.", Cause: err}
		case control.CodeDockerTimeout:
			return &cli.Failure{Code: output.CodeTimeout, Message: "the container engine did not answer in time",
				NextStep: "Check that the container engine is healthy, then run the command again.", Cause: err}
		case control.CodeNotFound:
			return &cli.Failure{Code: output.CodeNotFound, Message: "no such container",
				NextStep: "List the containers with 'qdeploy container list'.", Cause: err}
		case control.CodeTooLarge:
			return &cli.Failure{Code: output.CodeRuntime, Message: "the answer is too large for the service channel: " + remote.Message,
				NextStep: "Use 'qdeploy container show <name>' for single containers.", Cause: err}
		}
	}
	return failureOf(err)
}

func encodeContainerList(w io.Writer, format output.Format, res control.ContainerListResult, metrics bool) error {
	if format == output.FormatJSON || format == output.FormatTOON {
		fields := []output.Field{{Name: "groups", Value: structured(format, res.Groups)}}
		if res.MetricsIncomplete {
			fields = append(fields, output.Field{Name: "metrics_incomplete", Value: true})
		}
		return output.Encode(w, format, output.Object{Fields: fields})
	}
	columns := []string{"kind", "name", "state", "uptime", "restarts", "image"}
	if metrics {
		columns = append(columns, "cpu", "memory")
	}
	columns = append(columns, "details")
	rows := make([]output.Row, 0, len(res.Groups))
	for _, g := range res.Groups {
		name := g.Name
		if g.Conflict {
			name += " (conflict)"
		}
		row := output.Row{"kind": g.Kind, "name": name, "state": g.State, "uptime": uptimeText(g.UptimeSeconds),
			"restarts": int64(g.Restarts), "details": groupDetails(g)}
		metricsCells(row, g.Metrics)
		if g.Kind == control.ContainerKindSingle && len(g.Containers) == 1 {
			row["image"] = g.Containers[0].Image
			if g.Containers[0].Health != "" {
				row["details"] = "health " + g.Containers[0].Health
			}
			rows = append(rows, row)
			continue
		}
		rows = append(rows, row)
		for _, ct := range g.Containers {
			cr := output.Row{"name": "  " + ct.Name, "state": ct.State, "uptime": uptimeText(ct.UptimeSeconds),
				"restarts": int64(ct.Restarts), "image": ct.Image}
			if ct.Health != "" {
				cr["details"] = "health " + ct.Health
			}
			metricsCells(cr, ct.Metrics)
			rows = append(rows, cr)
		}
	}
	return output.Encode(w, format, output.Collection{Columns: columns, Rows: rows})
}

func groupDetails(g control.ContainerGroup) string {
	var parts []string
	if g.Conflict {
		parts = append(parts, "conflict: has the name of a stack of this host but was started from another folder")
	}
	if g.WorkingDir != "" {
		parts = append(parts, "dir "+g.WorkingDir)
	}
	if len(g.ConfigFiles) > 0 {
		parts = append(parts, "files "+strings.Join(g.ConfigFiles, ","))
	}
	return strings.Join(parts, "; ")
}

func metricsCells(row output.Row, m *control.ContainerMetrics) {
	if m == nil {
		return
	}
	row["cpu"] = fmt.Sprintf("%.1f%%", m.CPUPercent)
	row["memory"] = bytesText(m.MemoryUsed)
}

// uptimeText shortens seconds to the two largest units; a container that does not run has no uptime.
func uptimeText(s int64) string {
	switch {
	case s <= 0:
		return "-"
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh%dm", s/3600, s%3600/60)
	}
	return fmt.Sprintf("%dd%dh", s/86400, s%86400/3600)
}

func bytesText(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	v, suffix := float64(n), ""
	for _, s := range []string{"K", "M", "G", "T"} {
		v /= unit
		suffix = s
		if v < unit {
			break
		}
	}
	return fmt.Sprintf("%.1f%s", v, suffix)
}

func encodeContainerDetail(w io.Writer, format output.Format, d control.ContainerDetail) error {
	if format == output.FormatJSON || format == output.FormatTOON {
		return output.Encode(w, format, output.Object{Fields: []output.Field{
			{Name: "name", Value: d.Name}, {Name: "id", Value: d.ID}, {Name: "kind", Value: d.Kind},
			{Name: "project", Value: d.Project}, {Name: "conflict", Value: d.Conflict},
			{Name: "image", Value: d.Image}, {Name: "state", Value: d.State}, {Name: "engine_state", Value: d.EngineState},
			{Name: "health", Value: d.Health}, {Name: "uptime_seconds", Value: d.UptimeSeconds}, {Name: "restarts", Value: d.Restarts},
			{Name: "metrics", Value: structured(format, d.Metrics)},
			{Name: "ports", Value: structured(format, d.Ports)}, {Name: "mounts", Value: structured(format, d.Mounts)}, {Name: "networks", Value: structured(format, d.Networks)},
			{Name: "labels", Value: d.Labels}, {Name: "env_names", Value: d.EnvNames},
		}})
	}
	var ports, mounts, networks, labels []string
	for _, p := range d.Ports {
		s := fmt.Sprintf("%d/%s", p.PrivatePort, p.Protocol)
		if p.PublicPort != 0 {
			s = fmt.Sprintf("%s:%d->%s", p.HostIP, p.PublicPort, s)
		}
		ports = append(ports, s)
	}
	for _, m := range d.Mounts {
		src := m.Source
		if m.Name != "" {
			src = m.Name
		}
		s := fmt.Sprintf("%s %s:%s", m.Type, src, m.Destination)
		if m.ReadOnly {
			s += " (ro)"
		}
		mounts = append(mounts, s)
	}
	for _, n := range d.Networks {
		s := n.Name
		if n.IPAddress != "" {
			s += " " + n.IPAddress
		}
		networks = append(networks, s)
	}
	for k, v := range d.Labels {
		labels = append(labels, k+"="+v)
	}
	sort.Strings(labels)
	fields := []output.Field{
		{Name: "name", Value: d.Name}, {Name: "id", Value: d.ID}, {Name: "kind", Value: d.Kind},
		{Name: "project", Value: d.Project}, {Name: "conflict", Value: d.Conflict},
		{Name: "image", Value: d.Image}, {Name: "state", Value: d.State}, {Name: "engine_state", Value: d.EngineState},
		{Name: "health", Value: d.Health}, {Name: "uptime", Value: uptimeText(d.UptimeSeconds)}, {Name: "restarts", Value: int64(d.Restarts)},
		{Name: "ports", Value: strings.Join(ports, ", ")}, {Name: "mounts", Value: strings.Join(mounts, ", ")},
		{Name: "networks", Value: strings.Join(networks, ", ")}, {Name: "labels", Value: strings.Join(labels, ", ")},
		{Name: "env_names", Value: strings.Join(d.EnvNames, ", ")},
	}
	return output.Encode(w, format, output.Object{Fields: fields})
}

// structured hands JSON the typed value, which keeps the order of its fields, and TOON a generic copy, because
// the TOON encoder reads maps and lists but no structs.
func structured(format output.Format, v any) any {
	if format != output.FormatTOON {
		return v
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var generic any
	if json.Unmarshal(raw, &generic) != nil {
		return v
	}
	return generic
}
