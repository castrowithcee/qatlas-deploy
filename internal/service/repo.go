package service

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"unicode"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/hostkey"
	"github.com/castrowithcee/qatlas-deploy/internal/stackcheck"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const (
	knownHostsFile = "known_hosts"
	maxErrorChars  = 300
)

// WorkingCopy is what the service needs from a checked-out stacks repository.
type WorkingCopy interface {
	Fetch(ctx context.Context) error
	Head(ctx context.Context) (string, error)
	RemoteHead(ctx context.Context) (string, error)
	RemoteURL(ctx context.Context) (string, error)
	Remotes(ctx context.Context) ([]string, error)
	Status(ctx context.Context) ([]gitrepo.Change, error)
	ChangedFolders(ctx context.Context, from, to string) ([]string, error)
	FastForward(ctx context.Context, commit string) error
	RevertStack(ctx context.Context, stack, deployed, failed, reason string) (string, error)
	CommitFile(ctx context.Context, path string, edit func(old []byte) ([]byte, error), message string) (string, error)
	IsAncestor(ctx context.Context, a, b string) (bool, error)
	Push(ctx context.Context, commit string) error
	TrackedFiles(ctx context.Context, dir string) ([]string, error)
	IsIgnored(ctx context.Context, path string) (bool, error)
	// The steps that give a repository a remote, in the remote mode; see internal/gitrepo.
	CompareRemote(ctx context.Context, url string) (gitrepo.Comparison, error)
	AddRemote(ctx context.Context, url string) error
	FetchBranch(ctx context.Context, branch string) error
	CommitHostTree(ctx context.Context, parent string) (string, error)
	SaveLocalHistory(ctx context.Context, commit string) error
	ResetBranch(ctx context.Context, commit string) error
	SetUpstream(ctx context.Context) error
	PushBranch(ctx context.Context) error
	ScanPush(ctx context.Context, head, base string) (gitrepo.Scan, error)
}

// Git is the git layer behind the repository commands. Config.Git nil means internal/gitrepo.
type Git interface {
	Clone(ctx context.Context, cfg gitrepo.Config, url, dir string) (WorkingCopy, error)
	Open(cfg gitrepo.Config, dir string) (WorkingCopy, error)
	// CompareFolder and InitFromRemote work on a folder without a repository; see internal/gitrepo.
	CompareFolder(ctx context.Context, cfg gitrepo.Config, dir, url string, files []string) (gitrepo.Comparison, error)
	InitFromRemote(ctx context.Context, cfg gitrepo.Config, dir, url, branch, remoteCommit string, files, excluded []string) (string, error)
}

type realGit struct{}

func (realGit) Clone(ctx context.Context, cfg gitrepo.Config, url, dir string) (WorkingCopy, error) {
	return gitrepo.Clone(ctx, cfg, url, dir)
}

func (realGit) Open(cfg gitrepo.Config, dir string) (WorkingCopy, error) {
	return gitrepo.Open(cfg, dir)
}

func (realGit) CompareFolder(ctx context.Context, cfg gitrepo.Config, dir, url string, files []string) (gitrepo.Comparison, error) {
	return gitrepo.CompareFolder(ctx, cfg, dir, url, files)
}

func (realGit) InitFromRemote(ctx context.Context, cfg gitrepo.Config, dir, url, branch, remoteCommit string, files, excluded []string) (string, error) {
	return gitrepo.InitFromRemote(ctx, cfg, dir, url, branch, remoteCommit, files, excluded)
}

var plainPath = regexp.MustCompile(`^/[A-Za-z0-9_./@+-]+$`)

// ValidateStackDir reports whether dir has the shape the service accepts for a stack folder.
func ValidateStackDir(dir string) error { return checkDirShape(dir) }

func checkDirShape(dir string) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == "/" || !plainPath.MatchString(dir) {
		return errors.New("the directory must be an absolute, clean path of letters, digits and ._/@+-")
	}
	return nil
}

type repoService struct {
	// setMu serializes binding commands; hostMu guards the check and pin of host keys. Reading the sources
	// needs neither: the file is replaced as a whole.
	setMu    sync.Mutex
	hostMu   sync.Mutex
	stateDir string
	keys     *credentials.Store
	set      *sourceSet
	// stopped tells whether the engine holds the stopped mark of a stack.
	stopped func(name string) *control.HoldInfo
	// probe reads the host key of a server; tests replace it.
	probe func(ctx context.Context, host, port string) (hostkey.Key, error)
}

