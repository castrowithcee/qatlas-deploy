package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// localRig is a rig whose working copy has no remote, deployed at commit 1.
func localRig(t *testing.T, setup func(r *rig)) *rig {
	t.Helper()
	r := newRig(t, "web", "db")
	if setup != nil {
		setup(r)
	}
	r.copy.set(func(f *fakeCopy) { f.local = true })
	r.deployAll()
	return r
}

func TestLocalCommitIsDeployed(t *testing.T) {
	r := localRig(t, nil)
	// a person commits a change of web in the working copy
	r.copy.set(func(f *fakeCopy) {
		f.head = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
	})
	res := r.poll()
	if res.FastForwarded || !slices.Equal(res.Started, []string{"web"}) || res.Head != commit(2) || res.RemoteHead != commit(2) {
		t.Fatalf("result = %+v", res)
	}
	if got := r.dep.got(); len(got) != 1 || got[0] != (call{"web", commit(2)}) {
		t.Fatalf("deploy calls = %v", got)
	}
	if web, db := r.stack("web"), r.stack("db"); web.Deployed != commit(2) || db.Deployed != commit(1) || web.Pending != "" {
		t.Fatalf("web = %+v, db = %+v", web, db)
	}
	if r.copy.called("ff") != 0 || r.copy.called("push") != 0 {
		t.Fatalf("calls = %v", r.copy.calls)
	}
}

func TestLocalFailedUpdateIsRevertedLocally(t *testing.T) {
	r := localRig(t, func(r *rig) {
		if err := os.WriteFile(filepath.Join(r.wc, "web", ".env"), []byte(demoEnv), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	r.dep.fail("check", errBoom)
	res := r.update(step("1.0.0", "1.1.0"))
	if len(res) != 1 || res[0].Result != ResultRolledBack || res[0].Commit != commit(2001) {
		t.Fatalf("results = %+v", res)
	}
	if c := r.copy.commits; len(c) != 1 || c[0].Base != commit(1) {
		t.Fatalf("commits = %+v", c)
	}
	if rv := r.copy.reverts; len(rv) != 1 || rv[0].Base != commit(2001) || rv[0].Failed != commit(2001) || rv[0].Deployed != commit(1) {
		t.Fatalf("reverts = %+v", rv)
	}
	// both commits landed on the local branch, one after the other
	if !slices.Equal(r.copy.pushed, []string{commit(2001), commit(1001)}) {
		t.Fatalf("landed = %v", r.copy.pushed)
	}
	if r.env() != demoEnv {
		t.Fatalf(".env = %q", r.env())
	}
	st := r.stack("web")
	if st.Deployed != commit(1001) || st.FailedCommit != commit(2001) || st.Paused != nil {
		t.Fatalf("web = %+v", st)
	}
	if head, _ := r.copy.Head(t.Context()); head != commit(1001) {
		t.Fatalf("head = %s", head)
	}
	// the reverted state is current: the next poll starts nothing
	if res := r.poll(); len(res.Started) != 0 {
		t.Fatalf("poll after the rollback: %+v", res)
	}
}

func TestLocalUpdateLandsAgainAfterAPersonCommitted(t *testing.T) {
	r := localRig(t, func(r *rig) {
		if err := os.WriteFile(filepath.Join(r.wc, "web", ".env"), []byte(demoEnv), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	// a person commits a change of db while the first version commit is built
	r.copy.set(func(f *fakeCopy) {
		f.onCommit = func(f *fakeCopy) {
			f.onCommit = nil
			f.head = commit(3)
		}
	})
	res := r.update(step("1.0.0", "1.1.0"))
	if len(res) != 1 || res[0].Result != ResultSucceeded || res[0].Commit != commit(2002) {
		t.Fatalf("results = %+v", res)
	}
	if c := r.copy.commits; len(c) != 2 || c[0].Base != commit(1) || c[1].Base != commit(3) {
		t.Fatalf("commits = %+v", c)
	}
	if !slices.Equal(r.copy.pushed, []string{commit(2002)}) || r.env() != envWith("1.1.0") {
		t.Fatalf("landed %v, env %q", r.copy.pushed, r.env())
	}
	if head, _ := r.copy.Head(t.Context()); head != commit(2002) {
		t.Fatalf("head = %s", head)
	}
	if text := r.runLog(r.stack("web")); !strings.Contains(text, "rejected: the branch moved") {
		t.Fatalf("log lacks the rejection:\n%s", text)
	}
}

func TestUntrackedFilesBelowABindMountDoNotBlock(t *testing.T) {
	r := newRig(t, "web")
	compose := "services:\n  app:\n    image: registry.example.test/demo/app:${APP_VERSION}\n" +
		"    volumes:\n      - ./uploads:/srv/uploads\n      - type: bind\n        source: ./conf\n        target: /etc/app\n"
	if err := os.WriteFile(filepath.Join(r.wc, "web", "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	r.deployAll()
	r.copy.set(func(f *fakeCopy) {
		f.changes = []gitrepo.Change{{Code: "??", Path: "web/uploads/2026/demo.jpg"}, {Code: "??", Path: "web/conf/app.ini"}}
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
	})
	if res := r.poll(); !res.FastForwarded || !slices.Equal(res.Started, []string{"web"}) || len(r.stack("web").Findings) != 0 {
		t.Fatalf("result = %+v, web = %+v", res, r.stack("web"))
	}

	// a changed tracked file below the bind mount still blocks, and so does an untracked file beside it
	for _, c := range []gitrepo.Change{{Code: " M", Path: "web/uploads/index.html"}, {Code: "??", Path: "web/uploads.txt"}} {
		r.copy.set(func(f *fakeCopy) {
			f.changes = []gitrepo.Change{{Code: "??", Path: "web/uploads/2026/demo.jpg"}, c}
			f.remote = commit(3)
			f.folders[[2]string{commit(2), commit(3)}] = []string{"web"}
			f.calls = nil
		})
		res := r.poll()
		if res.FastForwarded || r.copy.called("ff") != 0 || len(res.Started) != 0 {
			t.Fatalf("%s: result = %+v", c.Path, res)
		}
		web := r.stack("web")
		if !hasFinding(web, FindingLocalChanges) || strings.Contains(web.Findings[0].Detail, "demo.jpg") ||
			!strings.Contains(web.Findings[0].Detail, c.Path) {
			t.Fatalf("%s: web = %+v", c.Path, web)
		}
	}
}

func TestLocalSettingsLandOnTheBranch(t *testing.T) {
	r := localRig(t, nil)
	ctx := context.Background()
	c, err := r.eng.SetSettings(ctx, "web", rulesOf(t, automaticSettings))
	if err != nil || c != commit(2001) {
		t.Fatalf("set = %q, %v", c, err)
	}
	if cs := r.copy.commits; len(cs) != 1 || cs[0].Base != commit(1) || cs[0].Path != ".qatlas/qdeploy/web.yaml" {
		t.Fatalf("commits = %+v", cs)
	}
	head, _ := r.copy.Head(ctx)
	if head != c || !slices.Equal(r.copy.pushed, []string{c}) {
		t.Fatalf("head %s, landed %v", head, r.copy.pushed)
	}
	if got, err := r.eng.StackSettings(ctx, "web"); err != nil || got == nil || got.Update.Mode != stacks.ModeAutomatic {
		t.Fatalf("settings after = %+v, %v", got, err)
	}
}
