package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

func newCheckCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "check [stack]",
		Short: "Check the stacks of the working copy against the stacks-repo standard",
		Long: "check reports everything in the working copy of the bound stacks repository that contradicts the\n" +
			"standard, one row per finding with stack, rule, severity (error or warning), cause and fix. Without\n" +
			"an argument every stack is checked. Secret values are never shown or logged, only key names are compared. It reads\n" +
			"the working copy as it is: it does not fetch, change files or fix anything. The exit code is 1 when\n" +
			"at least one error was found, 0 otherwise. See 'qdeploy help stack-standard' for the rules.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageFailure("check takes at most one argument, the name of a stack", "Run 'qdeploy stack check [stack]'.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			var req control.StacksCheckArgs
			if len(args) == 1 {
				req.Stack = args[0]
			}
			var res control.StacksCheckResult
			if err := credentialCall(c, state, control.StacksCheckCommand, req, &res); err != nil {
				return err
			}
			rows := make([]output.Row, 0, len(res.Findings))
			for _, f := range res.Findings {
				rows = append(rows, output.Row{
					"stack": f.Stack, "rule": f.Rule, "severity": f.Severity, "cause": f.Cause, "fix": f.Fix,
				})
			}
			if err := output.Encode(c.OutOrStdout(), program.Format(),
				output.Collection{Columns: []string{"stack", "rule", "severity", "cause", "fix"}, Rows: rows}); err != nil {
				return err
			}
			if res.Errors > 0 {
				return &cli.Failure{Code: output.CodeConfigInvalid,
					Message:  fmt.Sprintf("%d error(s) and %d warning(s) found in %d stack(s)", res.Errors, res.Warnings, len(res.Stacks)),
					NextStep: "Apply the fix of each error finding and run the command again."}
			}
			return nil
		},
	}
}