func newRepoService(stateDir string, keys *credentials.Store, set *sourceSet) *repoService {
	return &repoService{stateDir: stateDir, keys: keys, set: set, probe: hostkey.Probe}
}

func (s *repoService) knownHosts() string { return filepath.Join(s.stateDir, knownHostsFile) }

func targetOf(rawURL string) (host, port string, err error) {
	host, port, err = gitrepo.SSHTarget(rawURL)
	if err != nil {
		return "", "", control.NewError(control.CodeInvalid, gitrepo.ErrInvalidURL.Error())
	}
	return host, port, nil
}

func (s *repoService) handleProbe(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.RepoProbeArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	host, port, err := targetOf(a.URL)
	if err != nil {
		return nil, err
	}
	key, err := s.probeHost(ctx, host, port)
	if err != nil {
		return nil, err
	}
	s.hostMu.Lock()
	state, err := hostkey.Check(s.knownHosts(), host, port, key)
	s.hostMu.Unlock()
	if err != nil {
		return nil, err
	}
	if state == hostkey.Changed {
		return nil, changedKeyError(host, port)
	}
	return control.RepoProbeResult{
		Host: host, Port: port, KeyType: key.Type, PublicKey: key.Authorized, Fingerprint: key.Fingerprint,
		Pinned: state == hostkey.Pinned,
	}, nil
}

func (s *repoService) probeHost(ctx context.Context, host, port string) (hostkey.Key, error) {
	key, err := s.probe(ctx, host, port)
	switch {
	case errors.Is(err, hostkey.ErrUnreachable), errors.Is(err, hostkey.ErrNoHostKey):
		return hostkey.Key{}, control.NewError(control.CodeRemote, fmt.Sprintf("%s (%s)", err, hostkey.Pattern(host, port)))
	case err != nil:
		return hostkey.Key{}, err
	}
	return key, nil
}

func changedKeyError(host, port string) *control.Error {
	return control.NewError(control.CodeHostKey, fmt.Sprintf(
		"the host key of %s differs from the pinned key; the connection is refused", hostkey.Pattern(host, port)))
}

func (s *repoService) handleSet(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.RepoSetArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	host, port, err := targetOf(a.URL)
	if err != nil {
		return nil, err
	}
	if err := checkDirShape(a.Dir); err != nil {
		return nil, control.NewError(control.CodeInvalid, err.Error())
	}
	if err := checkFingerprint(a.Fingerprint); err != nil {
		return nil, err
	}
	if err := s.checkDirOutsideState(a.Dir); err != nil {
		return nil, err
	}

	s.setMu.Lock()
	defer s.setMu.Unlock()
	conf, rev, err := s.set.load()
	if err != nil {
		return nil, err
	}
	var src sourceConfig
	existing := s.set.byPath(a.Dir)
	switch {
	case existing != nil && existing.conf.URL != a.URL:
		return nil, control.NewError(control.CodeRepoBound,
			"the stack folder is already bound to another repository; a different binding is not accepted")
	case existing != nil:
		src = existing.conf
		// The working copy is the engine's to move; it stays where it is while it is looked at.
		existing.mu.Lock()
		defer existing.mu.Unlock()
	default:
		if src, err = s.set.newSourceConfig(conf, a.URL, a.Dir); err != nil {
			return nil, err
		}
	}
	if err := s.requireKey(a.Dir, src.Key); err != nil {
		return nil, err
	}

	// The key is read again here: only a key the operator confirmed is ever pinned.
	key, err := s.pinConfirmed(ctx, host, port, a.Fingerprint)
	if err != nil {
		return nil, err
	}

	gc := s.set.gitConfig(src)
	cloned := false
	wc, err := s.existingClone(ctx, gc, a)
	if err != nil {
		return nil, err
	}
	if wc == nil {
		if err := checkWritableFolder(a.Dir); err != nil {
			return nil, err
		}
		wc, err = s.set.git.Clone(ctx, gc, a.URL, a.Dir)
		if err != nil {
			return nil, gitFailure(err)
		}
		cloned = true
	}
	head, err := wc.Head(ctx)
	if err != nil {
		return nil, gitFailure(err)
	}
	if existing != nil {
		existing.eng.Wake()
	} else if _, err := s.set.add(src, conf, rev); err != nil {
		return nil, err
	}
	return control.RepoSetResult{URL: a.URL, Dir: a.Dir, Fingerprint: key.Fingerprint, Head: head, Cloned: cloned}, nil
}

