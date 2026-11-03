package service_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/registry"
	"github.com/castrowithcee/qatlas-deploy/internal/registry/registrytest"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
)

func withRegistry(url string) func(*service.Config) {
	return func(c *service.Config) {
		c.Registry = registry.Options{BaseURL: func(string) string { return url }, MinInterval: -1}
	}
}

func updatesHarness(t *testing.T) (*harness, *registrytest.Server) {
	t.Helper()
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": {"1.2.3", "1.2.4", "1.3.0", "2.0.0", "2.1.0-rc.1"}}})
	return boundWith(t, nil, nil, withRegistry(srv.URL)), srv
}

func tagListCalls(srv *registrytest.Server) int {
	n := 0
	for _, r := range srv.Requests() {
		if strings.HasSuffix(r, "/tags/list") {
			n++
		}
	}
	return n
}

func stackNamed(res control.UpdatesResult, name string) control.StackUpdates {
	for _, s := range res.Stacks {
		if s.Name == name {
			return s
		}
	}
	return control.StackUpdates{}
}

func TestUpdatesNeverCheckedThenRefreshThenStored(t *testing.T) {
	h, srv := updatesHarness(t)
	var res control.UpdatesResult
	if err := h.call(control.UpdatesListCommand, control.UpdatesArgs{}, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Stacks) != 3 || tagListCalls(srv) != 0 {
		t.Fatalf("stacks = %+v, tag lists %d", res.Stacks, tagListCalls(srv))
	}
	for _, name := range []string{"demo-stack", "plain-stack"} {
		if s := stackNamed(res, name); !s.Checked.IsZero() || len(s.Items) != 0 || s.Error != "" {
			t.Fatalf("%s should be never checked: %+v", name, s)
		}
	}
	if s := stackNamed(res, "broken-stack"); s.Error == "" {
		t.Fatalf("broken-stack shows no problem: %+v", s)
	}

	var fresh control.UpdatesResult
	if err := h.call(control.UpdatesListCommand, control.UpdatesArgs{Refresh: true}, &fresh); err != nil {
		t.Fatal(err)
	}
	demo := stackNamed(fresh, "demo-stack")
	if demo.Checked.IsZero() || len(demo.Items) != 1 || demo.Update != "automatic" {
		t.Fatalf("demo-stack = %+v", demo)
	}
	if it := demo.Items[0]; it.Variable != "DEMO_APP_VERSION" || it.Current != "1.2.3" || it.Selected != "1.3.0" || it.MajorAvailable != "2.0.0" || it.Status != "update" {
		t.Fatalf("demo-stack item = %+v", it)
	}
	if it := stackNamed(fresh, "plain-stack").Items[0]; it.Current != "0.9.1" || it.Status != "current" {
		t.Fatalf("plain-stack (no rules) = %+v", it)
	}
	if s := stackNamed(fresh, "broken-stack"); s.Error == "" || !s.Checked.IsZero() {
		t.Fatalf("broken-stack = %+v", s)
	}
	if n := tagListCalls(srv); n != 1 {
		t.Fatalf("tag lists = %d, want one for the shared repository", n)
	}

	var stored control.UpdatesResult
	if err := h.call(control.UpdatesListCommand, control.UpdatesArgs{}, &stored); err != nil {
		t.Fatal(err)
	}
	if got := stackNamed(stored, "demo-stack"); !got.Checked.Equal(demo.Checked) || len(got.Items) != 1 || got.Items[0] != demo.Items[0] {
		t.Fatalf("stored = %+v, fresh = %+v", got, demo)
	}
	if n := tagListCalls(srv); n != 1 {
		t.Fatalf("a read without refresh asked the registry: %d", n)
	}
}

func TestUpdatesSingleStackAndRefusals(t *testing.T) {
	h, srv := updatesHarness(t)
	var res control.UpdatesResult
	if err := h.call(control.UpdatesListCommand, control.UpdatesArgs{Stack: "plain-stack", Refresh: true}, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Stacks) != 1 || res.Stacks[0].Name != "plain-stack" || res.Stacks[0].Checked.IsZero() {
		t.Fatalf("result = %+v", res)
	}
	var all control.UpdatesResult
	if err := h.call(control.UpdatesListCommand, control.UpdatesArgs{}, &all); err != nil {
		t.Fatal(err)
	}
	if !stackNamed(all, "demo-stack").Checked.IsZero() || stackNamed(all, "plain-stack").Checked.IsZero() {
		t.Fatalf("only the named stack is checked: %+v", all)
	}
	wantCode(t, h.call(control.UpdatesListCommand, control.UpdatesArgs{Stack: "nope", Refresh: true}, nil), control.CodeNoSuchStack)
	wantCode(t, h.call(control.UpdatesListCommand, control.UpdatesArgs{Stack: "broken-stack"}, nil), control.CodeUnreadable)
	wantCode(t, h.call(control.UpdatesListCommand, map[string]any{"bogus": 1}, nil), control.CodeBadRequest)
	if n := tagListCalls(srv); n != 1 {
		t.Fatalf("tag lists = %d", n)
	}
}

