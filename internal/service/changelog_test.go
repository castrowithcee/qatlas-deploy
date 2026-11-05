package service_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/changelog"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/registry"
	"github.com/castrowithcee/qatlas-deploy/internal/registry/registrytest"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
	"github.com/castrowithcee/qatlas-deploy/internal/service/servicetest"
)

const (
	fakeGitHubToken = "fake-github-token-789"
	demoRepo        = "example-org/example-app"
	sourceLabel     = "org.opencontainers.image.source"
	demoSourceURL   = "https://github.com/" + demoRepo
	demoDetectedVar = "DEMO_APP_VERSION"
)

var demoTags = []string{"1.2.3", "1.2.4", "1.3.0", "2.0.0", "2.1.0-rc.1"}

// fakeGitHub serves the releases of demoRepo and records what it was asked.
type fakeGitHub struct {
	srv      *httptest.Server
	mu       sync.Mutex
	paths    []string
	auths    []string
	status   int
	repoName string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{repoName: demoRepo}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		status := f.status
		f.mu.Unlock()
		if status != 0 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(status)
			return
		}
		if r.URL.Path != "/repos/"+f.repoName+"/releases" {
			http.NotFound(w, r)
			return
		}
		var out []map[string]any
		for _, tag := range []string{"v2.0.0", "v1.4.0", "v1.3.0", "v1.2.4", "v1.2.3"} {
			out = append(out, map[string]any{
				"tag_name": tag, "name": "Release " + tag, "body": "Notes of " + tag + "\n\n- change",
				"html_url": "https://github.com/" + demoRepo + "/releases/tag/" + tag, "published_at": "2026-01-02T03:04:05Z",
			})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) setStatus(s int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = s
}

func (f *fakeGitHub) requests() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...), append([]string(nil), f.auths...)
}

func withGitHub(f *fakeGitHub) func(*service.Config) {
	return func(c *service.Config) {
		c.Changelog = changelog.Options{BaseURL: f.srv.URL, MinInterval: -1}
	}
}

// changelogHarness binds the demo working copy with a registry whose images carry the given labels.
func changelogHarness(t *testing.T, labels map[string]string, files map[string]string, opts ...func(*service.Config)) (*harness, *fakeGitHub) {
	t.Helper()
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": demoTags}, Labels: labels})
	gh := newFakeGitHub(t)
	all := append([]func(*service.Config){withRegistry(srv.URL), withGitHub(gh)}, opts...)
	return boundWith(t, nil, files, all...), gh
}

func refreshWithChangelog(t *testing.T, h *harness) control.StackUpdates {
	t.Helper()
	var res control.UpdatesResult
	if err := h.call(control.UpdatesListCommand, control.UpdatesArgs{Stack: "demo-stack", Refresh: true, Changelog: true}, &res); err != nil {
		t.Fatal(err)
	}
	return stackNamed(res, "demo-stack")
}

func releaseVersions(info control.ChangelogInfo) string {
	var v []string
	for _, r := range info.Releases {
		v = append(v, r.Version)
	}
	return strings.Join(v, ",")
}

func TestChangelogRangeFromImageLabel(t *testing.T) {
	h, gh := changelogHarness(t, map[string]string{sourceLabel: demoSourceURL}, nil)
	s := refreshWithChangelog(t, h)
	if len(s.Changelogs) != 1 {
		t.Fatalf("changelogs = %+v", s.Changelogs)
	}
	c := s.Changelogs[0]
	if c.Variable != demoDetectedVar || c.Current != "1.2.3" || c.Target != "1.3.0" || c.Source != demoRepo ||
		releaseVersions(c) != "1.3.0,1.2.4" || c.Reason != "" || c.Link != demoSourceURL+"/releases" {
		t.Fatalf("changelog = %+v", c)
	}
	if r := c.Releases[0]; r.Name != "Release v1.3.0" || !strings.HasPrefix(r.Body, "Notes of v1.3.0") || r.URL == "" || r.Published.IsZero() {
		t.Fatalf("release = %+v", r)
	}

	// The stored detection serves the single-stack command without a registry query.
	var one control.ChangelogResult
	if err := h.call(control.ChangelogShowCommand, control.ChangelogArgs{Stack: "demo-stack"}, &one); err != nil {
		t.Fatal(err)
	}
	if one.Stack != "demo-stack" || one.Checked.IsZero() || len(one.Items) != 1 || releaseVersions(one.Items[0]) != "1.3.0,1.2.4" {
		t.Fatalf("show = %+v", one)
	}
	if paths, _ := gh.requests(); len(paths) != 1 {
		t.Fatalf("the release list should come from the cache the second time: %v", paths)
	}
}

