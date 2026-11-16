package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/castrowithcee/qatlas-kit/docker"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// DefaultDockerSocket is the socket of the container engine unless the configuration names another.
const DefaultDockerSocket = "/var/run/docker.sock"

const (
	// metricsWorkers bounds the readings in flight; each one takes about two seconds at the engine.
	metricsWorkers = 8
	// metricsBudget bounds the whole reading phase of one list.
	metricsBudget = time.Minute
	// envelopeReserve is left of MaxResponseBytes for the frame around the result.
	envelopeReserve = 4 << 10
)

// Docker is the part of the container engine client the service uses; *docker.Client implements it.
type Docker interface {
	List(ctx context.Context) ([]docker.Container, error)
	Inspect(ctx context.Context, ref string) (docker.Details, error)
	Stats(ctx context.Context, ref string) (docker.Stats, error)
	Start(ctx context.Context, ref string) error
	Stop(ctx context.Context, ref string, grace time.Duration) error
	Restart(ctx context.Context, ref string, grace time.Duration) error
	WaitHealthy(ctx context.Context, refs []string, opts docker.WaitOptions) ([]docker.WaitResult, error)
	Logs(ctx context.Context, ref string, opts docker.LogOptions) (LogReader, error)
}

type containerService struct {
	docker Docker
	// stackDirs returns the folder of every stack of the bound repository by name; empty without a binding.
	stackDirs func() map[string]string
	now       func() time.Time
	log       *slog.Logger
	eng       *sourceSet
	// wait bounds how long a start or restart waits for the container to be ready.
	wait time.Duration
	// secrets redacts stack secret values that Compose interpolated into container text.
	secrets *redact.Redactor
}

// text prepares engine text for an answer: secret values are redacted, then control characters masked.
func (s *containerService) text(v string) string { return maskControl(s.secrets.Apply(v)) }

// texts applies text to every element.
func (s *containerService) texts(v []string) []string {
	out := make([]string, len(v))
	for i, e := range v {
		out[i] = s.text(e)
	}
	return out
}

// labelEnvFiles is the Compose label with the env files of a project, as absolute paths separated by commas.
const labelEnvFiles = "com.docker.compose.project.environment_file"