// requireKey refuses a stack folder whose deploy key does not exist and says how to create one.
func (s *repoService) requireKey(dir, key string) error {
	if _, err := s.keys.ShowNamedKey(key); err != nil {
		if errors.Is(err, credentials.ErrNoKey) {
			return control.NewError(control.CodeNotFound, fmt.Sprintf(
				"no deploy key exists for stack folder %s; run 'qdeploy key generate --dir %s' (or 'qdeploy key set --dir %s' to store one you have) and enter it in the git platform", dir, dir, dir))
		}
		return answerOf(err)
	}
	return nil
}

// checkFingerprint refuses a confirmed host key that is not in the form probe answers with.
func checkFingerprint(fingerprint string) error {
	if !strings.HasPrefix(fingerprint, "SHA256:") {
		return control.NewError(control.CodeInvalid, "the confirmed fingerprint must be a SHA256:... value")
	}
	return nil
}

// pinConfirmed reads the host key of the server again and pins it when, and only when, it is the one the
// operator confirmed.
func (s *repoService) pinConfirmed(ctx context.Context, host, port, fingerprint string) (hostkey.Key, error) {
	key, err := s.probeHost(ctx, host, port)
	if err != nil {
		return hostkey.Key{}, err
	}
	if key.Fingerprint != fingerprint {
		return hostkey.Key{}, control.NewError(control.CodeHostKey,
			"the host key presented now is not the confirmed one; nothing was stored")
	}
	s.hostMu.Lock()
	err = hostkey.Pin(s.knownHosts(), host, port, key)
	s.hostMu.Unlock()
	if err != nil {
		if errors.Is(err, hostkey.ErrConflict) {
			return hostkey.Key{}, changedKeyError(host, port)
		}
		return hostkey.Key{}, err
	}
	return key, nil
}

// checkWritableFolder refuses a stack folder the service could not clone into and says why and what to do.
// The service never creates the folder: the root client does that after the operator confirmed the path. The
// access check also reports a read-only mount, which is what the unit's sandbox shows for an unlisted path.
func checkWritableFolder(dir string) error {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return control.NewError(control.CodeInvalid, fmt.Sprintf(
			"stack folder %s does not exist; run sudo qdeploy to create it after confirming the path", dir))
	case err != nil:
		return control.NewError(control.CodeInvalid, fmt.Sprintf("stack folder %s cannot be inspected by the service", dir))
	case !info.IsDir():
		return control.NewError(control.CodeInvalid, fmt.Sprintf("stack folder %s is not a directory", dir))
	}
	if err := syscall.Access(dir, 0x2); err != nil {
		return control.NewError(control.CodeInvalid, fmt.Sprintf(
			"stack folder %s is not writable by the service (%v); run sudo qdeploy to grant access", dir, err))
	}
	return nil
}

// existingClone returns the working copy at the directory when it already is a clone of the URL, nil when
// the directory is free for a clone, and refuses everything else.
func (s *repoService) existingClone(ctx context.Context, gc gitrepo.Config, a control.RepoSetArgs) (WorkingCopy, error) {
	info, err := os.Lstat(a.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("the directory cannot be inspected")
	}
	if !info.IsDir() {
		return nil, control.NewError(control.CodeInvalid, "the directory is not a directory (a symbolic link is not followed)")
	}
	entries, err := os.ReadDir(a.Dir)
	if err != nil {
		return nil, errors.New("the directory cannot be read")
	}
	if len(entries) == 0 {
		return nil, nil
	}
	notClone := control.NewError(control.CodeInvalid, "the directory is not empty and not a clone of this repository")
	wc, err := s.set.git.Open(gc, a.Dir)
	if err != nil {
		return nil, notClone
	}
	if url, err := wc.RemoteURL(ctx); err != nil || url != a.URL {
		return nil, notClone
	}
	return wc, nil
}

