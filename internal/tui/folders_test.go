package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The folders the tests add, one per case of the inspection.
const (
	localDir  = "/srv/local-example"
	filesDir  = "/srv/files-example"
	cloneDir  = "/srv/new-example"
	adoptDir  = "/srv/clone-example"
	repoDir   = "/srv/repo-example"
	rejectDir = "/srv/reject-example"
)

// foldersBackend is the demo host with a remote and a local stack folder, the answers of the inspection for
// every case and a host side that needs to change the permissions of the folder of files.
func foldersBackend() *fakeBackend {
	b := settingsBackend()
	b.status.Engine.Sources = []control.SourceStatus{
		{ID: "srv-stacks-example", Path: stackDir, Kind: control.SourceKindRemote, URL: repoURL, LastPoll: at(30, 14, 30),
			Head: commitA, RemoteHead: commitA},
		{ID: "srv-local-example", Path: localDir, Kind: control.SourceKindLocal, LastPoll: at(30, 14, 25), Head: commitB,
			PollError: "the working copy has uncommitted changes in demo/compose.yaml"},
	}
	for i := range b.status.Engine.Stacks {
		b.status.Engine.Stacks[i].Source = "srv-stacks-example"
	}
	for i := range b.stacks.Stacks {
		b.stacks.Stacks[i].Source, b.stacks.Stacks[i].SourceDir = "srv-stacks-example", stackDir
	}
	no := false
	b.inspects = map[string]control.SourceInspectResult{
		filesDir: {Path: filesDir, Case: control.SourceCaseFiles,
			Access:       &control.SourceAccess{ModePath: filesDir, Owner: "root", Group: "root", Mode: "0755", SetsPermissions: true},
			PlannedFiles: []string{"demo/.env", "demo/compose.yaml"},
			Excluded:     []control.SourceExcluded{{Path: "demo/data", Reason: control.SourceExcludedBindMount, Stack: "demo"}},
			Stacks: []control.SourceStack{{Name: "demo", Folder: "demo",
				Functions: []control.StackFunction{{Function: "deploy", Works: true}, {Function: "backup"}},
				Findings:  []control.CheckFinding{{Stack: "demo", Rule: "secrets-env-not-ignored", Severity: "warning", Cause: "secrets.env is not listed in .gitignore"}},
				Conflicts: []control.SourceConflict{{Container: "demo-app-1", Project: "old-demo"}}}},
			ContainersChecked: true},
		cloneDir: {Path: cloneDir, Case: control.SourceCaseClone, NeedsURL: true,
			Access: &control.SourceAccess{ModePath: "/srv", Owner: "root", Group: "root", Mode: "0755"}, Stacks: []control.SourceStack{}},
		adoptDir: {Path: adoptDir, Case: control.SourceCaseAdopt, URL: repoURL,
			Access: &control.SourceAccess{ModePath: adoptDir, Owner: "admin", Group: "admin", Mode: "0755", SetsPermissions: true, GitWritable: &no},
			Stacks: []control.SourceStack{{Name: "wiki", Folder: "wiki", Functions: []control.StackFunction{{Function: "deploy", Works: true}}}}},
		repoDir: {Path: repoDir, Case: control.SourceCaseLocal,
			Access: &control.SourceAccess{ModePath: repoDir, Owner: "qdeploy", Group: "qdeploy", Mode: "0755", ServiceCanWrite: true},
			Stacks: []control.SourceStack{}},
		rejectDir: {Path: rejectDir, Case: control.SourceCaseReject, Reason: "the folder is a repository whose origin is no SSH address",
			Fix: "Set an SSH origin, or choose another folder.", Stacks: []control.SourceStack{}},
	}
	b.host = &fakeFolders{absent: true, plans: map[string]PermPlan{
		filesDir: NewPermPlan(3, []string{"3 paths get the group of the service account with group write; the owner stays"}, "files-plan"),
		adoptDir: NewPermPlan(9, []string{"9 paths get the group of the service account with group write; the owner stays"}, "adopt-plan"),
	}}
	return b
}

// addKeys open the form, type the folder and send it.
// onFolder marks the group row of the stack folder dir.
func onFolder(t *testing.T, m *Model, dir string) {
	t.Helper()
	send(m, "home")
	for i := 0; i < 40; i++ {
		if key, ok := m.list.SelectedGroup(); ok && key == dir {
			return
		}
		send(m, "down")
	}
	t.Fatalf("the folder %s cannot be marked", dir)
}

