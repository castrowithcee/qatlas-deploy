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
)

// RemoteState is how a remote branch relates to the tree of the host.
type RemoteState string

const (
	// RemoteEmpty means the remote has no commit on the branch.
	RemoteEmpty RemoteState = "empty"
	// RemoteEqual means the tree of the remote branch is the tree of the host.
	RemoteEqual RemoteState = "equal"
	// RemoteDifferent means the trees differ; Comparison lists the paths.
	RemoteDifferent RemoteState = "different"
)

// MaxListedPaths bounds Comparison.Paths.
const MaxListedPaths = 50

// defaultBranch is the branch of a folder without a repository when the remote names none.
const defaultBranch = "main"

// LocalHistoryRef keeps the local history that a remote with a history of its own replaces on the branch.
const LocalHistoryRef = "refs/qdeploy/local-before-remote"

const adoptMessage = "Add the stack files of this host"

var (
	// ErrHasRemote means the repository already has a remote.
	ErrHasRemote = errors.New("the repository already has a remote")
	// ErrHistorySaved means LocalHistoryRef exists already; it is never overwritten.
	ErrHistorySaved = errors.New("the local history is already saved")
	// ErrHistoryNotSaved means the history of the branch is neither saved nor part of the new commit.
	ErrHistoryNotSaved = errors.New("the local history is not saved")
	// ErrTreeMismatch means the commit does not hold the files the branch holds now.
	ErrTreeMismatch = errors.New("the commit does not hold the files of the branch")
	// ErrRemoteMoved means the remote branch is not at the commit the caller compared.
	ErrRemoteMoved = errors.New("the remote branch moved since it was compared")

	branchRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// SecretsError is returned by PushBranch when the commits it would publish hold secret-like content. It carries
// the places, never the values.
type SecretsError struct{ Scan Scan }

func (e *SecretsError) Error() string {
	return fmt.Sprintf("the commits to publish hold %d secret-like file(s); nothing was pushed", e.Scan.Total)
}

// Comparison is the result of comparing a remote branch with the tree of the host. Nothing of the folder or its
// .git was written to get it.
type Comparison struct {
	State RemoteState
	// Branch is the branch that was compared; Commit is its remote commit, empty for RemoteEmpty.
	Branch, Commit string
	// Paths are the sorted paths that differ between the remote branch and the host, at most MaxListedPaths of
	// the PathCount that do. They are data of the remote and the folder.
	Paths     []string
	PathCount int
}

// CompareRemote compares the branch of the remote url that has the name of the checked-out branch with the tree
// of HEAD. Another default branch of the remote does not matter: the branch named like the local one either
// exists or the remote counts as empty, and a later push would create it beside the others.
//
// The remote is read into a temporary repository outside the working copy, which lends it only its objects
// through an alternates file. The working copy, its index and its .git are not written.
func (r *Repo) CompareRemote(ctx context.Context, url string) (Comparison, error) {
	branch, err := r.branch(ctx)
	if err != nil {
		return Comparison{}, err
	}
	return r.compare(ctx, url, branch, nil, true)
}

// CompareFolder compares the remote url with the tree that exactly files, paths relative to dir, would make in
// a repository started in the folder dir, which has no .git. The branch is the default branch of the remote
// (the one its HEAD names), or "main" if the remote has none. The folder is only read: the tree is built in a
// temporary repository outside it.
func CompareFolder(ctx context.Context, cfg Config, dir, url string, files []string) (Comparison, error) {
	r, err := Open(cfg, dir)
	if err != nil {
		return Comparison{}, err
	}
	if len(files) == 0 {
		return Comparison{}, errors.New("there is no file to compare")
	}
	if err := r.checkFiles(files); err != nil {
		return Comparison{}, err
	}
	if _, err := r.claimGitDir(); err != nil {
		return Comparison{}, err
	}
	return r.compare(ctx, url, "", files, false)
}

// compare reads the remote branch (branch, or the default branch of the remote when empty) and compares it
// with the host tree: HEAD when fromHead, otherwise the tree of files in the folder.
func (r *Repo) compare(ctx context.Context, url, branch string, files []string, fromHead bool) (Comparison, error) {
	if err := r.needRemoteMode(); err != nil {
		return Comparison{}, err
	}
	if err := r.validateURL(url); err != nil {
		return Comparison{}, err
	}
	if branch == "" {
		def, err := r.remoteDefaultBranch(ctx, url)
		if err != nil {
			return Comparison{}, err
		}
		if branch = def; branch == "" {
			branch = defaultBranch
		}
	}
	if err := r.checkBranchName(ctx, branch); err != nil {
		return Comparison{}, err
	}
	commit, err := r.remoteBranchCommit(ctx, false, url, branch)
	if err != nil {
		return Comparison{}, err
	}
	if commit == "" {
		return Comparison{State: RemoteEmpty, Branch: branch}, nil
	}

	tmp, err := os.MkdirTemp("", "qdeploy-compare-*")
	if err != nil {
		return Comparison{}, fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(tmp)
	gitDir := filepath.Join(tmp, "git")
	if _, err := r.run(ctx, false, maxErrorText, "init", "--quiet", "--bare", "--template=", "--", gitDir); err != nil {
		return Comparison{}, fmt.Errorf("prepare the comparison: %w", err)
	}
	env := []string{"GIT_DIR=" + gitDir}
	var hostTree string
	if fromHead {
		if hostTree, err = r.lendObjects(ctx, gitDir); err != nil {
			return Comparison{}, err
		}
	}
	const local = "refs/heads/remote"
	if _, err := r.runEnv(ctx, false, maxStdout, env, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules",
		"--no-write-fetch-head", "--", url, "+refs/heads/"+branch+":"+local); err != nil {
		return Comparison{}, fmt.Errorf("fetch failed: %w", err)
	}
	if !fromHead {
		attrs := filepath.Join(gitDir, "info", "attributes")
		if err := os.MkdirAll(filepath.Dir(attrs), 0o700); err != nil {
			return Comparison{}, fmt.Errorf("prepare the comparison: %w", err)
		}
		if err := os.WriteFile(attrs, []byte(noFilterAttributes), 0o600); err != nil {
			return Comparison{}, fmt.Errorf("prepare the comparison: %w", err)
		}
		stageEnv := append(slices.Clone(env), "GIT_WORK_TREE="+r.dir, "GIT_INDEX_FILE="+filepath.Join(tmp, "index"))
		if hostTree, err = r.stageFiles(ctx, stageEnv, files); err != nil {
			return Comparison{}, err
		}
	}
	revParse := func(rev string) (string, error) {
		out, err := r.runEnv(ctx, false, maxErrorText, env, "rev-parse", "--verify", "--quiet", rev)
		id := strings.TrimSpace(out)
		if err != nil || !hexRe.MatchString(id) {
			return "", errors.New("the fetched branch cannot be read")
		}
		return id, nil
	}
	fetched, err := revParse(local + "^{commit}")
	if err != nil {
		return Comparison{}, err
	}
	remoteTree, err := revParse(local + "^{tree}")
	if err != nil {
		return Comparison{}, err
	}
	res := Comparison{State: RemoteEqual, Branch: branch, Commit: fetched}
	if remoteTree == hostTree {
		return res, nil
	}
	out, err := r.runEnv(ctx, false, maxStdout, env, "diff-tree", "-r", "--name-only", "-z", "--no-renames", remoteTree, hostTree, "--")
	if err != nil {
		return Comparison{}, fmt.Errorf("diff failed: %w", err)
	}
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			res.Paths = append(res.Paths, p)
		}
	}
	slices.Sort(res.Paths)
	res.State, res.PathCount = RemoteDifferent, len(res.Paths)
	res.Paths = res.Paths[:min(len(res.Paths), MaxListedPaths)]
	return res, nil
}

