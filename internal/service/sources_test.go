package service_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
)

const webCompose = "services:\n  app:\n    image: registry.example.test/demo/web:${WEB_VERSION}\n"

// moreStacks is the working copy of a second repository: a stack of its own and, with clash, one whose name
// the first repository already uses.
func moreStacks(clash bool) map[string]string {
	files := map[string]string{"web-stack/compose.yaml": webCompose, "web-stack/.env": "WEB_VERSION=3.0.0\n"}
	if clash {
		files["demo-stack/compose.yaml"] = webCompose
		files["demo-stack/.env"] = "WEB_VERSION=3.0.0\n"
	}
	return files
}

// addSource binds a further stack folder with its own deploy key and a working copy of files.
func (h *harness) addSource(files map[string]string) string {
	h.t.Helper()
	dir := filepath.Join(h.t.TempDir(), "more-stacks")
	if err := os.Mkdir(dir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := h.call(control.KeyGenerateCommand, control.KeyGenerateArgs{Dir: dir}, nil); err != nil {
		h.t.Fatal(err)
	}
	h.git.SetFiles(files)
	args := control.RepoSetArgs{URL: strings.Replace(h.url, "/stacks.git", "/more-stacks.git", 1), Dir: dir, Fingerprint: h.srv.Key().Fingerprint}
	if err := h.call(control.RepoSetCommand, args, &control.RepoSetResult{}); err != nil {
		h.t.Fatal(err)
	}
	return dir
}

// waitStatus polls the status until ok accepts it.
func (h *harness) waitStatus(what string, ok func(control.StatusResult) bool) control.StatusResult {
	h.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		var st control.StatusResult
		if err := h.call(control.StatusCommand, nil, &st); err != nil {
			h.t.Fatal(err)
		}
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("%s: %+v", what, st.Engine)
		}
	}
}

func stacksNamed(st control.StatusResult, name string) []control.StackStatus {
	var out []control.StackStatus
	for _, s := range st.Engine.Stacks {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

func readState(t *testing.T, state, dir string) string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(state, "sources", sourceID(dir), "deploy", "state.json"))
	return string(data)
}

func TestFurtherSourceHasItsOwnKeyStateAndFetch(t *testing.T) {
	h := start(t)
	h.withKey()
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	// A further folder never borrows the key of the first one.
	other := filepath.Join(t.TempDir(), "unkeyed")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	err := h.call(control.RepoSetCommand, control.RepoSetArgs{URL: h.url, Dir: other, Fingerprint: h.srv.Key().Fingerprint}, nil)
	wantCode(t, err, control.CodeNotFound)
	if !strings.Contains(err.Error(), "key generate --dir "+other) || h.git.Clones() != 1 {
		t.Fatalf("a further folder without its key: %v, %d clones", err, h.git.Clones())
	}

	dir := h.addSource(moreStacks(false))
	keyFile := filepath.Join(h.state, credentials.SourceKey(sourceID(dir)))
	if info, err := os.Stat(keyFile); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key of the further folder: %v, %v", info, err)
	}
	var first, second control.KeyResult
	if err := h.call(control.KeyShowCommand, nil, &first); err != nil {
		t.Fatal(err)
	}
	if err := h.call(control.KeyShowCommand, control.KeyShowArgs{Dir: dir}, &second); err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("both folders show the same deploy key")
	}

	h.waitStatus("both sources polled", func(st control.StatusResult) bool {
		return len(stacksNamed(st, "web-stack")) == 1 && len(stacksNamed(st, "demo-stack")) == 1
	})
	if got := h.git.ConfigFor(h.dir).KeyFile; got != filepath.Join(h.state, "deploy-key") {
		t.Fatalf("first folder uses key %s", got)
	}
	if got := h.git.ConfigFor(dir).KeyFile; got != keyFile {
		t.Fatalf("further folder uses key %s", got)
	}
	if h.git.Fetches(h.dir) == 0 || h.git.Fetches(dir) == 0 {
		t.Fatalf("fetches: %d and %d", h.git.Fetches(h.dir), h.git.Fetches(dir))
	}
	if a, b := readState(t, h.state, h.dir), readState(t, h.state, dir); !strings.Contains(a, "demo-stack") ||
		strings.Contains(a, "web-stack") || !strings.Contains(b, "web-stack") || strings.Contains(b, "demo-stack") {
		t.Fatalf("states are not apart:\n%s\n%s", a, b)
	}

	var list control.StacksResult
	if err := h.call(control.StacksListCommand, nil, &list); err != nil || len(list.Stacks) != 4 {
		t.Fatalf("stacks: %+v, %v", list, err)
	}
	if dirs := service.BoundDirs(h.state); len(dirs) != 2 || dirs[0] != h.dir || dirs[1] != dir {
		t.Fatalf("BoundDirs = %v", dirs)
	}
	// Binding the folder again changes nothing; another URL for it is refused.
	args := control.RepoSetArgs{URL: h.url, Dir: dir, Fingerprint: h.srv.Key().Fingerprint}
	wantCode(t, h.call(control.RepoSetCommand, args, nil), control.CodeRepoBound)
}

