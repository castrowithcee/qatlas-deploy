package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
)

// The sources of a value; tests replace them.
var (
	stdinStream = func() io.Reader { return os.Stdin }
	isTerminal  = func(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }
	readHidden  = func(f *os.File) ([]byte, error) { return term.ReadPassword(int(f.Fd())) }
)

func usageFailure(message, next string) error {
	return &cli.Failure{Code: output.CodeUsage, Message: message, NextStep: next, Usage: true}
}

func newCredentialCommand(program *cobracli.Program, state *notRunning, secrets *redact.Redactor) *cobra.Command {
	group := &cobra.Command{
		Use:   "credential",
		Short: "Manage registry tokens and notification credentials",
		Long: "Credentials are stored by the service and never shown again. Only root may use the service\n" +
			"channel, so run these commands with sudo.",
	}
	set := &cobra.Command{
		Use:   "set <name>",
		Short: "Store a credential read from the terminal or stdin",
		Long: "set reads the value without echo when standard input is a terminal, and otherwise from standard\n" +
			"input up to the end of input, dropping one trailing line end. The value is never an argument: it\n" +
			"would show in the process list and the shell history. A name has 1 to 64 characters, lowercase\n" +
			"letters, digits, '.', '_' or '-', starting with a letter. A value has 4 to 8192 bytes of text\n" +
			"without control characters. An existing credential of that name is replaced.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageFailure("credential set takes exactly one argument, the credential name; the value is never accepted as an argument",
					"Run 'qdeploy credential set <name>' and enter the value at the prompt or pipe it on standard input.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			name := args[0]
			if !credentials.ValidName(name) {
				return usageFailure(credentials.ErrInvalidName.Error(), "Choose another name and run the command again.")
			}
			value, err := readValue(c, name)
			if err != nil {
				return err
			}
			secrets.Add(value)
			if err := credentials.ValidateValue(value); err != nil {
				return usageFailure(err.Error(), "Provide another value and run the command again.")
			}
			if err := credentialCall(c, state, control.CredentialSetCommand,
				control.CredentialSetArgs{Name: name, Value: value}, nil); err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
				{Name: "name", Value: name}, {Name: "stored", Value: true},
			}})
		},
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List the stored credentials by name and modification time",
		Long:  "list shows the names and the modification times of the stored credentials, never their values.",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var res control.CredentialListResult
			if err := credentialCall(c, state, control.CredentialListCommand, nil, &res); err != nil {
				return err
			}
			rows := make([]output.Row, 0, len(res.Credentials))
			for _, e := range res.Credentials {
				rows = append(rows, output.Row{"name": e.Name, "modified": e.Modified.UTC().Format(time.RFC3339)})
			}
			return output.Encode(c.OutOrStdout(), program.Format(),
				output.Collection{Columns: []string{"name", "modified"}, Rows: rows})
		},
	}
	del := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a stored credential",
		Long:  "delete removes the credential of that name from the service's store.",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if !credentials.ValidName(args[0]) {
				return usageFailure(credentials.ErrInvalidName.Error(), "Check the names with 'qdeploy credential list'.")
			}
			if err := credentialCall(c, state, control.CredentialDeleteCommand, control.CredentialNameArgs{Name: args[0]}, nil); err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
				{Name: "name", Value: args[0]}, {Name: "deleted", Value: true},
			}})
		},
	}
	group.AddCommand(set, list, del)
	return group
}

func credentialCall(c *cobra.Command, state *notRunning, command string, args, result any) error {
	if err := call(c.Context(), serviceEndpoint(), command, args, result); err != nil {
		if errors.Is(err, client.ErrNotRunning) {
			state.seen = true
		}
		return failureOf(err)
	}
	return nil
}

// readValue reads the value from the terminal without echo, or from standard input if that is no terminal.
func readValue(c *cobra.Command, name string) (string, error) {
	in := stdinStream()
	if f, ok := in.(*os.File); ok && isTerminal(f) {
		_, _ = fmt.Fprintf(c.ErrOrStderr(), "Value for %s (input is hidden): ", name)
		raw, err := readHidden(f)
		_, _ = fmt.Fprintln(c.ErrOrStderr())
		if err != nil {
			return "", &cli.Failure{Code: output.CodeRuntime, Message: "cannot read the value from the terminal",
				NextStep: "Run the command again, or pipe the value on standard input.", Cause: err}
		}
		return string(raw), nil
	}
	raw, err := io.ReadAll(io.LimitReader(in, credentials.MaxValueBytes+3))
	if err != nil {
		return "", &cli.Failure{Code: output.CodeRuntime, Message: "cannot read the value from standard input",
			NextStep: "Run the command again.", Cause: err}
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if value == "" {
		return "", usageFailure("no value was provided on standard input",
			"Pipe the value on standard input, or run the command at a terminal.")
	}
	return value, nil
}