// lendObjects lets the temporary repository read the objects of the working copy and returns the tree of HEAD.
func (r *Repo) lendObjects(ctx context.Context, gitDir string) (string, error) {
	tree, err := r.revParse(ctx, "HEAD^{tree}")
	if err != nil {
		return "", err
	}
	out, err := r.run(ctx, true, maxErrorText, "rev-parse", "--path-format=absolute", "--git-path", "objects")
	objects := strings.TrimSpace(out)
	if err != nil || !filepath.IsAbs(objects) || strings.ContainsAny(objects, "\n\x00") {
		return "", errors.New("cannot locate the objects of the working copy")
	}
	info := filepath.Join(gitDir, "objects", "info")
	if err := os.MkdirAll(info, 0o700); err != nil {
		return "", fmt.Errorf("prepare the comparison: %w", err)
	}
	if err := os.WriteFile(filepath.Join(info, "alternates"), []byte(objects+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("prepare the comparison: %w", err)
	}
	return tree, nil
}

func (r *Repo) needRemoteMode() error {
	if r.cfg.Local {
		return errors.New("the remote mode is required")
	}
	return nil
}

func (r *Repo) validateURL(url string) error {
	if r.cfg.test.allowLocal {
		return validateLocalURL(url)
	}
	return ValidateURL(url)
}

// checkBranchName accepts a branch name that is plain text for git, a ref and a command line.
func (r *Repo) checkBranchName(ctx context.Context, name string) error {
	if !branchRe.MatchString(name) || len(name) > 200 || strings.Contains(name, "..") || strings.Contains(name, "//") {
		return errors.New("the branch name is not usable")
	}
	if _, err := r.run(ctx, false, maxErrorText, "check-ref-format", "--branch", name); err != nil {
		return errors.New("the branch name is not usable")
	}
	return nil
}

// remoteDefaultBranch is the branch the HEAD of the remote names, empty if it names none (an empty remote).
func (r *Repo) remoteDefaultBranch(ctx context.Context, target string) (string, error) {
	out, err := r.run(ctx, false, maxStdout, "ls-remote", "--symref", "--", target, "HEAD")
	if err != nil {
		return "", fmt.Errorf("cannot read the remote: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if ref, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
			name, _, _ := strings.Cut(ref, "\t")
			if err := r.checkBranchName(ctx, name); err != nil {
				return "", errors.New("the default branch of the remote has an unusable name")
			}
			return name, nil
		}
	}
	return "", nil
}

// remoteBranchCommit is the commit the branch of target has on the remote, empty if there is none. target is a
// URL or, with inRepo, a remote name.
func (r *Repo) remoteBranchCommit(ctx context.Context, inRepo bool, target, branch string) (string, error) {
	ref := "refs/heads/" + branch
	out, err := r.run(ctx, inRepo, maxStdout, "ls-remote", "--", target, ref)
	if err != nil {
		return "", fmt.Errorf("cannot read the remote: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if id, name, ok := strings.Cut(line, "\t"); ok && name == ref && hexRe.MatchString(id) {
			return id, nil
		}
	}
	return "", nil
}

// AddRemote gives a repository without any remote the remote "origin" at url.
func (r *Repo) AddRemote(ctx context.Context, url string) error {
	if err := r.needRemoteMode(); err != nil {
		return err
	}
	if err := r.validateURL(url); err != nil {
		return err
	}
	names, err := r.Remotes(ctx)
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return ErrHasRemote
	}
	if _, err := r.run(ctx, true, maxErrorText, "remote", "add", "--", "origin", url); err != nil {
		return fmt.Errorf("remote add failed: %w", err)
	}
	return nil
}

