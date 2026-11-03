package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/castrowithcee/qatlas-kit/configfile"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/hostkey"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const (
	repoConfigFile = "stacks-repo.json"
	knownHostsFile = "known_hosts"
	repoConfigVer  = 1
	maxErrorChars  = 300
)

// WorkingCopy is what the service needs from a checked-out stacks repository.
type WorkingCopy interface {
	Fetch(ctx context.Context) error
	Head(ctx context.Context) (string, error)
	RemoteHead(ctx context.Context) (string, error)
	RemoteURL(ctx context.Context) (string, error)
	Status(ctx context.Context) ([]gitrepo.Change, error)
	ChangedFolders(ctx context.Context, from, to string) ([]string, error)
	FastForward(ctx context.Context) error
}

// Git is the git layer behind the repository commands. Config.Git nil means internal/gitrepo.
type Git interface {
	Clone(ctx context.Context, cfg gitrepo.Config, url, dir string) (WorkingCopy, error)
	Open(cfg gitrepo.Config, dir string) (WorkingCopy, error)
}

type realGit struct{}

func (realGit) Clone(ctx context.Context, cfg gitrepo.Config, url, dir string) (WorkingCopy, error) {
	return gitrepo.Clone(ctx, cfg, url, dir)
}

func (realGit) Open(cfg gitrepo.Config, dir string) (WorkingCopy, error) {
	return gitrepo.Open(cfg, dir)
}

// repoConfig is the persisted binding to the stacks repository.
type repoConfig struct {
	Version int    `json:"version"`
	URL     string `json:"url"`
	Dir     string `json:"dir"`
}

var plainPath = regexp.MustCompile(`^/[A-Za-z0-9_./@+-]+$`)

func validateRepoConfig(c repoConfig) error {
	if c.Version != repoConfigVer {
		return fmt.Errorf("unsupported version %d", c.Version)
	}
	if err := gitrepo.ValidateURL(c.URL); err != nil {
		return err
	}
	return checkDirShape(c.Dir)
}

func checkDirShape(dir string) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == "/" || !plainPath.MatchString(dir) {
		return errors.New("the directory must be an absolute, clean path of letters, digits and ._/@+-")
	}
	return nil
}

var repoCodec = configfile.Codec[repoConfig]{
	Validate: validateRepoConfig,
	Encode:   func(c repoConfig) ([]byte, error) { return json.MarshalIndent(c, "", "  ") },
	Decode: func(b []byte) (repoConfig, error) {
		var c repoConfig
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		err := dec.Decode(&c)
		return c, err
	},
}

type repoService struct {
	mu       sync.Mutex
	stateDir string
	keys     *credentials.Store
	cfg      *configfile.Store[repoConfig]
	git      Git
	// onBound is called after a binding was stored, with mu held; it must not block.
	onBound func()
	// probe reads the host key of a server; tests replace it.
	probe func(ctx context.Context, host, port string) (hostkey.Key, error)
}

func newRepoService(stateDir string, keys *credentials.Store, git Git) (*repoService, error) {
	store, err := configfile.NewStore(filepath.Join(stateDir, repoConfigFile), repoCodec)
	if err != nil {
		return nil, err
	}
	if git == nil {
		git = realGit{}
	}
	return &repoService{stateDir: stateDir, keys: keys, cfg: store, git: git, probe: hostkey.Probe}, nil
}

func (s *repoService) knownHosts() string { return filepath.Join(s.stateDir, knownHostsFile) }

func (s *repoService) gitConfig() gitrepo.Config {
	return gitrepo.Config{KeyFile: s.keys.KeyPath(), KnownHosts: s.knownHosts()}
}

// bound returns the persisted binding, or the fixed answer that none exists.
func (s *repoService) bound() (repoConfig, error) {
	c, _, err := s.cfg.LoadVersioned()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return repoConfig{}, control.NewError(control.CodeNoRepo, "no stacks repository is bound")
	case err != nil:
		return repoConfig{}, errors.New("the stacks repository configuration cannot be read")
	}
	return c, nil
}

