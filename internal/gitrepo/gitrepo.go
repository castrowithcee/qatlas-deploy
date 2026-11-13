// Package gitrepo runs the system git against the host's stacks repository over SSH with the service's own
// deploy key, or, in local mode, against a repository without a remote. The remote and the working copy are
// untrusted: git gets a fresh minimal environment, no user or system configuration, no hooks, only the SSH
// transport and a pinned known_hosts file, and its output and run time are bounded. Nothing goes through a
// shell except what git itself hands to GIT_SSH_COMMAND, which here contains only validated, quote-free words.
package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Limits and defaults.
const (
	DefaultTimeout = 2 * time.Minute
	maxStdout      = 4 << 20
	maxStderr      = 64 << 10
	maxErrorText   = 512
)

const fixedPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

const (
	// RootStack names the stack of a working copy whose root is the stack: every path but those below
	// rootSettings.
	RootStack = "."
	// rootSettings is the top-level folder of qdeploy's stack settings; it belongs to no stack.
	rootSettings = ".qatlas"
)

var (
	// ErrOutputLimit means git printed more than the layer accepts; the process was killed.
	ErrOutputLimit = errors.New("git output exceeds the limit")
	// ErrNotEmpty means the clone target holds files.
	ErrNotEmpty = errors.New("clone target is not empty")

	hexRe      = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	safePathRe = regexp.MustCompile(`^/[A-Za-z0-9_./@+-]+$`)
)

// Config names the files git's SSH child may use. Both paths must be absolute and consist of plain path
// characters, because they are words of GIT_SSH_COMMAND; in local mode both may be empty.
type Config struct {
	// KeyFile is the private deploy key, KnownHosts the pinned host keys.
	KeyFile, KnownHosts string
	// Local selects the local mode: the checked-out branch itself is the base, nothing is fetched or pushed,
	// and Push lands a commit on the branch as a fast-forward. Otherwise the base is the remote-tracking ref
	// of origin.
	Local bool
	// Timeout bounds one git call; zero means DefaultTimeout.
	Timeout time.Duration

	// test is settable only from this package's tests.
	test testOptions
}

type testOptions struct {
	allowLocal bool   // permits local paths and the file protocol
	gitPath    string // replaces the git binary
}

// Repo is the working copy at one directory.
type Repo struct {
	dir string
	cfg Config
}

// Change is one entry of the working-copy status: Code is git's two-letter XY state, "??" for untracked.
type Change struct {
	Code, Path string
}

// Open returns the repository handle for the working copy at dir without touching it.
func Open(cfg Config, dir string) (*Repo, error) {
	if err := checkConfig(cfg); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return &Repo{dir: abs, cfg: cfg}, nil
}

// Clone clones url into dir, which must not exist or be empty, checks out the remote's default branch and
// returns the handle.
func Clone(ctx context.Context, cfg Config, url, dir string) (*Repo, error) {
	if cfg.Local {
		return nil, errors.New("clone needs the remote mode")
	}
	r, err := Open(cfg, dir)
	if err != nil {
		return nil, err
	}
	validate := ValidateURL
	if cfg.test.allowLocal {
		validate = validateLocalURL
	}
	if err := validate(url); err != nil {
		return nil, err
	}
	if err := requireEmpty(r.dir); err != nil {
		return nil, err
	}
	args := []string{"clone", "--quiet", "--no-tags", "--template=", "--no-recurse-submodules", "--", url, r.dir}
	if _, err := r.run(ctx, false, maxStdout, args...); err != nil {
		return nil, fmt.Errorf("clone failed: %w", err)
	}
	return r, nil
}

// Fetch updates the remote-tracking ref of the checked-out branch and leaves the working copy alone. In local
// mode there is nothing to fetch; it only requires a checked-out branch.
func (r *Repo) Fetch(ctx context.Context) error {
	branch, err := r.branch(ctx)
	if err != nil || r.cfg.Local {
		return err
	}
	spec := "+refs/heads/" + branch + ":refs/remotes/origin/" + branch
	_, err = r.run(ctx, true, maxStdout, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head",
		"--", "origin", spec)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}
	return nil
}