// Registries hand the image configuration to a CDN through a redirect; the label is still read from there.
func TestChangelogFromLabelBehindBlobRedirect(t *testing.T) {
	labels := map[string]string{sourceLabel: demoSourceURL}
	cdn := registrytest.NewCDN(t, registrytest.CDNConfig{Body: registrytest.ConfigBlob(labels), Redirects: 1})
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": demoTags}, Labels: labels, BlobRedirect: cdn.BlobURL()})
	gh := newFakeGitHub(t)
	h := boundWith(t, nil, nil, withRegistry(srv.URL), withGitHub(gh))
	s := refreshWithChangelog(t, h)
	if len(s.Changelogs) != 1 {
		t.Fatalf("changelogs = %+v", s.Changelogs)
	}
	if c := s.Changelogs[0]; c.Source != demoRepo || c.Reason != "" || releaseVersions(c) != "1.3.0,1.2.4" {
		t.Fatalf("changelog = %+v", c)
	}
	if len(cdn.Headers()) != 2 {
		t.Fatalf("blob host requests = %d, want 2", len(cdn.Headers()))
	}
}

func TestChangelogOnlyForItemsWithUpdate(t *testing.T) {
	h, _ := changelogHarness(t, map[string]string{sourceLabel: demoSourceURL}, nil)
	var res control.UpdatesResult
	if err := h.call(control.UpdatesListCommand, control.UpdatesArgs{Refresh: true, Changelog: true}, &res); err != nil {
		t.Fatal(err)
	}
	if got := stackNamed(res, "plain-stack"); len(got.Items) != 1 || got.Items[0].Status != "current" || len(got.Changelogs) != 0 {
		t.Fatalf("plain-stack = %+v", got)
	}
	if got := stackNamed(res, "demo-stack"); len(got.Changelogs) != 1 {
		t.Fatalf("demo-stack = %+v", got)
	}
}

func TestChangelogRuleBeatsLabel(t *testing.T) {
	files := map[string]string{servicetest.Settings("demo-stack"): "version: 1\nupdate:\n  mode: automatic\n  changelog:\n    " + demoDetectedVar + ": " + demoSourceURL + ".git\n"}
	h, gh := changelogHarness(t, map[string]string{sourceLabel: "https://github.com/other-org/other-app"}, files)
	c := refreshWithChangelog(t, h).Changelogs[0]
	if c.Source != demoRepo || releaseVersions(c) != "1.3.0,1.2.4" {
		t.Fatalf("changelog = %+v", c)
	}
	if paths, _ := gh.requests(); len(paths) != 1 || paths[0] != "/repos/"+demoRepo+"/releases" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestChangelogMissingLabelGivesNoSourceOrLink(t *testing.T) {
	h, gh := changelogHarness(t, nil, nil)
	c := refreshWithChangelog(t, h).Changelogs[0]
	if c.Source != "" || c.Link != "" || len(c.Releases) != 0 || c.Reason != string(changelog.ReasonNoRepository) {
		t.Fatalf("changelog = %+v", c)
	}
	if paths, _ := gh.requests(); len(paths) != 0 {
		t.Fatalf("GitHub was asked: %v", paths)
	}

	h, _ = changelogHarness(t, map[string]string{sourceLabel: "https://git.example.test/team/app?token=abc#frag"}, nil)
	c = refreshWithChangelog(t, h).Changelogs[0]
	if c.Source != "" || c.Link != "https://git.example.test/team/app" || len(c.Releases) != 0 || c.Reason == "" {
		t.Fatalf("other host = %+v", c)
	}
}

// manifestBreaker fails the label lookup of the registry client but lets the tag listing through.
type manifestBreaker struct {
	answer func(*http.Request) (*http.Response, error)
}

func (m manifestBreaker) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.Contains(r.URL.Path, "/manifests/") {
		return m.answer(r)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func TestChangelogLabelFailureFallsBack(t *testing.T) {
	answers := map[string]func(*http.Request) (*http.Response, error){
		"refused redirect": func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://blobs.example.test/x"}},
				Body: http.NoBody, Request: r}, nil
		},
		"transport error": func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("connection reset") },
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": demoTags}, Labels: map[string]string{sourceLabel: demoSourceURL}})
			gh := newFakeGitHub(t)
			h := boundWith(t, nil, nil, withGitHub(gh), func(c *service.Config) {
				c.Registry = registry.Options{BaseURL: func(string) string { return srv.URL }, MinInterval: -1, Transport: manifestBreaker{answer}}
			})
			s := refreshWithChangelog(t, h)
			if len(s.Items) != 1 || s.Items[0].Status != "update" || len(s.Changelogs) != 1 {
				t.Fatalf("stack = %+v", s)
			}
			if c := s.Changelogs[0]; c.Source != "" || len(c.Releases) != 0 || c.Reason != string(changelog.ReasonNoRepository) {
				t.Fatalf("changelog = %+v", c)
			}
			if paths, _ := gh.requests(); len(paths) != 0 {
				t.Fatalf("GitHub was asked: %v", paths)
			}
		})
	}
}