// engineSource is the engine's view of the binding: read again in every poll, so a binding made later by
// "repo set" is picked up. The engine takes mu itself around its use of the working copy.
func (s *repoService) engineSource() (engine.Binding, error) {
	s.mu.Lock()
	c, err := s.bound()
	s.mu.Unlock()
	var ce *control.Error
	if errors.As(err, &ce) && ce.Code == control.CodeNoRepo {
		return engine.Binding{}, engine.ErrNoRepo
	}
	if err != nil {
		return engine.Binding{}, err
	}
	wc, err := s.git.Open(s.gitConfig(), c.Dir)
	if err != nil {
		return engine.Binding{}, err
	}
	return engine.Binding{Dir: c.Dir, Copy: wc}, nil
}

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
	s.mu.Lock()
	defer s.mu.Unlock()
	key, err := s.probeHost(ctx, host, port)
	if err != nil {
		return nil, err
	}
	state, err := hostkey.Check(s.knownHosts(), host, port, key)
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
	if !strings.HasPrefix(a.Fingerprint, "SHA256:") {
		return nil, control.NewError(control.CodeInvalid, "the confirmed fingerprint must be a SHA256:... value")
	}
	if err := s.checkDirOutsideState(a.Dir); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.keys.ShowKey(); err != nil {
		return nil, answerOf(err)
	}
	existing, rev, err := s.cfg.LoadVersioned()
	switch {
	case err == nil:
		if existing.URL != a.URL || existing.Dir != a.Dir {
			return nil, control.NewError(control.CodeRepoBound,
				"a stacks repository is already bound; a different binding is not accepted")
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return nil, errors.New("the stacks repository configuration cannot be read")
	}

	// The key is read again here: only a key the operator confirmed is ever pinned.
	key, err := s.probeHost(ctx, host, port)
	if err != nil {
		return nil, err
	}
	if key.Fingerprint != a.Fingerprint {
		return nil, control.NewError(control.CodeHostKey,
			"the host key presented now is not the confirmed one; nothing was stored")
	}
	if err := hostkey.Pin(s.knownHosts(), host, port, key); err != nil {
		if errors.Is(err, hostkey.ErrConflict) {
			return nil, changedKeyError(host, port)
		}
		return nil, err
	}

	gc := s.gitConfig()
	cloned := false
	wc, err := s.existingClone(ctx, gc, a)
	if err != nil {
		return nil, err
	}
	if wc == nil {
		wc, err = s.git.Clone(ctx, gc, a.URL, a.Dir)
		if err != nil {
			return nil, gitFailure(err)
		}
		cloned = true
	}
	head, err := wc.Head(ctx)
	if err != nil {
		return nil, gitFailure(err)
	}
	if _, err := s.cfg.SaveIfUnchanged(repoConfig{Version: repoConfigVer, URL: a.URL, Dir: a.Dir}, rev); err != nil {
		return nil, errors.New("the stacks repository configuration cannot be written")
	}
	if s.onBound != nil {
		s.onBound()
	}
	return control.RepoSetResult{URL: a.URL, Dir: a.Dir, Fingerprint: key.Fingerprint, Head: head, Cloned: cloned}, nil
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
	wc, err := s.git.Open(gc, a.Dir)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.bound()
	if err != nil {
		return nil, err
	}
	wc, err := s.git.Open(s.gitConfig(), c.Dir)
	if err != nil {
		return nil, gitFailure(err)
	}
	if err := wc.Fetch(ctx); err != nil {
		return nil, gitFailure(err)
	}
	head, err := wc.Head(ctx)
	if err != nil {
		return nil, gitFailure(err)
	}
	remote, err := wc.RemoteHead(ctx)
	if err != nil {
		return nil, gitFailure(err)
	}
	return control.RepoFetchResult{Dir: c.Dir, Head: head, RemoteHead: remote, UpdatesAvailable: head != remote}, nil
}

func (s *repoService) handleStacks(_ context.Context, raw json.RawMessage) (any, error) {
	var none struct{}
	if err := decodeArgs(raw, &none); err != nil {
		return nil, err
	}
	s.mu.Lock()
	c, err := s.bound()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	list, err := stacks.Load(c.Dir)
	if err != nil {
		return nil, control.NewError(control.CodeNoRepo, "the working copy cannot be read; run 'qdeploy repo set' again")
	}
	res := control.StacksResult{Dir: c.Dir, Stacks: make([]control.StackInfo, 0, len(list))}
	// Everything below comes from the working copy and may reach a terminal: it is reduced to printable text.
	for _, st := range list {
		info := control.StackInfo{Name: printable(st.Name), Update: st.UpdateMode(), Images: []control.ImageInfo{}, Findings: len(st.Findings)}
		for _, img := range st.Images {
			info.Images = append(info.Images, control.ImageInfo{
				Service: printable(img.Service), Image: printable(img.Image), Var: printable(img.Var), Version: printable(img.Value),
			})
		}
		if st.Err != nil {
			info.Error = printable(st.Err.Error())
		}
		res.Stacks = append(res.Stacks, info)
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
		control.RepoProbeCommand:  s.handleProbe,
		control.RepoSetCommand:    s.handleSet,
		control.RepoFetchCommand:  s.handleFetch,
		control.StacksListCommand: s.handleStacks,
	}
	for name, h := range commands {
		if err := r.Register(name, h); err != nil {
			return err
		}
	}
	return nil
}