// FetchBranch updates refs/remotes/origin/<branch> from the remote; it fails if the remote has no such branch.
// Nothing else is changed.
func (r *Repo) FetchBranch(ctx context.Context, branch string) error {
	if err := r.needRemoteMode(); err != nil {
		return err
	}
	if err := r.checkBranchName(ctx, branch); err != nil {
		return err
	}
	spec := "+refs/heads/" + branch + ":refs/remotes/origin/" + branch
	if _, err := r.run(ctx, true, maxStdout, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules",
		"--no-write-fetch-head", "--", "origin", spec); err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}
	return nil
}

// CommitHostTree creates, without touching the working copy or its index, a commit that has the tree of HEAD
// and the commit parent, a full commit id present in the repository, as its only parent. It returns the new
// commit id and moves no ref. ErrUnchanged means the tree of parent is already that tree.
func (r *Repo) CommitHostTree(ctx context.Context, parent string) (string, error) {
	if !hexRe.MatchString(parent) {
		return "", errors.New("commit must be a full lowercase commit id")
	}
	parentTree, err := r.revParse(ctx, parent+"^{tree}")
	if err != nil {
		return "", err
	}
	tree, err := r.revParse(ctx, "HEAD^{tree}")
	if err != nil {
		return "", err
	}
	if tree == parentTree {
		return "", ErrUnchanged
	}
	commit, err := r.gitIn(ctx, identityEnv())("commit-tree", tree, "-p", parent, "-m", adoptMessage)
	if err != nil {
		return "", err
	}
	if !hexRe.MatchString(commit) {
		return "", errors.New("commit-tree returned no commit id")
	}
	return commit, nil
}

func identityEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=" + commitName, "GIT_AUTHOR_EMAIL=" + commitEmail,
		"GIT_COMMITTER_NAME=" + commitName, "GIT_COMMITTER_EMAIL=" + commitEmail,
	}
}

// SaveLocalHistory points LocalHistoryRef at commit, a full commit id, so the history of the branch survives
// when the branch moves. An existing LocalHistoryRef is never overwritten: ErrHistorySaved.
func (r *Repo) SaveLocalHistory(ctx context.Context, commit string) error {
	if !hexRe.MatchString(commit) {
		return errors.New("commit must be a full lowercase commit id")
	}
	if _, err := r.revParse(ctx, commit+"^{commit}"); err != nil {
		return err
	}
	if _, err := r.revParse(ctx, LocalHistoryRef); err == nil {
		return ErrHistorySaved
	}
	// The empty old value makes git refuse if the ref appeared in the meantime.
	if _, err := r.run(ctx, true, maxErrorText, "update-ref", LocalHistoryRef, commit, ""); err != nil {
		return fmt.Errorf("save the local history failed: %w", err)
	}
	return nil
}

// ResetBranch moves the checked-out branch to commit and sets origin/<branch> as its upstream. The commit must
// have exactly the tree of HEAD, so the index and the working copy stay as they are, and the history the
// branch has now must be safe: saved under LocalHistoryRef, or part of the new commit. The branch is only
// moved from the commit it is at now.
func (r *Repo) ResetBranch(ctx context.Context, commit string) error {
	if !hexRe.MatchString(commit) {
		return errors.New("commit must be a full lowercase commit id")
	}
	branch, err := r.branch(ctx)
	if err != nil {
		return err
	}
	head, err := r.Head(ctx)
	if err != nil {
		return err
	}
	tree, err := r.revParse(ctx, commit+"^{tree}")
	if err != nil {
		return err
	}
	if headTree, err := r.revParse(ctx, "HEAD^{tree}"); err != nil || headTree != tree {
		if err != nil {
			return err
		}
		return ErrTreeMismatch
	}
	if commit != head {
		safe := false
		if saved, err := r.revParse(ctx, LocalHistoryRef+"^{commit}"); err == nil {
			safe, _ = r.IsAncestor(ctx, head, saved)
		}
		if !safe {
			safe, _ = r.IsAncestor(ctx, head, commit)
		}
		if !safe {
			return ErrHistoryNotSaved
		}
		if _, err := r.run(ctx, true, maxErrorText, "update-ref", "-m", "qdeploy: follow the remote",
			"refs/heads/"+branch, commit, head); err != nil {
			return fmt.Errorf("move the branch failed: %w", err)
		}
	}
	return r.SetUpstream(ctx)
}

// SetUpstream makes origin/<branch> the upstream of the checked-out branch. The remote branch need not exist
// yet.
func (r *Repo) SetUpstream(ctx context.Context) error {
	branch, err := r.branch(ctx)
	if err != nil {
		return err
	}
	names, err := r.Remotes(ctx)
	if err != nil {
		return err
	}
	if !slices.Contains(names, "origin") {
		return errors.New("the working copy has no origin remote")
	}
	for _, kv := range [][2]string{{"remote", "origin"}, {"merge", "refs/heads/" + branch}} {
		if _, err := r.run(ctx, true, maxErrorText, "config", "--local", "branch."+branch+"."+kv[0], kv[1]); err != nil {
			return fmt.Errorf("config failed: %w", err)
		}
	}
	return nil
}

