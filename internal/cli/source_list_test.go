package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

func listHandlers() fakeHandlers {
	polled := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	return fakeHandlers{control.SourceListCommand: func(json.RawMessage) any {
		return control.SourceListResult{Sources: []control.SourceStatus{
			{ID: "srv-remote", Path: "/srv/remote", Kind: control.SourceKindRemote, URL: "ssh://git.example.test/stacks.git",
				LastPoll: polled, Head: "abc1234", PollError: "fetch: connection timed out"},
			{ID: "srv-local", Path: "/srv/local", Kind: control.SourceKindLocal, Head: "def5678", Note: "local changes"},
		}}
	}}
}

func TestSourceListText(t *testing.T) {
	startFakeService(t, listHandlers())
	code, stdout, stderr := run(t, "source", "list", "--output", "compact")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	for _, want := range []string{
		"source.srv-remote.path", "/srv/remote", "source.srv-remote.kind", "remote",
		"source.srv-remote.url", "ssh://git.example.test/stacks.git", "source.srv-remote.last_poll", "2026-03-04T05:06:07Z",
		"source.srv-remote.poll_error", "connection timed out", "source.srv-local.kind", "local", "source.srv-local.head", "def5678",
		"source.srv-local.note",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "source.srv-local.url") {
		t.Errorf("a local source shows an address:\n%s", stdout)
	}
}

func TestSourceListJSON(t *testing.T) {
	startFakeService(t, listHandlers())
	code, stdout, stderr := run(t, "source", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	var got struct {
		Sources []map[string]any `json:"sources"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil || len(got.Sources) != 2 {
		t.Fatalf("output %q: %v", stdout, err)
	}
	if got.Sources[0]["id"] != "srv-remote" || got.Sources[0]["url"] != "ssh://git.example.test/stacks.git" || got.Sources[0]["kind"] != "remote" {
		t.Fatalf("remote source = %v", got.Sources[0])
	}
	if got.Sources[1]["path"] != "/srv/local" || got.Sources[1]["kind"] != "local" || got.Sources[1]["url"] != nil {
		t.Fatalf("local source = %v", got.Sources[1])
	}
}

func TestSourceListWithoutSourcesShowsNone(t *testing.T) {
	startFakeService(t, fakeHandlers{control.SourceListCommand: func(json.RawMessage) any { return control.SourceListResult{} }})
	code, stdout, _ := run(t, "source", "list", "--output", "json")
	var got struct {
		Sources []map[string]any `json:"sources"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); code != 0 || err != nil || len(got.Sources) != 0 {
		t.Fatalf("code %d, stdout %q: %v", code, stdout, err)
	}
}

func removeHandlers(refusal *control.Error) fakeHandlers {
	return fakeHandlers{control.SourceRemoveCommand: func(raw json.RawMessage) any {
		if refusal != nil {
			return refusal
		}
		var a control.SourceRemoveArgs
		_ = json.Unmarshal(raw, &a)
		return control.SourceRemoveResult{ID: "srv-local", Path: a.Path}
	}}
}

func TestSourceRemoveWithoutConfirmationCallsNothing(t *testing.T) {
	svc := startFakeService(t, removeHandlers(nil))
	useStdin(t, strings.NewReader(""))
	code, stdout, stderr := run(t, "source", "remove", "/srv/local")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "confirmation-required") || !strings.Contains(stderr, "--yes") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	for _, want := range []string{"/srv/local", "No file in the folder is deleted", "no container is stopped", "deploy key stay"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("preview lacks %q:\n%s", want, stderr)
		}
	}
	if svc.count(control.SourceRemoveCommand) != 0 {
		t.Fatal("an unconfirmed source remove reached the service")
	}
}

func TestSourceRemoveWithYesCallsTheService(t *testing.T) {
	svc := startFakeService(t, removeHandlers(nil))
	useStdin(t, strings.NewReader(""))
	code, stdout, stderr := run(t, "source", "remove", "/srv/local", "--yes", "--output", "json")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil || got["id"] != "srv-local" || got["path"] != "/srv/local" {
		t.Fatalf("stdout %q: %v", stdout, err)
	}
	if svc.count(control.SourceRemoveCommand) != 1 {
		t.Fatalf("source.remove calls = %d", svc.count(control.SourceRemoveCommand))
	}
}

func TestSourceRemoveReportsTheServiceRefusal(t *testing.T) {
	for name, tc := range map[string]struct {
		refusal *control.Error
		want    string
	}{
		"unknown folder": {control.NewError(control.CodeNotFound, "no source is bound to this folder"), "source list"},
		"active run":     {control.NewError(control.CodeRunActive, "a run is active"), "stack runs"},
	} {
		t.Run(name, func(t *testing.T) {
			startFakeService(t, removeHandlers(tc.refusal))
			code, _, stderr := run(t, "source", "remove", "/srv/local", "--yes")
			if code == 0 || !strings.Contains(stderr, tc.refusal.Message) || !strings.Contains(stderr, tc.want) {
				t.Fatalf("code %d, stderr %q", code, stderr)
			}
		})
	}
}

func TestSourceRemoveNeedsOneCleanAbsolutePath(t *testing.T) {
	for _, args := range [][]string{{"source", "remove"}, {"source", "remove", "srv/local", "--yes"}, {"source", "remove", "/srv/../local", "--yes"}} {
		if code, _, _ := run(t, args...); code != 2 {
			t.Errorf("%v: code %d", args, code)
		}
	}
}
