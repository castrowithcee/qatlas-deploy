package changelog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const fakeGitHubToken = "fake-gh-token-1"

var testSource = ParseSource("example-org/example-app")

type fakeRelease struct {
	tag        string
	draft, pre bool
	body       string
}

// fakeGitHub serves releases of example-org/example-app in pages of three and records what it saw.
type fakeGitHub struct {
	*httptest.Server
	mu       sync.Mutex
	releases []fakeRelease
	requests atomic.Int32
	auths    []string
	status   int // when set, every request answers with it
	headers  map[string]string
}

func newFake(t *testing.T, rels []fakeRelease) *fakeGitHub {
	f := &fakeGitHub{releases: rels}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeGitHub) setStatus(status int, headers map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.headers = status, headers
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.requests.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auths = append(f.auths, r.Header.Get("Authorization"))
	if f.status != 0 {
		for k, v := range f.headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(f.status)
		fmt.Fprint(w, `{"message":"secret-answer-text"}`)
		return
	}
	if r.URL.Path != "/repos/example-org/example-app/releases" {
		http.NotFound(w, r)
		return
	}
	const per = 3
	page := 1
	fmt.Sscan(r.URL.Query().Get("page"), &page)
	lo, hi := (page-1)*per, min(page*per, len(f.releases))
	if hi < len(f.releases) {
		w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?per_page=100&page=%d>; rel="next", <http://%s%s?page=9>; rel="last"`, r.Host, r.URL.Path, page+1, r.Host, r.URL.Path))
	}
	var out []map[string]any
	for _, rel := range f.releases[lo:hi] {
		out = append(out, map[string]any{
			"tag_name": rel.tag, "name": "Release " + rel.tag, "body": rel.body, "draft": rel.draft, "prerelease": rel.pre,
			"html_url":     "https://github.com/example-org/example-app/releases/tag/" + rel.tag,
			"published_at": "2026-01-02T03:04:05Z",
		})
	}
	_ = json.NewEncoder(w).Encode(out)
}

func standardReleases() []fakeRelease {
	return []fakeRelease{
		{tag: "v1.5.0"}, {tag: "v1.4.0", body: "four"}, {tag: "v1.4.1-rc1", pre: true},
		{tag: "v1.3.5", draft: true}, {tag: "v1.3.1"}, {tag: "v1.3.0"},
		{tag: "1.2.5"}, {tag: "v1.2.4"}, {tag: "v1.2.3"}, {tag: "v1.2.0"},
	}
}

func versions(rs []Release) string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Version)
	}
	return strings.Join(out, ",")
}

func (f *fakeGitHub) client(opts Options) *Client {
	opts.BaseURL = f.URL
	opts.MinInterval = -1
	return New(opts)
}

func TestRangeAcrossPages(t *testing.T) {
	f := newFake(t, standardReleases())
	res := f.client(Options{Token: fakeGitHubToken}).Changelogs(context.Background(), testSource, "v1.2.3", "1.4.0")
	if res.Reason != "" {
		t.Fatalf("reason %q", res.Reason)
	}
	if got, want := versions(res.Releases), "1.4.0,1.3.1,1.3.0,1.2.5,1.2.4"; got != want {
		t.Fatalf("versions = %s, want %s", got, want)
	}
	if res.Link != "https://github.com/example-org/example-app/releases" {
		t.Fatalf("link = %q", res.Link)
	}
	if res.Releases[0].Body != "four" {
		t.Fatalf("body = %q", res.Releases[0].Body)
	}
	if f.requests.Load() != 4 {
		t.Fatalf("requests = %d, want 4 pages", f.requests.Load())
	}
}

func TestVariantTargetMatchesCoreVersion(t *testing.T) {
	f := newFake(t, standardReleases())
	res := f.client(Options{}).Changelogs(context.Background(), testSource, "1.3.1-alpine", "1.4.0-alpine")
	if got := versions(res.Releases); got != "1.4.0" {
		t.Fatalf("versions = %s", got)
	}
}

func TestPrereleaseTarget(t *testing.T) {
	f := newFake(t, standardReleases())
	res := f.client(Options{}).Changelogs(context.Background(), testSource, "1.4.0", "1.4.1-rc1")
	if got := versions(res.Releases); got != "1.4.1" || res.Releases[0].Tag != "v1.4.1-rc1" {
		t.Fatalf("got %s %+v", got, res.Releases)
	}
	res = f.client(Options{}).Changelogs(context.Background(), testSource, "1.4.0", "1.5.0")
	if got := versions(res.Releases); got != "1.5.0" {
		t.Fatalf("pre-release must be skipped, got %s", got)
	}
}