func addKeys(dir string) []string { return seq(toFolders, keys("a"), typed(dir), keys("f2")) }

// noChange fails when the service or the host changed anything; the probe of a host key only reads.
func noChange(t *testing.T, b *fakeBackend) {
	t.Helper()
	for _, a := range b.actions() {
		if !strings.HasPrefix(a, "probe:") {
			t.Fatalf("a changing call without yes: %v", b.actions())
		}
	}
	if got := b.host.changes(); len(got) != 0 {
		t.Fatalf("the host changed without yes: %v", got)
	}
}

func TestFolderListShowsEachFolderWithItsState(t *testing.T) {
	m := newSettingsModel(t, foldersBackend())
	send(m, toFolders...)
	onFolder(t, m, stackDir)
	contains(t, m, stackDir, localDir, "clone", "local", "polled", "poll failed: the working copy", "address: "+repoURL)
	onFolder(t, m, localDir)
	contains(t, m, "poll failed: the working copy has uncommitted changes in demo/compose.yaml")
}

func TestFolderAddInspectsWithoutChangingAnything(t *testing.T) {
	b := foldersBackend()
	m := newSettingsModel(t, b)
	send(m, addKeys(filesDir)...)
	contains(t, m, "case: files", "demo/compose.yaml", "Left out (1)", "demo/data: bind-mount (stack demo)",
		"works: deploy", "does not work: backup", "secrets-env-not-ignored", "demo-app-1", "3 paths get the group")
	if m.set.form != nil || m.modal == nil || b.called("sourceinspect") != 1 {
		t.Fatalf("form %v, question %v, inspections %d", m.set.form, m.modal, b.called("sourceinspect"))
	}
	if want := []string{"readonly:" + filesDir, "plan:" + filesDir}; !slices.Equal(b.host.calls, want) {
		t.Fatalf("host calls = %v, want %v", b.host.calls, want)
	}
	noChange(t, b)
}

func TestFolderAddAsksForTheAddressOnlyForAMissingOrEmptyFolder(t *testing.T) {
	b := foldersBackend()
	m := newSettingsModel(t, b)
	send(m, addKeys(cloneDir)...)
	contains(t, m, "Clone a repository into "+cloneDir, "nearest existing folder is /srv", "created with owner qdeploy:qdeploy")
	send(m, "y")
	if m.set.form == nil || m.set.form.kind != formFolderURL {
		t.Fatal("no address asked for an empty folder")
	}
	for _, dir := range []string{adoptDir, repoDir, filesDir} {
		b := foldersBackend()
		b.dirKeys = map[string]control.KeyResult{adoptDir: {Fingerprint: fingerprint}}
		m := newSettingsModel(t, b)
		send(m, addKeys(dir)...)
		send(m, "y")
		if m.set.form != nil {
			t.Fatalf("%s: an address was asked", dir)
		}
	}
}

func TestFolderAddChangesNothingWithoutYes(t *testing.T) {
	for _, c := range []struct {
		name string
		keys []string
	}{
		{"local", seq(addKeys(repoDir), keys("n"))},
		{"local enter", seq(addKeys(repoDir), keys("enter"))},
		{"files", seq(addKeys(filesDir), keys("n"))},
		{"adopt", seq(addKeys(adoptDir), keys("n"))},
		{"adopt host key", seq(addKeys(adoptDir), keys("y", "n"))},
		{"adopt last question", seq(addKeys(adoptDir), keys("y", "y", "n"))},
		{"clone last question", seq(addKeys(cloneDir), keys("y"), typed(repoURL), keys("f2", "y", "enter"))},
		{"reject", seq(addKeys(rejectDir), keys("y", "enter"))},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := foldersBackend()
			b.dirKeys = map[string]control.KeyResult{adoptDir: {Fingerprint: fingerprint}, cloneDir: {Fingerprint: fingerprint}}
			m := newSettingsModel(t, b)
			send(m, c.keys...)
			noChange(t, b)
			if len(b.repoSet) != 0 || len(b.sourceAdds) != 0 {
				t.Fatalf("binds %v, adds %v", b.repoSet, b.sourceAdds)
			}
		})
	}
}

