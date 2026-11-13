package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
)

// maxSecretsListed bounds the places a refusal names.
const maxSecretsListed = 10

var commitIDRe = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

// checkRemoteArgs validates what every command that connects a folder to an address takes.
func checkRemoteArgs(path, url, fingerprint string) error {
	if _, _, err := targetOf(url); err != nil {
		return err
	}
	if err := checkDirShape(path); err != nil {
		return control.NewError(control.CodeInvalid, err.Error())
	}
	return checkFingerprint(fingerprint)
}

// checkExpected validates the state and the commit the operator was shown.
func checkExpected(state, commit string) error {
	switch state {
	case control.SourceRemoteEmpty:
		if commit == "" {
			return nil
		}
	case control.SourceRemoteEqual, control.SourceRemoteDifferent:
		if commitIDRe.MatchString(commit) {
			return nil
		}
	}
	return control.NewError(control.CodeInvalid, "the expected state of the remote and its commit are not valid")
}

// remoteConfig describes the remote source that the folder dir becomes at url. A local source of the folder
// does not stand in the way of its deploy key.
func (s *repoService) remoteConfig(conf sourcesConfig, url, dir string) (sourceConfig, error) {
	conf.Sources = slices.DeleteFunc(slices.Clone(conf.Sources), func(c sourceConfig) bool { return c.Path == dir && c.Kind == kindLocal })
	return s.set.newSourceConfig(conf, url, dir)
}

// confirmRemote requires the deploy key of the source and pins the host key the operator confirmed.
func (s *repoService) confirmRemote(ctx context.Context, rc sourceConfig, url, fingerprint string) error {
	host, port, err := targetOf(url)
	if err != nil {
		return err
	}
	if err := s.requireKey(rc.Path, rc.Key); err != nil {
		return err
	}
	_, err = s.pinConfirmed(ctx, host, port, fingerprint)
	return err
}

// checkOrigin accepts a repository without a remote and one whose only remote is origin at url, which an earlier
// attempt left behind.
func checkOrigin(ctx context.Context, wc WorkingCopy, url string) error {
	names, err := wc.Remotes(ctx)
	if err != nil {
		return gitFailure(err)
	}
	if len(names) == 0 {
		return nil
	}
	if len(names) == 1 && names[0] == "origin" {
		if have, err := wc.RemoteURL(ctx); err == nil && have == url {
			return nil
		}
	}
	return control.NewError(control.CodeInvalid, "the repository already has a remote that is not this address")
}

func compareResult(kase string, c gitrepo.Comparison) control.SourceCompareResult {
	res := control.SourceCompareResult{Case: kase, State: string(c.State), Branch: c.Branch, Commit: c.Commit, PathCount: c.PathCount}
	for _, p := range c.Paths {
		res.Paths = append(res.Paths, maskControl(p))
	}
	return res
}

func secretsOf(scan gitrepo.Scan) *control.SourceSecrets {
	out := &control.SourceSecrets{Total: scan.Total}
	for _, f := range scan.Findings {
		out.Findings = append(out.Findings, control.SourceSecretFinding{Commit: f.Commit, Path: maskControl(f.Path), Kind: string(f.Kind)})
	}
	return out
}

func shortID(id string) string { return id[:min(len(id), 12)] }

// secretsRefusal is the answer to a push that would publish secret-like files. It names commit, path and kind of
// the first findings and never a value; after says what happened to the folder.
func secretsRefusal(scan gitrepo.Scan, after string) *control.Error {
	var parts []string
	for _, f := range scan.Findings[:min(len(scan.Findings), maxSecretsListed)] {
		parts = append(parts, fmt.Sprintf("%s %s (%s)", shortID(f.Commit), strconv.Quote(printable(f.Path)), f.Kind))
	}
	more := ""
	if scan.Total > len(parts) {
		more = fmt.Sprintf(" and %d more", scan.Total-len(parts))
	}
	return control.NewError(control.CodeSecrets, fmt.Sprintf(
		"the commits to publish hold %d secret-like file(s) and are not pushed: %s%s; %s",
		scan.Total, strings.Join(parts, ", "), more, after))
}