func TestSetRefusesAFolderInsideAnother(t *testing.T) {
	h := start(t)
	h.withKey()
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(h.dir, "demo-stack")
	if err := h.call(control.KeyGenerateCommand, control.KeyGenerateArgs{Dir: inner}, nil); err != nil {
		t.Fatal(err)
	}
	args := control.RepoSetArgs{URL: h.url, Dir: inner, Fingerprint: h.srv.Key().Fingerprint}
	err := h.call(control.RepoSetCommand, args, nil)
	wantCode(t, err, control.CodeInvalid)
	if !strings.Contains(err.Error(), "lie inside the stack folder "+h.dir) {
		t.Fatalf("message %q", err)
	}
}

func TestFetchErrorOfOneSourceLeavesTheOther(t *testing.T) {
	d := &fakeDeployer{}
	h := startWith(t, d)
	h.withKey()
	h.git.SetFetchErrFor(h.dir, errors.New("fetch failed: connection timed out"))
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	dir := h.addSource(moreStacks(false))
	st := h.waitStatus("the healthy source deploys", func(st control.StatusResult) bool {
		web := stacksNamed(st, "web-stack")
		return len(web) == 1 && web[0].Deployed != "" && st.Engine.PollError != ""
	})
	if !strings.Contains(st.Engine.PollError, "connection timed out") || len(stacksNamed(st, "demo-stack")) != 0 {
		t.Fatalf("status of the failing source: %+v", st.Engine)
	}

	var res control.RepoFetchResult
	if err := h.call(control.RepoFetchCommand, nil, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Sources) != 2 || res.Sources[0].Error == "" || res.Sources[1].Error != "" ||
		res.Sources[1].Dir != dir || res.Sources[1].Head != h.git.Head {
		t.Fatalf("fetch = %+v", res)
	}
	h.git.SetFetchErrFor(dir, errors.New("fetch failed: connection timed out"))
	wantCode(t, h.call(control.RepoFetchCommand, nil, nil), control.CodeRemote)
}

func TestNameCollisionKeepsTheDeployedStack(t *testing.T) {
	d := &fakeDeployer{}
	h := bound(t, d)
	h.addSource(moreStacks(true))
	st := h.waitStatus("the further source settles", func(st control.StatusResult) bool {
		web := stacksNamed(st, "web-stack")
		return len(web) == 1 && web[0].Deployed != "" && len(stacksNamed(st, "demo-stack")) == 2
	})
	demo := stacksNamed(st, "demo-stack")
	owner, blocked := demo[0], demo[1]
	if owner.Deployed == "" || len(owner.Findings) != 0 {
		t.Fatalf("the deployed stack lost its name: %+v", owner)
	}
	if blocked.Deployed != "" || blocked.Pending != "" || len(blocked.Findings) != 1 ||
		blocked.Findings[0].Kind != "name-collision" || !strings.Contains(blocked.Findings[0].Detail, h.dir) {
		t.Fatalf("the second stack of the name: %+v", blocked)
	}
	if calls := d.callsOf("demo-stack"); len(calls) != 1 {
		t.Fatalf("demo-stack runs: %v", calls)
	}

	// A command on the name reaches the stack that owns it.
	var res control.DeployResult
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "demo-stack"}, &res); err != nil {
		t.Fatal(err)
	}
	h.waitStatus("the manual run ends", func(st control.StatusResult) bool {
		demo := stacksNamed(st, "demo-stack")
		return len(demo) == 2 && demo[0].ActiveRun == "" && demo[0].LastRun == "succeeded"
	})
	if b := stacksNamed(h.waitStatus("status", func(control.StatusResult) bool { return true }), "demo-stack")[1]; b.Deployed != "" {
		t.Fatalf("the manual run deployed the blocked stack: %+v", b)
	}
	var runs control.RunsResult
	if err := h.call(control.RunsCommand, control.RunsArgs{Stack: "demo-stack"}, &runs); err != nil || len(runs.Runs) != 2 {
		t.Fatalf("runs: %+v, %v", runs, err)
	}
	var log control.LogResult
	if err := h.call(control.LogCommand, control.LogArgs{ID: res.ID}, &log); err != nil || !strings.Contains(log.Text, "deploy demo-stack") {
		t.Fatalf("log: %q, %v", log.Text, err)
	}
}

