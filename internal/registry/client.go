// Package registry reads tags, digests and image labels from an OCI registry through the distribution API.
// It authenticates anonymously, with Basic or with a Bearer token obtained from the realm the registry names.
//
// Everything a registry answers is untrusted data. Answers are size-bounded, redirects are never followed, and
// no answer text and no secret reaches an error or a log record.
package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-kit/httpx"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
)

// LabelSource is the image label that names the source repository.
const LabelSource = "org.opencontainers.image.source"

// Limits and defaults.
const (
	DefaultRequestTimeout = 30 * time.Second
	DefaultMaxPages       = 50
	// DefaultMinInterval is the spacing of requests to one host.
	DefaultMinInterval = 100 * time.Millisecond

	maxTagPageBytes  = 4 << 20
	maxManifestBytes = 1 << 20
	maxConfigBytes   = 1 << 20
	maxTokenBytes    = 64 << 10
	maxRetryHold     = 5 * time.Minute
	pageSize         = 1000
)

const (
	mediaOCIIndex       = "application/vnd.oci.image.index.v1+json"
	mediaOCIManifest    = "application/vnd.oci.image.manifest.v1+json"
	mediaDockerList     = "application/vnd.docker.distribution.manifest.list.v2+json"
	mediaDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	manifestAccept      = mediaOCIIndex + ", " + mediaOCIManifest + ", " + mediaDockerList + ", " + mediaDockerManifest
)

var (
	digestRe = regexp.MustCompile(`^[a-z0-9]+(?:[.+_-][a-z0-9]+)*:[a-zA-Z0-9=_-]+$`)
	// tagRe is the grammar of the distribution spec. A registry cannot hand out a tag outside it.
	tagRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
)

// Platform selects the manifest of a multi-platform image.
type Platform struct {
	OS, Arch string
}

// Options configure a Client. The zero value is usable.
type Options struct {
	// Credential is the login for every registry this client talks to. Zero is anonymous.
	Credential Credential
	// Transport is the round tripper of the HTTP client; nil is Go's default transport.
	Transport http.RoundTripper
	// BaseURL maps a registry host to its base URL, such as "https://host". The default is HTTPS on the
	// host; tests point it at a local server.
	BaseURL func(host string) string
	// Platform defaults to linux and the architecture of this process.
	Platform       Platform
	RequestTimeout time.Duration
	MaxPages       int
	// MinInterval spaces requests to one host. Zero is DefaultMinInterval; a negative value turns it off.
	MinInterval time.Duration
	// Logger receives one debug record per request with method, URL without query and status.
	Logger *slog.Logger
}

// Client talks to registries. It is safe for concurrent use. Bearer tokens are kept for the life of the
// client, so use a client per detection run.
type Client struct {
	opts   Options
	hc     *http.Client
	limits *ratelimit.Registry

	mu     sync.Mutex
	tokens map[string]string // "host\x00scope" -> bearer token
}

// New returns a client with defaults applied.
func New(opts Options) *Client {
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = DefaultRequestTimeout
	}
	if opts.MaxPages <= 0 {
		opts.MaxPages = DefaultMaxPages
	}
	if opts.Platform.OS == "" {
		opts.Platform.OS = "linux"
	}
	if opts.Platform.Arch == "" {
		opts.Platform.Arch = runtime.GOARCH
	}
	if opts.BaseURL == nil {
		opts.BaseURL = func(host string) string { return "https://" + host }
	}
	c := &Client{opts: opts, hc: httpx.NoRedirectClient(opts.RequestTimeout, opts.Transport), tokens: map[string]string{}}
	if opts.MinInterval >= 0 {
		interval := opts.MinInterval
		if interval == 0 {
			interval = DefaultMinInterval
		}
		c.limits = ratelimit.NewRegistry(interval)
	}
	return c
}

// Format keeps the credential and the tokens held in unexported fields out of every fmt verb.
func (c *Client) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "registry.Client") }

