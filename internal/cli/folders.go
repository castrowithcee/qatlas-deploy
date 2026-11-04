package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
)

const (
	// stackFoldersDropIn is the systemd drop-in that lists the stack folders the sandboxed service may write.
	stackFoldersDropIn = "/etc/systemd/system/qdeploy.service.d/10-stack-folders.conf"
	// serviceAccount owns the stack folders the client creates.
	serviceAccount = "qdeploy"
	// folderMode is the mode of a created stack folder.
	folderMode = 0o755
	// serviceWait bounds the wait for the control socket after a restart.
	serviceWait = 30 * time.Second
)

// folderHost is everything the stack folder setup takes from the host. Production code never changes it;
// tests replace it so that no real /etc, systemctl or service account is involved.
type folderHost struct {
	euid func() int
	// root is prepended to the drop-in path.
	root string
	// systemctl runs the service manager with the given arguments.
	systemctl func(ctx context.Context, args ...string) error
	// owner returns the ids of the service account.
	owner func() (uid, gid int, err error)
	// waitService returns once the control socket of the service answers.
	waitService func(ctx context.Context) error
}

var folders = folderHost{
	euid: os.Geteuid,
	systemctl: func(ctx context.Context, args ...string) error {
		out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	},
	owner: func() (int, int, error) {
		u, err := user.Lookup(serviceAccount)
		if err != nil {
			return 0, 0, err
		}
		uid, err := strconv.Atoi(u.Uid)
		if err != nil {
			return 0, 0, err
		}
		gid, err := strconv.Atoi(u.Gid)
		return uid, gid, err
	},
	waitService: waitForService,
}