func TestPackagePrefixAndCalVerTags(t *testing.T) {
	f := newFake(t, []fakeRelease{
		{tag: "Example-App@2.47.0"}, {tag: "@example-app/extra@2.46.0"}, {tag: "other@2.45.0"},
		{tag: "example-app@2.44.0"}, {tag: "v26.09.1-ls287"}, {tag: "v26.09.1-ls286"}, {tag: "v26.05.5"},
	})
	res := f.client(Options{}).Changelogs(context.Background(), testSource, "2.43.0", "2.47.0")
	if got := versions(res.Releases); got != "2.47.0,2.44.0" {
		t.Fatalf("versions = %s", got)
	}
	if res.Releases[0].Tag != "Example-App@2.47.0" {
		t.Fatalf("tag = %q", res.Releases[0].Tag)
	}
	res = f.client(Options{}).Changelogs(context.Background(), testSource, "26.05.5", "26.09.1")
	if got := versions(res.Releases); got != "26.9.1" {
		t.Fatalf("versions = %s", got)
	}
}

func TestUnknownCurrentReturnsTargetOnly(t *testing.T) {
	f := newFake(t, standardReleases())
	res := f.client(Options{}).Changelogs(context.Background(), testSource, "latest", "v1.3.0")
	if got := versions(res.Releases); got != "1.3.0" {
		t.Fatalf("versions = %s", got)
	}
}

func TestNothingToShow(t *testing.T) {
	f := newFake(t, standardReleases())
	c := f.client(Options{})
	for name, tc := range map[string]struct {
		src             Source
		current, target string
		reason          Reason
		link            string
	}{
		"no repository":    {Source{}, "1.0.0", "1.1.0", ReasonNoRepository, ""},
		"other host":       {ParseSource("https://git.example.org/a/b?x=1"), "1.0.0", "1.1.0", ReasonNoRepository, "https://git.example.org/a/b"},
		"target unusable":  {testSource, "1.0.0", "latest", ReasonNotComparable, "https://github.com/example-org/example-app/releases"},
		"target not newer": {testSource, "1.5.0", "1.4.0", ReasonNoReleases, "https://github.com/example-org/example-app/releases"},
		"no such release":  {testSource, "1.5.0", "1.6.0", ReasonNoReleases, "https://github.com/example-org/example-app/releases"},
	} {
		res := c.Changelogs(context.Background(), tc.src, tc.current, tc.target)
		if len(res.Releases) != 0 || res.Reason != tc.reason || res.Link != tc.link {
			t.Errorf("%s: %+v", name, res)
		}
	}
	if f.requests.Load() != 4 { // only "no such release" asks GitHub, over four pages
		t.Fatalf("requests = %d", f.requests.Load())
	}
}

func TestFailuresYieldLink(t *testing.T) {
	const link = "https://github.com/example-org/example-app/releases"
	for name, tc := range map[string]struct {
		status  int
		headers map[string]string
		reason  Reason
	}{
		"not found":      {404, nil, ReasonNotFound},
		"rate limit 429": {429, map[string]string{"Retry-After": "60"}, ReasonRateLimited},
		"rate limit 403": {403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1"}, ReasonRateLimited},
		"denied 403":     {403, nil, ReasonDenied},
		"denied 401":     {401, nil, ReasonDenied},
		"server error":   {500, nil, ReasonUnreachable},
		"unavailable":    {503, nil, ReasonUnreachable},
		"redirect":       {302, map[string]string{"Location": "http://127.0.0.1:1/elsewhere"}, ReasonRedirect},
	} {
		f := newFake(t, standardReleases())
		f.setStatus(tc.status, tc.headers)
		res := f.client(Options{Token: fakeGitHubToken}).Changelogs(context.Background(), testSource, "1.0.0", "1.4.0")
		if len(res.Releases) != 0 || res.Link != link || res.Reason != tc.reason {
			t.Errorf("%s: %+v", name, res)
		}
		if strings.Contains(fmt.Sprint(res), "secret-answer-text") {
			t.Errorf("%s: answer text in result", name)
		}
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	var other atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		other.Add(1)
		fmt.Fprint(w, "[]")
	}))
	defer target.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusFound)
	}))
	defer api.Close()
	res := New(Options{BaseURL: api.URL, Token: fakeGitHubToken, MinInterval: -1}).Changelogs(context.Background(), testSource, "1.0.0", "1.1.0")
	if res.Reason != ReasonRedirect || other.Load() != 0 {
		t.Fatalf("reason %q, redirect target hit %d times", res.Reason, other.Load())
	}
}