// checkDirOutsideState keeps the working copy and the service's private state apart.
func (s *repoService) checkDirOutsideState(dir string) error {
	state := filepath.Clean(s.stateDir)
	if dir == state || strings.HasPrefix(dir, state+"/") || strings.HasPrefix(state, dir+"/") {
		return control.NewError(control.CodeInvalid, "the directory must not contain or lie inside the service state directory")
	}
	return nil
}

func (s *repoService) handleFetch(ctx context.Context, raw json.RawMessage) (any, error) {
	var none struct{}
	if err := decodeArgs(raw, &none); err != nil {
		return nil, err
	}
	srcs := s.set.sources()
	if len(srcs) == 0 {
		return nil, control.NewError(control.CodeNoRepo, "no stacks repository is bound")
	}
	res := control.RepoFetchResult{Sources: make([]control.RepoFetchSource, 0, len(srcs))}
	var firstErr error
	failed := 0
	// A source that cannot be fetched is reported in its entry; the others are fetched all the same.
	for _, src := range srcs {
		item, err := s.fetch(ctx, src)
		if err != nil {
			failed++
			firstErr = cmp.Or(firstErr, err)
			var ce *control.Error
			if errors.As(err, &ce) {
				item.Error = ce.Message
			} else {
				item.Error = "the working copy cannot be fetched"
			}
		}
		res.Sources = append(res.Sources, item)
	}
	if failed == len(srcs) {
		return nil, firstErr
	}
	first := res.Sources[0]
	res.Dir, res.Head, res.RemoteHead, res.UpdatesAvailable = first.Dir, first.Head, first.RemoteHead, first.UpdatesAvailable
	return res, nil
}

func (s *repoService) fetch(ctx context.Context, src *source) (control.RepoFetchSource, error) {
	item := control.RepoFetchSource{ID: src.conf.ID, Dir: src.conf.Path}
	wc, err := s.set.git.Open(s.set.gitConfig(src.conf), src.conf.Path)
	if err != nil {
		return item, gitFailure(err)
	}
	// The ref lock alone: a fetch moves no working copy, so nobody waits for it but other fetches and pushes.
	src.refs.Lock()
	defer src.refs.Unlock()
	// A local source has no remote to fetch from.
	if src.conf.Kind != kindLocal {
		if err := wc.Fetch(ctx); err != nil {
			return item, gitFailure(err)
		}
	}
	if item.Head, err = wc.Head(ctx); err != nil {
		return item, gitFailure(err)
	}
	if item.RemoteHead, err = wc.RemoteHead(ctx); err != nil {
		return item, gitFailure(err)
	}
	item.UpdatesAvailable = item.Head != item.RemoteHead
	return item, nil
}

// firstDir is the stack folder of the source bound first.
func (s *repoService) firstDir() string {
	if srcs := s.set.sources(); len(srcs) > 0 {
		return srcs[0].conf.Path
	}
	return ""
}

func (s *repoService) handleStacks(_ context.Context, raw json.RawMessage) (any, error) {
	var none struct{}
	if err := decodeArgs(raw, &none); err != nil {
		return nil, err
	}
	_, r, err := s.set.ownedStacks()
	if err != nil {
		return nil, err
	}
	res := control.StacksResult{Dir: s.firstDir(), Stacks: make([]control.StackInfo, 0, len(r.all))}
	// Everything below comes from the working copy and may reach a terminal: it is reduced to printable text.
	for _, ref := range sortedRefs(r.all) {
		st := ref.st
		info := control.StackInfo{Name: printable(st.Name), Folder: printable(st.Folder), Update: st.UpdateMode(), Images: []control.ImageInfo{},
			Findings: len(st.Findings), Source: ref.src.conf.ID, SourceDir: ref.src.conf.Path}
		for _, img := range st.Images {
			info.Images = append(info.Images, control.ImageInfo{
				Service: printable(img.Service), Image: printable(img.Image), Var: printable(img.Var), Version: printable(img.Value),
			})
		}
		detail, blocked := r.blocked[ref.src][st.Name]
		if blocked {
			info.Collision = printable(detail)
		}
		switch {
		case st.Err != nil:
			info.Error = printable(st.Err.Error())
		case !blocked && s.stopped != nil:
			info.Stopped = s.stopped(st.Name)
		}
		res.Stacks = append(res.Stacks, info)
	}
	return res, nil
}

