package stackcheck

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// loadAt writes the files of demoFiles below prefix of a stacks folder named demo-stack and loads it: prefix
// "demo-stack/" gives a subfolder stack, "" the root stack. extra files go to their paths as given.
func loadAt(t *testing.T, prefix string, extra map[string]string) []stacks.Stack {
	t.Helper()
	root := filepath.Join(t.TempDir(), "demo-stack")
	files := map[string]string{}
	for k, v := range demoFiles() {
		files[prefix+k] = v
	}
	for k, v := range extra {
		files[k] = v
	}
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list, err := stacks.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// The report of a root stack equals that of the same stack in a subfolder: tracked secrets.env and data/ are
// found in the root.
func TestRootStackReportsLikeASubfolder(t *testing.T) {
	var reports []Report
	for _, prefix := range []string{"demo-stack/", ""} {
		list := loadAt(t, prefix, nil)
		if len(list) != 1 || list[0].Name != "demo-stack" {
			t.Fatalf("%q: stacks = %+v", prefix, list)
		}
		git := fakeGit{tracked: []string{prefix + "secrets.env", prefix + "data/db/x.bin", prefix + "compose.yaml", "other/data/y"}}
		got, err := Check(context.Background(), list, git, true)
		if err != nil {
			t.Fatal(err)
		}
		reports = append(reports, got[0])
	}
	if !slices.Equal(rules(reports[0].Findings), []string{RuleTrackedSecretsEnv, RuleTrackedData}) {
		t.Fatalf("findings = %+v", reports[0].Findings)
	}
	for i := range reports[0].Findings {
		if reports[0].Findings[i] != reports[1].Findings[i] {
			t.Errorf("finding %d: subfolder %+v, root %+v", i, reports[0].Findings[i], reports[1].Findings[i])
		}
	}
	if f := has(reports[1].Findings, RuleTrackedData); f == nil || !strings.Contains(f.Cause, "1 file(s)") || !strings.Contains(f.Cause, "data/db/x.bin") {
		t.Fatalf("root data finding = %+v", f)
	}
}

func TestRootStackNamesTheIgnoredSubfolders(t *testing.T) {
	list := loadAt(t, "", map[string]string{"a-app/compose.yaml": goodCompose, "b-app/docker-compose.yml": goodCompose, "notes/readme.txt": "x"})
	rs, err := Check(context.Background(), list, fakeGit{}, true)
	if err != nil {
		t.Fatal(err)
	}
	var ignored []string
	for _, f := range rs[0].Findings {
		if f.Rule == RuleIgnoredFolder {
			if f.Severity != SeverityWarning || !strings.Contains(f.Cause, "not a stack") || !strings.Contains(f.Fix, "subfolder") {
				t.Errorf("finding = %+v", f)
			}
			ignored = append(ignored, strings.Fields(f.Cause)[2])
		}
	}
	if !slices.Equal(ignored, []string{"a-app", "b-app"}) || len(rs[0].Findings) != 2 || !function(rs[0], FuncDeploy).Works {
		t.Fatalf("report = %+v", rs[0])
	}
}
