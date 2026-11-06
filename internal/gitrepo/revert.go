package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	// ErrRevertConflict means the stack folder changed on the branch after the failed commit, so reverting it
	// would drop foreign work. Nothing was created.
	ErrRevertConflict = errors.New("the stack changed after the failed commit")
	// ErrNothingToRevert means the stack folder at the base ref already equals the deployed commit.
	ErrNothingToRevert = errors.New("the stack already equals the deployed state")
	// ErrPushRejected means the push is not a fast-forward because the branch moved: the remote refused it, or
	// in local mode the checked-out branch is no longer an ancestor of the commit. Fetch, rebuild and retry.
	ErrPushRejected = errors.New("push rejected: remote branch moved")

	stackNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

const (
	maxReasonRunes = 200
	maxListedIDs   = 20

	commitName  = "qdeploy"
	commitEmail = "qdeploy@localhost.invalid"
)

// RevertStack creates, without touching the working copy or its index, a commit on top of the base ref (see
// RemoteHead) that restores the folder stack to its state in the
// commit deployed. The stack must be a single top-level folder name or RootStack, which restores the whole
// tree but the settings folder; deployed and failed are full lowercase commit ids and reason is a short note
// (sanitized and bounded here). It returns the new commit id and does not move any ref.
//
// ErrRevertConflict is returned if the stack differs between the base ref and failed, and
// ErrNothingToRevert if it already equals its state in deployed; in both cases nothing is created.
func (r *Repo) RevertStack(ctx context.Context, stack, deployed, failed, reason string) (string, error) {
	if stack != RootStack && (checkRepoPath(stack) != nil || !stackNameRe.MatchString(stack) || stack == ".git") {
		return "", errors.New("the stack must be a single folder name")
	}
	if !hexRe.MatchString(deployed) || !hexRe.MatchString(failed) {
		return "", errors.New("commits must be full lowercase commit ids")
	}
	base, err := r.RemoteHead(ctx)
	if err != nil {
		return "", err
	}
	for _, c := range []string{deployed, failed} {
		if _, err := r.revParse(ctx, c+"^{commit}"); err != nil {
			return "", err
		}
	}
	atBase, err := r.stackEntry(ctx, base, stack)
	if err != nil {
		return "", err
	}
	atFailed, err := r.stackEntry(ctx, failed, stack)
	if err != nil {
		return "", err
	}
	if atBase != atFailed {
		return "", ErrRevertConflict
	}
	atDeployed, err := r.stackEntry(ctx, deployed, stack)
	if err != nil {
		return "", err
	}
	if atBase == atDeployed {
		return "", ErrNothingToRevert
	}

	// The pathspecs of the root stack are fixed text, so only they use pathspec magic.
	specs := []string{"--literal-pathspecs", "rev-list", "--reverse", "--max-count=" + fmt.Sprint(maxListedIDs+1), deployed + ".." + failed, "--", stack}
	if stack == RootStack {
		specs = append(specs[1:], ":(exclude)"+rootSettings)
	}
	ids, err := r.run(ctx, true, maxStdout, specs...)
	if err != nil {
		return "", fmt.Errorf("rev-list failed: %w", err)
	}
	message := revertMessage(stack, deployed, strings.Fields(ids), reason)

	dir, err := os.MkdirTemp("", "qdeploy-index-*")
	if err != nil {
		return "", fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(dir)
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
	// The index starts as keep, and path in it is replaced by entry. The root stack is the tree of deployed with
	// the settings folder of the base ref.
	keep, path, entry := base, stack, atDeployed
	if stack == RootStack {
		keep, path = deployed, rootSettings
		if entry, err = r.stackEntry(ctx, base, rootSettings); err != nil {
			return "", err
		}
	}
	if _, err := git("read-tree", keep); err != nil {
		return "", err
	}
	// Not "git rm --cached": that refuses entries that differ from HEAD.
	listed, err := git("ls-files", "-z", "--cached", "--", path)
	if err != nil {
		return "", err
	}
	paths := strings.Split(strings.Trim(listed, "\x00"), "\x00")
	for len(paths) > 0 && paths[0] != "" {
		n := min(len(paths), 200)
		args := append([]string{"update-index", "--force-remove", "--"}, paths[:n]...)
		if _, err := git(args...); err != nil {
			return "", err
		}
		paths = paths[n:]
	}
	if entry != "" {
		mode, rest, _ := strings.Cut(entry, " ")
		kind, id, _ := strings.Cut(rest, " ")
		if kind == "tree" {
			_, err = git("read-tree", "--prefix="+path+"/", id)
		} else {
			_, err = git("update-index", "--add", "--cacheinfo", mode+","+id+","+path)
		}
		if err != nil {
			return "", err
		}
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

// stackEntry returns the tree entry ("mode type id") of the top-level path stack in commit, empty if absent.
// For RootStack it returns every top-level entry but the settings folder, with names, as one comparable text.
func (r *Repo) stackEntry(ctx context.Context, commit, stack string) (string, error) {
	if stack == RootStack {
		out, err := r.run(ctx, true, maxStdout, "ls-tree", "-z", commit)
		if err != nil {
			return "", fmt.Errorf("ls-tree failed: %w", err)
		}
		entries := strings.Split(out, "\x00")
		entries = slices.DeleteFunc(entries, func(e string) bool {
			_, name, _ := strings.Cut(e, "\t")
			return e == "" || name == rootSettings
		})
		return strings.Join(entries, "\x00"), nil
	}
	out, err := r.run(ctx, true, maxErrorText, "--literal-pathspecs", "ls-tree", "-z", commit, "--", stack)
	if err != nil {
		return "", fmt.Errorf("ls-tree failed: %w", err)
	}
	entry, _, _ := strings.Cut(out, "\t")
	return entry, nil
}

func revertMessage(stack, deployed string, ids []string, reason string) string {
	var b strings.Builder
	if stack == RootStack {
		stack = "the stack folder"
	}
	b.WriteString("Revert " + stack + " to " + deployed[:12] + "\n")
	if len(ids) > 0 {
		b.WriteString("\nReverted commits:\n")
		for i, id := range ids {
			if i == maxListedIDs {
				b.WriteString("- ...\n")
				break
			}
			b.WriteString("- " + id[:min(12, len(id))] + "\n")
		}
	}
	if reason = sanitizeLine(reason, maxReasonRunes); reason != "" {
		b.WriteString("\nReason: " + reason + "\n")
	}
	return b.String()
}

// sanitizeLine makes s a single printable line of at most limit runes.
func sanitizeLine(s string, limit int) string {
	s = strings.ToValidUTF8(s, " ")
	s = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return ' '
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > limit {
		s = string([]rune(s)[:limit])
	}
	return s
}

// Push publishes the commit to the checked-out branch of origin as a plain fast-forward update, never forced.
// If the remote branch is not an ancestor of the commit it returns ErrPushRejected and changes nothing. On
// success the remote-tracking ref points to the commit, so FastForward can follow.
//
// In local mode Push lands the commit instead: it fast-forwards the checked-out branch and the working copy to
// it, which leaves a following FastForward nothing to do. A branch that moved so that it is no longer an
// ancestor of the commit gives ErrPushRejected and stays untouched.
func (r *Repo) Push(ctx context.Context, commit string) error {
	if !hexRe.MatchString(commit) {
		return errors.New("commit must be a full lowercase commit id")
	}
	branch, err := r.branch(ctx)
	if err != nil {
		return err
	}
	if _, err := r.revParse(ctx, commit+"^{commit}"); err != nil {
		return err
	}
	if r.cfg.Local {
		return r.land(ctx, commit)
	}
	_, err = r.run(ctx, true, maxStdout, "push", "--porcelain", "--no-verify", "--no-recurse-submodules",
		"--", "origin", commit+":refs/heads/"+branch)
	var failure *runFailure
	if errors.As(err, &failure) {
		if isNonFastForward(failure.stdout) {
			return ErrPushRejected
		}
	}
	if err != nil {
		return fmt.Errorf("push failed: %w", err)
	}
	if _, err := r.run(ctx, true, maxErrorText, "update-ref", "refs/remotes/origin/"+branch, commit); err != nil {
		return fmt.Errorf("update tracking ref failed: %w", err)
	}
	return nil
}

// land fast-forwards the checked-out branch to commit. A failed fast-forward is a rejection when the branch is
// not an ancestor of the commit, checked again after the attempt because a person may commit meanwhile.
func (r *Repo) land(ctx context.Context, commit string) error {
	behind := func() (bool, error) {
		head, err := r.Head(ctx)
		if err != nil {
			return false, err
		}
		return r.IsAncestor(ctx, head, commit)
	}
	if ok, err := behind(); err != nil {
		return err
	} else if !ok {
		return ErrPushRejected
	}
	err := r.FastForward(ctx, commit)
	if err != nil {
		if ok, aerr := behind(); aerr == nil && !ok {
			return ErrPushRejected
		}
	}
	return err
}

// isNonFastForward reads git's --porcelain ref lines ("!\t<ref>\t[rejected] (<reason>)"). A "[remote
// rejected]" refusal (hook, protection) is deliberately not a match.
func isNonFastForward(porcelain string) bool {
	for _, line := range strings.Split(porcelain, "\n") {
		f := strings.Split(line, "\t")
		if len(f) >= 3 && f[0] == "!" && (strings.HasPrefix(f[2], "[rejected] (non-fast-forward)") ||
			strings.HasPrefix(f[2], "[rejected] (fetch first)")) {
			return true
		}
	}
	return false
}