// RemoteURL returns the URL of the remote "origin". A working copy in local mode has none.
func (r *Repo) RemoteURL(ctx context.Context) (string, error) {
	if r.cfg.Local {
		return "", errors.New("the working copy is in local mode and has no remote")
	}
	out, err := r.run(ctx, true, maxErrorText, "config", "--local", "--get", "remote.origin.url")
	url := strings.TrimSpace(out)
	if err != nil || url == "" {
		return "", errors.New("the working copy has no origin remote")
	}
	return url, nil
}

// Head returns the full commit id of HEAD.
func (r *Repo) Head(ctx context.Context) (string, error) {
	return r.revParse(ctx, "HEAD^{commit}")
}

// RemoteHead returns the commit id of the base ref: the branch as of the last Fetch, in local mode the
// checked-out branch itself.
func (r *Repo) RemoteHead(ctx context.Context) (string, error) {
	ref, err := r.baseRef(ctx)
	if err != nil {
		return "", err
	}
	return r.revParse(ctx, ref+"^{commit}")
}

// baseRef is the ref new commits are built on: refs/remotes/origin/<branch>, in local mode refs/heads/<branch>.
func (r *Repo) baseRef(ctx context.Context) (string, error) {
	branch, err := r.branch(ctx)
	if err != nil {
		return "", err
	}
	if r.cfg.Local {
		return "refs/heads/" + branch, nil
	}
	return "refs/remotes/origin/" + branch, nil
}

// FastForward moves the checked-out branch to commit, a full commit id the caller read before, and only if that
// is a fast-forward. Naming the commit keeps a fetch that moves the remote-tracking ref in between from moving
// the branch further. It never resets, stashes or checks out: git itself refuses when the update would
// overwrite a local change, and a diverged branch stays untouched. Callers still check Status first, because
// git would merge around local changes that do not collide with the update.
func (r *Repo) FastForward(ctx context.Context, commit string) error {
	if !hexRe.MatchString(commit) {
		return errors.New("commit must be a full lowercase commit id")
	}
	if _, err := r.branch(ctx); err != nil {
		return err
	}
	_, err := r.run(ctx, true, maxStdout, "merge", "--quiet", "--ff-only", "--no-autostash", "--no-verify", commit)
	if err != nil {
		return fmt.Errorf("fast-forward failed: %w", err)
	}
	return nil
}

// Status lists the local modifications and untracked files of the working copy; ignored files are left out.
func (r *Repo) Status(ctx context.Context) ([]Change, error) {
	out, err := r.run(ctx, true, maxStdout, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames",
		"--ignore-submodules=none")
	if err != nil {
		return nil, fmt.Errorf("status failed: %w", err)
	}
	var changes []Change
	for _, rec := range strings.Split(out, "\x00") {
		if len(rec) < 4 || rec[2] != ' ' {
			continue
		}
		changes = append(changes, Change{Code: rec[:2], Path: rec[3:]})
	}
	return changes, nil
}

// TrackedFiles returns the sorted paths of the files git tracks below the folder dir, relative to the working
// copy root; dir RootStack lists every tracked file. The index is read; the working copy is not touched.
func (r *Repo) TrackedFiles(ctx context.Context, dir string) ([]string, error) {
	if err := checkRepoPath(dir); err != nil {
		return nil, err
	}
	spec := dir + "/"
	if dir == RootStack {
		spec = RootStack
	}
	out, err := r.run(ctx, true, maxStdout, "--literal-pathspecs", "ls-files", "-z", "--full-name", "--", spec)
	if err != nil {
		return nil, fmt.Errorf("ls-files failed: %w", err)
	}
	var files []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	slices.Sort(files)
	return files, nil
}

// IsIgnored reports whether the ignore rules of the working copy cover path, a path relative to the working
// copy root, whether or not the file exists or is tracked.
func (r *Repo) IsIgnored(ctx context.Context, path string) (bool, error) {
	if err := checkRepoPath(path); err != nil {
		return false, err
	}
	_, err := r.run(ctx, true, maxErrorText, "check-ignore", "--quiet", "--no-index", "--", path)
	var failure *runFailure
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &failure) && failure.code == 1:
		return false, nil
	}
	return false, fmt.Errorf("check-ignore failed: %w", err)
}

// checkRepoPath accepts only clean relative paths inside the working copy.
func checkRepoPath(p string) error {
	if p == "" || filepath.IsAbs(p) || strings.ContainsRune(p, 0) || filepath.Clean(p) != p || p == ".." ||
		strings.HasPrefix(p, "../") || strings.HasPrefix(p, "-") {
		return errors.New("the path must be a clean path relative to the working copy")
	}
	return nil
}

