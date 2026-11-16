package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

const remoteURL = "ssh://git.example.test/ops/local-example.git"

// remoteBackend is the demo host with a local folder that has a deploy key, and the given comparison.
func remoteBackend(cmp control.SourceCompareResult) *fakeBackend {
	b := foldersBackend()
	b.dirKeys = map[string]control.KeyResult{localDir: {Fingerprint: fingerprint, PublicKey: "ssh-ed25519 AAAAexamplekey qdeploy"},
		filesDir: {Fingerprint: fingerprint, PublicKey: "ssh-ed25519 AAAAexamplekey qdeploy"}}
	b.compare = cmp
	return b
}

var (
	cmpEmpty = control.SourceCompareResult{Case: control.SourceCaseLocal, State: control.SourceRemoteEmpty, Branch: "main",
		Secrets: &control.SourceSecrets{}}
	cmpEqual = control.SourceCompareResult{Case: control.SourceCaseLocal, State: control.SourceRemoteEqual, Branch: "main", Commit: commitA}
	cmpDiff  = control.SourceCompareResult{Case: control.SourceCaseLocal, State: control.SourceRemoteDifferent, Branch: "main",
		Commit: commitA, PathCount: 12, Secrets: &control.SourceSecrets{},
		Paths: []string{"demo/compose.yaml", "demo/.env", "wiki/compose.yaml", "wiki/data", "a", "b", "c", "d", "e", "f"}}
	cmpSecrets = control.SourceCompareResult{Case: control.SourceCaseLocal, State: control.SourceRemoteDifferent, Branch: "main",
		Commit: commitA, PathCount: 1, Paths: []string{"demo/compose.yaml"},
		Secrets: &control.SourceSecrets{Total: 3, Findings: []control.SourceSecretFinding{
			{Commit: commitB, Path: "demo/secrets.env", Kind: control.SourceSecretFile},
			{Commit: commitB, Path: "demo/.env", Kind: control.SourceSecretEnv},
			{Commit: commitA, Path: "keys/id_ed25519", Kind: control.SourceSecretPrivateKey}}}}
)

// remoteKeys mark the local folder, open the form, enter the address and confirm the host key.
func remoteKeys() []string {
	return seq(toFolders, keys("up", "up", "O"), typed(remoteURL), keys("f2", "y"))
}

func TestRemoteKeyOnlyOnTheRowOfALocalFolder(t *testing.T) {
	m := newSettingsModel(t, remoteBackend(cmpEmpty))
	send(m, toFolders...)
	onFolder(t, m, stackDir)
	if slices.Contains(hintLabels(m), "add remote") {
		t.Fatalf("hint on a folder with a remote: %v", hintLabels(m))
	}
	send(m, "O")
	if m.set.form != nil || m.set.add != nil {
		t.Fatal("O acted on a folder with a remote")
	}
	contains(t, m, "runs against a remote already")
	send(m, "down")
	if _, onFolder := m.selectedFolder(); onFolder {
		t.Fatal("the mark is still on the folder")
	}
	send(m, "O")
	if m.set.form != nil {
		t.Fatal("O acted on a stack row")
	}
	onFolder(t, m, localDir)
	if !slices.Contains(hintLabels(m), "add remote") {
		t.Fatalf("no hint on a local folder: %v", hintLabels(m))
	}
}

func TestRemoteOfAnEmptyRepositoryPushesAfterYes(t *testing.T) {
	b := remoteBackend(cmpEmpty)
	m := newSettingsModel(t, b)
	send(m, seq(toFolders, keys("up", "up", "O"))...)
	if m.set.form == nil || m.set.form.kind != formFolderURL {
		t.Fatal("no form for the address")
	}
	send(m, seq(typed(remoteURL), keys("f2"))...)
	contains(t, m, "Trust this host key for", "local-", localDir, fingerprint)
	send(m, "y")
	contains(t, m, "Push the history of "+localDir+" to "+remoteURL+" (branch main)?", "ssh-ed25519 AAAAexamplekey", "write access")
	noChange(t, b)
	if len(b.remotes) != 0 {
		t.Fatalf("remote call without yes: %v", b.remotes)
	}
	send(m, "y")
	want := control.SourceRemoteArgs{Path: localDir, URL: remoteURL, Fingerprint: fingerprint, ExpectState: control.SourceRemoteEmpty}
	if len(b.remotes) != 1 || b.remotes[0] != want {
		t.Fatalf("remote calls = %+v, want %+v", b.remotes, want)
	}
	contains(t, m, localDir+" runs against", "now.", "pushed: yes")
}

