package cli

import (
	"fmt"
	"regexp"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

var runIDShape = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)

func newDeployCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "deploy <stack>",
		Short: "Start a manual run of a stack",
		Long: "deploy starts a run of the stack at the commit the working copy has now; it does not fetch. It returns as\n" +
			"soon as the run is started and prints its id; follow it with 'qdeploy runs' and 'qdeploy log <run>'. A\n" +
			"stack with a run in progress, with local changes or that cannot be read is refused.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.DeployResult
			if err := credentialCall(c, state, control.DeployCommand, control.DeployArgs{Stack: args[0]}, &res); err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
				{Name: "run", Value: res.ID}, {Name: "stack", Value: res.Stack}, {Name: "commit", Value: res.Commit},
			}})
		},
	}
}

func newRunsCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "runs [stack]",
		Short: "List the recorded runs, newest first",
		Long:  "runs lists the recorded runs of all stacks, or of one stack, newest first, with trigger, result and duration.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var a control.RunsArgs
			if len(args) == 1 {
				a.Stack = args[0]
			}
			var res control.RunsResult
			if err := credentialCall(c, state, control.RunsCommand, a, &res); err != nil {
				return err
			}
			rows := make([]output.Row, 0, len(res.Runs))
			for _, r := range res.Runs {
				commit := r.Commit
				if len(commit) > 12 && program.Format() == output.FormatTable {
					commit = commit[:12]
				}
				rows = append(rows, output.Row{
					"run": r.ID, "stack": r.Stack, "commit": commit, "trigger": r.Trigger,
					"started":  r.Started.UTC().Format(time.RFC3339),
					"duration": (time.Duration(r.DurationMS) * time.Millisecond).String(),
					"result":   r.Result, "error": r.Error,
				})
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Collection{
				Columns: []string{"run", "stack", "commit", "trigger", "started", "duration", "result", "error"}, Rows: rows})
		},
	}
}

func newLogCommand(_ *cobracli.Program, state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "log <run>",
		Short: "Print the log of one run",
		Long:  "log prints the log of the run with the given id, as listed by 'qdeploy runs'.",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if !runIDShape.MatchString(args[0]) {
				return &cli.Failure{Code: output.CodeNotFound, Message: "no such run",
					NextStep: "List the runs with 'qdeploy runs'.", Cause: fmt.Errorf("malformed run id")}
			}
			var res control.LogResult
			if err := credentialCall(c, state, control.LogCommand, control.LogArgs{ID: args[0]}, &res); err != nil {
				return err
			}
			_, err := fmt.Fprint(c.OutOrStdout(), res.Text)
			return err
		},
	}
}