// pushFailure answers a push that did not happen: the secret check of the push, or the remote. note says in
// what state the repository is.
func pushFailure(err error, note string) error {
	var secrets *gitrepo.SecretsError
	if errors.As(err, &secrets) {
		return secretsRefusal(secrets.Scan, note)
	}
	failure := gitFailure(err)
	var ce *control.Error
	if errors.As(failure, &ce) {
		return control.NewError(ce.Code, fmt.Sprintf("the push failed: %s; %s", ce.Message, note))
	}
	return failure
}

var errRemoteChanged = control.NewError(control.CodeInvalid,
	"the remote or the folder changed since the comparison was shown; nothing was changed, compare again")

// handleCompare compares a folder with the repository at an address and changes nothing but the pinned host key.
func (s *inspectService) handleCompare(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.SourceCompareArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if err := checkRemoteArgs(a.Path, a.URL, a.Fingerprint); err != nil {
		return nil, err
	}
	s.repo.setMu.Lock()
	defer s.repo.setMu.Unlock()
	var res control.SourceCompareResult
	var err error
	if src := s.repo.set.byPath(a.Path); src != nil {
		res, err = s.compareLocal(ctx, src, a)
	} else {
		res, err = s.compareFiles(ctx, a)
	}
	if err != nil {
		return nil, err
	}
	if err := fitAnswer(res, "the comparison lists too many places to fit one answer"); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *inspectService) compareLocal(ctx context.Context, src *source, a control.SourceCompareArgs) (control.SourceCompareResult, error) {
	r := s.repo
	if src.conf.Kind != kindLocal {
		return control.SourceCompareResult{}, control.NewError(control.CodeInvalid, "the source runs against a remote already")
	}
	conf, _, err := r.set.load()
	if err != nil {
		return control.SourceCompareResult{}, err
	}
	rc, err := r.remoteConfig(conf, a.URL, a.Path)
	if err != nil {
		return control.SourceCompareResult{}, err
	}
	// The working copy is the engine's to move; it stays where it is while it is compared.
	src.mu.Lock()
	defer src.mu.Unlock()
	if err := r.confirmRemote(ctx, rc, a.URL, a.Fingerprint); err != nil {
		return control.SourceCompareResult{}, err
	}
	wc, err := r.set.git.Open(r.set.gitConfig(rc), a.Path)
	if err != nil {
		return control.SourceCompareResult{}, gitFailure(err)
	}
	if err := checkOrigin(ctx, wc, a.URL); err != nil {
		return control.SourceCompareResult{}, err
	}
	cmp, err := wc.CompareRemote(ctx, a.URL)
	if err != nil {
		return control.SourceCompareResult{}, gitFailure(err)
	}
	res := compareResult(control.SourceCaseLocal, cmp)
	switch cmp.State {
	case gitrepo.RemoteEmpty:
		head, err := wc.Head(ctx)
		if err != nil {
			return control.SourceCompareResult{}, gitFailure(err)
		}
		scan, err := wc.ScanPush(ctx, head, "")
		if err != nil {
			return control.SourceCompareResult{}, gitFailure(err)
		}
		res.Secrets = secretsOf(scan)
	case gitrepo.RemoteDifferent:
		res.Secrets = secretsOf(cmp.Scan)
	}
	return res, nil
}

func (s *inspectService) compareFiles(ctx context.Context, a control.SourceCompareArgs) (control.SourceCompareResult, error) {
	r := s.repo
	v, _, err := s.classify(ctx, control.SourceInspectArgs{Path: a.Path, URL: a.URL})
	if v.plan != nil {
		defer v.plan.close()
	}
	if err != nil {
		return control.SourceCompareResult{}, err
	}
	switch v.kase {
	case control.SourceCaseFiles:
	case control.SourceCaseReject:
		return control.SourceCompareResult{}, control.NewError(control.CodeInvalid, fmt.Sprintf("%s. %s", v.reason, v.fix))
	case control.SourceCaseLocal:
		return control.SourceCompareResult{}, control.NewError(control.CodeInvalid,
			"the folder is a repository that is not a source yet; bind it with 'qdeploy source add' first")
	default:
		return control.SourceCompareResult{}, control.NewError(control.CodeInvalid,
			"the folder is neither a local source nor a folder of files without a repository")
	}
	if len(v.plan.planned) == 0 {
		return control.SourceCompareResult{}, control.NewError(control.CodeInvalid, "the folder holds no file the first commit could take")
	}
	conf, _, err := r.set.load()
	if err != nil {
		return control.SourceCompareResult{}, err
	}
	rc, err := r.remoteConfig(conf, a.URL, a.Path)
	if err != nil {
		return control.SourceCompareResult{}, err
	}
	if err := r.confirmRemote(ctx, rc, a.URL, a.Fingerprint); err != nil {
		return control.SourceCompareResult{}, err
	}
	cmp, err := r.set.git.CompareFolder(ctx, r.set.gitConfig(rc), a.Path, a.URL, v.plan.rawPlanned)
	if err != nil {
		return control.SourceCompareResult{}, gitFailure(err)
	}
	res := compareResult(control.SourceCaseFiles, cmp)
	res.PlannedFiles = v.plan.planned
	if cmp.State == gitrepo.RemoteDifferent {
		res.Secrets = secretsOf(cmp.Scan)
	}
	return res, nil
}

