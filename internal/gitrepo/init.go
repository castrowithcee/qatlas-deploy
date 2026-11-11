package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrHasGit means the folder already has a .git entry.
var ErrHasGit = errors.New("the folder already has a .git entry")

const firstCommitMessage = "Import the stack files"

// Init turns the folder dir, which has no .git, into a repository without a remote: branch main,
// core.sharedRepository=group, and a first commit by qdeploy that holds exactly the regular files in files, paths
// relative to dir. excluded are paths relative to dir that the repository leaves out; they are written as
// anchored patterns to .git/info/exclude only, so no file of the folder is created, changed or removed. Hooks,
// filters and the configuration of the caller do not apply, and the files are stored as they are on disk.
// It returns the id of the first commit. If a step fails after .git was created, Init removes that .git again.
func Init(ctx context.Context, cfg Config, dir string, files, excluded []string) (string, error) {
	r, err := Open(cfg, dir)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", errors.New("there is no file to commit")
	}
	for _, f := range files {
		if err := checkRepoPath(f); err != nil || strings.Contains("/"+f+"/", "/.git/") {
			return "", errors.New("every file must be a clean path inside the folder and outside .git")
		}
		fi, err := os.Lstat(filepath.Join(r.dir, f))
		if err != nil || !fi.Mode().IsRegular() {
			return "", fmt.Errorf("%q is not a regular file", f)
		}
	}
	var patterns strings.Builder
	for _, e := range excluded {
		pat, ok := excludePattern(e)
		if !ok {
			return "", fmt.Errorf("%q cannot be written as an ignore pattern", e)
		}
		patterns.WriteString(pat + "\n")
	}
	gitDir := filepath.Join(r.dir, ".git")
	if _, err := os.Lstat(gitDir); err == nil {
		return "", ErrHasGit
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("look for .git: %w", err)
	}

	// From here on gitDir is ours: it did not exist a moment ago.
	commit, err := r.initIn(ctx, gitDir, files, patterns.String())
	if err != nil {
		_ = os.RemoveAll(gitDir)
		return "", err
	}
	return commit, nil
}

func (r *Repo) initIn(ctx context.Context, gitDir string, files []string, patterns string) (string, error) {
	// --template= keeps the host's template (hooks, sample files) out of the new repository.
	if _, err := r.run(ctx, false, maxErrorText, "init", "--quiet", "--template=", "--initial-branch=main",
		"--shared=group", "--", r.dir); err != nil {
		return "", fmt.Errorf("init failed: %w", err)
	}
	if _, err := r.run(ctx, true, maxErrorText, "config", "--local", "core.sharedRepository", "group"); err != nil {
		return "", fmt.Errorf("config failed: %w", err)
	}
	info := filepath.Join(gitDir, "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		return "", fmt.Errorf("write the ignore rules: %w", err)
	}
	if err := os.WriteFile(filepath.Join(info, "exclude"), []byte(patterns), 0o644); err != nil {
		return "", fmt.Errorf("write the ignore rules: %w", err)
	}
	// An attribute rule of the folder could convert line endings or run a filter; this one wins and goes away
	// with the first commit, so the blobs hold the bytes of the files.
	attrs := filepath.Join(info, "attributes")
	if err := os.WriteFile(attrs, []byte("* -text -filter -ident -working-tree-encoding\n"), 0o644); err != nil {
		return "", fmt.Errorf("write the attribute rules: %w", err)
	}
	list, err := os.CreateTemp("", "qdeploy-init-*")
	if err != nil {
		return "", fmt.Errorf("create temporary file: %w", err)
	}
	defer os.Remove(list.Name())
	_, err = list.WriteString(strings.Join(files, "\x00") + "\x00")
	if cerr := list.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("write the file list: %w", err)
	}
	env := []string{
		"GIT_AUTHOR_NAME=" + commitName, "GIT_AUTHOR_EMAIL=" + commitEmail,
		"GIT_COMMITTER_NAME=" + commitName, "GIT_COMMITTER_EMAIL=" + commitEmail,
	}
	git := func(args ...string) (string, error) {
		out, err := r.runEnv(ctx, true, maxStdout, env, append([]string{"--literal-pathspecs"}, args...)...)
		if err != nil {
			return "", fmt.Errorf("%s failed: %w", args[0], err)
		}
		return strings.TrimSpace(out), nil
	}
	if _, err := git("add", "--pathspec-from-file="+list.Name(), "--pathspec-file-nul"); err != nil {
		return "", err
	}
	if err := os.Remove(attrs); err != nil {
		return "", fmt.Errorf("remove the attribute rules: %w", err)
	}
	tree, err := git("write-tree")
	if err != nil {
		return "", err
	}
	commit, err := git("commit-tree", tree, "-m", firstCommitMessage)
	if err != nil {
		return "", err
	}
	if !hexRe.MatchString(commit) {
		return "", errors.New("commit-tree returned no commit id")
	}
	if _, err := git("update-ref", "refs/heads/main", commit); err != nil {
		return "", err
	}
	return commit, nil
}