func TestRemoteOfAnEqualRepositoryIsTakenOver(t *testing.T) {
	b := remoteBackend(cmpEqual)
	m := newSettingsModel(t, b)
	send(m, remoteKeys()...)
	contains(t, m, "Take over the history of "+localDir+" from "+remoteURL+"?", "nothing is pushed")
	send(m, "y")
	want := control.SourceRemoteArgs{Path: localDir, URL: remoteURL, Fingerprint: fingerprint,
		ExpectState: control.SourceRemoteEqual, ExpectCommit: commitA}
	if len(b.remotes) != 1 || b.remotes[0] != want {
		t.Fatalf("remote calls = %+v, want %+v", b.remotes, want)
	}
}

func TestRemoteOfADifferentRepositoryListsThePathsAndAsksForTheCommit(t *testing.T) {
	b := remoteBackend(cmpDiff)
	m := newSettingsModel(t, b)
	send(m, remoteKeys()...)
	contains(t, m, "Put the state of the host in "+localDir+" as one commit on branch main of",
		"Paths that differ (12)", "demo/compose.yaml", "... and 2 more", "put and push")
	for _, k := range []string{"n", "enter", "esc"} {
		send(m, k)
		if len(b.remotes) != 0 {
			t.Fatalf("%s changed the folder: %v", k, b.remotes)
		}
		send(m, seq(toFolders, keys("up", "up", "O"), typed(remoteURL), keys("f2", "y"))...)
	}
	send(m, "y")
	if len(b.remotes) != 1 || b.remotes[0].ExpectState != control.SourceRemoteDifferent || b.remotes[0].ExpectCommit != commitA {
		t.Fatalf("remote calls = %+v", b.remotes)
	}
}

func TestRemoteSecretFindingListsPlacesAndCannotBeOverridden(t *testing.T) {
	b := remoteBackend(cmpSecrets)
	m := newSettingsModel(t, b)
	send(m, remoteKeys()...)
	contains(t, m, "hold 3 secret-like files", "Secret-like files (3)", "commit "+short(commitB)+": demo/secrets.env (secrets-file)",
		"keys/id_ed25519 (private-key)", "no way around this")
	for _, k := range []string{"y", "w", "enter"} {
		send(m, k)
		if len(b.remotes) != 0 || len(b.sourceAdds) != 0 {
			t.Fatalf("%s pushed: %v %v", k, b.remotes, b.sourceAdds)
		}
	}
	if m.modal != nil {
		t.Fatal("the notice did not close")
	}
}

func TestRemotePathsAreShownAsText(t *testing.T) {
	cmp := cmpDiff
	cmp.Paths = []string{"evil\x1b[31mred\x1b]0;title\x07.yaml"}
	cmp.PathCount = 1
	m := newSettingsModel(t, remoteBackend(cmp))
	send(m, remoteKeys()...)
	screen := m.render()
	if strings.ContainsRune(screen, 0x07) || strings.Contains(screen, "\x1b[31mred") || strings.Contains(screen, "\x1b]0;") {
		t.Fatalf("a control sequence reached the screen: %q", screen)
	}
	contains(t, m, "evil")
}

func TestRemoteWithoutDeployKeyOpensTheKeyViewAndComparesNothing(t *testing.T) {
	b := remoteBackend(cmpEmpty)
	b.dirKeys = map[string]control.KeyResult{}
	m := newSettingsModel(t, b)
	send(m, remoteKeys()...)
	if !m.set.keyOpen || m.set.keyFor != localDir || m.set.add != nil {
		t.Fatalf("key view %v for %q, flow %v", m.set.keyOpen, m.set.keyFor, m.set.add)
	}
	contains(t, m, localDir+" has no deploy key yet", "on the git platform with write access")
	if b.called("sourcecompare") != 0 || len(b.remotes) != 0 {
		t.Fatal("compared or changed without a key")
	}
	noChange(t, b)
	// The key view of a local folder offers to generate the key; its public part is shown after.
	send(m, "enter", "e", "right", "f2", "enter")
	contains(t, m, "SHA256:ExampleKey", "public key")
}

func TestRemoteCancelAtEveryStepChangesNothing(t *testing.T) {
	for name, ks := range map[string][]string{
		"form":     seq(toFolders, keys("up", "up", "O", "esc")),
		"host key": seq(toFolders, keys("up", "up", "O"), typed(remoteURL), keys("f2", "n")),
		"question": seq(remoteKeys(), keys("n")),
	} {
		t.Run(name, func(t *testing.T) {
			b := remoteBackend(cmpDiff)
			m := newSettingsModel(t, b)
			send(m, ks...)
			noChange(t, b)
			if len(b.remotes) != 0 || len(b.sourceAdds) != 0 {
				t.Fatalf("remotes %v adds %v", b.remotes, b.sourceAdds)
			}
		})
	}
}

