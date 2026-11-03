package service_test

import (
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