func TestUpdatesWithoutRepoAndErrorsStayFixed(t *testing.T) {
	h := startWith(t, nil, withRegistry("http://127.0.0.1:1"))
	wantCode(t, h.call(control.UpdatesListCommand, control.UpdatesArgs{}, nil), control.CodeNoRepo)
	h.withKey()
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	var res control.UpdatesResult
	if err := h.call(control.UpdatesListCommand, control.UpdatesArgs{Stack: "demo-stack", Refresh: true}, &res); err != nil {
		t.Fatal(err)
	}
	if it := res.Stacks[0].Items[0]; it.Status != "error" || it.Error != "the registry could not be reached" || it.Selected != "" {
		t.Fatalf("unreachable registry: %+v", it)
	}
}

func TestUpdateResultSurvivesInStateFile(t *testing.T) {
	h, _ := updatesHarness(t)
	if err := h.call(control.UpdatesListCommand, control.UpdatesArgs{Refresh: true}, nil); err != nil {
		t.Fatal(err)
	}
	var found []string
	_ = filepath.WalkDir(h.state, func(p string, d os.DirEntry, _ error) error {
		if d != nil && d.Name() == "state.json" {
			raw, _ := os.ReadFile(p)
			if strings.Contains(string(raw), `"updates"`) && strings.Contains(string(raw), "1.3.0") {
				found = append(found, p)
			}
		}
		return nil
	})
	if len(found) != 1 || !slices.ContainsFunc(found, func(p string) bool { return strings.Contains(p, "deploy") }) {
		t.Fatalf("state files with the result: %v", found)
	}
}

func TestRegistryCredentialName(t *testing.T) {
	cases := []struct {
		host, want string
		ok         bool
	}{
		{"registry.example.test", "registry.registry.example.test", true},
		{"registry.example.test:5000", "registry.registry.example.test-5000", true},
		{"Registry.Example.TEST", "registry.registry.example.test", true},
		{"registry-1.docker.io", "registry.docker.io", true},
		{"bad host/x", "registry.bad host/x", false},
	}
	for _, c := range cases {
		got, ok := service.RegistryCredentialName(c.host)
		if got != c.want || ok != c.ok {
			t.Fatalf("name(%q) = %q, %v; want %q, %v", c.host, got, ok, c.want, c.ok)
		}
	}
}

func TestParseRegistryLogin(t *testing.T) {
	cred, err := service.ParseRegistryLogin("fake-user-1:fake-token:42")
	if err != nil || cred.User != "fake-user-1" || cred.Token != "fake-token:42" {
		t.Fatalf("valid login: err = %v", err)
	}
	for _, bad := range []string{"fake-user-only", ":fake-token-42", "fake-user-1:"} {
		if _, err := service.ParseRegistryLogin(bad); err == nil || strings.Contains(err.Error(), "fake") {
			t.Fatalf("login %d bytes: err = %v", len(bad), err)
		}
	}
}

const (
	fakeRegistryUser  = "fake-user-1"
	fakeRegistryToken = "fake-token-42"
	noCredentialText  = "no credential could be determined for the registry"
	authText          = "the registry requires a login or did not accept the credential"
)

func TestUpdatesRegistryLogin(t *testing.T) {
	const credName = "registry.registry.example.test"
	cases := []struct {
		name  string
		auth  registrytest.Auth
		value string // empty: no credential
		want  string
	}{
		{"basic with credential", registrytest.AuthBasic, fakeRegistryUser + ":" + fakeRegistryToken, ""},
		{"bearer with credential", registrytest.AuthBearer, fakeRegistryUser + ":" + fakeRegistryToken, ""},
		{"basic without credential", registrytest.AuthBasic, "", authText},
		{"malformed credential", registrytest.AuthBasic, "fake-no-separator", noCredentialText},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := registrytest.New(t, registrytest.Config{
				Auth: c.auth, User: fakeRegistryUser, Password: fakeRegistryToken,
				Repos: map[string][]string{"demo/app": {"1.2.3", "1.3.0"}},
			})
			h := boundWith(t, nil, nil, withRegistry(srv.URL))
			if c.value != "" {
				if err := h.call(control.CredentialSetCommand, control.CredentialSetArgs{Name: credName, Value: c.value}, nil); err != nil {
					t.Fatal(err)
				}
			}
			var res control.UpdatesResult
			if err := h.call(control.UpdatesListCommand, control.UpdatesArgs{Stack: "demo-stack", Refresh: true}, &res); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(res)
			if strings.Contains(string(raw), fakeRegistryToken) || strings.Contains(string(raw), "fake-no-separator") {
				t.Fatal("a credential value reached the result")
			}
			items := stackNamed(res, "demo-stack").Items
			if len(items) != 1 {
				t.Fatalf("items = %+v", items)
			}
			if it := items[0]; c.want == "" && (it.Error != "" || it.Selected != "1.3.0") || c.want != "" && it.Error != c.want {
				t.Fatalf("item = %+v", it)
			}
		})
	}
}