// ChangedFolders returns the sorted top-level folders that differ between the commits from and to, each given
// as a full commit id. Files in the repository root count only for RootStack, which is listed when any path
// outside rootSettings differs.
func (r *Repo) ChangedFolders(ctx context.Context, from, to string) ([]string, error) {
	if !hexRe.MatchString(from) || !hexRe.MatchString(to) {
		return nil, errors.New("commits must be full lowercase commit ids")
	}
	out, err := r.run(ctx, true, maxStdout, "diff-tree", "-r", "--name-only", "-z", "--no-renames", "--no-commit-id", from, to, "--")
	if err != nil {
		return nil, fmt.Errorf("diff failed: %w", err)
	}
	set := map[string]bool{}
	for _, p := range strings.Split(out, "\x00") {
		top, _, nested := strings.Cut(p, "/")
		if top != "" && top != rootSettings {
			set[RootStack] = true
		}
		if nested && top != "" {
			set[top] = true
		}
	}
	folders := make([]string, 0, len(set))
	for f := range set {
		folders = append(folders, f)
	}
	slices.Sort(folders)
	return folders, nil
}

func (r *Repo) branch(ctx context.Context) (string, error) {
	out, err := r.run(ctx, true, maxErrorText, "symbolic-ref", "--quiet", "--short", "HEAD")
	name := strings.TrimSpace(out)
	if err != nil || name == "" || strings.HasPrefix(name, "-") {
		return "", errors.New("the working copy is not on a branch")
	}
	return name, nil
}

func (r *Repo) revParse(ctx context.Context, rev string) (string, error) {
	out, err := r.run(ctx, true, maxErrorText, "rev-parse", "--verify", "--quiet", rev)
	id := strings.TrimSpace(out)
	if err != nil || !hexRe.MatchString(id) {
		return "", fmt.Errorf("cannot resolve %s", strings.TrimSuffix(rev, "^{commit}"))
	}
	return id, nil
}

// run executes git with the hardened environment. inRepo selects the working copy as git's directory.
func (r *Repo) run(ctx context.Context, inRepo bool, outLimit int, args ...string) (string, error) {
	return r.runEnv(ctx, inRepo, outLimit, nil, args...)
}

// runEnv is run with extra environment entries appended to the minimal environment.
func (r *Repo) runEnv(ctx context.Context, inRepo bool, outLimit int, extraEnv []string, args ...string) (string, error) {
	return r.runIO(ctx, inRepo, outLimit, extraEnv, ioOptions{}, args...)
}

// ioOptions are the rarely needed knobs of runIO: stdin is what git reads, and truncate makes output beyond
// the limit end the process quietly, returning its head, instead of failing with ErrOutputLimit.
type ioOptions struct {
	stdin    io.Reader
	truncate bool
}

