package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnchanged means the edit of CommitFile returned the content the file already has; nothing was created.
var ErrUnchanged = errors.New("the edit does not change the file")

const maxMessageRunes = 200

// CommitFile creates, without touching the working copy or its index, a commit on top of the base ref (see
// RemoteHead) that differs from it only in the file path: its
// content becomes what edit returns for the content the file has there. The file must exist there as a
// regular file; its mode is kept. message is one line, sanitized and bounded here. It returns the new commit
// id and does not move any ref.
func (r *Repo) CommitFile(ctx context.Context, path string, edit func(old []byte) ([]byte, error), message string) (string, error) {
	if checkRepoPath(path) != nil || strings.Contains("/"+path+"/", "/.git/") {
		return "", errors.New("the path must be a clean path of a file in the working copy")
	}
	if message = sanitizeLine(message, maxMessageRunes); message == "" {
		return "", errors.New("the commit message is empty")
	}
	base, err := r.RemoteHead(ctx)
	if err != nil {
		return "", err
	}
	out, err := r.run(ctx, true, maxErrorText, "--literal-pathspecs", "ls-tree", "-z", base, "--", path)
	if err != nil {
		return "", fmt.Errorf("ls-tree failed: %w", err)
	}
	entry, name, _ := strings.Cut(strings.TrimSuffix(out, "\x00"), "\t")
	mode, rest, _ := strings.Cut(entry, " ")
	kind, blob, _ := strings.Cut(rest, " ")
	if name != path || kind != "blob" || (mode != "100644" && mode != "100755") || !hexRe.MatchString(blob) {
		return "", fmt.Errorf("%s is not a regular file on the branch", path)
	}
	old, err := r.run(ctx, true, maxStdout, "cat-file", "blob", blob)
	if err != nil {
		return "", fmt.Errorf("cat-file failed: %w", err)
	}
	content, err := edit([]byte(old))
	if err != nil {
		return "", err
	}
	if bytes.Equal(content, []byte(old)) {
		return "", ErrUnchanged
	}

	dir, err := os.MkdirTemp("", "qdeploy-commit-*")
	if err != nil {
		return "", fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "content")
	if err := os.WriteFile(file, content, 0o600); err != nil {
		return "", fmt.Errorf("write temporary file: %w", err)
	}
	env := []string{
		"GIT_INDEX_FILE=" + filepath.Join(dir, "index"),
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
	// --no-filters: the bytes are stored as given, whatever the attributes of the repository say.
	id, err := git("hash-object", "-w", "--no-filters", "--", file)
	if err != nil {
		return "", err
	}
	if _, err := git("read-tree", base); err != nil {
		return "", err
	}
	if _, err := git("update-index", "--cacheinfo", mode+","+id+","+path); err != nil {
		return "", err
	}
	tree, err := git("write-tree")
	if err != nil {
		return "", err
	}
	commit, err := git("commit-tree", tree, "-p", base, "-m", message)
	if err != nil {
		return "", err
	}
	if !hexRe.MatchString(commit) {
		return "", errors.New("commit-tree returned no commit id")
	}
	return commit, nil
}

// IsAncestor reports whether the commit a is the commit b or one of its ancestors; both are full commit ids.
func (r *Repo) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	if !hexRe.MatchString(a) || !hexRe.MatchString(b) {
		return false, errors.New("commits must be full lowercase commit ids")
	}
	_, err := r.run(ctx, true, maxErrorText, "merge-base", "--is-ancestor", a, b)
	var failure *runFailure
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &failure) && failure.code == 1:
		return false, nil
	}
	return false, fmt.Errorf("merge-base failed: %w", err)
}