// sortedRefs orders stacks by name and, for one name, by source.
func sortedRefs(refs []stackRef) []stackRef {
	out := slices.Clone(refs)
	slices.SortStableFunc(out, func(a, b stackRef) int { return cmp.Compare(a.st.Name, b.st.Name) })
	return out
}

func (s *repoService) handleCheck(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.StacksCheckArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	_, r, err := s.set.ownedStacks()
	if err != nil {
		return nil, err
	}
	bySource := map[*source][]stacks.Stack{}
	var order []*source
	for _, ref := range r.all {
		if a.Stack != "" && ref.st.Name != a.Stack {
			continue
		}
		if _, ok := bySource[ref.src]; !ok {
			order = append(order, ref.src)
		}
		bySource[ref.src] = append(bySource[ref.src], ref.st)
	}
	if a.Stack != "" && len(order) == 0 {
		return nil, control.NewError(control.CodeNotFound, "no such stack in the working copy")
	}
	type sourced struct {
		stackcheck.Report
		source string
	}
	var reports []sourced
	for _, src := range order {
		cfg := s.set.gitConfig(src.conf)
		wc, err := s.set.git.Open(cfg, src.conf.Path)
		if err != nil {
			return nil, gitFailure(err)
		}
		// Only the local mode has no remote a versioned secret could reach.
		reps, err := stackcheck.Check(ctx, bySource[src], wc, !cfg.Local)
		if err != nil {
			return nil, gitFailure(err)
		}
		for _, r := range reps {
			reports = append(reports, sourced{r, src.conf.ID})
		}
	}
	slices.SortStableFunc(reports, func(a, b sourced) int { return cmp.Compare(a.Stack, b.Stack) })
	res := control.StacksCheckResult{Dir: s.firstDir(), Stacks: make([]control.StackReport, 0, len(reports))}
	// Everything below comes from the working copy and may reach a terminal: it is reduced to printable text.
	for _, r := range reports {
		rep := control.StackReport{Stack: printable(r.Stack), Source: r.source, Functions: make([]control.StackFunction, 0, len(r.Functions)),
			Findings: make([]control.CheckFinding, 0, len(r.Findings))}
		for _, f := range r.Functions {
			rep.Functions = append(rep.Functions, control.StackFunction{
				Function: f.Name, Works: f.Works, Mode: f.Mode, Reason: printable(f.Reason), Action: printable(f.Action),
			})
		}
		for _, f := range r.Findings {
			if f.Severity == stackcheck.SeverityError {
				res.Errors++
			} else {
				res.Warnings++
			}
			rep.Findings = append(rep.Findings, control.CheckFinding{
				Stack: printable(f.Stack), Rule: f.Rule, Severity: f.Severity, Cause: printable(f.Cause), Fix: printable(f.Fix),
			})
		}
		res.Stacks = append(res.Stacks, rep)
	}
	return res, nil
}

// gitFailure turns an error of the git layer into a fixed-class answer. Text that came from git or the remote
// is reduced to printable characters and a short length.
func gitFailure(err error) error {
	text := printable(err.Error())
	lower := strings.ToLower(text)
	if strings.Contains(lower, "host key verification failed") || strings.Contains(lower, "host identification has changed") {
		return control.NewError(control.CodeHostKey,
			"the server's host key does not match the pinned key; the connection was refused")
	}
	if errors.Is(err, gitrepo.ErrNotEmpty) {
		return control.NewError(control.CodeInvalid, "the directory is not empty")
	}
	return control.NewError(control.CodeRemote, text)
}

func printable(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n == maxErrorChars {
			b.WriteString("...")
			break
		}
		if unicode.IsPrint(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
		n++
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func registerRepoCommands(r *control.Registry, s *repoService) error {
	commands := map[string]control.Handler{
		control.RepoProbeCommand:   s.handleProbe,
		control.RepoSetCommand:     s.handleSet,
		control.RepoFetchCommand:   s.handleFetch,
		control.StacksListCommand:  s.handleStacks,
		control.StacksCheckCommand: s.handleCheck,
	}
	for name, h := range commands {
		if err := r.Register(name, h); err != nil {
			return err
		}
	}
	return nil
}
