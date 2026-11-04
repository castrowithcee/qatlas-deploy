// Package changelog reads the release notes of a GitHub repository between two versions. It caches the
// release list per repository. Release texts are untrusted and reduced to plain text.
//
// Nothing here aborts a caller: every failure yields a Result with the link to the releases page and a short
// fixed Reason. No GitHub answer text and no token reaches a result, a log record or an error.
package changelog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-kit/httpx"
	"github.com/castrowithcee/qatlas-kit/ratelimit"

	"github.com/castrowithcee/qatlas-deploy/internal/version"
)

// Limits and defaults.
const (
	DefaultBaseURL        = "https://api.github.com"
	DefaultRequestTimeout = 30 * time.Second
	DefaultMaxPages       = 5
	DefaultCacheTTL       = time.Hour
	DefaultMinInterval    = 100 * time.Millisecond
	// MaxTotalBytes bounds the release texts of one Result.
	MaxTotalBytes = 64 << 10

	maxBodyBytes     = 8 << 10
	maxNameBytes     = 256
	maxPageBytes     = 8 << 20
	maxRetryHold     = 5 * time.Minute
	minPartialBudget = 256
	pageSize         = 100
	apiVersion       = "2022-11-28"
)

// Reason says in a short fixed text why a Result holds no release notes.
type Reason string

// The reasons. They are constants of this package, never taken from an answer.
const (
	ReasonNoRepository   Reason = "no GitHub repository is known for this source"
	ReasonNotComparable  Reason = "the versions cannot be compared"
	ReasonNoReleases     Reason = "no release notes in this version range"
	ReasonNotFound       Reason = "the repository or its releases were not found"
	ReasonDenied         Reason = "GitHub denied access"
	ReasonRateLimited    Reason = "the GitHub rate limit was reached"
	ReasonRedirect       Reason = "GitHub answered with a redirect"
	ReasonUnreachable    Reason = "GitHub could not be reached"
	ReasonInvalidAnswer  Reason = "GitHub sent an unusable answer"
	ReasonCanceled       Reason = "the request was canceled"
	ReasonInvalidRequest Reason = "the request could not be built"
)

// Options configure a Client. The zero value is usable but caches nothing.
type Options struct {
	// Token is sent only as Authorization header to the host of BaseURL. Zero is anonymous.
	Token Token
	// BaseURL is the API address; tests point it at a local server. Default DefaultBaseURL.
	BaseURL string
	// Transport is the round tripper of the HTTP client; nil is Go's default transport.
	Transport http.RoundTripper
	// CacheDir holds the per-repository cache files. Empty disables the cache.
	CacheDir string
	// CacheTTL is how long a cache file counts as fresh. Default DefaultCacheTTL.
	CacheTTL time.Duration
	// RequestTimeout limits each request including reading the answer. Default DefaultRequestTimeout.
	RequestTimeout time.Duration
	// MaxPages caps the pages read per repository; older releases beyond it are not listed.
	MaxPages int
	// MinInterval spaces requests to one host. Zero is DefaultMinInterval; a negative value turns it off.
	MinInterval time.Duration
	// Now is the clock; default time.Now.
	Now func() time.Time
	// Logger receives one debug record per request with method, URL without query and status.
	Logger *slog.Logger
}

// Client reads changelogs. It is safe for concurrent use.
type Client struct {
	opts    Options
	hc      *http.Client
	base    *url.URL
	baseErr error
	limits  *ratelimit.Registry
	mu      sync.Mutex // serializes cache writes of one client
}

