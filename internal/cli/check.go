package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

func newCheckCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "check [stack]",
		Short: "Show per stack which functions work with its files, and why not",
		Long: "check reports per stack of the bound stacks repository's working copy whether deploy, update (with\n" +
			"its mode automatic, manual or none), rollback with data, backup and the redaction of secrets work\n" +
			"with the stack's files, why not, and what a person would change; then the findings about the files,\n" +
			"each with severity, cause and fix. Without an argument every stack is checked. With --output json\n" +
			"the answer is the whole report. Secret values are never shown or logged, only key names are read. It\n" +
			"reads the working copy as it is: it does not fetch and never changes a file. The exit code is 1 when\n" +
			"at least one finding is an error: a stack that cannot be deployed or a secret that would reach the\n" +
			"remote; 0 otherwise. See 'qdeploy help stack-capabilities' for the functions and their conditions.",
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
			if err := encodeCheck(c.OutOrStdout(), program.Format(), res); err != nil {
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

// encodeCheck writes the report: in JSON and TOON the result as it is, as text one row per function and per
// finding of each stack.
func encodeCheck(w io.Writer, format output.Format, res control.StacksCheckResult) error {
	if format == output.FormatJSON || format == output.FormatTOON {
		return output.Encode(w, format, output.Object{Fields: []output.Field{
			{Name: "dir", Value: res.Dir},
			{Name: "stacks", Value: structured(format, res.Stacks)},
			{Name: "errors", Value: res.Errors},
			{Name: "warnings", Value: res.Warnings},
		}})
	}
	var rows []output.Row
	for _, st := range res.Stacks {
		for _, f := range st.Functions {
			result := "no"
			if f.Works {
				result = "yes"
			}
			if f.Mode != "" {
				result += ", " + f.Mode
			}
			rows = append(rows, output.Row{"stack": st.Stack, "item": f.Function, "result": result, "reason": f.Reason, "action": f.Action})
		}
		for _, f := range st.Findings {
			rows = append(rows, output.Row{"stack": st.Stack, "item": f.Rule, "result": f.Severity, "reason": f.Cause, "action": f.Fix})
		}
	}
	return output.Encode(w, format, output.Collection{Columns: []string{"stack", "item", "result", "reason", "action"}, Rows: rows})
}