func TestInvalidAnswers(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"not json": func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>") },
		"wrong shape": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"tag_name":"v1.0.0"}`)
		},
		"foreign next link": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Link", `<https://evil.example.org/x>; rel="next"`)
			fmt.Fprint(w, "[]")
		},
		"repeating next link": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Link", fmt.Sprintf(`<http://%s%s>; rel="next"`, r.Host, r.URL.RequestURI()))
			fmt.Fprint(w, "[]")
		},
		"oversized": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "[")
			chunk := strings.Repeat(" ", 1<<20)
			for i := 0; i < 9; i++ {
				fmt.Fprint(w, chunk)
			}
			fmt.Fprint(w, "]")
		},
	} {
		srv := httptest.NewServer(handler)
		res := New(Options{BaseURL: srv.URL, MinInterval: -1}).Changelogs(context.Background(), testSource, "1.0.0", "1.1.0")
		srv.Close()
		if res.Reason != ReasonInvalidAnswer || res.Link == "" {
			t.Errorf("%s: %+v", name, res)
		}
	}
}

func TestUnreachableAndBadBase(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if res := New(Options{BaseURL: url, MinInterval: -1}).Changelogs(context.Background(), testSource, "1.0.0", "1.1.0"); res.Reason != ReasonUnreachable {
		t.Fatalf("closed server: %+v", res)
	}
	if res := New(Options{BaseURL: "ftp://x", MinInterval: -1}).Changelogs(context.Background(), testSource, "1.0.0", "1.1.0"); res.Reason != ReasonInvalidRequest {
		t.Fatalf("bad base: %+v", res)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := newFake(t, standardReleases())
	if res := f.client(Options{}).Changelogs(ctx, testSource, "1.0.0", "1.1.0"); res.Reason != ReasonCanceled {
		t.Fatalf("canceled: %+v", res)
	}
}

func TestMaxPages(t *testing.T) {
	f := newFake(t, standardReleases())
	res := f.client(Options{MaxPages: 1}).Changelogs(context.Background(), testSource, "1.0.0", "1.4.0")
	if f.requests.Load() != 1 || versions(res.Releases) != "1.4.0" {
		t.Fatalf("requests %d versions %s", f.requests.Load(), versions(res.Releases))
	}
}

func TestSanitize(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"plain":        {"a\tb\nc", "a\tb\nc"},
		"csi":          {"x\x1b[31mred\x1b[0my", "xredy"},
		"osc bel":      {"x\x1b]0;title\x07y", "xy"},
		"osc st":       {"x\x1b]8;;http://a\x1b\\link\x1b]8;;\x1b\\y", "xlinky"},
		"dcs":          {"x\x1bPdata\x1b\\y", "xy"},
		"unterminated": {"x\x1b]0;no end\nnext", "x\nnext"},
		"c1 csi":       {"x\u009b31my", "xy"},
		"nul bel bs":   {"a\x00b\x07c\x08d\x7fe", "abcde"},
		"crlf":         {"a\r\nb\rc", "a\nb\nc"},
		"bidi":         {"a‮b⁦c", "abc"},
		"bad utf8":     {"a\xffb", "ab"},
		"lone esc":     {"a\x1b", "a"},
	} {
		if got := Sanitize(tc.in); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestSanitizedInResult(t *testing.T) {
	f := newFake(t, []fakeRelease{{tag: "v1.1.0", body: "ok\x1b[2J\x1b]0;x\x07 done\x00"}})
	res := f.client(Options{}).Changelogs(context.Background(), testSource, "1.0.0", "1.1.0")
	if len(res.Releases) != 1 || res.Releases[0].Body != "ok done" {
		t.Fatalf("%+v", res)
	}
}

func TestLengthBounds(t *testing.T) {
	long := strings.Repeat("é", maxBodyBytes) // 2 bytes per rune: a cut must not split one
	f := newFake(t, []fakeRelease{{tag: "v1.3.0", body: long}, {tag: "v1.2.0", body: long}, {tag: "v1.1.0", body: long},
		{tag: "v1.0.5", body: long}, {tag: "v1.0.4", body: long}, {tag: "v1.0.3", body: long}, {tag: "v1.0.2", body: long},
		{tag: "v1.0.1", body: long}, {tag: "v1.0.0", body: long}})
	res := f.client(Options{}).Changelogs(context.Background(), testSource, "0.9.0", "1.3.0")
	total := 0
	for _, r := range res.Releases {
		total += len(r.Body)
		if len(r.Body) > maxBodyBytes+len(truncationNotice) || !strings.HasSuffix(r.Body, truncationNotice) {
			t.Fatalf("release %s: %d bytes, suffix %q", r.Tag, len(r.Body), r.Body[max(0, len(r.Body)-20):])
		}
		if !strings.Contains(strings.TrimSuffix(r.Body, truncationNotice), "é") || strings.ContainsRune(r.Body, '�') {
			t.Fatalf("cut inside a rune")
		}
	}
	if total > MaxTotalBytes {
		t.Fatalf("total = %d", total)
	}
	if res.Omitted == 0 || len(res.Releases)+res.Omitted != 9 {
		t.Fatalf("releases %d omitted %d", len(res.Releases), res.Omitted)
	}
}

func TestCacheHitExpiryAndCorruption(t *testing.T) {
	f := newFake(t, standardReleases())
	dir := filepath.Join(t.TempDir(), "cache")
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	c := f.client(Options{CacheDir: dir, CacheTTL: time.Hour, Now: clock})
	ask := func() Result { return c.Changelogs(context.Background(), testSource, "1.2.3", "1.4.0") }

	first := ask()
	n := f.requests.Load()
	if n == 0 || first.Reason != "" {
		t.Fatalf("first: %d %+v", n, first)
	}
	now = now.Add(30 * time.Minute)
	hit := ask()
	if f.requests.Load() != n || versions(hit.Releases) != versions(first.Releases) || !hit.FetchedAt.Equal(first.FetchedAt) {
		t.Fatalf("cache not used: %d requests", f.requests.Load())
	}
	now = now.Add(31 * time.Minute)
	ask()
	if f.requests.Load() != 2*n {
		t.Fatalf("expired cache not refreshed: %d requests", f.requests.Load())
	}

	path := c.cachePath(testSource.Repo)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := f.requests.Load()
	if res := ask(); res.Reason != "" || f.requests.Load() == before {
		t.Fatalf("corrupt cache: %+v, %d requests", res, f.requests.Load()-before)
	}
	var cf cacheFile
	data, _ := os.ReadFile(path)
	if json.Unmarshal(data, &cf) != nil || len(cf.Releases) == 0 {
		t.Fatalf("corrupt entry not replaced: %q", data)
	}
	// An expired cache is no fallback: a failure yields the link.
	now = now.Add(2 * time.Hour)
	f.setStatus(500, nil)
	if res := ask(); res.Reason != ReasonUnreachable || len(res.Releases) != 0 {
		t.Fatalf("expired cache served on failure: %+v", res)
	}
}

func TestCacheTextsAreSanitizedOnRead(t *testing.T) {
	dir := t.TempDir()
	f := newFake(t, nil)
	c := f.client(Options{CacheDir: dir})
	c.writeCache(testSource.Repo, cacheFile{Version: cacheVersion, Repo: "example-org/example-app", FetchedAt: time.Now(),
		Releases: []Release{{Version: "1.1.0", Tag: "v1.1.0", Body: "x\x1b[31my", URL: "javascript:alert(1)"}}})
	res := c.Changelogs(context.Background(), testSource, "1.0.0", "1.1.0")
	if len(res.Releases) != 1 || res.Releases[0].Body != "xy" || res.Releases[0].URL != "" || f.requests.Load() != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestCacheModes(t *testing.T) {
	f := newFake(t, standardReleases())
	dir := filepath.Join(t.TempDir(), "a", "cache")
	f.client(Options{CacheDir: dir}).Changelogs(context.Background(), testSource, "1.2.3", "1.4.0")
	di, err := os.Stat(dir)
	if err != nil || di.Mode().Perm() != 0o700 {
		t.Fatalf("dir: %v %v", di, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("entries = %d (temporary files left?)", len(entries))
	}
	fi, _ := entries[0].Info()
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v", fi.Mode().Perm())
	}
}

func TestCleanCache(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	write := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	write("old_repo.json", 48*time.Hour)
	write("new_repo.json", time.Hour)
	write(".tmp-123", 48*time.Hour)
	write("notes.txt", 48*time.Hour)
	if err := os.Mkdir(filepath.Join(dir, "sub.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if n := CleanCache(dir, 24*time.Hour, now); n != 2 {
		t.Fatalf("removed %d", n)
	}
	var left []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if strings.Join(left, ",") != "new_repo.json,notes.txt,sub.json" {
		t.Fatalf("left %v", left)
	}
	if CleanCache(filepath.Join(dir, "missing"), time.Hour, now) != 0 {
		t.Fatal("missing dir")
	}
}

func TestTokenOnlyAsAuthorizationHeaderToAPIHost(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	f := newFake(t, standardReleases())
	dir := t.TempDir()
	opts := Options{Token: " " + fakeGitHubToken + " ", CacheDir: dir, Logger: logger}
	c := f.client(opts)
	res := c.Changelogs(context.Background(), testSource, "1.2.3", "1.4.0")

	f.mu.Lock()
	for _, a := range f.auths {
		if a != "Bearer "+fakeGitHubToken {
			t.Errorf("Authorization = %q", a)
		}
	}
	f.mu.Unlock()

	shown := fmt.Sprintf("%v|%+v|%#v|%s|%v|%+v|%v|%v", opts, c, c, Token(fakeGitHubToken), res, res, Options{Token: fakeGitHubToken}, &opts)
	slog.New(slog.NewTextHandler(&logs, nil)).Info("x", "token", Token(fakeGitHubToken), "opts", opts)
	if strings.Contains(shown, fakeGitHubToken) || strings.Contains(logs.String(), fakeGitHubToken) {
		t.Fatalf("token leaked: %s %s", shown, logs.String())
	}
	if !strings.Contains(logs.String(), "changelog request") {
		t.Fatal("no request log")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		if strings.Contains(string(data), fakeGitHubToken) {
			t.Fatal("token in cache file")
		}
	}
	if Token("").String() != "changelog.Token(anonymous)" {
		t.Fatal("anonymous form")
	}
}

func TestTokenNotSentOnForeignPaginationLink(t *testing.T) {
	var foreign atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreign.Add(1) }))
	defer other.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", "<"+other.URL+"/next>; rel=\"next\"")
		fmt.Fprint(w, "[]")
	}))
	defer api.Close()
	res := New(Options{BaseURL: api.URL, Token: fakeGitHubToken, MinInterval: -1}).Changelogs(context.Background(), testSource, "1.0.0", "1.1.0")
	if res.Reason != ReasonInvalidAnswer || foreign.Load() != 0 {
		t.Fatalf("reason %q, foreign requests %d", res.Reason, foreign.Load())
	}
}