// handleRemote gives a local source a remote and turns it into a remote source, as the operator saw it.
func (s *inspectService) handleRemote(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.SourceRemoteArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if err := checkRemoteArgs(a.Path, a.URL, a.Fingerprint); err != nil {
		return nil, err
	}
	if err := checkExpected(a.ExpectState, a.ExpectCommit); err != nil {
		return nil, err
	}
	r := s.repo
	r.setMu.Lock()
	defer r.setMu.Unlock()
	src := r.set.byPath(a.Path)
	switch {
	case src == nil:
		return nil, control.NewError(control.CodeNotFound, "no source is bound to this folder")
	case src.conf.Kind != kindLocal:
		return nil, control.NewError(control.CodeInvalid, "the source runs against a remote already")
	}
	if err := runActive(src); err != nil {
		return nil, err
	}
	conf, _, err := r.set.load()
	if err != nil {
		return nil, err
	}
	rc, err := r.remoteConfig(conf, a.URL, a.Path)
	if err != nil {
		return nil, err
	}
	res, err := s.connectLocal(ctx, src, rc, a)
	if err != nil {
		return nil, err
	}
	// The locks of the old source are released: stopping its engine waits for work that needs them.
	conf, rev, err := r.set.load()
	if err != nil {
		return nil, err
	}
	if _, err := r.set.convert(src, rc, conf, rev); err != nil {
		var ce *control.Error
		if errors.As(err, &ce) {
			return nil, err
		}
		return nil, control.NewError(control.CodeInternal, fmt.Sprintf(
			"%s; the repository follows the remote already (branch %s at %s), the source is still local; run the command again", err, res.Branch, shortID(res.Head)))
	}
	return res, nil
}

func runActive(src *source) error {
	snap, err := src.eng.Snapshot()
	if err != nil {
		return errors.New("the engine state cannot be read")
	}
	if len(snap.Active) > 0 {
		return control.NewError(control.CodeRunActive, "a run of a stack of this source is active; connect the source when it has finished")
	}
	return nil
}

// connectLocal compares again, acts on the state the operator saw and leaves the repository following the remote.
// It holds the locks of the source while it works.
func (s *inspectService) connectLocal(ctx context.Context, src *source, rc sourceConfig, a control.SourceRemoteArgs) (control.SourceRemoteResult, error) {
	var zero control.SourceRemoteResult
	r := s.repo
	src.mu.Lock()
	defer src.mu.Unlock()
	src.refs.Lock()
	defer src.refs.Unlock()
	if err := runActive(src); err != nil {
		return zero, err
	}
	if err := r.confirmRemote(ctx, rc, a.URL, a.Fingerprint); err != nil {
		return zero, err
	}
	wc, err := r.set.git.Open(r.set.gitConfig(rc), a.Path)
	if err != nil {
		return zero, gitFailure(err)
	}
	if err := checkOrigin(ctx, wc, a.URL); err != nil {
		return zero, err
	}
	cmp, err := wc.CompareRemote(ctx, a.URL)
	if err != nil {
		return zero, gitFailure(err)
	}
	if string(cmp.State) != a.ExpectState || cmp.Commit != a.ExpectCommit {
		return zero, errRemoteChanged
	}
	res := control.SourceRemoteResult{ID: rc.ID, Path: rc.Path, URL: a.URL, State: string(cmp.State), Branch: cmp.Branch}
	switch cmp.State {
	case gitrepo.RemoteEmpty:
		err = followEmpty(ctx, wc, a.URL, cmp, &res)
	case gitrepo.RemoteEqual:
		err = followEqual(ctx, wc, a.URL, cmp, &res)
	default:
		err = followDifferent(ctx, wc, a.URL, cmp, &res)
	}
	if err != nil {
		return zero, err
	}
	if res.Head, err = wc.Head(ctx); err != nil {
		return zero, gitFailure(err)
	}
	return res, nil
}