// labelList splits a Compose label that holds a comma-separated list; empty entries are dropped.
func labelList(v string) []string {
	var out []string
	for _, f := range strings.Split(v, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func registerContainerCommands(r *control.Registry, repo *repoService, eng *sourceSet, secrets *redact.Redactor, d Docker, wait time.Duration, log *slog.Logger) error {
	if wait <= 0 {
		wait = containerReadyWait
	}
	s := &containerService{docker: d, now: time.Now, log: log, eng: eng, wait: wait, stackDirs: func() map[string]string {
		// Secrets of every stack are known before an answer is redacted, also those never deployed since start.
		return repo.stackDirs(log, func(st stacks.Stack) { registerStackSecrets(secrets, st) })
	}, secrets: secrets}
	for name, h := range map[string]control.Handler{
		control.ContainerListCommand: s.handleList, control.ContainerGetCommand: s.handleGet,
		control.ContainerLogsCommand:    s.handleLogs,
		control.ContainerStartCommand:   s.action("start"),
		control.ContainerStopCommand:    s.action("stop"),
		control.ContainerRestartCommand: s.action("restart"),
	} {
		if err := r.Register(name, h); err != nil {
			return err
		}
	}
	return nil
}

// stackDirs maps the names of the stacks that own their name to their folders; onStack sees every stack of
// every source. Without a source or a readable working copy it is empty, so nothing counts as own: the
// narrower reading. A working copy that cannot be read contributes nothing. It takes no lock.
func (s *repoService) stackDirs(log *slog.Logger, onStack func(stacks.Stack)) map[string]string {
	srcs := s.set.sources()
	if len(srcs) == 0 {
		return nil
	}
	r := s.set.resolve()
	if r.readable < len(srcs) {
		log.Warn("a stack folder cannot be read; none of its containers counts as own")
	}
	for _, ref := range r.all {
		if onStack != nil {
			onStack(ref.st)
		}
	}
	dirs := make(map[string]string, len(r.owned))
	for _, ref := range r.owned {
		if abs, err := filepath.Abs(ref.st.Dir); err == nil && ref.st.Dir != "" {
			dirs[ref.st.Name] = abs
		}
	}
	return dirs
}

// dockerAnswer maps a failure of the engine to a fixed answer. The kit's messages carry no engine data, but the
// answer stays fixed text anyway.
func (s *containerService) dockerAnswer(err error) error {
	var de *docker.Error
	if !errors.As(err, &de) {
		return err
	}
	switch de.Code {
	case output.CodeUnreachable:
		s.log.Warn("container engine unreachable")
		return control.NewError(control.CodeDockerUnreachable, "the container engine is not reachable")
	case output.CodePermission:
		s.log.Warn("container engine refused access")
		return control.NewError(control.CodeDockerPermission, "the service may not use the container engine")
	case output.CodeTimeout:
		s.log.Warn("container engine timed out")
		return control.NewError(control.CodeDockerTimeout, "the container engine did not answer in time")
	case output.CodeNotFound:
		return control.NewError(control.CodeNotFound, "no such container")
	case output.CodeInvalidRequest:
		return control.NewError(control.CodeInvalid, "the container name is not valid")
	}
	return err
}

// classify decides the kind of a container by its Compose project and working directory (raw label values).
func classify(project, workingDir string, own map[string]string) (kind string, conflict bool) {
	if project == "" {
		return control.ContainerKindSingle, false
	}
	dir, ok := own[project]
	switch {
	case !ok:
		return control.ContainerKindForeign, false
	case workingDir != "" && filepath.Clean(workingDir) == filepath.Clean(dir):
		return control.ContainerKindOwn, false
	}
	return control.ContainerKindForeign, true
}

var stateRank = map[string]int{
	control.ContainerUnhealthy: 4, control.ContainerRestarting: 3, control.ContainerStopped: 2,
	control.ContainerStarting: 1, control.ContainerOK: 0,
}

func stateOf(c docker.Container) string {
	running := c.State == "running"
	switch {
	case running && c.Health == "unhealthy":
		return control.ContainerUnhealthy
	case c.State == "restarting":
		return control.ContainerRestarting
	case !running:
		return control.ContainerStopped
	case c.Health == "starting":
		return control.ContainerStarting
	}
	return control.ContainerOK
}

func (s *containerService) summary(c docker.Container) control.ContainerSummary {
	var up int64
	if c.State == "running" && !c.StartedAt.IsZero() {
		up = max(0, int64(s.now().Sub(c.StartedAt)/time.Second))
	}
	return control.ContainerSummary{
		Name: s.text(c.Name), Image: s.text(c.Image), State: stateOf(c), EngineState: s.text(c.State),
		Health: s.text(c.Health), UptimeSeconds: up, Restarts: c.RestartCount,
	}
}

func (s *containerService) handleList(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.ContainerListArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	all, err := s.docker.List(ctx)
	if err != nil {
		return nil, s.dockerAnswer(err)
	}
	own := s.stackDirs()
	projects, loose := docker.Group(all)

	type entry struct {
		g    control.ContainerGroup
		refs []docker.Container
	}
	var entries []entry
	for _, p := range projects {
		kind, conflict := classify(p.Name, p.WorkingDir, own)
		g := control.ContainerGroup{Kind: kind, Name: s.text(p.Name), Conflict: conflict}
		if kind == control.ContainerKindForeign {
			g.WorkingDir = s.text(p.WorkingDir)
			for _, f := range p.ConfigFiles {
				g.ConfigFiles = append(g.ConfigFiles, s.text(f))
			}
			for _, c := range p.Containers {
				if files := labelList(c.Labels[labelEnvFiles]); files != nil {
					g.EnvFiles = s.texts(files)
					break
				}
			}
		}
		entries = append(entries, entry{g, p.Containers})
	}
	for _, c := range loose {
		entries = append(entries, entry{control.ContainerGroup{Kind: control.ContainerKindSingle, Name: s.text(c.Name)},
			[]docker.Container{c}})
	}
	rank := map[string]int{control.ContainerKindOwn: 0, control.ContainerKindForeign: 1, control.ContainerKindSingle: 2}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i].g, entries[j].g
		if rank[a.Kind] != rank[b.Kind] {
			return rank[a.Kind] < rank[b.Kind]
		}
		return a.Name < b.Name
	})

	var readings map[string]docker.Stats
	incomplete := false
	if a.Metrics {
		readings, incomplete = s.readMetrics(ctx, all)
	}
	res := control.ContainerListResult{Groups: make([]control.ContainerGroup, 0, len(entries)), MetricsIncomplete: incomplete}
	for _, e := range entries {
		g := e.g
		worst, shortest := control.ContainerOK, int64(-1)
		var sum control.ContainerMetrics
		measured := false
		for _, c := range e.refs {
			cs := s.summary(c)
			if st, ok := readings[c.ID]; ok {
				cs.Metrics = &control.ContainerMetrics{CPUPercent: st.CPUPercent, MemoryUsed: st.MemoryUsed, MemoryLimit: st.MemoryLimit}
				sum.CPUPercent += st.CPUPercent
				sum.MemoryUsed += st.MemoryUsed
				measured = true
			}
			if stateRank[cs.State] > stateRank[worst] {
				worst = cs.State
			}
			if c.State == "running" && (shortest < 0 || cs.UptimeSeconds < shortest) {
				shortest = cs.UptimeSeconds
			}
			g.Restarts += cs.Restarts
			g.Containers = append(g.Containers, cs)
		}
		g.State = worst
		g.UptimeSeconds = max(0, shortest)
		if measured {
			g.Metrics = &sum
		}
		res.Groups = append(res.Groups, g)
	}
	return res, fitAnswer(res, "the container list is too large for one answer; ask without metrics or for a single container")
}

