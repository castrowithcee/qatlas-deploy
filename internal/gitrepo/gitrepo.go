// Package gitrepo runs the system git against the host's stacks repository over SSH with the service's own
// deploy key. The remote and the working copy are untrusted: git gets a fresh minimal environment, no user or
// system configuration, no hooks, only the SSH transport and a pinned known_hosts file, and its output and run
// time are bounded. Nothing goes through a shell except what git itself hands to GIT_SSH_COMMAND, which here
// contains only validated, quote-free words.
package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

var (
	// ErrOutputLimit means git printed more than the layer accepts; the process was killed.
	ErrOutputLimit = errors.New("git output exceeds the limit")
	// ErrNotEmpty means the clone target holds files.
	ErrNotEmpty = errors.New("clone target is not empty")

	hexRe      = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	safePathRe = regexp.MustCompile(`^/[A-Za-z0-9_./@+-]+$`)
)

// Config names the files git's SSH child may use. Both paths must be absolute and consist of plain path
// characters, because they are words of GIT_SSH_COMMAND.
type Config struct {
	// KeyFile is the private deploy key, KnownHosts the pinned host keys.
	KeyFile, KnownHosts string
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

// Fetch updates the remote-tracking ref of the checked-out branch and leaves the working copy alone.
func (r *Repo) Fetch(ctx context.Context) error {
	branch, err := r.branch(ctx)
	if err != nil {
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

// Head returns the full commit id of HEAD.
func (r *Repo) Head(ctx context.Context) (string, error) {
	return r.revParse(ctx, "HEAD^{commit}")
}

// RemoteHead returns the commit id of the branch as of the last Fetch.
func (r *Repo) RemoteHead(ctx context.Context) (string, error) {
	branch, err := r.branch(ctx)
	if err != nil {
		return "", err
	}
	return r.revParse(ctx, "refs/remotes/origin/"+branch+"^{commit}")
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

// ChangedFolders returns the sorted top-level folders that differ between the commits from and to, each given
// as a full commit id. Files in the repository root do not count.
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
		if top, _, nested := strings.Cut(p, "/"); nested && top != "" {
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
	env := r.environment(tmp)

	timeout := r.cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	full := append(r.baseOptions(), args...)
	if inRepo {
		full = slices.Insert(full, len(r.baseOptions()), "-C", r.dir)
	}
	cmd := exec.CommandContext(ctx, gitPath, full...)
	cmd.Dir = tmp
	cmd.Env = env
	stdout := &capped{limit: outLimit, onOver: cancel}
	stderr := &capped{limit: maxStderr, onOver: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	confine(cmd)

	runErr := cmd.Run()
	switch {
	case stdout.over || stderr.over:
		return "", ErrOutputLimit
	case ctx.Err() != nil:
		return "", ctx.Err()
	case runErr != nil:
		return "", errors.New(failureText(runErr, stderr.String()))
	}
	return stdout.String(), nil
}

// baseOptions switches off everything repository-defined code could hang on and the transports other than
// SSH.
func (r *Repo) baseOptions() []string {
	fileAllow := "never"
	if r.cfg.test.allowLocal {
		fileAllow = "always"
	}
	return []string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "core.untrackedCache=false",
		"-c", "core.attributesFile=/dev/null",
		"-c", "credential.helper=",
		"-c", "protocol.file.allow=" + fileAllow,
		"-c", "protocol.ext.allow=never",
		"-c", "submodule.recurse=false",
		"-c", "fetch.recurseSubmodules=false",
		"-c", "transfer.fsckObjects=true",
		"-c", "gc.auto=0",
		"-c", "maintenance.auto=false",
	}
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