// ensureOrigin gives the repository the remote origin unless it has it already.
func ensureOrigin(ctx context.Context, wc WorkingCopy, url string) error {
	names, err := wc.Remotes(ctx)
	if err != nil {
		return gitFailure(err)
	}
	if len(names) > 0 {
		return nil
	}
	if err := wc.AddRemote(ctx, url); err != nil {
		return gitFailure(err)
	}
	return nil
}

// followEmpty pushes the branch to a remote that has none, unless the history holds secret-like files; nothing
// is changed before that is known.
func followEmpty(ctx context.Context, wc WorkingCopy, url string, cmp gitrepo.Comparison, res *control.SourceRemoteResult) error {
	head, err := wc.Head(ctx)
	if err != nil {
		return gitFailure(err)
	}
	scan, err := wc.ScanPush(ctx, head, "")
	if err != nil {
		return gitFailure(err)
	}
	if scan.Total > 0 {
		return secretsRefusal(scan, "nothing was changed")
	}
	if err := ensureOrigin(ctx, wc, url); err != nil {
		return err
	}
	if err := wc.SetUpstream(ctx); err != nil {
		return gitFailure(err)
	}
	note := fmt.Sprintf("the remote origin was added and branch %s is unchanged; the source stays local", cmp.Branch)
	if err := wc.PushBranch(ctx); err != nil {
		return pushFailure(err, note)
	}
	res.Pushed = true
	return nil
}

// fetchTip fetches the compared branch and requires it to be at the commit that was compared.
func fetchTip(ctx context.Context, wc WorkingCopy, cmp gitrepo.Comparison) error {
	if err := wc.FetchBranch(ctx, cmp.Branch); err != nil {
		return gitFailure(err)
	}
	tip, err := wc.RemoteHead(ctx)
	if err != nil {
		return gitFailure(err)
	}
	if tip != cmp.Commit {
		return errRemoteChanged
	}
	return nil
}

// saveHistory keeps the commit the branch leaves; a history saved by an earlier attempt stays, and the move of the
// branch itself checks that it is safe.
func saveHistory(ctx context.Context, wc WorkingCopy, head string, res *control.SourceRemoteResult) error {
	if err := wc.SaveLocalHistory(ctx, head); err != nil && !errors.Is(err, gitrepo.ErrHistorySaved) {
		return gitFailure(err)
	}
	res.HistoryRef = gitrepo.LocalHistoryRef
	return nil
}

// followEqual moves the branch to the commit of the remote, which has the files of the folder already.
func followEqual(ctx context.Context, wc WorkingCopy, url string, cmp gitrepo.Comparison, res *control.SourceRemoteResult) error {
	if err := ensureOrigin(ctx, wc, url); err != nil {
		return err
	}
	if err := fetchTip(ctx, wc, cmp); err != nil {
		return err
	}
	head, err := wc.Head(ctx)
	if err != nil {
		return gitFailure(err)
	}
	if head != cmp.Commit {
		if err := saveHistory(ctx, wc, head, res); err != nil {
			return err
		}
	}
	if err := wc.ResetBranch(ctx, cmp.Commit); err != nil {
		return gitFailure(err)
	}
	if err := wc.SetUpstream(ctx); err != nil {
		return gitFailure(err)
	}
	return nil
}