func TestInvalidTokenIsNotSent(t *testing.T) {
	f := newFake(t, standardReleases())
	res := f.client(Options{Token: "bad\ntoken"}).Changelogs(context.Background(), testSource, "1.0.0", "1.1.0")
	if res.Reason != ReasonInvalidRequest || f.requests.Load() != 0 {
		t.Fatalf("%+v, %d requests", res, f.requests.Load())
	}
}

func TestParseSource(t *testing.T) {
	const want = "example-org/example-app"
	const sshUser = "git"
	for _, ref := range []string{
		"example-org/example-app", "github.com/example-org/example-app", "https://github.com/example-org/example-app.git",
		sshUser + "@github.com:example-org/example-app.git", "ssh://" + sshUser + "@github.com/example-org/example-app", "https://github.com/example-org/example-app/",
	} {
		s := ParseSource(ref)
		if !s.GitHub || s.Repo.String() != want || s.Link != "https://github.com/example-org/example-app/releases" {
			t.Errorf("%q: %+v", ref, s)
		}
	}
	for _, ref := range []string{"", "plain", "a b/c", "../x", "https://github.com/onlyowner", "ftp://example.org/x"} {
		if s := ParseSource(ref); s.GitHub || s.Link != "" {
			t.Errorf("%q: %+v", ref, s)
		}
	}
}
