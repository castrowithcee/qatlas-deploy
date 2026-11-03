package cli

import (
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

func newWindowCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	group := &cobra.Command{
		Use:   "window",
		Short: "Show or change the maintenance window of the host",
		Long: "In the maintenance window the service asks the registries for the versions of every stack and stores\n" +
			"the result, as 'qdeploy updates --refresh' does. Then it updates the stacks in automatic mode one after\n" +
			"another, as 'qdeploy update <stack>' would, within the ceiling of each stack. A stack in manual mode is\n" +
			"only reported in the service log. A stack is skipped, with a log line that says why, when it is paused or\n" +
			"pinned ('qdeploy pause', 'qdeploy pin'), paused after a failed rollback, removed, has local changes, a run\n" +
			"in progress, was never deployed or has nothing to update.\n\n" +
			"Nothing new begins once the window has closed, neither a stack nor a further version step of a stepwise\n" +
			"update; a step that has begun runs to its end. What did not begin waits for the next window. The work runs\n" +
			"once per opening, also across a restart of the service. 'qdeploy update', 'qdeploy updates --refresh' and\n" +
			"'qdeploy deploy' work at any time. The window is a setting of the service and kept in its own state.\n" +
			"Only root may use the service channel, so run these commands with sudo.",
	}
	show := &cobra.Command{
		Use:   "show",
		Short: "Show the maintenance window and when it opens next",
		Long: "show prints the weekdays, the start time, the duration and the time zone of the window, whether it is\n" +
			"open now (with open_until), when it opens next and when the work of an opening last began. The times\n" +
			"are given in the window's time zone.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var res control.WindowResult
			if err := credentialCall(c, state, control.WindowShowCommand, nil, &res); err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: windowFields(res)})
		},
	}
	var days, start, duration, zone string
	set := &cobra.Command{
		Use:   "set [--days <days>] [--start HH:MM] [--duration <d>] [--zone <zone>]",
		Short: "Change the maintenance window",
		Long: "set replaces the parts of the window given as flags and keeps the others. The running service follows\n" +
			"at once: a window that opens earlier or later is waited for anew, and a window shortened while open\n" +
			"begins nothing after its new end.\n\n" +
			"--days takes weekday names separated by commas (mon,tue or monday,tuesday) or daily. --start takes the\n" +
			"start time HH:MM on the 24-hour clock. --duration takes whole minutes such as 2h, 90m or 1h30m, from 1m\n" +
			"to 12h. --zone takes local (the host's time zone) or an IANA time zone name such as Europe/Berlin or\n" +
			"UTC. The duration is elapsed time, also on a day the clocks change. The default window is daily at 03:00\n" +
			"for 2h, local time.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var a control.WindowSetArgs
			flags := c.Flags()
			if flags.Changed("days") {
				a.Days = strings.Split(days, ",")
				for i := range a.Days {
					a.Days[i] = strings.TrimSpace(a.Days[i])
				}
			}
			a.Start, a.Duration, a.Zone = start, duration, zone
			for _, f := range []struct {
				name, value string
			}{{"start", start}, {"duration", duration}, {"zone", zone}} {
				if flags.Changed(f.name) && f.value == "" {
					return usageFailure("--"+f.name+" needs a value", "Run 'qdeploy window set --help' for the accepted values.")
				}
			}
			if a.Days == nil && a.Start == "" && a.Duration == "" && a.Zone == "" {
				return usageFailure("window set needs at least one of --days, --start, --duration and --zone",
					"Run 'qdeploy window set --start 02:30', for example; 'qdeploy window show' prints the window.")
			}
			var res control.WindowResult
			if err := credentialCall(c, state, control.WindowSetCommand, a, &res); err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: windowFields(res)})
		},
	}
	set.Flags().StringVar(&days, "days", "", "weekdays the window opens on, such as mon,wed,fri, or daily")
	set.Flags().StringVar(&start, "start", "", "start time HH:MM on the 24-hour clock")
	set.Flags().StringVar(&duration, "duration", "", "how long the window stays open, such as 2h or 1h30m")
	set.Flags().StringVar(&zone, "zone", "", "local or an IANA time zone name such as Europe/Berlin")
	group.AddCommand(show, set)
	return group
}

// windowFields lists the window as scalar fields, which every output format can carry.
func windowFields(w control.WindowResult) []output.Field {
	fields := []output.Field{
		{Name: "days", Value: strings.Join(w.Days, ",")},
		{Name: "start", Value: w.Start},
		{Name: "duration", Value: w.Duration},
		{Name: "zone", Value: w.Zone},
		{Name: "open", Value: w.Open},
	}
	if w.Open {
		fields = append(fields, output.Field{Name: "open_until", Value: w.OpenUntil.Format(time.RFC3339)})
	}
	fields = append(fields, output.Field{Name: "next_opening", Value: w.NextOpening.Format(time.RFC3339)})
	if !w.LastRun.IsZero() {
		fields = append(fields, output.Field{Name: "last_run", Value: w.LastRun.Format(time.RFC3339)})
	}
	if w.Running {
		fields = append(fields, output.Field{Name: "running", Value: true})
	}
	return fields
}
