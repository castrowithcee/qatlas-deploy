// Package cli defines the command surface of the qdeploy binary. Commands stay thin adapters: they parse
// options, hand typed results to the encoders, and leave errors and exit codes to the kit.
package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/helptopics"
)

// version is overridden at build time with
// -ldflags "-X github.com/castrowithcee/qatlas-deploy/internal/cli.version=<v>".
var version = "dev"

// Run executes the command line against the given streams and returns the process exit code. It never
// terminates the process, so callers and tests share the same path.
func Run(args []string, stdout, stderr io.Writer) int {
	_, program, state, err := newRoot()
	if err != nil {
		return cli.ExitRuntime
	}
	if _, _, ok := terminalStreams(stdout); ok && len(args) == 0 {
		args = []string{"tui"}
	}
	code := program.Execute(args, stdout, stderr)
	if code == cli.ExitRuntime && state.seen {
		return ExitServiceNotRunning
	}
	return code
}

// newRoot builds the full command tree. Only host commands sit at the top level; every command that
// concerns a stack lives under "stack".
func newRoot() (*cobra.Command, *cobracli.Program, *notRunning, error) {
	root := &cobra.Command{
		Use:   "qdeploy",
		Short: "Roll out and update the Docker stacks of a Linux host from its stacks repository",
		Long: "qdeploy runs as a service on a Linux host and keeps the Docker stacks of that host rolled out and\n" +
			"up to date from its stacks repository.",
		// At a terminal Run starts the interface instead, so the help shows only where no terminal is.
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	secrets := &redact.Redactor{}
	program, err := cobracli.New(root, cobracli.Options{Version: version, Topics: helptopics.All(), Redactor: secrets})
	if err != nil {
		return nil, nil, nil, err
	}
	state := &notRunning{}
	root.AddCommand(newVersionCommand(program), newRunCommand(), newStatusCommand(program, state),
		newKeyCommand(program, state), newRepoCommand(program, state), newStackCommand(program, state),
		newWindowCommand(program, state), newCredentialCommand(program, state, secrets), newTUICommand(state),
		newManpageCommand(program))
	return root, program, state, nil
}

func newVersionCommand(program *cobracli.Program) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of qdeploy",
		RunE: func(c *cobra.Command, _ []string) error {
			return output.Encode(c.OutOrStdout(), program.Format(),
				output.Object{Fields: []output.Field{{Name: "version", Value: version}}})
		},
	}
}
