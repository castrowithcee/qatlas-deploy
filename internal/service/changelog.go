package service

import (
	"context"
	"errors"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/changelog"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/registry"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const (
	// githubCredential is the credential that holds the GitHub token alone.
	githubCredential = "github.com"
	// sourceLabel is the image label that names the source repository.
	sourceLabel = "org.opencontainers.image.source"
	// changelogCacheDir is the cache directory below the state directory.
	changelogCacheDir = "changelog-cache"
	// changelogCacheMaxAge is how long an unused cache file stays on disk; the client treats older data as stale
	// anyway.
	changelogCacheMaxAge = 24 * time.Hour
	// maxChangelogBytes bounds the release texts of one answer, well below control.MaxResponseBytes.
	maxChangelogBytes = 512 << 10
	// releaseOverhead is what a release costs in the answer besides its texts.
	releaseOverhead = 256
)

type changelogSource struct {
	store    *credentials.Store
	regOpts  registry.Options
	opts     changelog.Options
	cacheDir string
	regCred  func(string) (registry.Credential, error)
}

func newChangelogSource(store *credentials.Store, regOpts registry.Options, opts changelog.Options, cacheDir string) *changelogSource {
	opts.CacheDir = cacheDir
	return &changelogSource{store: store, regOpts: regOpts, opts: opts, cacheDir: cacheDir, regCred: registryCredentialFrom(store)}
}

// clean removes stale cache files. It runs at start and before changelogs are fetched.
func (c *changelogSource) clean() { changelog.CleanCache(c.cacheDir, changelogCacheMaxAge, time.Now()) }

// token returns the stored GitHub token, or anonymous access when there is none or it cannot be read.
func (c *changelogSource) token() changelog.Token {
	value, err := c.store.Value(githubCredential)
	if err != nil {
		return ""
	}
	return changelog.Token(value)
}

// changelogBudget bounds the bytes of release texts across one answer.
type changelogBudget struct{ left int }

// forStack returns the release notes of every item of the stored detection that has an update. Any failure
// leaves the link or nothing for that item; this never fails a request.
func (c *changelogSource) forStack(ctx context.Context, st stacks.Stack, check engine.UpdateCheck, budget *changelogBudget) []control.ChangelogInfo {
	var out []control.ChangelogInfo
	var client *changelog.Client
	labels := &labelReader{c: c, clients: map[string]*registry.Client{}}
	for _, it := range check.Items {
		if it.Status != engine.UpdateAvailable || it.Selected == "" {
			continue
		}
		if client == nil {
			c.clean()
			opts := c.opts
			opts.Token = c.token()
			client = changelog.New(opts)
		}
		var src changelog.Source
		if ref := ruleSource(st, it.Variable); ref != "" {
			src = changelog.ParseSource(ref)
		} else {
			src = changelog.ParseSource(labels.source(ctx, it.Image, it.Selected))
		}
		res := client.Changelogs(ctx, src, it.Current, it.Selected)
		info := control.ChangelogInfo{
			Variable: printable(it.Variable), Current: printable(it.Current), Target: printable(it.Selected),
			Link: printable(res.Link), Reason: string(res.Reason), Releases: []control.ChangelogRelease{}, Omitted: res.Omitted,
		}
		if src.GitHub {
			info.Source = printable(src.Repo.String())
		}
		for _, r := range res.Releases {
			cost := len(r.Body) + len(r.Name) + len(r.URL) + len(r.Version) + releaseOverhead
			if cost > budget.left {
				info.Omitted++
				continue
			}
			budget.left -= cost
			info.Releases = append(info.Releases, control.ChangelogRelease{
				Version: printable(r.Version), Name: printable(r.Name), Published: r.Published, URL: printable(r.URL), Body: r.Body,
			})
		}
		if len(info.Releases) == 0 && info.Reason == "" {
			info.Reason = string(changelog.ReasonNoReleases)
		}
		out = append(out, info)
	}
	return out
}

// labelReader reads the source label of images, one registry client per host and one lookup per image tag.
type labelReader struct {
	c       *changelogSource
	clients map[string]*registry.Client
	seen    map[string]string
}

// source returns the label value, or "" when the image has none or the lookup fails for any reason.
func (l *labelReader) source(ctx context.Context, image, tag string) string {
	key := image + "\x00" + tag
	if v, ok := l.seen[key]; ok {
		return v
	}
	if l.seen == nil {
		l.seen = map[string]string{}
	}
	v := l.read(ctx, image, tag)
	l.seen[key] = v
	return v
}

func (l *labelReader) read(ctx context.Context, image, tag string) string {
	repo, err := registry.ParseRepo(image)
	if err != nil {
		return ""
	}
	cl, ok := l.clients[repo.Host]
	if !ok {
		cred, err := l.c.regCred(repo.Host)
		if err != nil && !errors.Is(err, credentials.ErrNotFound) {
			l.clients[repo.Host] = nil
			return ""
		}
		opts := l.c.regOpts
		opts.Credential = cred
		cl = registry.New(opts)
		l.clients[repo.Host] = cl
	}
	if cl == nil {
		return ""
	}
	labels, err := cl.Labels(ctx, repo, tag)
	if err != nil {
		return ""
	}
	return labels[sourceLabel]
}

// ruleSource is the repository the stack settings name for the variable; it beats the image label.
func ruleSource(st stacks.Stack, variable string) string {
	if st.Rules == nil {
		return ""
	}
	return st.Rules.Update.Changelog[variable]
}
