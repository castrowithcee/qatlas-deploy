package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/uiprefs"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/tui"
)

// runTUI shows the interface; tests replace it.
var runTUI = tui.Run

// terminalStreams returns standard input and stdout when both are terminals. A buffer or a pipe is no terminal.
func terminalStreams(stdout io.Writer) (in, out *os.File, ok bool) {
	in, inOK := stdinStream().(*os.File)
	out, outOK := stdout.(*os.File)
	if !inOK || !outOK || !isTerminal(in) || !isTerminal(out) {
		return nil, nil, false
	}
	return in, out, true
}

// prefsPath is the device-local preferences file of the interface, ~/.qatlas/qdeploy/tui.yaml.
func prefsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return uiprefs.Path(filepath.Join(home, ".qatlas", "qdeploy")), nil
}

func newTUICommand(state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal interface",
		Long: "tui opens the terminal interface. It has three areas, switched with 1 to 3: Stacks lists every stack with its\n" +
			"current and available version, update mode, state, last run and findings; Stack detail shows the chosen\n" +
			"stack with its image versions, changelog, backups and last runs; Runs lists the runs, and enter opens the\n" +
			"log of a run, which follows a run in progress. ? opens the help, r reads the service again, q quits.\n\n" +
			"In Stacks the keys act on the stack under the cursor, or on the stacks ticked with space, one stack\n" +
			"after another: u update, d deploy, p pause, P unpause, i pin, I unpin, g resume (only for a stack paused\n" +
			"after a failed rollback) and c asking the registries now about all stacks. Every action first asks:\n" +
			"y runs it, n, esc and enter cancel. The update question lists the version change of each variable and\n" +
			"the changelog, and a newer major version is flagged as not installed automatically. A failed stack does\n" +
			"not stop the others; the result lists every stack, the stacks that failed stay ticked, and l opens the\n" +
			"log of the run an action started, or the Runs list when it started several. One action runs at a time.\n\n" +
			"It needs a terminal on both standard input and output and the running service; only root may use the\n" +
			"service channel, so run it with sudo. Without a running service it ends with exit code 3 before it draws\n" +
			"anything. Device-local preferences are read from ~/.qatlas/qdeploy/tui.yaml; a missing file means\n" +
			"the defaults.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			in, out, ok := terminalStreams(c.OutOrStdout())
			if !ok {
				return usageFailure("the terminal interface needs a terminal on standard input and standard output",
					"Run 'qdeploy tui' in a terminal, or use the commands such as 'qdeploy status' in scripts.")
			}
			backend := tui.NewBackend(serviceEndpoint())
			// The interface refuses before it takes the terminal, so a missing service or root fails as every
			// channel command does.
			if _, err := backend.Status(c.Context()); err != nil {
				if errors.Is(err, client.ErrNotRunning) {
					state.seen = true
				}
				return failureOf(err)
			}
			opts := tui.Options{Backend: backend, Folders: tuiFolders{}, In: in, Out: out}
			schema, err := tui.PrefsSchema()
			if err != nil {
				return &cli.Failure{Code: output.CodeRuntime, Message: "the terminal interface cannot start: " + err.Error(),
					NextStep: "Report this as a bug of qdeploy.", Cause: err}
			}
			opts.Prefs = uiprefs.Default(schema)
			if path, err := prefsPath(); err != nil {
				opts.PrefsWarnings = []string{fmt.Sprintf("no home directory, using the defaults: %v", err)}
			} else {
				opts.Prefs, opts.PrefsWarnings = uiprefs.Load(path, schema)
			}
			if err := runTUI(opts); err != nil {
				return &cli.Failure{Code: output.CodeRuntime, Message: "the terminal interface failed: " + err.Error(),
					NextStep: "Run 'qdeploy tui' again; if it keeps failing, use the commands such as 'qdeploy status'.", Cause: err}
			}
			return nil
		},
	}
}
