package tui

import (
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/datadir"
)

// dataDirBackend is the demo with a first stack that waits for two data directories of one proposed owner.
func dataDirBackend() *fakeBackend {
	b := settingsBackend()
	b.stacks.Dir = "/srv/stacks"
	b.status.Engine.Stacks[0].Findings = []control.FindingInfo{{Kind: "data-dir-missing", Detail: "data/db, data/files",
		Dirs: []control.DirInfo{{Path: "data/db", Owner: "1000:1000"}, {Path: "data/files", Owner: "1000:1000"}}}}
	return b
}

var toDataDirs = keys("enter", "m")

// dirMaker records the requests of the creation instead of touching the disk.
type dirMaker struct {
	reqs []datadir.Request
	err  error
}

func (d *dirMaker) make(req datadir.Request) (datadir.Result, error) {
	d.reqs = append(d.reqs, req)
	if d.err != nil {
		return datadir.Result{}, d.err
	}
	return datadir.Result{Created: req.Paths[:1], Parents: []string{"data"}, Existing: req.Paths[1:]}, nil
}

func dataDirModel(t *testing.T, b *fakeBackend, euid int) (*Model, *dirMaker) {
	t.Helper()
	m := newSettingsModel(t, b)
	d := &dirMaker{}
	m.euid, m.makeDirs = func() int { return euid }, d.make
	return m, d
}

func TestDataDirFindingIsShownInTheDetailWithTheAction(t *testing.T) {
	m, _ := dataDirModel(t, dataDirBackend(), 0)
	contains(t, m, "needs data dirs")
	send(m, "enter")
	contains(t, m, "Missing data directories", "data/db  owner 1000:1000", "data/files", "m creates them", "create data directories")
}

func TestDataDirActionIsAbsentWithoutTheFinding(t *testing.T) {
	m, d := dataDirModel(t, settingsBackend(), 0)
	send(m, "enter")
	screen := m.render()
	if contains2(screen, "Missing data directories") || contains2(screen, "create data directories") {
		t.Fatalf("the finding or action is offered:\n%s", screen)
	}
	send(m, "m")
	contains(t, m, "no missing data directories")
	if m.set.form != nil || len(d.reqs) != 0 {
		t.Fatal("a form was opened without a finding")
	}
}

func TestDataDirFormPrefillsTheCommonOwner(t *testing.T) {
	m, _ := dataDirModel(t, dataDirBackend(), 0)
	send(m, toDataDirs...)
	contains(t, m, "Create data directories", "data/db", "1000:1000", "0750")
	if v := m.set.form.form.Values(); v["owner"].Text != "1000:1000" || v["mode"].Text != "0750" {
		t.Fatalf("values %v", v)
	}
}

func TestDataDirFormLeavesTheOwnerEmptyForDifferentProposals(t *testing.T) {
	b := dataDirBackend()
	b.status.Engine.Stacks[0].Findings[0].Dirs[1].Owner = "999:999"
	m, _ := dataDirModel(t, b, 0)
	send(m, toDataDirs...)
	if v := m.set.form.form.Values(); v["owner"].Text != "" {
		t.Fatalf("owner %q", v["owner"].Text)
	}
	send(m, "f2")
	contains(t, m, "The owner must be UID:GID")
	if m.modal != nil {
		t.Fatal("an empty owner reached the confirmation")
	}
}

func TestDataDirFormRefusesInvalidOwnerAndMode(t *testing.T) {
	m, d := dataDirModel(t, dataDirBackend(), 0)
	send(m, toDataDirs...)
	send(m, "backspace", "backspace", "x")
	send(m, "down", "backspace", "backspace", "backspace", "backspace", "4", "7", "5", "0")
	send(m, "f2")
	contains(t, m, "The owner must be UID:GID", "The mode must be octal")
	if m.modal != nil || len(d.reqs) != 0 {
		t.Fatal("an invalid form reached the confirmation or the creation")
	}
}

func TestDataDirsAreCreatedOnlyAfterConfirmationThenDeployed(t *testing.T) {
	b := dataDirBackend()
	m, d := dataDirModel(t, b, 0)
	send(m, toDataDirs...)
	send(m, "f2")
	contains(t, m, "Create the missing data directories", "/srv/stacks/demo-stack",
		"owner: 1000:1000", "mode: 0750", "data/files")
	// Cancel is the default: enter creates nothing.
	send(m, "enter")
	if len(d.reqs) != 0 || slices.Contains(b.actions(), "deploy:demo-stack") {
		t.Fatal("created by the default answer")
	}
	send(m, "f2", "y")
	want := datadir.Request{StackDir: "/srv/stacks/demo-stack", Paths: []string{"data/db", "data/files"},
		Owner: datadir.Owner{UID: 1000, GID: 1000}, Mode: 0o750}
	if len(d.reqs) != 1 || !slices.Equal(d.reqs[0].Paths, want.Paths) || d.reqs[0].StackDir != want.StackDir ||
		d.reqs[0].Owner != want.Owner || d.reqs[0].Mode != want.Mode {
		t.Fatalf("requests %+v", d.reqs)
	}
	if !slices.Contains(b.actions(), "deploy:demo-stack") {
		t.Fatalf("actions %v", b.actions())
	}
	contains(t, m, "Data directories created for the stack demo-stack", "created: data/db", "parent directories: data",
		"already existing: data/files", "run 20261031T100000Z-demo-stack started")
	send(m, "enter")
	if m.set.form != nil {
		t.Fatal("the form stays open after the creation")
	}
}

func TestDataDirsCancelAtEveryStepCreatesNothing(t *testing.T) {
	b := dataDirBackend()
	m, d := dataDirModel(t, b, 0)
	send(m, toDataDirs...)
	send(m, "esc")
	if m.set.form != nil {
		t.Fatal("esc keeps the form")
	}
	send(m, "m", "f2", "n")
	if m.modal != nil || m.set.form == nil {
		t.Fatal("n should close the question and keep the form")
	}
	send(m, "f2", "esc")
	send(m, "esc")
	if len(d.reqs) != 0 || len(b.actions()) != 0 {
		t.Fatalf("created %v, actions %v", d.reqs, b.actions())
	}
}

func TestDataDirsWithoutRootOnlySaySo(t *testing.T) {
	b := dataDirBackend()
	m, d := dataDirModel(t, b, 1000)
	send(m, toDataDirs...)
	contains(t, m, "needs root")
	if m.set.form != nil || m.modal != nil || len(d.reqs) != 0 || len(b.actions()) != 0 {
		t.Fatal("something was opened or created without root")
	}
}

func TestDataDirsCreationFailureStaysInTheForm(t *testing.T) {
	b := dataDirBackend()
	m, d := dataDirModel(t, b, 0)
	d.err = errTest("disk full")
	send(m, toDataDirs...)
	send(m, "f2", "y")
	contains(t, m, "Cannot create the data directories: disk full")
	if m.set.form == nil || slices.Contains(b.actions(), "deploy:demo-stack") {
		t.Fatal("the form was closed or a run was started after a failure")
	}
}

func TestDataDirsDeployFailureKeepsTheCreationVisible(t *testing.T) {
	b := dataDirBackend()
	b.errs["deploy:demo-stack"] = errServiceBusy()
	m, _ := dataDirModel(t, b, 0)
	send(m, toDataDirs...)
	send(m, "f2", "y")
	contains(t, m, "Data directories created", "created: data/db", "The run could not be started")
}

type errTest string

func (e errTest) Error() string { return string(e) }