func (r *Repo) runIO(ctx context.Context, inRepo bool, outLimit int, extraEnv []string, opts ioOptions, args ...string) (string, error) {
	gitPath := r.cfg.test.gitPath
	if gitPath == "" {
		p, err := lookIn(fixedPath, "git")
		if err != nil {
			return "", errors.New("git is not installed")
		}
		gitPath = p
	}
	tmp, err := os.MkdirTemp("", "qdeploy-git-*")
	if err != nil {
		return "", fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(tmp)
	env := append(r.environment(tmp), extraEnv...)

	timeout := r.cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	full := r.baseOptions()
	if inRepo {
		full = append(full, "-C", r.dir)
	}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, gitPath, full...)
	cmd.Dir = tmp
	cmd.Env = env
	stdout := &capped{limit: outLimit, onOver: cancel}
	stderr := &capped{limit: maxStderr, onOver: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.Stdin = opts.stdin
	confine(cmd)

	runErr := cmd.Run()
	switch {
	case stderr.over || (stdout.over && !opts.truncate):
		return "", ErrOutputLimit
	case stdout.over:
		return stdout.String(), nil
	case ctx.Err() != nil:
		return "", ctx.Err()
	case runErr != nil:
		failure := &runFailure{text: failureText(runErr, stderr.String()), code: -1, stdout: stdout.String()}
		var exit *exec.ExitError
		if errors.As(runErr, &exit) {
			failure.code = exit.ExitCode()
		}
		return "", failure
	}
	return stdout.String(), nil
}

// runFailure is a failed git call; code is git's exit status, -1 when git did not run to its end.
type runFailure struct {
	text   string
	code   int
	stdout string
}

func (f *runFailure) Error() string { return f.text }

// baseOptions switches off everything repository-defined code could hang on and the transports other than
// SSH. safe.directory admits the working copy when another user owns it; git honours it only from protected
// configuration such as the command line, and compares it with the resolved path.
func (r *Repo) baseOptions() []string {
	fileAllow := "never"
	if r.cfg.test.allowLocal {
		fileAllow = "always"
	}
	opts := []string{"-c", "safe.directory=" + r.dir}
	if real, err := filepath.EvalSymlinks(r.dir); err == nil && real != r.dir {
		opts = append(opts, "-c", "safe.directory="+real)
	}
	return append(opts,
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "core.untrackedCache=false",
		"-c", "core.attributesFile=/dev/null",
		"-c", "credential.helper=",
		"-c", "protocol.file.allow="+fileAllow,
		"-c", "protocol.ext.allow=never",
		"-c", "submodule.recurse=false",
		"-c", "fetch.recurseSubmodules=false",
		"-c", "transfer.fsckObjects=true",
		"-c", "gc.auto=0",
		"-c", "maintenance.auto=false",
	)
}

// environment is everything the child gets; nothing of the caller's environment is passed on.
func (r *Repo) environment(tmp string) []string {
	protocols := "ssh"
	if r.cfg.test.allowLocal {
		protocols = "ssh:file"
	}
	home := filepath.Join(tmp, "home")
	env := []string{
		"PATH=" + fixedPath,
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"TMPDIR=" + tmp,
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_ALLOW_PROTOCOL=" + protocols,
		"GIT_CEILING_DIRECTORIES=" + filepath.Dir(r.dir),
		"SSH_AUTH_SOCK=",
		"GIT_SSH_COMMAND=" + strings.Join([]string{
			"ssh", "-F", "/dev/null",
			"-o", "BatchMode=yes",
			"-o", "IdentitiesOnly=yes",
			"-o", "IdentityFile=" + r.cfg.KeyFile,
			"-o", "StrictHostKeyChecking=yes",
			"-o", "UserKnownHostsFile=" + r.cfg.KnownHosts,
			"-o", "GlobalKnownHostsFile=/dev/null",
			"-o", "PasswordAuthentication=no",
			"-o", "KbdInteractiveAuthentication=no",
			"-o", "ForwardAgent=no",
			"-o", "UpdateHostKeys=no",
			"-o", "ConnectTimeout=20",
		}, " "),
	}
	_ = os.MkdirAll(filepath.Join(home, ".config"), 0o700)
	return env
}

func checkConfig(cfg Config) error {
	if cfg.Local && cfg.KeyFile == "" && cfg.KnownHosts == "" {
		return nil
	}
	for _, p := range []string{cfg.KeyFile, cfg.KnownHosts} {
		if !safePathRe.MatchString(p) || filepath.Clean(p) != p {
			return errors.New("key and known_hosts paths must be absolute, clean and use only letters, digits and ._/@+-")
		}
	}
	return nil
}

func requireEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("clone target: %w", err)
	case len(entries) > 0:
		return ErrNotEmpty
	}
	return nil
}

func lookIn(path, name string) (string, error) {
	for _, d := range filepath.SplitList(path) {
		p := filepath.Join(d, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

var userinfoRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/@\s'"]*@`)

// failureText is git's stderr on one line, without URL userinfo and cut to a short length.
func failureText(runErr error, stderr string) string {
	text := strings.Join(strings.Fields(userinfoRe.ReplaceAllString(stderr, "${1}")), " ")
	if text == "" {
		text = runErr.Error()
	}
	if len(text) > maxErrorText {
		text = text[:maxErrorText] + "..."
	}
	return text
}

// capped keeps the head of what is written; once more arrives it calls onOver, which ends the process.
type capped struct {
	buf    bytes.Buffer
	limit  int
	over   bool
	onOver func()
}

func (c *capped) Write(p []byte) (int, error) {
	n := len(p)
	if room := c.limit - c.buf.Len(); n > room {
		if !c.over && c.onOver != nil {
			c.onOver()
		}
		c.over = true
		p = p[:max(room, 0)]
	}
	c.buf.Write(p)
	return n, nil
}

func (c *capped) String() string { return c.buf.String() }