func TestChangelogRateLimitGivesLink(t *testing.T) {
	h, gh := changelogHarness(t, map[string]string{sourceLabel: demoSourceURL}, nil)
	gh.setStatus(http.StatusTooManyRequests)
	c := refreshWithChangelog(t, h).Changelogs[0]
	if len(c.Releases) != 0 || c.Reason != string(changelog.ReasonRateLimited) || c.Link != demoSourceURL+"/releases" || c.Source != demoRepo {
		t.Fatalf("changelog = %+v", c)
	}
}

func TestChangelogTokenOnlyToGitHubAndNeverInOutput(t *testing.T) {
	h, gh := changelogHarness(t, map[string]string{sourceLabel: demoSourceURL}, nil)
	if err := h.call(control.CredentialSetCommand, control.CredentialSetArgs{Name: "github.com", Value: fakeGitHubToken}, nil); err != nil {
		t.Fatal(err)
	}
	s := refreshWithChangelog(t, h)
	var one control.ChangelogResult
	if err := h.call(control.ChangelogShowCommand, control.ChangelogArgs{Stack: "demo-stack"}, &one); err != nil {
		t.Fatal(err)
	}
	if _, auths := gh.requests(); len(auths) == 0 || auths[0] != "Bearer "+fakeGitHubToken {
		t.Fatalf("authorization = %v", auths)
	}
	for _, v := range []any{s, one} {
		raw, _ := json.Marshal(v)
		if strings.Contains(string(raw), fakeGitHubToken) {
			t.Fatalf("token in result: %s", raw)
		}
	}
	filepath.WalkDir(filepath.Join(h.state, "changelog-cache"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if data, _ := os.ReadFile(p); strings.Contains(string(data), fakeGitHubToken) {
				t.Errorf("token in cache file %s", p)
			}
		}
		return nil
	})
}

func TestChangelogAnonymousWithoutCredential(t *testing.T) {
	h, gh := changelogHarness(t, map[string]string{sourceLabel: demoSourceURL}, nil)
	refreshWithChangelog(t, h)
	if _, auths := gh.requests(); len(auths) != 1 || auths[0] != "" {
		t.Fatalf("authorization = %v", auths)
	}
}

func TestChangelogNeverCheckedUnknownAndBadRequests(t *testing.T) {
	h, gh := changelogHarness(t, map[string]string{sourceLabel: demoSourceURL}, nil)
	var one control.ChangelogResult
	if err := h.call(control.ChangelogShowCommand, control.ChangelogArgs{Stack: "demo-stack"}, &one); err != nil {
		t.Fatal(err)
	}
	if !one.Checked.IsZero() || len(one.Items) != 0 {
		t.Fatalf("never checked: %+v", one)
	}
	var res control.UpdatesResult
	if err := h.call(control.UpdatesListCommand, control.UpdatesArgs{Changelog: true}, &res); err != nil {
		t.Fatal(err)
	}
	if s := stackNamed(res, "demo-stack"); !s.Checked.IsZero() || len(s.Changelogs) != 0 {
		t.Fatalf("never checked list: %+v", s)
	}
	if paths, _ := gh.requests(); len(paths) != 0 {
		t.Fatalf("GitHub was asked: %v", paths)
	}
	wantCode(t, h.call(control.ChangelogShowCommand, control.ChangelogArgs{Stack: "nope"}, nil), control.CodeNoSuchStack)
	wantCode(t, h.call(control.ChangelogShowCommand, control.ChangelogArgs{Stack: "broken-stack"}, nil), control.CodeUnreadable)
	wantCode(t, h.call(control.ChangelogShowCommand, control.ChangelogArgs{}, nil), control.CodeBadRequest)
	wantCode(t, h.call(control.ChangelogShowCommand, map[string]any{"stack": "demo-stack", "bogus": 1}, nil), control.CodeBadRequest)
}

func TestChangelogCacheCleanedAtStart(t *testing.T) {
	var stale, fresh, foreign string
	h, _ := changelogHarness(t, nil, nil, func(c *service.Config) {
		dir := filepath.Join(c.StateDir, "changelog-cache")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		stale, fresh, foreign = filepath.Join(dir, "old-org_old-app.json"), filepath.Join(dir, "new-org_new-app.json"), filepath.Join(dir, "notes.txt")
		for _, p := range []string{stale, fresh, foreign} {
			if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		old := time.Now().Add(-72 * time.Hour)
		for _, p := range []string{stale, foreign} {
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
	})
	_ = h
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale cache file stays: %v", err)
	}
	for _, p := range []string{fresh, foreign} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed: %v", p, err)
		}
	}
}

func TestChangelogDoesNotTouchStackDeployState(t *testing.T) {
	h, _ := changelogHarness(t, map[string]string{sourceLabel: demoSourceURL}, nil)
	refreshWithChangelog(t, h)
	var runs control.RunsResult
	if err := h.call(control.RunsCommand, control.RunsArgs{}, &runs); err != nil || len(runs.Runs) != 0 {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
}
