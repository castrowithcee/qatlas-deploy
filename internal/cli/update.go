package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
)

// updateCallLimit bounds one update call, which waits for every step of every stack.
const updateCallLimit = 24 * time.Hour

// The result shown for a stack without steps and for a step that was not attempted.
const (
	resultCurrent  = "current"
	resultRefused  = "refused"
	resultSkipped  = "skipped"
	maxShownCommit = 12
)

var updateRunColumns = []string{"stack", "variable", "from", "to", "run", "commit", "result", "error"}

func newUpdateCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:   "update <stack>... [--version <v>]",
		Short: "Update the image versions of stacks with a commit and a secured run per version step",
		Long: "update moves the image version variables of each named stack to the version its rules allow, asking\n" +
			"the registries now; --version names the version instead, also above the ceiling, for a stack with one\n" +
			"image version variable. It must be in the registry's tag list, of the same variant and higher than the\n" +
			"current version. Under update.major stepwise a jump across major versions takes the highest version of\n" +
			"each major version on the way, one step each.\n\n" +
			"Each step stops the stack, backs up its data, commits the new version to the stack's .env (only that\n" +
			"line changes), pushes the commit to the branch, then pulls, starts and checks the stack. A step that\n" +
			"fails is rolled back like any run, and the later steps are not attempted; the earlier steps stay. A\n" +
			"failure before the push leaves the branch as it was and starts the previous state again. The stacks run\n" +
			"one after another. Every stack is checked first: one that has no qdeploy.yaml, was never deployed, has\n" +
			"local changes or undeployed commits, is paused or has a run in progress refuses the whole call before\n" +
			"anything changes. The deploy key needs write access to the branch. The command waits until the last\n" +
			"step is done and exits with 1 when a stack was not updated.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageFailure("update needs at least one stack", "Run 'qdeploy update <stack>... [--version <v>]'.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			var res control.UpdateRunResult
			a := control.UpdateRunArgs{Stacks: args, Version: target}
			if err := remoteCall(c, state, updateCallLimit, control.UpdateRunCommand, a, &res); err != nil {
				return err
			}
			rows, failed := updateRunRows(res, program.Format() == output.FormatTable)
			if err := output.Encode(c.OutOrStdout(), program.Format(), output.Collection{Columns: updateRunColumns, Rows: rows}); err != nil {
				return err
			}
			if failed > 0 {
				return &cli.Failure{Code: output.CodeRuntime,
					Message:  fmt.Sprintf("%d of %d stack(s) were not updated completely", failed, len(res.Stacks)),
					NextStep: "Find out why with 'qdeploy runs <stack>' and 'qdeploy log <run>'."}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "version", "", "the version to move to instead of the one the rules allow")
	return cmd
}

// updateRunRows lists one row per step, or one per stack without steps, and counts the stacks not updated.
func updateRunRows(res control.UpdateRunResult, short bool) ([]output.Row, int) {
	var rows []output.Row
	failed := 0
	for _, s := range res.Stacks {
		switch {
		case s.Error != "":
			failed++
			rows = append(rows, output.Row{"stack": s.Name, "result": resultRefused, "error": s.Error})
			continue
		case len(s.Steps) == 0:
			rows = append(rows, output.Row{"stack": s.Name, "result": resultCurrent})
			continue
		}
		ok := true
		for _, st := range s.Steps {
			result, commit := st.Result, st.Commit
			if result == "" {
				result = resultSkipped
			}
			if short && len(commit) > maxShownCommit {
				commit = commit[:maxShownCommit]
			}
			ok = ok && result == engine.ResultSucceeded
			rows = append(rows, output.Row{"stack": s.Name, "variable": st.Variable, "from": st.From, "to": st.To,
				"run": st.Run, "commit": commit, "result": result, "error": st.Error})
		}
		if !ok {
			failed++
		}
	}
	return rows, failed
}
