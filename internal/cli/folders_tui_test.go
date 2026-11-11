package cli

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

func TestTUIFoldersPlanChangesNothingAndApplyDoesWhatSourceAddDoes(t *testing.T) {
	d := useFolderHost(t)
	tree := newPermTree(t)
	before := tree.snapshot(t)
	res := control.SourceInspectResult{Path: tree.dir, Case: control.SourceCaseLocal,
		Access: &control.SourceAccess{ModePath: tree.dir, SetsPermissions: true},
		Stacks: []control.SourceStack{{BindMounts: []string{"demo/data"}}}}

	var f tuiFolders
	plan, err := f.PlanPermissions(context.Background(), res)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Empty() || plan.Changed < 5 || len(plan.Summary) == 0 {
		t.Fatalf("plan = %+v", plan)
	}
	if !equalModes(before, tree.snapshot(t)) || len(d.lchowns) != 0 {
		t.Fatal("planning changed the tree")
	}

	if err := f.ApplyPermissions(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"top.txt", "demo/compose.yaml", "demo/run.sh"} {
		path := filepath.Join(tree.dir, name)
		if want := before[path] | 0o060; modeOf(t, path) != want {
			t.Errorf("%s: mode %v, want %v", name, modeOf(t, path), want)
		}
	}
	for _, name := range []string{"", "demo"} {
		path := filepath.Join(tree.dir, name)
		if want := before[path] | 0o070 | fs.ModeSetgid; modeOf(t, path) != want {
			t.Errorf("%q: mode %v, want %v", name, modeOf(t, path), want)
		}
	}
	if mode := modeOf(t, filepath.Join(tree.dir, "demo", "secrets.env")); mode != before[filepath.Join(tree.dir, "demo", "secrets.env")] {
		t.Errorf("an unversioned file changed: %v", mode)
	}
	if len(d.lchowns) != plan.Changed {
		t.Errorf("%d group changes for a plan of %d paths", len(d.lchowns), plan.Changed)
	}
}

func TestTUIFoldersPlanIsEmptyWhenTheServiceCanWrite(t *testing.T) {
	d := useFolderHost(t)
	tree := newPermTree(t)
	writable := true
	var f tuiFolders
	for _, res := range []control.SourceInspectResult{
		{Path: tree.dir, Case: control.SourceCaseLocal, Access: &control.SourceAccess{ModePath: tree.dir, ServiceCanWrite: true, GitWritable: &writable}},
		{Path: tree.dir, Case: control.SourceCaseLocal},
		{Path: tree.dir, Case: control.SourceCaseClone, Access: &control.SourceAccess{ModePath: tree.dir, ServiceCanWrite: true}},
	} {
		plan, err := f.PlanPermissions(context.Background(), res)
		if err != nil || !plan.Empty() || plan.Changed != 0 {
			t.Fatalf("plan = %+v, %v", plan, err)
		}
		if err := f.ApplyPermissions(context.Background(), plan); err != nil {
			t.Fatal(err)
		}
	}
	if len(d.lchowns) != 0 {
		t.Fatal("an empty plan changed a group")
	}
}

func TestTUIFoldersPlanForAnEmptyCloneTargetAndForListedFiles(t *testing.T) {
	d := useFolderHost(t)
	var f tuiFolders

	empty := t.TempDir()
	plan, err := f.PlanPermissions(context.Background(), control.SourceInspectResult{Path: empty, Case: control.SourceCaseClone,
		Access: &control.SourceAccess{ModePath: empty, SetsPermissions: true}})
	if err != nil || plan.Changed != 1 {
		t.Fatalf("clone plan = %+v, %v", plan, err)
	}

	tree := newFilesTree(t)
	plan, err = f.PlanPermissions(context.Background(), control.SourceInspectResult{Path: tree.dir, Case: control.SourceCaseFiles,
		Access:       &control.SourceAccess{ModePath: tree.dir, SetsPermissions: true},
		PlannedFiles: []string{"compose.yaml", "conf/app.yaml"}})
	if err != nil || plan.Changed < 3 {
		t.Fatalf("files plan = %+v, %v", plan, err)
	}
	if len(d.lchowns) != 0 {
		t.Fatal("planning changed a group")
	}
}

func TestTUIFoldersRefuseWithoutRootAndReportReadOnly(t *testing.T) {
	d := useFolderHost(t)
	var f tuiFolders
	d.readOnlyFS = true
	if ro, err := f.ReadOnly("/var/tmp/demo-stacks"); err != nil || !ro || len(d.checked) != 1 {
		t.Fatalf("read-only = %v, %v, checked %v", ro, err, d.checked)
	}
	folders.euid = func() int { return 1000 }
	res := control.SourceInspectResult{Path: t.TempDir(), Case: control.SourceCaseLocal,
		Access: &control.SourceAccess{SetsPermissions: true}}
	if _, err := f.PlanPermissions(context.Background(), res); err == nil {
		t.Fatal("planning without root succeeded")
	}
}