func TestFolderAddClonesAfterTheHostKeyAndTheLastQuestion(t *testing.T) {
	b := foldersBackend()
	b.dirKeys = map[string]control.KeyResult{cloneDir: {Fingerprint: fingerprint}}
	m := newSettingsModel(t, b)
	send(m, addKeys(cloneDir)...)
	send(m, "y")
	send(m, typed(repoURL)...)
	send(m, "f2")
	contains(t, m, "Trust this host key for", repoURL, "host: example.test", fingerprint)
	send(m, "y")
	contains(t, m, "Clone "+repoURL+" into "+cloneDir, "created with owner qdeploy:qdeploy")
	noChange(t, b)
	send(m, "y")
	if got := b.host.changes(); !slices.Equal(got, []string{"grant:" + cloneDir + ":true"}) {
		t.Fatalf("host changes = %v", got)
	}
	want := control.RepoSetArgs{URL: repoURL, Dir: cloneDir, Fingerprint: fingerprint}
	if len(b.repoSet) != 1 || b.repoSet[0] != want {
		t.Fatalf("binds = %+v, want %+v", b.repoSet, want)
	}
	contains(t, m, "The stack folder "+cloneDir+" is bound")
}

func TestFolderAddWithoutDeployKeyEndsOnTheKeyViewOfTheFolder(t *testing.T) {
	b := foldersBackend()
	m := newSettingsModel(t, b)
	send(m, addKeys(adoptDir)...)
	send(m, "y", "y")
	if m.area != areaStacks || !m.set.keyOpen || m.set.keyFor != adoptDir || m.set.add != nil {
		t.Fatalf("area %s, key view %v for %q, flow %v", m.area, m.set.keyOpen, m.set.keyFor, m.set.add)
	}
	contains(t, m, adoptDir+" has no deploy key yet")
	send(m, "enter")
	contains(t, m, "stack folder: "+adoptDir, "No deploy key yet")
	noChange(t, b)
}

func TestFolderAddTakesOverAClone(t *testing.T) {
	b := foldersBackend()
	b.dirKeys = map[string]control.KeyResult{adoptDir: {Fingerprint: fingerprint}}
	m := newSettingsModel(t, b)
	send(m, addKeys(adoptDir)...)
	send(m, "y", "y")
	contains(t, m, "Take over the clone "+adoptDir, "9 paths get the group")
	send(m, "y")
	if got := b.host.changes(); !slices.Equal(got, []string{"apply:9", "grant:" + adoptDir + ":false"}) {
		t.Fatalf("host changes = %v", got)
	}
	if len(b.repoSet) != 1 || b.repoSet[0].URL != repoURL || b.repoSet[0].Dir != adoptDir {
		t.Fatalf("binds = %+v", b.repoSet)
	}
}

func TestFolderAddOfALocalRepository(t *testing.T) {
	b := foldersBackend()
	m := newSettingsModel(t, b)
	send(m, addKeys(repoDir)...)
	contains(t, m, "No permission changes")
	send(m, "y")
	if got := b.host.changes(); !slices.Equal(got, []string{"grant:" + repoDir + ":false"}) {
		t.Fatalf("host changes = %v", got)
	}
	if len(b.sourceAdds) != 1 || b.sourceAdds[0].Path != repoDir || len(b.sourceAdds[0].Files) != 0 {
		t.Fatalf("adds = %+v", b.sourceAdds)
	}
	contains(t, m, "The stack folder "+repoDir+" is added")
}

func TestFolderAddOfFilesTakesTheListShown(t *testing.T) {
	b := foldersBackend()
	m := newSettingsModel(t, b)
	send(m, addKeys(filesDir)...)
	send(m, "y")
	if b.called("sourceinspect") != 2 {
		t.Fatalf("%d inspections, want a second one before the change", b.called("sourceinspect"))
	}
	if got := b.host.changes(); !slices.Equal(got, []string{"apply:3", "grant:" + filesDir + ":false"}) {
		t.Fatalf("host changes = %v", got)
	}
	if len(b.sourceAdds) != 1 || !slices.Equal(b.sourceAdds[0].Files, []string{"demo/.env", "demo/compose.yaml"}) {
		t.Fatalf("adds = %+v", b.sourceAdds)
	}
}

func TestFolderAddOfFilesStopsWhenTheListChanged(t *testing.T) {
	b := foldersBackend()
	m := newSettingsModel(t, b)
	send(m, addKeys(filesDir)...)
	res := b.inspects[filesDir]
	res.PlannedFiles = append(slices.Clone(res.PlannedFiles), "demo/new.yaml")
	b.inspects[filesDir] = res
	send(m, "y")
	noChange(t, b)
	if len(b.sourceAdds) != 0 {
		t.Fatalf("adds = %+v", b.sourceAdds)
	}
	contains(t, m, "changed since its list was shown")
}

