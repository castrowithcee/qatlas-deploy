package engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// The same stack once as the subfolder web and once as the root stack: classify gives both the same answer.
func TestClassifyRootStackLikeASubfolder(t *testing.T) {
	cases := []struct {
		rel     string
		code    string
		inStack bool // else the host's own: neither in the stack nor dirty
	}{
		{"compose.yaml", " M", true},
		{".env", " M", true},
		{"sub/compose.yaml", "??", true},
		{"secrets.env", "??", false},
		{"secrets.env", " M", false},
		{"data/db/x.bin", "??", false},
		{"uploads/a.png", "??", false},
		{"uploads/tracked.txt", " M", true},
		{"uploadsx/a.png", "??", true},
	}
	for _, form := range []struct{ folder, prefix string }{{"web", "web/"}, {stacks.RootFolder, ""}} {
		list := []stacks.Stack{{Name: "web", Folder: form.folder, BindMounts: []string{"uploads"}}}
		for _, tc := range cases {
			path := form.prefix + tc.rel
			byStack, dirty := classify([]gitrepo.Change{{Code: tc.code, Path: path}}, list)
			want := map[string][]string{}
			if tc.inStack {
				want[form.folder] = []string{path}
			}
			if dirty != tc.inStack || len(byStack) != len(want) || !slices.Equal(byStack[form.folder], want[form.folder]) {
				t.Errorf("%s %q: byStack = %v, dirty = %v", form.folder, path, byStack, dirty)
			}
		}
		// The settings belong to no stack, and a change there still keeps the working copy from moving.
		byStack, dirty := classify([]gitrepo.Change{{Code: " M", Path: stacks.SettingsPath("web")}}, list)
		if !dirty || len(byStack[form.folder]) != 0 {
			t.Errorf("%s settings: byStack = %v, dirty = %v", form.folder, byStack, dirty)
		}
		if got := folderOf(list, "web"); got != form.folder {
			t.Errorf("folderOf = %q", got)
		}
	}
}

// rootRig deploys at commit 1 the root stack web, whose compose file lies in the root of the working copy; the
// subfolder db with a compose file of its own is part of it.
func rootRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, "db")
	body := "name: web\nservices:\n  app:\n    image: registry.example.test/demo/app:${APP_VERSION}\n"
	for name, content := range map[string]string{"compose.yaml": body, ".env": demoEnv} {
		if err := os.WriteFile(filepath.Join(r.wc, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r.deployAll()
	if st := r.stack("web"); st.Deployed != commit(1) || st.Folder != stacks.RootFolder {
		t.Fatalf("web = %+v", st)
	}
	if _, ok, _ := r.eng.Stack("db"); ok {
		t.Fatal("the subfolder db became a stack")
	}
	return r
}

func TestRootStackRunsOnChangesOutsideTheSettings(t *testing.T) {
	r := rootRig(t)
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{".qatlas"}
	})
	if res := r.poll(); len(res.Started) != 0 || r.stack("web").Deployed != commit(1) {
		t.Fatalf("a settings change started %v", res.Started)
	}
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(3)
		f.folders[[2]string{commit(1), commit(3)}] = []string{stacks.RootFolder, "db"}
	})
	if res := r.poll(); !slices.Equal(res.Started, []string{"web"}) || r.stack("web").Deployed != commit(3) {
		t.Fatalf("result = %+v", res)
	}
}

func TestRootStackLocalChangeIsItsOwn(t *testing.T) {
	r := rootRig(t)
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{stacks.RootFolder}
		f.changes = []gitrepo.Change{{Code: "??", Path: "secrets.env"}, {Code: "??", Path: "data/x"}}
	})
	if res := r.poll(); !res.FastForwarded || !slices.Equal(res.Started, []string{"web"}) {
		t.Fatalf("host-owned files blocked the run: %+v", res)
	}
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(3)
		f.folders[[2]string{commit(2), commit(3)}] = []string{stacks.RootFolder}
		f.changes = []gitrepo.Change{{Code: " M", Path: "db/compose.yaml"}}
	})
	if res := r.poll(); res.FastForwarded || len(res.Started) != 0 || !hasFinding(r.stack("web"), FindingLocalChanges) {
		t.Fatalf("result = %+v, web = %+v", res, r.stack("web"))
	}
}

func TestRootStackUpdateCommitsItsEnvAndRevertsTheWholeStack(t *testing.T) {
	r := rootRig(t)
	r.dep.fail("check", errBoom)
	res, err := r.eng.Update(t.Context(), "web", []VersionStep{step("1.0.0", "1.1.0")})
	if err != nil || len(res) != 1 || res[0].Result != ResultRolledBack {
		t.Fatalf("results = %+v, %v", res, err)
	}
	if c := r.copy.commits; len(c) != 1 || c[0].Path != ".env" || string(c[0].New) != envWith("1.1.0") {
		t.Fatalf("commits = %+v", c)
	}
	if rv := r.copy.reverts; len(rv) != 1 || rv[0].Stack != stacks.RootFolder || rv[0].Failed != commit(2001) {
		t.Fatalf("reverts = %+v", rv)
	}
	if !slices.Contains(r.dep.stepsOf("web"), "backup") || !strings.HasPrefix(r.dep.stepsOf("web")[0], "stop") {
		t.Fatalf("steps = %v", r.dep.stepsOf("web"))
	}
	env, err := os.ReadFile(filepath.Join(r.wc, ".env"))
	if err != nil || string(env) != demoEnv {
		t.Fatalf(".env = %q, %v", env, err)
	}
}
