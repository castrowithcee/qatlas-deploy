package service

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/configfile"

	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
)

const exampleURL = "ssh://git.example.test/stacks.git"

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestSourceIDFollowsThePath(t *testing.T) {
	for path, id := range map[string]string{"/opt/stacks": "opt-stacks", "/srv/services/stacks": "srv-services-stacks"} {
		if got := sourceID(path); got != id {
			t.Errorf("sourceID(%s) = %s, want %s", path, got, id)
		}
	}
}

// A start that ended after the sources file was written resumes the migration at the next start.
func TestMigrationResumesAndRepeats(t *testing.T) {
	state := t.TempDir()
	store, err := configfile.NewStore(filepath.Join(state, sourcesFile), sourcesCodec)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(state, legacyFile), `{"version":1,"url":"`+exampleURL+`","dir":"/srv/stacks"}`)
	put(t, filepath.Join(state, "deploy", "state.json"), `{"schema":1}`)
	conf := sourcesConfig{Version: sourcesVersion, Sources: []sourceConfig{{
		ID: "srv-stacks", Path: "/srv/stacks", Kind: kindRemote, URL: exampleURL, Key: credentials.DefaultKey,
	}}}
	if _, err := store.SaveIfUnchanged(conf, configfile.RevisionAbsent); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	for range 2 {
		if err := migrateLegacy(state, store, log); err != nil {
			t.Fatal(err)
		}
		if exists(filepath.Join(state, legacyFile)) || exists(filepath.Join(state, "deploy")) ||
			!exists(filepath.Join(state, "sources", "srv-stacks", "deploy", "state.json")) {
			t.Fatal("the migration did not finish")
		}
	}
	got, _, err := store.LoadVersioned()
	if err != nil || len(got.Sources) != 1 || got.Sources[0] != conf.Sources[0] {
		t.Fatalf("sources = %+v, %v", got, err)
	}
}

// State that would have to replace other state is never moved, and the old binding stays until it can be.
func TestMigrationLeavesConflictingStateInPlace(t *testing.T) {
	state := t.TempDir()
	store, err := configfile.NewStore(filepath.Join(state, sourcesFile), sourcesCodec)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(state, legacyFile), `{"version":1,"url":"`+exampleURL+`","dir":"/srv/stacks"}`)
	put(t, filepath.Join(state, "deploy", "state.json"), `{"schema":1,"note":"old"}`)
	put(t, filepath.Join(state, "sources", "srv-stacks", "deploy", "state.json"), `{"schema":1,"note":"new"}`)
	if err := migrateLegacy(state, store, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	old, _ := os.ReadFile(filepath.Join(state, "deploy", "state.json"))
	moved, _ := os.ReadFile(filepath.Join(state, "sources", "srv-stacks", "deploy", "state.json"))
	if !strings.Contains(string(old), "old") || !strings.Contains(string(moved), "new") || !exists(filepath.Join(state, legacyFile)) {
		t.Fatal("conflicting state was touched")
	}
}

// twoSources prepares a service state with two sources whose working copies both have a stack "web".
func twoSources(t *testing.T, states map[string]string) *sourceSet {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "state")
	keys, err := credentials.Open(state, nil)
	if err != nil {
		t.Fatal(err)
	}
	conf := sourcesConfig{Version: sourcesVersion}
	for i, name := range []string{"first", "second"} {
		dir := filepath.Join(root, name)
		put(t, filepath.Join(dir, "web", "compose.yaml"), "services:\n  app:\n    image: registry.example.test/demo/web:1.0.0\n")
		key := credentials.DefaultKey
		if i > 0 {
			key = credentials.SourceKey(sourceID(dir))
		}
		conf.Sources = append(conf.Sources, sourceConfig{ID: sourceID(dir), Path: dir, Kind: kindRemote, URL: exampleURL, Key: key})
		if body, ok := states[name]; ok {
			put(t, filepath.Join(state, sourcesDir, sourceID(dir), "deploy", "state.json"), body)
		}
	}
	store, err := configfile.NewStore(filepath.Join(state, sourcesFile), sourcesCodec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveIfUnchanged(conf, configfile.RevisionAbsent); err != nil {
		t.Fatal(err)
	}
	set, err := openSources(state, keys, nil, engine.Config{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestNameGoesToTheDeployedStackElseToTheFirstSource(t *testing.T) {
	deployed := `{"schema":1,"stacks":{"web":{"deployed":"0123456789abcdef0123456789abcdef01234567"}}}`
	for _, c := range []struct {
		name   string
		states map[string]string
		owner  int
	}{
		{"none deployed", nil, 0},
		{"second deployed", map[string]string{"second": deployed}, 1},
		{"both deployed", map[string]string{"first": deployed, "second": deployed}, 0},
	} {
		set := twoSources(t, c.states)
		srcs := set.sources()
		r := set.resolve()
		owner, other := srcs[c.owner], srcs[1-c.owner]
		if r.owner["web"] != owner || len(r.owned) != 1 || r.owned[0].src != owner {
			t.Errorf("%s: web is owned by %v", c.name, r.owner["web"].conf.Path)
		}
		if _, ok := r.blocked[other]["web"]; !ok || len(r.blocked[owner]) != 0 {
			t.Errorf("%s: blocked = %v", c.name, r.blocked)
		}
		// The decision rests on the stored state alone, so it holds after a restart.
		reopened, err := openSources(set.stateDir, set.keys, nil, engine.Config{}, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		if again := reopened.resolve(); again.owner["web"].conf.ID != owner.conf.ID {
			t.Errorf("%s: the owner changed between two starts", c.name)
		}
	}
}

// A source whose working copy cannot be read keeps the names its state deploys.
func TestUnreadableSourceKeepsItsDeployedNames(t *testing.T) {
	set := twoSources(t, map[string]string{
		"first": `{"schema":1,"stacks":{"web":{"deployed":"0123456789abcdef0123456789abcdef01234567"}}}`,
	})
	srcs := set.sources()
	if err := os.RemoveAll(srcs[0].conf.Path); err != nil {
		t.Fatal(err)
	}
	r := set.resolve()
	if r.owner["web"] != srcs[0] || len(r.owned) != 0 {
		t.Fatalf("web is owned by %s", r.owner["web"].conf.Path)
	}
	if _, ok := r.blocked[srcs[1]]["web"]; !ok {
		t.Fatalf("blocked = %v", r.blocked)
	}
}
