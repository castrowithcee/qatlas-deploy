// Package servicetest holds a stand-in for the git layer of the service, so tests can bind a stacks repository
// without a git server.
package servicetest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	// CloneErr and FetchErr make the call fail.
	CloneErr, FetchErr error

	mu      sync.Mutex
	origins map[string]string
	clones  int
	last    gitrepo.Config
}

// New returns a fake with a fixed commit id.
func New() *Git {
	return &Git{Head: "0123456789abcdef0123456789abcdef01234567", origins: map[string]string{}}
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

func (c *copyOf) Fetch(context.Context) error { return c.g.FetchErr }

func (c *copyOf) Head(context.Context) (string, error) { return c.g.Head, nil }

func (c *copyOf) RemoteHead(context.Context) (string, error) {
	if c.g.Remote != "" {
		return c.g.Remote, nil
	}
	return c.g.Head, nil
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
