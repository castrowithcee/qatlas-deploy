package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// renameProject gives the stack in folder a new project name, through .env or the compose file's name.
func renameProject(t *testing.T, wc, folder, how, name string) {
	t.Helper()
	file, line := ".env", "COMPOSE_PROJECT_NAME="+name+"\n"
	if how == "compose" {
		file, line = "compose.yaml", "name: "+name+"\n"
	}
	path := filepath.Join(wc, folder, file)
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append([]byte(line), old...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectRenameIsNotDeployedAutomatically(t *testing.T) {
	for _, how := range []string{"env", "compose"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t, "web", "db")
			r.deployAll()
			renameProject(t, r.wc, "web", how, "shop")
			r.copy.set(func(f *fakeCopy) {
				f.remote = commit(2)
				f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
			})

			for range 2 {
				if res := r.poll(); len(res.Started) != 0 || len(r.dep.got()) != 0 {
					t.Fatalf("the renamed stack ran: %v / %v", res.Started, r.dep.got())
				}
			}
			shop, web := r.stack("shop"), r.stack("web")
			if !hasFinding(shop, FindingProjectRenamed) || shop.Deployed != "" || shop.Folder != "web" {
				t.Fatalf("shop = %+v", shop)
			}
			if d := shop.Findings[0].Detail; !strings.Contains(d, "project web") || !strings.Contains(d, "now project shop") {
				t.Fatalf("detail = %q", d)
			}
			if !web.Removed || !hasFinding(web, FindingProjectRenamed) || hasFinding(web, FindingRemoved) || web.Deployed != commit(1) {
				t.Fatalf("web = %+v", web)
			}
			if v, _ := r.eng.WindowEligibility("web"); v.Allowed {
				t.Fatal("the window may act on the old project")
			}
			if _, err := r.eng.CheckUpdate(context.Background(), "shop"); err == nil {
				t.Fatal("the renamed stack can be updated")
			}

			// The person decides: a manual deploy of the new name.
			if _, err := r.eng.Trigger(context.Background(), "shop"); err != nil {
				t.Fatal(err)
			}
			r.eng.Wait()
			if shop := r.stack("shop"); shop.Deployed != commit(2) || hasFinding(shop, FindingProjectRenamed) {
				t.Fatalf("shop after the manual deploy = %+v", shop)
			}
			if res := r.poll(); len(res.Started) != 0 || hasFinding(r.stack("shop"), FindingProjectRenamed) {
				t.Fatalf("poll after the manual deploy: %v, %+v", res.Started, r.stack("shop"))
			}
			if !hasFinding(r.stack("web"), FindingProjectRenamed) || len(r.dep.stepsOf("web")) != 0 {
				t.Fatalf("the old project was touched or lost its finding: %+v %v", r.stack("web"), r.dep.stepsOf("web"))
			}
		})
	}
}

func TestStateWithoutFolderMeansFolderIsTheName(t *testing.T) {
	r := newRig(t, "web", "db")
	state := `{"schema":1,"stacks":{"web":{"deployed":"` + commit(1) + `"},"db":{"deployed":"` + commit(1) + `"}}}`
	if err := os.MkdirAll(filepath.Join(r.state, engineDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.state, engineDir, stateFile), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := r.poll(); len(res.Started) != 0 {
		t.Fatalf("an unchanged stack ran: %v", res.Started)
	}
	raw, err := os.ReadFile(filepath.Join(r.state, engineDir, stateFile))
	if err != nil || strings.Contains(string(raw), `"folder"`) {
		t.Fatalf("a stack named like its folder stores the folder: %s %v", raw, err)
	}

	renameProject(t, r.wc, "web", "env", "shop")
	if res := r.poll(); len(res.Started) != 0 || !hasFinding(r.stack("shop"), FindingProjectRenamed) {
		t.Fatalf("rename of a stack from the old state: %v %+v", res.Started, r.stack("shop"))
	}
}

func TestUnreadableStackIsNoProjectRename(t *testing.T) {
	r := newRig(t, "web")
	renameProject(t, r.wc, "web", "compose", "shop")
	r.deployAll()
	if r.stack("shop").Deployed != commit(1) || r.stack("shop").Folder != "web" {
		t.Fatalf("shop = %+v", r.stack("shop"))
	}
	// A broken compose file leaves the stack with its folder as name.
	if err := os.WriteFile(filepath.Join(r.wc, "web", "compose.yaml"), []byte("name: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.poll()
	web := r.stack("web")
	if hasFinding(web, FindingProjectRenamed) || !hasFinding(web, FindingUnreadable) {
		t.Fatalf("web = %+v", web)
	}
	if hasFinding(r.stack("shop"), FindingProjectRenamed) || len(r.dep.got()) != 0 {
		t.Fatalf("shop = %+v, calls %v", r.stack("shop"), r.dep.got())
	}
	// The folder is still there; the old entry must not claim it is gone.
	shop := r.stack("shop")
	if !shop.Removed || !hasFinding(shop, FindingRemoved) || strings.Contains(shop.Findings[0].Detail, "gone") ||
		!strings.Contains(shop.Findings[0].Detail, "cannot be read") {
		t.Fatalf("shop findings = %+v", shop.Findings)
	}
}

func TestNewStackNextToARenameStillRuns(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	renameProject(t, r.wc, "web", "env", "shop")
	writeStack(t, r.wc, "fresh")
	if res := r.poll(); !slices.Equal(res.Started, []string{"fresh"}) {
		t.Fatalf("started %v", res.Started)
	}
}