func TestRemoteChangedInBetweenIsRefusedByTheServiceAndShown(t *testing.T) {
	b := remoteBackend(cmpDiff)
	b.errs["sourceremote:"+localDir] = control.NewError(control.CodeInvalid,
		"the remote or the folder changed since the comparison was shown; nothing was changed, compare again")
	m := newSettingsModel(t, b)
	send(m, remoteKeys()...)
	send(m, "y")
	contains(t, m, "changed since the comparison was shown")
	if len(b.remotes) != 0 {
		t.Fatalf("remotes %v", b.remotes)
	}
}

func TestAClickOnRemoteActsLikeTheKeyAndTheAnswerLikeItsKey(t *testing.T) {
	b := remoteBackend(cmpDiff)
	m := folderModel(t, b)
	x, y := cellOf(t, m, localDir)
	clickAt(m, x, y)
	x, y = cellOf(t, m, "add remote")
	clickAt(m, x, y)
	if m.set.form == nil || m.set.form.kind != formFolderURL || m.set.add == nil || m.set.add.res.Path != localDir {
		t.Fatal("a click on the key did not open the form for the marked folder")
	}
	send(m, seq(typed(remoteURL), keys("f2", "y"))...)
	x, y = cellOf(t, m, "put and push")
	clickAt(m, x, y)
	if len(b.remotes) != 1 || b.remotes[0].Path != localDir {
		t.Fatalf("remotes %v", b.remotes)
	}
}

// Files without git, added together with the repository that holds content already.
func filesRemoteKeys() []string {
	return seq(addKeys(filesDir), keys("w"), typed(remoteURL), keys("f2", "y"))
}

func TestFolderOfFilesWithAddressComparesAndAddsAfterYes(t *testing.T) {
	cmp := cmpDiff
	cmp.Case, cmp.PlannedFiles = control.SourceCaseFiles, []string{"demo/.env", "demo/compose.yaml"}
	b := remoteBackend(cmp)
	m := newSettingsModel(t, b)
	send(m, seq(addKeys(filesDir), keys("w"))...)
	if m.set.form == nil || m.set.form.kind != formFolderURL {
		t.Fatal("no address asked")
	}
	send(m, seq(typed(remoteURL), keys("f2", "y"))...)
	contains(t, m, "Put the state of the host in "+filesDir, "address: "+remoteURL, "Paths that differ (12)")
	noChange(t, b)
	if len(b.sourceAdds) != 0 {
		t.Fatalf("adds %v", b.sourceAdds)
	}
	send(m, "y")
	if len(b.sourceAdds) != 1 {
		t.Fatalf("adds %v", b.sourceAdds)
	}
	a := b.sourceAdds[0]
	if a.URL != remoteURL || a.Fingerprint != fingerprint || a.ExpectState != control.SourceRemoteDifferent ||
		a.ExpectCommit != commitA || !slices.Equal(a.Files, []string{"demo/.env", "demo/compose.yaml"}) {
		t.Fatalf("add = %+v", a)
	}
}

func TestFolderOfFilesWithAnEmptyRepositoryIsNotAddedWithIt(t *testing.T) {
	cmp := cmpEmpty
	cmp.Case = control.SourceCaseFiles
	b := remoteBackend(cmp)
	m := newSettingsModel(t, b)
	send(m, filesRemoteKeys()...)
	contains(t, m, "has no commit", "without an address first")
	send(m, "y")
	noChange(t, b)
	if len(b.sourceAdds) != 0 {
		t.Fatalf("adds %v", b.sourceAdds)
	}
}

func TestFolderOfFilesWithSecretsInThePushIsRefused(t *testing.T) {
	cmp := cmpSecrets
	cmp.Case = control.SourceCaseFiles
	b := remoteBackend(cmp)
	m := newSettingsModel(t, b)
	send(m, filesRemoteKeys()...)
	contains(t, m, "Secret-like files (3)")
	send(m, "y", "enter")
	if len(b.sourceAdds) != 0 {
		t.Fatalf("adds %v", b.sourceAdds)
	}
}

func TestGoldenStacksRemoteForm(t *testing.T) {
	goldenScreens(t, "stacks.remote.form", settingsRows, func() *fakeBackend { return remoteBackend(cmpDiff) },
		seq(toFolders, keys("up", "up", "O"))...)
}

func TestGoldenStacksRemoteDifferent(t *testing.T) {
	goldenScreens(t, "stacks.remote.different", settingsRows, func() *fakeBackend { return remoteBackend(cmpDiff) }, remoteKeys()...)
}

func TestGoldenStacksRemoteSecrets(t *testing.T) {
	goldenScreens(t, "stacks.remote.secrets", settingsRows, func() *fakeBackend { return remoteBackend(cmpSecrets) }, remoteKeys()...)
}