// Release is one published GitHub release as plain text.
type Release struct {
	// Version is the core version of the tag without prefix or variant, such as "1.4.0".
	Version string `json:"version"`
	Tag     string `json:"tag"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	URL     string `json:"url,omitempty"`
	// Prerelease reports a pre-release flag of GitHub or a pre-release suffix of the tag.
	Prerelease bool      `json:"prerelease,omitempty"`
	Published  time.Time `json:"published,omitzero"`
}

// Result is the answer of Changelogs.
type Result struct {
	// Releases are the releases above the current and up to the target version, newest first.
	Releases []Release
	// Omitted counts releases left out because the texts reached MaxTotalBytes.
	Omitted int
	// Link is the address to show instead of or besides the notes: the releases page of the repository, or the
	// address the source gave for another host. Empty when the source holds none.
	Link string
	// Reason is set when Releases is empty.
	Reason Reason
	// FetchedAt is when the underlying release list was read from GitHub.
	FetchedAt time.Time
}

// New returns a client with defaults applied.
func New(opts Options) *Client {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	if opts.CacheTTL <= 0 {
		opts.CacheTTL = DefaultCacheTTL
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = DefaultRequestTimeout
	}
	if opts.MaxPages <= 0 {
		opts.MaxPages = DefaultMaxPages
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	opts.Token = Token(strings.TrimSpace(string(opts.Token)))
	c := &Client{opts: opts, hc: httpx.NoRedirectClient(opts.RequestTimeout, opts.Transport)}
	c.base, c.baseErr = parseBase(opts.BaseURL)
	if opts.MinInterval >= 0 {
		interval := opts.MinInterval
		if interval == 0 {
			interval = DefaultMinInterval
		}
		c.limits = ratelimit.NewRegistry(interval)
	}
	return c
}

// Format keeps the token and the paths held in unexported fields out of every fmt verb.
func (c *Client) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "changelog.Client") }

func parseBase(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return nil, errors.New("invalid GitHub API base URL")
	}
	return u, nil
}

// Changelogs returns the releases of the source above current and up to target (target inclusive), matched
// by core version so that "29.0.2-apache" finds the release "v29.0.2". Drafts and pre-releases are skipped
// unless the release is the target itself. With an unknown current version only the release of target is
// returned. The release list comes from a fresh cache or from GitHub. On any failure the Result has no
// Releases, the link and a Reason.
func (c *Client) Changelogs(ctx context.Context, src Source, current, target string) Result {
	res := Result{Link: src.Link}
	if !src.GitHub || !src.Repo.valid() {
		res.Reason = ReasonNoRepository
		return res
	}
	res.Link = src.Repo.ReleasesURL()
	tc, ok := version.ParseCore(target)
	if !ok {
		res.Reason = ReasonNotComparable
		return res
	}
	cc, haveCur := version.ParseCore(current)
	if haveCur && cc.Compare(tc) >= 0 {
		res.Reason = ReasonNoReleases
		return res
	}
	list, fetched, reason := c.list(ctx, src.Repo)
	if reason != "" {
		res.Reason = reason
		return res
	}
	res.FetchedAt = fetched
	res.Releases, res.Omitted = bound(pick(list, src.Repo.Name, cc, haveCur, tc, target))
	if len(res.Releases) == 0 {
		res.Reason = ReasonNoReleases
	}
	return res
}

// stripPackage removes the "<name>@" prefix that monorepos put on release tags, such as "n8n@2.47.0", when
// name is the repository name. Tags of other packages keep their prefix and are not versions.
func stripPackage(tag, name string) string {
	if n := len(name) + 1; len(tag) > n && tag[n-1] == '@' && strings.EqualFold(tag[:n-1], name) {
		return tag[n:]
	}
	return tag
}

func normalize(tag, name string) string { return strings.TrimPrefix(stripPackage(tag, name), "v") }

// pick selects the releases in (cur, target], newest first, one per version.
func pick(all []Release, name string, cur version.Core, haveCur bool, tc version.Core, target string) []Release {
	type item struct {
		core version.Core
		rel  Release
	}
	var items []item
	for _, r := range all {
		core, ok := version.ParseCore(stripPackage(r.Tag, name))
		if !ok || core.Compare(tc) > 0 {
			continue
		}
		if haveCur && core.Compare(cur) <= 0 || !haveCur && core.Compare(tc) != 0 {
			continue
		}
		exact := normalize(r.Tag, name) == normalize(target, name)
		if (r.Prerelease || core.Prerelease) && !exact {
			continue
		}
		// A pre-release target is older than the stable release of the same numbers.
		if tc.Prerelease && core.Compare(tc) == 0 && !exact {
			continue
		}
		items = append(items, item{core, r})
	}
	// Equal versions: the plain tag beats a variant tag, then list order decides.
	sort.SliceStable(items, func(i, j int) bool {
		if c := items[i].core.Compare(items[j].core); c != 0 {
			return c > 0
		}
		return !strings.Contains(items[i].rel.Tag, "-") && strings.Contains(items[j].rel.Tag, "-")
	})
	var out []Release
	for _, it := range items {
		if len(out) > 0 && out[len(out)-1].Version == it.core.String() {
			continue
		}
		out = append(out, it.rel)
	}
	return out
}

// bound keeps the texts of the releases within MaxTotalBytes, newest first. A text that does not fit is cut
// with the truncation notice; releases after the budget is spent are counted as omitted.
func bound(rels []Release) ([]Release, int) {
	left := MaxTotalBytes
	for i := range rels {
		body := rels[i].Body
		if len(body) > left {
			if left < minPartialBudget {
				return rels[:i], len(rels) - i
			}
			body = truncate(body, left-len(truncationNotice))
			rels[i].Body = body
		}
		left -= len(body)
	}
	return rels, 0
}

func (c *Client) list(ctx context.Context, repo Repo) ([]Release, time.Time, Reason) {
	if cached, ok := c.readCache(repo); ok && c.opts.Now().Sub(cached.FetchedAt) < c.opts.CacheTTL {
		return cached.Releases, cached.FetchedAt, ""
	}
	rels, reason := c.fetch(ctx, repo)
	if reason != "" {
		return nil, time.Time{}, reason
	}
	now := c.opts.Now()
	c.mu.Lock()
	c.writeCache(repo, cacheFile{Version: cacheVersion, Repo: repo.String(), FetchedAt: now, Releases: rels})
	c.mu.Unlock()
	return rels, now, ""
}

type rawRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// cleanRelease makes every text field safe and bounded.
func cleanRelease(r Release) Release {
	r.Tag = oneLine(Sanitize(r.Tag))
	r.Name = truncate(oneLine(Sanitize(r.Name)), maxNameBytes)
	r.Body = truncate(strings.TrimSpace(Sanitize(r.Body)), maxBodyBytes)
	r.URL = safeLink(r.URL)
	return r
}

// fetch reads the release list page by page. Beyond MaxPages older releases are left out.
func (c *Client) fetch(ctx context.Context, repo Repo) ([]Release, Reason) {
	if c.baseErr != nil {
		return nil, ReasonInvalidRequest
	}
	next := c.base.String() + "/repos/" + repo.Owner + "/" + repo.Name + "/releases?per_page=" + strconv.Itoa(pageSize)
	var out []Release
	for page := 0; next != "" && page < c.opts.MaxPages; page++ {
		var raw []rawRelease
		header, reason := c.get(ctx, next, &raw)
		if reason != "" {
			return nil, reason
		}
		for _, r := range raw {
			if r.Draft {
				continue
			}
			core, ok := version.ParseCore(stripPackage(r.TagName, repo.Name))
			if !ok {
				continue
			}
			out = append(out, cleanRelease(Release{
				Version:    core.String(),
				Tag:        r.TagName,
				Name:       r.Name,
				Body:       r.Body,
				URL:        r.HTMLURL,
				Prerelease: r.Prerelease || core.Prerelease,
				Published:  r.PublishedAt,
			}))
		}
		var ok bool
		if next, ok = nextLink(header.Values("Link"), next, c.base); !ok {
			return nil, ReasonInvalidAnswer
		}
	}
	return out, ""
}

// safeLink keeps only plain http(s) addresses without user info.
func safeLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Scheme != "https" && u.Scheme != "http" || strings.ContainsFunc(raw, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return ""
	}
	return u.String()
}

// get performs one request and decodes the JSON answer into out. The status maps to a Reason; the answer
// text is never kept.
func (c *Client) get(ctx context.Context, rawURL string, out any) (http.Header, Reason) {
	const op = "read GitHub releases"
	if strings.ContainsFunc(string(c.opts.Token), func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return nil, ReasonInvalidRequest
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, ReasonInvalidRequest
	}
	if c.limits != nil {
		if err := c.limits.For(req.URL.Host).Wait(ctx); err != nil {
			return nil, failReason(ctx, err)
		}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "qdeploy")
	// Redirects are never followed and pagination links stay on the API host, so this is the only place the
	// token can go.
	if c.opts.Token != "" {
		req.Header.Set("Authorization", "Bearer "+string(c.opts.Token))
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, failReason(ctx, err)
	}
	defer res.Body.Close()
	c.logRequest(req.URL, res.StatusCode)
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		if err := httpx.ReadJSON(op, "GitHub", res.Body, maxPageBytes, out); err != nil {
			return nil, ReasonInvalidAnswer
		}
		return res.Header, ""
	case res.StatusCode >= 300 && res.StatusCode < 400:
		return nil, ReasonRedirect
	case res.StatusCode == http.StatusTooManyRequests,
		res.StatusCode == http.StatusForbidden && (res.Header.Get("X-RateLimit-Remaining") == "0" || res.Header.Get("Retry-After") != ""):
		if c.limits != nil {
			c.limits.For(req.URL.Host).HoldFor(min(httpx.RetryAfter(res.Header), maxRetryHold))
		}
		return nil, ReasonRateLimited
	}
	cls := httpx.ClassifyStatus(op, res.StatusCode, httpx.StatusTexts{}).Class
	switch cls {
	case httpx.ClassAuth, httpx.ClassPermission:
		return nil, ReasonDenied
	case httpx.ClassNotFound:
		return nil, ReasonNotFound
	case httpx.ClassRateLimited:
		return nil, ReasonRateLimited
	}
	return nil, ReasonUnreachable
}

func failReason(ctx context.Context, err error) Reason {
	var beyond *ratelimit.BeyondDeadlineError
	switch {
	case errors.As(err, &beyond):
		return ReasonRateLimited
	case ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled):
		return ReasonCanceled
	}
	return ReasonUnreachable
}

func (c *Client) logRequest(u *url.URL, status int) {
	if c.opts.Logger == nil {
		return
	}
	clean := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
	c.opts.Logger.Debug("changelog request", "method", http.MethodGet, "url", clean.String(), "status", status)
}

// nextLink returns the absolute URL of the rel="next" link, or "" if there is none. Only links on the host
// and scheme of the API are followed, so a hostile response cannot send the token elsewhere. ok is false for
// an unusable link.
func nextLink(values []string, from string, base *url.URL) (next string, ok bool) {
	cur, err := url.Parse(from)
	if err != nil {
		return "", false
	}
	for _, v := range values {
		for _, part := range splitLinks(v) {
			target, params, found := strings.Cut(part, ">")
			target = strings.TrimSpace(target)
			if !found || !strings.HasPrefix(target, "<") || !hasRelNext(params) {
				continue
			}
			ref, err := url.Parse(strings.TrimPrefix(target, "<"))
			if err != nil {
				return "", false
			}
			abs := cur.ResolveReference(ref)
			if abs.Host != base.Host || abs.Scheme != base.Scheme || abs.User != nil || abs.String() == from {
				return "", false
			}
			return abs.String(), true
		}
	}
	return "", true
}

// splitLinks splits a Link header value on the commas that start a new "<url>" entry.
func splitLinks(v string) []string {
	var parts []string
	for {
		i := strings.Index(v, ", <")
		if i < 0 {
			return append(parts, v)
		}
		parts = append(parts, v[:i])
		v = v[i+2:]
	}
}

func hasRelNext(params string) bool {
	for _, p := range strings.Split(params, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if ok && strings.EqualFold(k, "rel") {
			for _, rel := range strings.Fields(strings.Trim(v, `"`)) {
				if strings.EqualFold(rel, "next") {
					return true
				}
			}
		}
	}
	return false
}