// PushBranch publishes HEAD to the branch of origin that has the name of the checked-out branch, as a plain
// fast-forward update (see Push). Before anything is sent it reads the remote branch again and scans every
// commit that is not on it with ScanPush; a finding gives a *SecretsError and nothing is pushed. There is no way
// around the scan.
func (r *Repo) PushBranch(ctx context.Context) error {
	if err := r.needRemoteMode(); err != nil {
		return err
	}
	branch, err := r.branch(ctx)
	if err != nil {
		return err
	}
	head, err := r.Head(ctx)
	if err != nil {
		return err
	}
	base, err := r.remoteBranchCommit(ctx, true, "origin", branch)
	if err != nil {
		return err
	}
	if base != "" {
		if err := r.FetchBranch(ctx, branch); err != nil {
			return err
		}
		if base, err = r.revParse(ctx, "refs/remotes/origin/"+branch+"^{commit}"); err != nil {
			return err
		}
	}
	scan, err := r.ScanPush(ctx, head, base)
	if err != nil {
		return err
	}
	if scan.Total > 0 {
		return &SecretsError{Scan: scan}
	}
	return r.Push(ctx, head)
}

// InitFromRemote turns the folder dir, which has no .git, into a repository on branch with the remote url as
// origin and the history of the remote: it stages exactly files, as Init does, and puts the branch on
// remoteCommit when that commit has the same tree, on a new commit with remoteCommit as its parent otherwise.
// remoteCommit is the commit CompareFolder reported for branch, or empty for an empty remote, where the branch
// starts with a first commit like Init's. The remote branch is fetched and must still be at remoteCommit
// (ErrRemoteMoved). Nothing is pushed, and no file of the folder is created, changed or removed other than .git;
// if a step fails after .git was created, that .git is removed again. It returns the commit the branch is at.
func InitFromRemote(ctx context.Context, cfg Config, dir, url, branch, remoteCommit string, files, excluded []string) (string, error) {
	r, err := Open(cfg, dir)
	if err != nil {
		return "", err
	}
	if err := r.needRemoteMode(); err != nil {
		return "", err
	}
	if err := r.validateURL(url); err != nil {
		return "", err
	}
	if err := r.checkBranchName(ctx, branch); err != nil {
		return "", err
	}
	if remoteCommit != "" && !hexRe.MatchString(remoteCommit) {
		return "", errors.New("commit must be a full lowercase commit id")
	}
	patterns, err := r.checkFolder(files, excluded)
	if err != nil {
		return "", err
	}
	gitDir, err := r.claimGitDir()
	if err != nil {
		return "", err
	}
	commit, err := r.initFromRemoteIn(ctx, gitDir, url, branch, remoteCommit, files, patterns)
	if err != nil {
		_ = os.RemoveAll(gitDir)
		return "", err
	}
	return commit, nil
}

func (r *Repo) initFromRemoteIn(ctx context.Context, gitDir, url, branch, remoteCommit string, files []string, patterns string) (string, error) {
	git, tree, err := r.stage(ctx, gitDir, files, patterns, branch)
	if err != nil {
		return "", err
	}
	if _, err := git("remote", "add", "--", "origin", url); err != nil {
		return "", err
	}
	args := []string{"commit-tree", tree, "-m", firstCommitMessage}
	commit := ""
	if remoteCommit != "" {
		if err := r.FetchBranch(ctx, branch); err != nil {
			return "", err
		}
		tip, err := r.revParse(ctx, "refs/remotes/origin/"+branch+"^{commit}")
		if err != nil {
			return "", err
		}
		if tip != remoteCommit {
			return "", ErrRemoteMoved
		}
		remoteTree, err := r.revParse(ctx, tip+"^{tree}")
		if err != nil {
			return "", err
		}
		if remoteTree == tree {
			commit = tip
		}
		args = []string{"commit-tree", tree, "-p", tip, "-m", adoptMessage}
	}
	if commit == "" {
		if commit, err = git(args...); err != nil {
			return "", err
		}
	}
	if !hexRe.MatchString(commit) {
		return "", errors.New("commit-tree returned no commit id")
	}
	if _, err := git("update-ref", "refs/heads/"+branch, commit); err != nil {
		return "", err
	}
	if err := r.SetUpstream(ctx); err != nil {
		return "", err
	}
	return commit, nil
}