// Tags lists every tag of the repository, following the pagination links of the registry. Entries that are
// not valid tag names are dropped.
func (c *Client) Tags(ctx context.Context, repo Repo) ([]string, error) {
	op := "list tags of " + repo.String()
	base, err := c.baseURL(op, repo)
	if err != nil {
		return nil, err
	}
	next := base.String() + "/v2/" + repo.Name + "/tags/list?n=" + fmt.Sprint(pageSize)
	var tags []string
	for page := 0; next != ""; page++ {
		if page >= c.opts.MaxPages {
			return nil, invalid(op, fmt.Sprintf("the tag list exceeds %d pages", c.opts.MaxPages))
		}
		resp, err := c.do(ctx, op, repo, http.MethodGet, next, "application/json", maxTagPageBytes)
		if err != nil {
			return nil, err
		}
		var body struct {
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(resp.body, &body); err != nil {
			return nil, invalid(op, "the registry returned an invalid tag list")
		}
		for _, tag := range body.Tags {
			if tagRe.MatchString(tag) {
				tags = append(tags, tag)
			}
		}
		if next, err = nextLink(op, resp.header.Values("Link"), next, base); err != nil {
			return nil, err
		}
	}
	return tags, nil
}

// Digest returns the manifest digest of a tag, read from a HEAD request.
func (c *Client) Digest(ctx context.Context, repo Repo, tag string) (string, error) {
	op := "read digest of " + repo.String()
	if err := checkReference(tag); err != nil {
		return "", err
	}
	base, err := c.baseURL(op, repo)
	if err != nil {
		return "", err
	}
	resp, err := c.do(ctx, op, repo, http.MethodHead, base.String()+"/v2/"+repo.Name+"/manifests/"+tag, manifestAccept, 0)
	if err != nil {
		return "", err
	}
	d := resp.header.Get("Docker-Content-Digest")
	if !digestRe.MatchString(d) {
		return "", invalid(op, "the registry did not report a usable digest")
	}
	return d, nil
}

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Platform  *struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
	} `json:"platform"`
}

type manifestDoc struct {
	MediaType string       `json:"mediaType"`
	Config    *descriptor  `json:"config"`
	Manifests []descriptor `json:"manifests"`
}

// Labels returns the labels of the image configuration for the tag; the map is empty if the image has none.
// For a multi-platform image the manifest of the configured platform is used. The configuration blob is
// requested like everything else, so a registry that hands blobs to another host through a redirect fails
// here with a redirect error.
func (c *Client) Labels(ctx context.Context, repo Repo, tag string) (map[string]string, error) {
	op := "read labels of " + repo.String()
	if err := checkReference(tag); err != nil {
		return nil, err
	}
	base, err := c.baseURL(op, repo)
	if err != nil {
		return nil, err
	}
	manifestURL := func(ref string) string { return base.String() + "/v2/" + repo.Name + "/manifests/" + ref }
	doc, err := c.manifest(ctx, op, repo, manifestURL(tag))
	if err != nil {
		return nil, err
	}
	if len(doc.Manifests) > 0 || doc.MediaType == mediaOCIIndex || doc.MediaType == mediaDockerList {
		digest := ""
		for _, m := range doc.Manifests {
			if m.Platform != nil && m.Platform.OS == c.opts.Platform.OS && m.Platform.Architecture == c.opts.Platform.Arch {
				digest = m.Digest
				break
			}
		}
		if digest == "" {
			return nil, invalid(op, fmt.Sprintf("the image has no manifest for %s/%s", c.opts.Platform.OS, c.opts.Platform.Arch))
		}
		if !digestRe.MatchString(digest) {
			return nil, invalid(op, "the index names an invalid manifest digest")
		}
		if doc, err = c.manifest(ctx, op, repo, manifestURL(digest)); err != nil {
			return nil, err
		}
	}
	if doc.Config == nil || !digestRe.MatchString(doc.Config.Digest) {
		return nil, invalid(op, "the manifest has no usable image configuration")
	}
	resp, err := c.do(ctx, op, repo, http.MethodGet, base.String()+"/v2/"+repo.Name+"/blobs/"+doc.Config.Digest, "application/json", maxConfigBytes)
	if err != nil {
		return nil, err
	}
	if algo, hexSum, _ := strings.Cut(doc.Config.Digest, ":"); algo == "sha256" {
		sum := sha256.Sum256(resp.body)
		if hex.EncodeToString(sum[:]) != strings.ToLower(hexSum) {
			return nil, invalid(op, "the image configuration does not match its digest")
		}
	}
	var cfg struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err := json.Unmarshal(resp.body, &cfg); err != nil {
		return nil, invalid(op, "the registry returned an invalid image configuration")
	}
	if cfg.Config.Labels == nil {
		return map[string]string{}, nil
	}
	return cfg.Config.Labels, nil
}

func (c *Client) manifest(ctx context.Context, op string, repo Repo, u string) (manifestDoc, error) {
	resp, err := c.do(ctx, op, repo, http.MethodGet, u, manifestAccept, maxManifestBytes)
	if err != nil {
		return manifestDoc{}, err
	}
	var doc manifestDoc
	if err := json.Unmarshal(resp.body, &doc); err != nil {
		return manifestDoc{}, invalid(op, "the registry returned an invalid manifest")
	}
	if doc.MediaType == "" {
		doc.MediaType = strings.TrimSpace(strings.SplitN(resp.header.Get("Content-Type"), ";", 2)[0])
	}
	return doc, nil
}

// invalid is a registry answer that cannot be used. The message is written here, never taken from the answer.
func invalid(op, message string) error {
	return &httpx.Error{Class: httpx.ClassInvalidResponse, Op: op, Message: message}
}