// followDifferent puts one commit with the files of the folder on top of the remote commit and pushes it, unless
// that commit holds secret-like files; the branch moves only after that is known.
func followDifferent(ctx context.Context, wc WorkingCopy, url string, cmp gitrepo.Comparison, res *control.SourceRemoteResult) error {
	if err := ensureOrigin(ctx, wc, url); err != nil {
		return err
	}
	if err := fetchTip(ctx, wc, cmp); err != nil {
		return err
	}
	head, err := wc.Head(ctx)
	if err != nil {
		return gitFailure(err)
	}
	// A branch that already follows the remote commit is what an earlier attempt that failed at the push left.
	follows, err := wc.IsAncestor(ctx, cmp.Commit, head)
	if err != nil {
		return gitFailure(err)
	}
	next := head
	if !follows {
		if next, err = wc.CommitHostTree(ctx, cmp.Commit); err != nil {
			if errors.Is(err, gitrepo.ErrUnchanged) {
				return errRemoteChanged
			}
			return gitFailure(err)
		}
	}
	scan, err := wc.ScanPush(ctx, next, cmp.Commit)
	if err != nil {
		return gitFailure(err)
	}
	if scan.Total > 0 {
		return secretsRefusal(scan, "the branch was not moved")
	}
	note := fmt.Sprintf("branch %s follows the remote commit now and holds the files of the folder", cmp.Branch)
	if next != head {
		if err := saveHistory(ctx, wc, head, res); err != nil {
			return err
		}
		if err := wc.ResetBranch(ctx, next); err != nil {
			return gitFailure(err)
		}
		note += "; the local history before is saved under " + gitrepo.LocalHistoryRef
	}
	if err := wc.SetUpstream(ctx); err != nil {
		return gitFailure(err)
	}
	if err := wc.PushBranch(ctx); err != nil {
		return pushFailure(err, note+"; the source stays local")
	}
	res.Pushed = true
	return nil
}

// addFilesRemote starts a repository in a folder of files on the history of the repository at the address and
// binds it as a remote source. It compares again and refuses, creating nothing, unless the answer is the one the
// operator saw. The caller holds setMu, closes plan and has checked the confirmed file list.
func (s *inspectService) addFilesRemote(ctx context.Context, a control.SourceAddArgs, plan *filePlan) (any, error) {
	r := s.repo
	if err := checkRemoteArgs(a.Path, a.URL, a.Fingerprint); err != nil {
		return nil, err
	}
	if err := checkExpected(a.ExpectState, a.ExpectCommit); err != nil {
		return nil, err
	}
	conf, _, err := r.set.load()
	if err != nil {
		return nil, err
	}
	rc, err := r.remoteConfig(conf, a.URL, a.Path)
	if err != nil {
		return nil, err
	}
	if err := r.confirmRemote(ctx, rc, a.URL, a.Fingerprint); err != nil {
		return nil, err
	}
	gc := r.set.gitConfig(rc)
	cmp, err := r.set.git.CompareFolder(ctx, gc, a.Path, a.URL, plan.rawPlanned)
	if err != nil {
		return nil, gitFailure(err)
	}
	if string(cmp.State) != a.ExpectState || cmp.Commit != a.ExpectCommit {
		return nil, errRemoteChanged
	}
	switch cmp.State {
	case gitrepo.RemoteEmpty:
		return nil, control.NewError(control.CodeInvalid,
			"the repository at the address has no commit; take the folder over without an address, then run 'qdeploy source remote'")
	case gitrepo.RemoteDifferent:
		if cmp.Scan.Total > 0 {
			return nil, secretsRefusal(cmp.Scan, "nothing was created")
		}
	}
	excluded := make([]string, len(plan.excluded))
	for i, e := range plan.excluded {
		excluded[i] = e.Path
	}
	commit, err := r.set.git.InitFromRemote(ctx, gc, a.Path, a.URL, cmp.Branch, cmp.Commit, plan.rawPlanned, excluded)
	if errors.Is(err, gitrepo.ErrRemoteMoved) {
		return nil, errRemoteChanged
	}
	if err != nil {
		return nil, control.NewError(control.CodeInvalid, fmt.Sprintf("the repository could not be started: %s", printable(err.Error())))
	}
	pushed := false
	if cmp.State == gitrepo.RemoteDifferent {
		note := fmt.Sprintf("the repository exists in the folder on branch %s at %s and the source is not bound", cmp.Branch, shortID(commit))
		wc, err := r.set.git.Open(gc, a.Path)
		if err != nil {
			return nil, control.NewError(control.CodeInvalid, fmt.Sprintf("%s; %s", printable(err.Error()), note))
		}
		if err := wc.PushBranch(ctx); err != nil {
			return nil, pushFailure(err, note)
		}
		pushed = true
	}
	conf, rev, err := r.set.load()
	if err != nil {
		return nil, err
	}
	if _, err := r.set.add(rc, conf, rev); err != nil {
		return nil, err
	}
	return control.SourceAddResult{ID: rc.ID, Path: a.Path, Kind: control.SourceKindRemote, Head: commit, Files: len(plan.planned), Pushed: pushed}, nil
}
