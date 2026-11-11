package service

import (
	"path/filepath"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
)

func TestValidateSourcesKinds(t *testing.T) {
	remote := sourceConfig{ID: "srv-a", Path: "/srv/a", Kind: kindRemote, URL: exampleURL, Key: credentials.DefaultKey}
	local := func(dir string) sourceConfig { return sourceConfig{ID: sourceID(dir), Path: dir, Kind: kindLocal} }
	for _, c := range []struct {
		name    string
		sources []sourceConfig
		ok      bool
	}{
		{"remote only, as an old file has it", []sourceConfig{remote}, true},
		{"local", []sourceConfig{local("/srv/b")}, true},
		{"two locals share the empty key", []sourceConfig{local("/srv/b"), local("/srv/c")}, true},
		{"remote and local", []sourceConfig{remote, local("/srv/b")}, true},
		{"local with a url", []sourceConfig{{ID: "srv-b", Path: "/srv/b", Kind: kindLocal, URL: exampleURL}}, false},
		{"local with a key", []sourceConfig{{ID: "srv-b", Path: "/srv/b", Kind: kindLocal, Key: credentials.DefaultKey}}, false},
		{"remote without a key", []sourceConfig{{ID: "srv-a", Path: "/srv/a", Kind: kindRemote, URL: exampleURL}}, false},
		{"remote without a url", []sourceConfig{{ID: "srv-a", Path: "/srv/a", Kind: kindRemote, Key: credentials.DefaultKey}}, false},
		{"unknown kind", []sourceConfig{{ID: "srv-a", Path: "/srv/a", Kind: "files"}}, false},
		{"overlapping locals", []sourceConfig{local("/srv/b"), local("/srv/b/c")}, false},
	} {
		err := validateSources(sourcesConfig{Version: sourcesVersion, Sources: c.sources})
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	old := `{"version":2,"sources":[{"id":"srv-a","path":"/srv/a","kind":"remote","url":"` + exampleURL + `","key":"deploy-key"}]}`
	if c, err := sourcesCodec.Decode([]byte(old)); err != nil || validateSources(c) != nil {
		t.Fatalf("an existing sources file is no longer valid: %v", err)
	}
}

func TestGitConfigOfALocalSourceHasNoKeyAndNoHosts(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	keys, err := credentials.Open(state, nil)
	if err != nil {
		t.Fatal(err)
	}
	set := &sourceSet{stateDir: state, keys: keys}
	got := set.gitConfig(sourceConfig{Kind: kindLocal})
	if !got.Local || got.KeyFile != "" || got.KnownHosts != "" {
		t.Fatalf("local config = %+v", got)
	}
	got = set.gitConfig(sourceConfig{Kind: kindRemote, Key: credentials.DefaultKey})
	if got.Local || got.KeyFile == "" || got.KnownHosts == "" {
		t.Fatalf("remote config = %+v", got)
	}
}

func TestKeyForKeepsTheKeyOfAReleasedFolder(t *testing.T) {
	remote := func(dir, key string) sourceConfig {
		return sourceConfig{ID: sourceID(dir), Path: dir, Kind: kindRemote, URL: exampleURL, Key: key}
	}
	own := func(dir string) string { return credentials.SourceKey(sourceID(dir)) }
	has := func(names ...string) func(string) bool {
		return func(n string) bool {
			for _, x := range names {
				if x == n {
					return true
				}
			}
			return false
		}
	}
	// The first folder was released and the second keeps the default key it never had: it gets its own again.
	conf := sourcesConfig{Sources: []sourceConfig{remote("/srv/a", credentials.DefaultKey)}}
	if got := keyFor(conf, "/srv/b", has()); got != own("/srv/b") {
		t.Errorf("a further folder got %s", got)
	}
	conf = sourcesConfig{}
	if got := keyFor(conf, "/srv/a", has(credentials.DefaultKey)); got != credentials.DefaultKey {
		t.Errorf("the first folder got %s", got)
	}
	if got := keyFor(conf, "/srv/b", has(credentials.DefaultKey, own("/srv/b"))); got != own("/srv/b") {
		t.Errorf("a released folder with its own key got %s", got)
	}
	if got := keyFor(sourcesConfig{Sources: []sourceConfig{{ID: "srv-b", Path: "/srv/b", Kind: kindLocal}}}, "/srv/b", has()); got != "" {
		t.Errorf("a local folder got the key %q", got)
	}
}