// An installation with the single binding of earlier versions keeps its state, key and history.
func TestLegacyBindingBecomesTheFirstSource(t *testing.T) {
	const runID = "20260101T000000Z-0a1b2c3d"
	h := newHarness(t)
	store, err := credentials.Open(h.state, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.GenerateKey(false)
	if err != nil {
		t.Fatal(err)
	}
	for rel, body := range h.git.Files {
		writeFile(t, filepath.Join(h.dir, rel), body)
	}
	h.git.SetOrigin(h.dir, h.url)
	legacy, _ := json.Marshal(map[string]any{"version": 1, "url": h.url, "dir": h.dir})
	head := h.git.Head
	writeFile(t, filepath.Join(h.state, "stacks-repo.json"), string(legacy))
	writeFile(t, filepath.Join(h.state, "deploy", "state.json"), `{"schema":1,"head":"`+head+
		`","stacks":{"demo-stack":{"deployed":"`+head+`"}},"runs":[{"id":"`+runID+`","stack":"demo-stack","commit":"`+head+
		`","trigger":"poll","started":"2026-01-01T00:00:00Z","duration":0,"result":"succeeded"}]}`)
	writeFile(t, filepath.Join(h.state, "deploy", "runs", runID+".log"), "earlier run\n")
	h.run(nil)

	for _, gone := range []string{"stacks-repo.json", "deploy"} {
		if _, err := os.Lstat(filepath.Join(h.state, gone)); err == nil {
			t.Errorf("%s is still there", gone)
		}
	}
	data, err := os.ReadFile(filepath.Join(h.state, "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var conf struct {
		Version int
		Sources []map[string]string
	}
	if err := json.Unmarshal(data, &conf); err != nil || conf.Version != 2 || len(conf.Sources) != 1 {
		t.Fatalf("sources.json = %s, %v", data, err)
	}
	if src := conf.Sources[0]; src["id"] != sourceID(h.dir) || src["path"] != h.dir || src["kind"] != "remote" ||
		src["url"] != h.url || src["key"] != "deploy-key" {
		t.Fatalf("source = %v", src)
	}

	st := h.waitStatus("the migrated source polls", func(st control.StatusResult) bool { return !st.Engine.LastPoll.IsZero() })
	if demo := stacksNamed(st, "demo-stack"); len(demo) != 1 || demo[0].Deployed != h.git.Head {
		t.Fatalf("demo-stack = %+v", demo)
	}
	var log control.LogResult
	if err := h.call(control.LogCommand, control.LogArgs{ID: runID}, &log); err != nil || log.Text != "earlier run\n" {
		t.Fatalf("log: %q, %v", log.Text, err)
	}
	if got := h.git.ConfigFor(h.dir).KeyFile; got != filepath.Join(h.state, "deploy-key") {
		t.Fatalf("the migrated source uses key %s", got)
	}
	var shown control.KeyResult
	if err := h.call(control.KeyShowCommand, control.KeyShowArgs{Dir: h.dir}, &shown); err != nil || shown.Fingerprint != key.Fingerprint {
		t.Fatalf("key of the migrated source: %+v, %v", shown, err)
	}
	res, err := h.set(h.srv.Key().Fingerprint)
	if err != nil || res.Cloned {
		t.Fatalf("binding the migrated folder again: %+v, %v", res, err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
