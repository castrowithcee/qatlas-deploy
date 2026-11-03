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
		Long: "tui opens the terminal interface: every stack with its current and available version, update mode,\n" +
			"state, last run and findings. qdeploy without a command opens it too when standard input and output\n" +
			"are a terminal. It needs a terminal on both and the running service; only root may use the service\n" +
			"channel, so run it with sudo. Without a running service it ends with exit code 3 before it draws\n" +
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
			opts := tui.Options{Backend: backend, In: in, Out: out}
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