// waitForService polls the control socket until the service answers; any answer counts, also a refusal.
func waitForService(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, serviceWait)
	defer cancel()
	for {
		var res control.StatusResult
		err := client.Do(ctx, serviceEndpoint(), 2*time.Second, control.StatusCommand, nil, &res)
		var remote *control.Error
		if err == nil || errors.As(err, &remote) || errors.Is(err, client.ErrNoAccess) {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("the service did not answer after the restart")
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (h folderHost) dropIn() string { return filepath.Join(h.root, stackFoldersDropIn) }

func folderEntry(dir string) string { return "ReadWritePaths=-" + dir }

func hasEntry(content, dir string) bool {
	for _, l := range strings.Split(content, "\n") {
		if strings.TrimSpace(l) == folderEntry(dir) {
			return true
		}
	}
	return false
}

// addEntry appends the folder to the drop-in text and keeps everything already in it, so a folder is never
// dropped from the set.
func addEntry(content, dir string) string {
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if !strings.Contains(content, "[Service]") {
		content += "[Service]\n"
	}
	return content + folderEntry(dir) + "\n"
}

func (h folderHost) requireRoot() error {
	if h.euid() != 0 {
		return &cli.Failure{Code: output.CodePermission, Message: "setting up a stack folder needs root; run with sudo",
			NextStep: "Run the same command with sudo."}
	}
	return nil
}

// missing reports whether dir does not exist; something that is not a directory is an error.
func (h folderHost) missing(dir string) (bool, error) {
	if err := service.ValidateStackDir(dir); err != nil {
		return false, usageFailure("the stack folder "+dir+": "+err.Error(), "Pass an absolute path without spaces.")
	}
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return true, nil
	case err != nil:
		return false, folderFailure("cannot inspect the stack folder "+dir, err)
	case !info.IsDir():
		return false, usageFailure("the stack folder "+dir+" is not a directory (a symbolic link is not followed)",
			"Pass the path of a directory.")
	}
	return false, nil
}

func folderFailure(message string, cause error) error {
	return &cli.Failure{Code: output.CodeRuntime, Message: message, Cause: cause,
		NextStep: "Fix the cause and run the command again."}
}

// grant makes the stack folder usable by the service: it creates a missing folder when create is set, adds the
// folder to the drop-in and, when anything changed, reloads systemd, restarts the service and waits for its
// control socket. The caller has confirmed the path before it passes create.
func (h folderHost) grant(ctx context.Context, dir string, create bool, notice io.Writer) error {
	if err := h.requireRoot(); err != nil {
		return err
	}
	absent, err := h.missing(dir)
	if err != nil {
		return err
	}
	created := false
	if absent {
		if !create {
			return &cli.Failure{Code: output.CodeConfirmationRequired, Usage: true,
				Message:  "the stack folder " + dir + " does not exist and was not created",
				NextStep: "Confirm creating it, or create it yourself and run the command again."}
		}
		if err := h.create(dir); err != nil {
			return err
		}
		created = true
		_, _ = fmt.Fprintf(notice, "Created the stack folder %s (owner %s:%s, mode %04o).\n", dir, serviceAccount, serviceAccount, folderMode)
	}
	added, err := h.addToDropIn(dir)
	if err != nil {
		return err
	}
	if !added && !created {
		return nil
	}
	if added {
		_, _ = fmt.Fprintf(notice, "Granted the service write access to %s in %s.\n", dir, h.dropIn())
	}
	return h.restart(ctx, notice)
}

// create makes the folder and its missing parents; only the folder itself belongs to the service account.
func (h folderHost) create(dir string) error {
	uid, gid, err := h.owner()
	if err != nil {
		return folderFailure("cannot find the service account "+serviceAccount, err)
	}
	if err := os.MkdirAll(filepath.Dir(dir), folderMode); err != nil {
		return folderFailure("cannot create the parent of the stack folder "+dir, err)
	}
	if err := os.Mkdir(dir, folderMode); err != nil {
		return folderFailure("cannot create the stack folder "+dir, err)
	}
	// The umask may have narrowed the mode.
	if err := os.Chmod(dir, folderMode); err != nil {
		return folderFailure("cannot set the mode of the stack folder "+dir, err)
	}
	if err := os.Chown(dir, uid, gid); err != nil {
		return folderFailure("cannot give the stack folder "+dir+" to "+serviceAccount, err)
	}
	return nil
}

// addToDropIn lists dir in the drop-in; added is false when it was listed already.
func (h folderHost) addToDropIn(dir string) (added bool, err error) {
	path := h.dropIn()
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, folderFailure("cannot read "+path, err)
	}
	content := string(data)
	if hasEntry(content, dir) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, folderFailure("cannot create "+filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".10-stack-folders-*")
	if err != nil {
		return false, folderFailure("cannot write "+path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(addEntry(content, dir)); err != nil {
		_ = tmp.Close()
		return false, folderFailure("cannot write "+path, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return false, folderFailure("cannot write "+path, err)
	}
	if err := tmp.Close(); err != nil {
		return false, folderFailure("cannot write "+path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, folderFailure("cannot write "+path, err)
	}
	return true, nil
}

func (h folderHost) restart(ctx context.Context, notice io.Writer) error {
	_, _ = fmt.Fprintln(notice, "Restarting the qdeploy service.")
	for _, args := range [][]string{{"daemon-reload"}, {"restart", serviceAccount}} {
		if err := h.systemctl(ctx, args...); err != nil {
			return &cli.Failure{Code: output.CodeRuntime, Message: "the service manager refused: " + err.Error(), Cause: err,
				NextStep: "Run 'systemctl daemon-reload' and 'systemctl restart qdeploy', then run the command again."}
		}
	}
	if err := h.waitService(ctx); err != nil {
		return &cli.Failure{Code: output.CodeUnreachable, Message: err.Error(), Cause: err,
			NextStep: "Check 'systemctl status qdeploy', then run the command again."}
	}
	return nil
}

// repairBound grants the already bound stack folder when the drop-in lacks it, as after an installation that
// predates the drop-in. Without root, a binding or a missing entry it does nothing.
func (h folderHost) repairBound(ctx context.Context, notice io.Writer) error {
	if h.euid() != 0 {
		return nil
	}
	dir, ok := service.BoundDir(stateDir())
	if !ok {
		return nil
	}
	data, err := os.ReadFile(h.dropIn())
	if err == nil && hasEntry(string(data), dir) {
		return nil
	}
	_, _ = fmt.Fprintf(notice, "Notice: the bound stack folder %s has no write grant for the service yet.\n", dir)
	added, err := h.addToDropIn(dir)
	if err != nil || !added {
		return err
	}
	_, _ = fmt.Fprintf(notice, "Granted the service write access to %s in %s.\n", dir, h.dropIn())
	return h.tryRestart(ctx, notice)
}

// tryRestart restarts the service only if it runs, so a service the operator stopped stays stopped; the grant
// then takes effect at its next start.
func (h folderHost) tryRestart(ctx context.Context, notice io.Writer) error {
	for _, args := range [][]string{{"daemon-reload"}, {"try-restart", serviceAccount}} {
		if err := h.systemctl(ctx, args...); err != nil {
			return &cli.Failure{Code: output.CodeRuntime, Message: "the service manager refused: " + err.Error(), Cause: err,
				NextStep: "Run 'systemctl daemon-reload' and 'systemctl try-restart qdeploy'."}
		}
	}
	if h.systemctl(ctx, "is-active", "--quiet", serviceAccount) != nil {
		_, _ = fmt.Fprintln(notice, "The qdeploy service is not running; the grant takes effect at its next start.")
		return nil
	}
	_, _ = fmt.Fprintln(notice, "Restarted the qdeploy service.")
	if err := h.waitService(ctx); err != nil {
		return &cli.Failure{Code: output.CodeUnreachable, Message: err.Error(), Cause: err,
			NextStep: "Check 'systemctl status qdeploy'."}
	}
	return nil
}

// repairBoundFolder is the hook every root client command runs first. A failure only warns: the command the
// operator asked for still runs.
func repairBoundFolder(c *cobra.Command) {
	switch c.Name() {
	case "run", "version", "manpage", "help", "completion":
		return
	}
	if err := folders.repairBound(c.Context(), c.ErrOrStderr()); err != nil {
		_, _ = fmt.Fprintf(c.ErrOrStderr(), "warning: %v\n", err)
	}
}

// prepareFolder is the step of 'repo set' that precedes the clone. A missing folder is created only after the
// operator confirmed the path: by --create, or by answering at a terminal.
func prepareFolder(c *cobra.Command, dir string, create bool) error {
	if err := folders.requireRoot(); err != nil {
		return err
	}
	absent, err := folders.missing(dir)
	if err != nil {
		return err
	}
	if absent && !create {
		if err := confirmFolderCreate(c, dir); err != nil {
			return err
		}
		create = true
	}
	return folders.grant(c.Context(), dir, create, c.ErrOrStderr())
}

// confirmFolderCreate shows the exact path and asks at a terminal; without a terminal it refuses and names the flag.
func confirmFolderCreate(c *cobra.Command, dir string) error {
	in := stdinStream()
	f, ok := in.(*os.File)
	if !ok || !isTerminal(f) {
		return &cli.Failure{Code: output.CodeConfirmationRequired, Usage: true,
			Message:  fmt.Sprintf("the stack folder %s does not exist and was not created", dir),
			NextStep: "Run the command again with --create to create exactly this folder."}
	}
	w := c.ErrOrStderr()
	_, _ = fmt.Fprintf(w, "The stack folder %s does not exist.\n", dir)
	_, _ = fmt.Fprintf(w, "Create it with owner %s:%s and mode %04o and let the service write to it? [y/N] ", serviceAccount, serviceAccount, folderMode)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return &cli.Failure{Code: output.CodeRuntime, Message: "cannot read the answer from the terminal",
			NextStep: "Run the command again.", Cause: err}
	}
	if answer := strings.ToLower(strings.TrimSpace(line)); answer != "y" && answer != "yes" {
		return &cli.Failure{Code: output.CodeConfirmationDeclined, Message: "the stack folder was not confirmed; nothing was created",
			NextStep: "Run the command again with the path you want, or with --create."}
	}
	return nil
}

// tuiFolders gives the terminal interface the same steps as the command.
type tuiFolders struct{}

func (tuiFolders) Missing(dir string) (bool, error) {
	if err := folders.requireRoot(); err != nil {
		return false, err
	}
	return folders.missing(dir)
}

func (tuiFolders) Grant(ctx context.Context, dir string, create bool) error {
	return folders.grant(ctx, dir, create, io.Discard)
}
