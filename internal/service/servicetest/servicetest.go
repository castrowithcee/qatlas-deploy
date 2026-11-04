// Package servicetest holds a stand-in for the git layer of the service, so tests can bind a stacks repository
// without a git server.
package servicetest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
)

// Git implements service.Git. A clone writes Files below the target directory.
type Git struct {
	// Files are written on every clone, as paths relative to the directory.
	Files map[string]string
	// Head is reported as HEAD; Remote as the fetched branch, Head when empty.
	Head   string
	Remote string
	// Tracked lists the paths git reports as tracked, from the repository root.
	Tracked []string
	// NotIgnored makes every ignore question answer "not ignored"; by default everything is ignored.
	NotIgnored bool
	// CloneErr and FetchErr make the call fail; set them before the service starts or use SetFetchErr.
	CloneErr, FetchErr error
	// RevertErr is what RevertStack answers; nil means gitrepo.ErrNothingToRevert. Use SetRevertErr.
	RevertErr error

	mu      sync.Mutex
	gate    chan struct{} // set by BlockFetch
	entered chan struct{}
	origins map[string]string
	clones  int
	last    gitrepo.Config
	commits map[string]fakeCommit
	pushed  []string
}

// fakeCommit is a commit CommitFile made: the file it changes and its message.
type fakeCommit struct {
	path, message string
	content       []byte
}

// New returns a fake with a fixed commit id.
func New() *Git {
	return &Git{Head: "0123456789abcdef0123456789abcdef01234567", origins: map[string]string{}}
}

// SetFetchErr and SetRemote change the fake while a service is running; the service polls in the background,
// so a plain field write would race.
func (g *Git) SetFetchErr(err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.FetchErr = err
}

// BlockFetch makes every Fetch wait until release is called; entered gets a value for each Fetch that waits.
// The wait is outside the fake's mutex, so the other calls go on meanwhile.
func (g *Git) BlockFetch() (entered <-chan struct{}, release func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	gate, in := make(chan struct{}), make(chan struct{}, 16)
	g.gate, g.entered = gate, in
	var once sync.Once
	return in, func() {
		once.Do(func() {
			g.mu.Lock()
			g.gate, g.entered = nil, nil
			g.mu.Unlock()
			close(gate)
		})
	}
}

// SetRevertErr sets the answer of RevertStack.
func (g *Git) SetRevertErr(err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.RevertErr = err
}

// SetRemote sets the fetched branch.
func (g *Git) SetRemote(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Remote = id
}

// Clones is the number of clones made.
func (g *Git) Clones() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.clones
}

// LastConfig is the gitrepo configuration of the latest call.
func (g *Git) LastConfig() gitrepo.Config {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.last
}

// SetOrigin declares dir a working copy of url, as an earlier clone would have left it.
func (g *Git) SetOrigin(dir, url string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.origins[dir] = url
}

// Clone implements service.Git.
func (g *Git) Clone(_ context.Context, cfg gitrepo.Config, url, dir string) (service.WorkingCopy, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.last = cfg
	if g.CloneErr != nil {
		return nil, g.CloneErr
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for rel, body := range g.Files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return nil, err
		}
	}
	g.clones++
	g.origins[dir] = url
	return &copyOf{g: g, dir: dir}, nil
}

// Open implements service.Git.
func (g *Git) Open(cfg gitrepo.Config, dir string) (service.WorkingCopy, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.last = cfg
	return &copyOf{g: g, dir: dir}, nil
}

type copyOf struct {
	g   *Git
	dir string
}