func checkReference(ref string) error {
	if ref == "" || strings.ContainsAny(ref, "/?#\\ ") {
		return errors.New("invalid tag or reference")
	}
	return nil
}

func (c *Client) baseURL(op string, repo Repo) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(c.opts.BaseURL(repo.Host), "/"))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return nil, invalid(op, "the registry base URL is invalid")
	}
	return u, nil
}

// nextLink returns the absolute URL of the rel="next" link, or "" if there is none. Only links on the host
// and scheme of the registry are followed, so a hostile response cannot send credentials elsewhere.
func nextLink(op string, values []string, from string, base *url.URL) (string, error) {
	cur, err := url.Parse(from)
	if err != nil {
		return "", invalid(op, "invalid pagination link")
	}
	for _, v := range values {
		for _, part := range splitLinks(v) {
			target, params, ok := strings.Cut(part, ">")
			if !ok || !strings.HasPrefix(strings.TrimSpace(target), "<") {
				continue
			}
			if !hasRelNext(params) {
				continue
			}
			ref, err := url.Parse(strings.TrimPrefix(strings.TrimSpace(target), "<"))
			if err != nil {
				return "", invalid(op, "invalid pagination link")
			}
			abs := cur.ResolveReference(ref)
			if abs.Host != base.Host || abs.Scheme != base.Scheme || abs.User != nil {
				return "", invalid(op, "the pagination link leaves the registry host")
			}
			if abs.String() == from {
				return "", invalid(op, "the pagination link repeats the current page")
			}
			return abs.String(), nil
		}
	}
	return "", nil
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

type response struct {
	header http.Header
	body   []byte
}

// statusTexts are the messages for a failed status. The credential hint depends on whether one is set.
func (c *Client) statusTexts(repo Repo) httpx.StatusTexts {
	auth := "the registry requires a login; configure a registry credential for this image"
	permission := "the registry denied access to this repository"
	if c.opts.Credential.set() {
		auth = "the configured registry credential was not accepted"
		permission = "the configured registry credential lacks pull access to this repository"
	}
	return httpx.StatusTexts{
		Subject:    "registry " + repo.Host,
		Auth:       auth,
		Permission: permission,
		NotFound:   "the repository or tag does not exist on registry " + repo.Host,
	}
}

// do performs one request with authentication. A 401 triggers one retry with the credentials the challenge
// asks for.
func (c *Client) do(ctx context.Context, op string, repo Repo, method, rawURL, accept string, limit int64) (response, error) {
	base, err := c.baseURL(op, repo)
	if err != nil {
		return response{}, err
	}
	scope := repo.scopeKey()
	auth := c.cachedAuth(repo.Host, scope)
	for attempt := 0; ; attempt++ {
		resp, status, err := c.roundTrip(ctx, op, repo, method, rawURL, accept, auth, limit)
		if err != nil {
			return response{}, err
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			if auth, err = c.authorize(ctx, op, repo, base, resp.header.Values("Www-Authenticate"), scope); err != nil {
				return response{}, err
			}
			if auth == "" {
				return response{}, httpx.ClassifyStatus(op, status, c.statusTexts(repo))
			}
			continue
		}
		if status >= 200 && status < 300 {
			return resp, nil
		}
		return response{}, httpx.ClassifyStatus(op, status, c.statusTexts(repo))
	}
}

func (c *Client) cachedAuth(host, scope string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.tokens[host+"\x00"+scope]; ok {
		return "Bearer " + t
	}
	return ""
}

// roundTrip sends one request and reads the limited body. A 3xx answer is an error: the request would carry
// its credential to whatever the answer names.
func (c *Client) roundTrip(ctx context.Context, op string, repo Repo, method, rawURL, accept, auth string, limit int64) (response, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return response{}, 0, invalid(op, "invalid request URL")
	}
	if err := c.wait(ctx, op, req.URL.Host); err != nil {
		return response{}, 0, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "qdeploy")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return response{}, 0, c.transportError(ctx, op, repo, err)
	}
	defer res.Body.Close()
	c.logRequest(method, req.URL, res.StatusCode)
	switch {
	case res.StatusCode >= 300 && res.StatusCode < 400:
		return response{}, 0, httpx.ClassifyStatus(op, res.StatusCode, c.statusTexts(repo))
	case res.StatusCode == http.StatusTooManyRequests:
		if c.limits != nil {
			c.limits.For(req.URL.Host).HoldFor(min(httpx.RetryAfter(res.Header), maxRetryHold))
		}
		return response{}, 0, httpx.ClassifyStatus(op, res.StatusCode, c.statusTexts(repo))
	}
	out := response{header: res.Header}
	if method != http.MethodHead && res.StatusCode >= 200 && res.StatusCode < 300 {
		body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
		if err != nil {
			return response{}, 0, c.transportError(ctx, op, repo, err)
		}
		if int64(len(body)) > limit {
			return response{}, 0, invalid(op, fmt.Sprintf("the registry response is larger than %d bytes", limit))
		}
		out.body = body
	}
	return out, res.StatusCode, nil
}