// readMetrics reads the running containers with bounded parallelism. A container whose reading fails has no
// entry; incomplete reports that.
func (s *containerService) readMetrics(ctx context.Context, all []docker.Container) (map[string]docker.Stats, bool) {
	ctx, cancel := context.WithTimeout(ctx, metricsBudget)
	defer cancel()
	var (
		mu         sync.Mutex
		wg         sync.WaitGroup
		out        = map[string]docker.Stats{}
		incomplete bool
		sem        = make(chan struct{}, metricsWorkers)
	)
	for _, c := range all {
		if c.State != "running" {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			mu.Lock()
			incomplete = true
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			st, err := s.docker.Stats(ctx, c.ID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				incomplete = true
				return
			}
			out[c.ID] = st
		}()
	}
	wg.Wait()
	return out, incomplete
}

func (s *containerService) handleGet(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.ContainerGetArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Name == "" {
		return nil, control.NewError(control.CodeBadRequest, "a container name is required")
	}
	d, err := s.docker.Inspect(ctx, a.Name)
	if err != nil {
		return nil, s.dockerAnswer(err)
	}
	project, dir := d.Labels["com.docker.compose.project"], d.Labels["com.docker.compose.project.working_dir"]
	kind, conflict := classify(project, dir, s.stackDirs())
	res := control.ContainerDetail{
		ContainerSummary: s.summary(d.Container), ID: s.text(d.ID), Kind: kind, Project: s.text(project), Conflict: conflict,
		Ports: []control.ContainerPort{}, Mounts: []control.ContainerMount{}, Networks: []control.ContainerNetwork{},
		Labels: map[string]string{}, EnvNames: []string{},
	}
	if kind == control.ContainerKindForeign {
		res.WorkingDir = s.text(dir)
		res.ConfigFiles = s.texts(labelList(d.Labels["com.docker.compose.project.config_files"]))
		res.EnvFiles = s.texts(labelList(d.Labels[labelEnvFiles]))
	}
	for _, p := range d.Ports {
		res.Ports = append(res.Ports, control.ContainerPort{PrivatePort: p.PrivatePort, PublicPort: p.PublicPort,
			Protocol: s.text(p.Protocol), HostIP: s.text(p.HostIP)})
	}
	for _, m := range d.Mounts {
		res.Mounts = append(res.Mounts, control.ContainerMount{Type: s.text(m.Type), Name: s.text(m.Name),
			Source: s.text(m.Source), Destination: s.text(m.Destination), ReadOnly: m.ReadOnly})
	}
	for _, n := range d.Networks {
		res.Networks = append(res.Networks, control.ContainerNetwork{Name: s.text(n.Name), IPAddress: s.text(n.IPAddress)})
	}
	for k, v := range d.Labels {
		res.Labels[s.text(k)] = s.text(v)
	}
	for _, n := range d.EnvNames {
		res.EnvNames = append(res.EnvNames, s.text(n))
	}
	return res, fitAnswer(res, "the container details are too large for one answer")
}

// maskControl replaces every rune that is not printable (control characters, line breaks, tabs, C1, format
// controls, invalid UTF-8) with U+FFFD. Everything else stays: no truncation and no folding of spaces, so a
// name or path still matches what the engine holds.
func maskControl(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return unicode.ReplacementChar
	}, s)
}

// fitAnswer refuses an answer that would not fit one frame, instead of letting anything be cut.
func fitAnswer(v any, message string) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > control.MaxResponseBytes-envelopeReserve {
		return control.NewError(control.CodeTooLarge, message)
	}
	return nil
}