func (c *copyOf) Fetch(ctx context.Context) error {
	c.g.mu.Lock()
	gate, in := c.g.gate, c.g.entered
	c.g.mu.Unlock()
	if gate != nil {
		select {
		case in <- struct{}{}:
		default:
		}
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	return c.g.FetchErr
}

func (c *copyOf) Head(context.Context) (string, error) {
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	return c.g.Head, nil
}

func (c *copyOf) RemoteHead(context.Context) (string, error) {
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	if c.g.Remote != "" {
		return c.g.Remote, nil
	}
	return c.g.Head, nil
}

// Status and ChangedFolders report an unchanged, clean working copy.
func (c *copyOf) Status(context.Context) ([]gitrepo.Change, error) { return nil, nil }

func (c *copyOf) ChangedFolders(context.Context, string, string) ([]string, error) { return nil, nil }

// IsAncestor answers yes: the fake's working copy never runs ahead of the branch.
func (c *copyOf) IsAncestor(context.Context, string, string) (bool, error) { return true, nil }

// FastForward follows a pushed commit of CommitFile and does nothing otherwise.
func (c *copyOf) FastForward(_ context.Context, to string) error {
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	fc, ok := c.g.commits[to]
	if !ok || c.g.Head == to {
		return nil
	}
	if err := os.WriteFile(filepath.Join(c.dir, filepath.FromSlash(fc.path)), fc.content, 0o644); err != nil {
		return err
	}
	c.g.Head = to
	return nil
}

// RevertStack creates nothing: the branch already has the deployed state unless RevertErr says otherwise.
func (c *copyOf) RevertStack(context.Context, string, string, string, string) (string, error) {
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	if c.g.RevertErr != nil {
		return "", c.g.RevertErr
	}
	return "", gitrepo.ErrNothingToRevert
}

// CommitFile makes a commit on the remote head that changes path, read from the working copy, which the fake
// keeps at the remote head after every push.
func (c *copyOf) CommitFile(_ context.Context, path string, edit func([]byte) ([]byte, error), message string) (string, error) {
	old, err := os.ReadFile(filepath.Join(c.dir, filepath.FromSlash(path)))
	if err != nil {
		return "", err
	}
	content, err := edit(old)
	if err != nil {
		return "", err
	}
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	if c.g.commits == nil {
		c.g.commits = map[string]fakeCommit{}
	}
	id := fmt.Sprintf("%040x", 0xc0de00+len(c.g.commits))
	c.g.commits[id] = fakeCommit{path: path, message: message, content: content}
	return id, nil
}

// Push accepts the commits CommitFile made and refuses every other one.
func (c *copyOf) Push(_ context.Context, id string) error {
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	if _, ok := c.g.commits[id]; !ok {
		return errors.New("the fake repository takes no pushes of other commits")
	}
	c.g.Remote = id
	c.g.pushed = append(c.g.pushed, id)
	return nil
}

// Pushed returns the messages of the pushed commits, oldest first.
func (g *Git) Pushed() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, 0, len(g.pushed))
	for _, id := range g.pushed {
		out = append(out, g.commits[id].message)
	}
	return out
}

func (c *copyOf) TrackedFiles(_ context.Context, dir string) ([]string, error) {
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	var out []string
	for _, p := range c.g.Tracked {
		if strings.HasPrefix(p, dir+"/") {
			out = append(out, p)
		}
	}
	return out, nil
}

func (c *copyOf) IsIgnored(context.Context, string) (bool, error) {
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	return !c.g.NotIgnored, nil
}

func (c *copyOf) RemoteURL(context.Context) (string, error) {
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	if url, ok := c.g.origins[c.dir]; ok {
		return url, nil
	}
	return "", errors.New("no origin")
}

// Stacks is a small working copy: a valid stack with rules, one with an invalid rule file and one without.
func Stacks() map[string]string {
	compose := "services:\n  app:\n    image: registry.example.test/demo/app:${DEMO_APP_VERSION}\n"
	return map[string]string{
		"README.md":                 "stacks\n",
		"demo-stack/compose.yaml":   compose,
		"demo-stack/.env":           "DEMO_APP_VERSION=1.2.3\n",
		"demo-stack/qdeploy.yaml":   "version: 1\nupdate:\n  mode: automatic\n  ceiling: minor\n",
		"broken-stack/compose.yaml": compose,
		"broken-stack/.env":         "DEMO_APP_VERSION=2.0.0\n",
		"broken-stack/qdeploy.yaml": "version: 1\nupdate:\n  mode: sometimes\n",
		"plain-stack/compose.yaml":  compose,
		"plain-stack/.env":          "DEMO_APP_VERSION=0.9.1\n",
	}
}
