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

// savePrefs writes the preferences file at path; the Settings area saves through it.
func savePrefs(path string, schema uiprefs.Schema) func(uiprefs.Prefs) error {
	return func(p uiprefs.Prefs) error { return uiprefs.Save(path, p, schema) }
}

func newTUICommand(state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal interface",
		Long: "tui opens the terminal interface. It has five areas, switched with 1 to 5, or with the left and right keys\n" +
			"on the tab line, where the interface starts. Down or enter lead into the content; up in the first row or\n" +
			"at the top of a text, and esc in a list, lead back to the tab line. Tab moves the focus between the two\n" +
			"as well:\n" +
			"Stacks lists every stack with its current and available version, update mode, state, last run and\n" +
			"findings, under a row for each stack folder with its kind and last poll; Stack detail shows the chosen\n" +
			"stack with its image versions, changelog, backups and last runs; Runs lists the runs, and enter opens\n" +
			"the log of a run, which follows a run in progress; Settings holds what applies to the whole host or\n" +
			"this device, on four sub-tabs below the tab line: General (the preferences of this device),\n" +
			"Credentials, Maintenance window and Notifications; Containers lists every container of the\n" +
			"host. ? opens the help, r reads the service again, q quits.\n\n" +
			"The sub-tabs of Settings are a second line under the tab line. From the tab line, down leads to it, and\n" +
			"there left and right choose the sub-tab with wrap-around and show its content at once; down, enter or\n" +
			"tab lead into the content, and up in its first row or field, at the top of a text, or esc lead back to\n" +
			"the sub-tab line, and up or esc again to the tab line. Leaving the General form with unsaved changes,\n" +
			"or choosing another sub-tab or area, first asks whether to discard them. Click a sub-tab to choose it.\n\n" +
			"In Stacks the keys act on the stack under the cursor, or on the stacks ticked with space, one stack\n" +
			"after another: u update, d deploy, p pause, P unpause, i pin, I unpin, g resume (only for a stack paused\n" +
			"after a failed rollback) and c asking the registries now about all stacks. Every action first asks:\n" +
			"y runs it, n, esc and enter cancel. The update question lists the version change of each variable and\n" +
			"the changelog, and a newer major version is flagged as not installed automatically. A failed stack does\n" +
			"not stop the others; the result lists every stack, the stacks that failed stay ticked, and l opens the\n" +
			"log of the run an action started, or the Runs list when it started several. One action runs at a time.\n\n" +
			"The stack folders are managed in Stacks too: a adds a folder (with the steps of qdeploy source add; an\n" +
			"empty host starts there), and on a folder row F fetches it, x releases it after a question and e shows,\n" +
			"generates or stores its deploy key; a folder with no remote has no key to offer. The private key is\n" +
			"never shown. On the row of a local folder O adds a remote: it asks for the SSH address, the host key and\n" +
			"the deploy key (without one, the key view opens), then compares the folder with the repository. A\n" +
			"repository without a commit receives the history, an equal one is taken over, and for a different one\n" +
			"the paths are listed and the state of the host is put on it as one commit after your answer. Secret-like\n" +
			"files in what would be pushed stop it, with no way around. Adding a folder of files offers the same\n" +
			"comparison with an address.\n\n" +
			"The mouse works too: click a tab, a key in the header or footer, or a row, and turn the wheel to scroll.\n" +
			"A click does what the key does, so every action still asks first. The mouse is on by default; with it\n" +
			"on, hold Shift to select text. Settings, General switches it off.\n\n" +
			"It needs a terminal on both standard input and output and the running service; only root may use the\n" +
			"service channel, so run it with sudo. Without a running service it ends with exit code 3 before it draws\n" +
			"anything. Device-local preferences are read from ~/.qatlas/qdeploy/tui.yaml; a missing file means\n" +
			"the defaults. Change them in Settings under General (theme, start area, language and mouse) instead of\n" +
			"editing the file by hand.",
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
				opts.SavePrefs = savePrefs(path, schema)
			}
			if err := runTUI(opts); err != nil {
				return &cli.Failure{Code: output.CodeRuntime, Message: "the terminal interface failed: " + err.Error(),
					NextStep: "Run 'qdeploy tui' again; if it keeps failing, use the commands such as 'qdeploy status'.", Cause: err}
			}
			return nil
		},
	}
}