func TestFolderAddSaysWhichPermissionsStayChanged(t *testing.T) {
	b := foldersBackend()
	b.host.grantErr = errors.New("the service manager refused")
	m := newSettingsModel(t, b)
	send(m, addKeys(filesDir)...)
	send(m, "y")
	contains(t, m, "the host side failed: the service manager refused", "3 paths stay changed")
	if len(b.sourceAdds) != 0 {
		t.Fatalf("adds = %+v", b.sourceAdds)
	}
}

func TestFolderAddRejectsAReadOnlyFolder(t *testing.T) {
	b := foldersBackend()
	b.host.readOnly = true
	m := newSettingsModel(t, b)
	send(m, addKeys(repoDir)...)
	contains(t, m, repoDir+" cannot be taken over", "read-only file system")
	send(m, "y")
	if m.modal == nil {
		t.Fatal("y answered a rejection, which has only close")
	}
	send(m, "enter")
	noChange(t, b)
}

func TestFolderFetchFetchesTheChosenFolder(t *testing.T) {
	b := foldersBackend()
	m := newSettingsModel(t, b)
	send(m, toFolders...)
	onFolder(t, m, stackDir)
	send(m, "F", "n")
	if len(b.actions()) != 0 {
		t.Fatalf("calls %v", b.actions())
	}
	send(m, "F", "y")
	if !slices.Equal(b.fetchDirs, []string{stackDir}) {
		t.Fatalf("fetched %v", b.fetchDirs)
	}
	contains(t, m, "Fetched "+stackDir+"; the remote has newer commits")
	send(m, "enter")
	onFolder(t, m, localDir)
	send(m, "F")
	if m.modal != nil || len(b.fetchDirs) != 1 {
		t.Fatal("a local folder was fetched")
	}
	contains(t, m, "no remote to fetch from")
}

func TestFolderFetchFailureNamesTheFolder(t *testing.T) {
	b := foldersBackend()
	b.errs["fetch:"+stackDir] = control.NewError(control.CodeRemote, "cannot reach the server")
	m := newSettingsModel(t, b)
	send(m, toFolders...)
	onFolder(t, m, stackDir)
	send(m, "F", "y")
	contains(t, m, "the fetch of "+stackDir+" failed", "cannot reach the server")
}

func TestFolderReleaseOnlyAfterYes(t *testing.T) {
	b := foldersBackend()
	m := newSettingsModel(t, b)
	send(m, toFolders...)
	onFolder(t, m, stackDir)
	send(m, "x")
	contains(t, m, "Release the stack folder "+stackDir, "Nothing is deleted")
	send(m, "n")
	send(m, "x", "enter")
	if len(b.removes) != 0 {
		t.Fatalf("released without yes: %v", b.removes)
	}
	send(m, "x", "y")
	if !slices.Equal(b.removes, []string{stackDir}) {
		t.Fatalf("released %v", b.removes)
	}
	contains(t, m, "The stack folder "+stackDir+" is released")
}

func TestFolderDeployKeyOpensTheKeyViewOfThatFolder(t *testing.T) {
	b := foldersBackend()
	m := newSettingsModel(t, b)
	send(m, toFolders...)
	onFolder(t, m, stackDir)
	send(m, "e")
	if !m.set.keyOpen || m.set.keyFor != stackDir {
		t.Fatalf("key view %v for %q", m.set.keyOpen, m.set.keyFor)
	}
	contains(t, m, "stack folder: "+stackDir, "No deploy key yet")
	// The form is for this folder alone: no folder to choose, and the key goes to this folder.
	send(m, "e")
	if _, ok := m.set.form.form.Values()["folder"]; ok {
		t.Fatal("the form offers a folder choice")
	}
	send(m, "right", "f2")
	if got := b.actions(); !slices.Equal(got, []string{"keygen:" + stackDir}) {
		t.Fatalf("calls = %v", got)
	}
	send(m, "enter")
	contains(t, m, "SHA256:ExampleKey")
	send(m, "esc")
	if m.set.keyOpen {
		t.Fatal("esc leaves the key view open")
	}
	onFolder(t, m, localDir)
	send(m, "e")
	if m.set.keyOpen {
		t.Fatal("a local folder opened the key view")
	}
	contains(t, m, "has no deploy key")
}