// wait spaces requests to one host.
func (c *Client) wait(ctx context.Context, op, host string) error {
	if c.limits == nil {
		return nil
	}
	err := c.limits.For(host).Wait(ctx)
	var beyond *ratelimit.BeyondDeadlineError
	if errors.As(err, &beyond) {
		return &httpx.Error{Class: httpx.ClassRateLimited, Op: op, Message: "the request spacing for this registry exceeds the time left"}
	}
	return err
}

// transportError reports a failed exchange without the text of the underlying error, which may carry URLs.
func (c *Client) transportError(ctx context.Context, op string, repo Repo, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var timeout interface{ Timeout() bool }
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return &httpx.Error{Class: httpx.ClassTimeout, Op: op, Message: "registry " + repo.Host + " did not answer in time"}
	}
	return &httpx.Error{Class: httpx.ClassUnreachable, Op: op, Message: "registry " + repo.Host + " could not be reached"}
}

func (c *Client) logRequest(method string, u *url.URL, status int) {
	if c.opts.Logger == nil {
		return
	}
	clean := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
	c.opts.Logger.Debug("registry request", "method", method, "url", clean.String(), "status", status)
}

// authorize turns a WWW-Authenticate challenge into an Authorization header value and remembers bearer
// tokens. It returns "" when no credential can answer the challenge.
func (c *Client) authorize(ctx context.Context, op string, repo Repo, base *url.URL, challenges []string, scope string) (string, error) {
	for _, ch := range challenges {
		scheme, params := parseChallenge(ch)
		switch scheme {
		case "bearer":
			return c.bearer(ctx, op, repo, base, params, scope)
		case "basic":
			if c.opts.Credential.set() {
				return basicHeader(c.opts.Credential), nil
			}
		}
	}
	return "", nil
}

func basicHeader(cred Credential) string {
	r := &http.Request{Header: http.Header{}}
	r.SetBasicAuth(cred.User, cred.Token)
	return r.Header.Get("Authorization")
}

func (c *Client) bearer(ctx context.Context, op string, repo Repo, base *url.URL, params map[string]string, scope string) (string, error) {
	realm, err := url.Parse(params["realm"])
	if err != nil || realm.Host == "" || realm.User != nil {
		return "", invalid(op, "the registry sent an unusable authentication challenge")
	}
	if realm.Scheme != "https" && (realm.Scheme != base.Scheme || realm.Host != base.Host) {
		return "", invalid(op, "the registry sent an insecure authentication realm")
	}
	if s := params["scope"]; s != "" {
		scope = s
	}
	q := realm.Query()
	if s := params["service"]; s != "" {
		q.Set("service", s)
	}
	q.Set("scope", scope)
	realm.RawQuery = q.Encode()

	auth := ""
	if c.opts.Credential.set() {
		auth = basicHeader(c.opts.Credential)
	}
	resp, status, err := c.roundTrip(ctx, op, repo, http.MethodGet, realm.String(), "application/json", auth, maxTokenBytes)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", httpx.ClassifyStatus(op, status, c.statusTexts(repo))
	}
	var tok struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(resp.body, &tok); err != nil {
		return "", invalid(op, "the token service sent an invalid response")
	}
	t := tok.Token
	if t == "" {
		t = tok.AccessToken
	}
	if t == "" || strings.ContainsAny(t, "\r\n") {
		return "", invalid(op, "the token service sent no usable token")
	}
	c.mu.Lock()
	c.tokens[repo.Host+"\x00"+repo.scopeKey()] = t
	c.mu.Unlock()
	return "Bearer " + t, nil
}

func (r Repo) scopeKey() string { return "repository:" + r.Name + ":pull" }

// parseChallenge reads one WWW-Authenticate value: a scheme followed by key="value" pairs.
func parseChallenge(v string) (string, map[string]string) {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(v), " ")
	params := map[string]string{}
	for rest = strings.TrimSpace(rest); rest != ""; {
		k, after, ok := strings.Cut(rest, "=")
		if !ok {
			break
		}
		k = strings.ToLower(strings.TrimSpace(k))
		after = strings.TrimLeft(after, " ")
		var val string
		if strings.HasPrefix(after, `"`) {
			end := strings.Index(after[1:], `"`)
			if end < 0 {
				break
			}
			val, rest = after[1:1+end], after[2+end:]
		} else {
			val, rest, _ = strings.Cut(after, ",")
			val = strings.TrimSpace(val)
			rest = "," + rest
		}
		params[k] = val
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), ","))
	}
	return strings.ToLower(scheme), params
}
