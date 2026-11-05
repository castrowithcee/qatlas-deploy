package cli

import (
	"context"
	"fmt"
	"io"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// logsPollInterval is how often --follow asks for new lines.
var logsPollInterval = time.Second

// defaultLogTail is the number of last lines shown when --tail is not given.
const defaultLogTail = 100

func newContainerLogs(program *cobracli.Program, state *notRunning) *cobra.Command {
	var stack, since, until string
	var tail int
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <name> | logs --stack <stack> [--since <time>] [--until <time>] [--tail <n>] [--follow]",
		Short: "Show the output of a container or of all containers of a stack, also as it comes in",
		Long: "logs shows what a container wrote to stdout and stderr, or, with --stack, the merged output of all\n" +
			"containers of a stack of this host, ordered by time, each line with its container. It is not the\n" +
			"protocol of a qdeploy run (that is 'qdeploy stack log <run>'). Secret values of the stacks are\n" +
			"replaced by a mask and control characters by U+FFFD.\n\n" +
			"--since and --until take a duration back from now (90s, 15m, 2h) or a time (2026-01-02T15:04:05Z).\n" +
			"--tail shows the last n lines (default 100, 0 for all). --follow keeps running, asks about once a\n" +
			"second for new lines and ends with Ctrl-C. With --output json every line is an object.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if (len(args) == 0) == (stack == "") {
				return &cli.Failure{Code: output.CodeInvalidRequest, Message: "give exactly one of a container name and --stack",
					Usage: true, NextStep: "Run 'qdeploy container logs <name>' or 'qdeploy container logs --stack <stack>'."}
			}
			a := control.ContainerLogsArgs{Stack: stack, Tail: tail}
			if len(args) == 1 {
				a.Name = args[0]
			}
			var err error
			now := time.Now()
			if a.Since, err = parseLogTime("--since", since, now); err != nil {
				return err
			}
			if a.Until, err = parseLogTime("--until", until, now); err != nil {
				return err
			}
			if tail < 0 || tail > control.ContainerLogsMaxTail {
				return &cli.Failure{Code: output.CodeInvalidRequest, Usage: true,
					Message:  fmt.Sprintf("--tail must be between 0 and %d", control.ContainerLogsMaxTail),
					NextStep: "Use a smaller --tail, or narrow the lines with --since."}
			}
			ctx := c.Context()
			if follow {
				var stop context.CancelFunc
				ctx, stop = signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
				defer stop()
				c.SetContext(ctx)
			}
			return readLogs(ctx, c, state, program.Format(), a, follow, a.Stack != "")
		},
	}
	cmd.Flags().StringVar(&stack, "stack", "", "show the merged output of all containers of this stack")
	cmd.Flags().StringVar(&since, "since", "", "only lines from this time on: a duration back from now or an RFC 3339 time")
	cmd.Flags().StringVar(&until, "until", "", "only lines up to this time: a duration back from now or an RFC 3339 time")
	cmd.Flags().IntVar(&tail, "tail", defaultLogTail, "show only the last n lines (0 for all)")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep running and show new lines until interrupted")
	return cmd
}

// parseLogTime reads a duration back from now or an RFC 3339 time; empty means not set.
func parseLogTime(flag, v string, now time.Time) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	if d, err := time.ParseDuration(v); err == nil && d >= 0 {
		return now.Add(-d), nil
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, &cli.Failure{Code: output.CodeInvalidRequest, Usage: true,
			Message:  flag + " is neither a duration such as 15m nor a time such as 2026-01-02T15:04:05Z",
			NextStep: "Fix the value and run the command again."}
	}
	return t, nil
}

// readLogs asks for the log page by page. Every later request continues at the cursor of the answer before, so
// no line is repeated or missed; with follow it keeps asking until the context ends, which is a normal end.
func readLogs(ctx context.Context, c *cobra.Command, state *notRunning, format output.Format, a control.ContainerLogsArgs, follow, withSource bool) error {
	var cursor time.Time
	for {
		req := a
		if !cursor.IsZero() {
			req.Cursor, req.Tail = cursor, 0
		}
		var res control.ContainerLogsResult
		if err := containerCall(c, state, control.ContainerLogsTimeout+client.CallTimeout, control.ContainerLogsCommand, req, &res); err != nil {
			if follow && ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := encodeContainerLogs(c.OutOrStdout(), format, res.Lines, withSource); err != nil {
			return err
		}
		if !res.Cursor.IsZero() {
			cursor = res.Cursor
		}
		if res.More {
			continue
		}
		if !follow {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(logsPollInterval):
		}
	}
}

func encodeContainerLogs(w io.Writer, format output.Format, lines []control.ContainerLogLine, withSource bool) error {
	for _, l := range lines {
		if format == output.FormatJSON || format == output.FormatTOON {
			if err := output.Encode(w, format, output.Object{Fields: []output.Field{
				{Name: "time", Value: l.Time.Format(time.RFC3339Nano)}, {Name: "source", Value: l.Source},
				{Name: "stream", Value: l.Stream}, {Name: "text", Value: l.Text}, {Name: "truncated", Value: l.Truncated},
			}}); err != nil {
				return err
			}
			continue
		}
		text := l.Time.Format("2006-01-02T15:04:05.000Z") + " "
		if withSource {
			text += l.Source + " "
		}
		if l.Stream == "stderr" {
			text += "[stderr] "
		}
		if _, err := fmt.Fprintln(w, text+l.Text); err != nil {
			return err
		}
	}
	return nil
}
