package tui

import (
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The goldens of the stack folders in the Stacks area: the rows of the folders, the detail of the marked one,
// the steps of adding one and the deploy key view. Typed secrets never show.

// threeFoldersBackend is two folders with stacks and a third, local one that has none.
func threeFoldersBackend() *fakeBackend {
	b := twoFoldersBackend()
	b.status.Engine.Sources = append(b.status.Engine.Sources,
		control.SourceStatus{ID: "srv-empty-example", Path: "/srv/empty-example", Kind: control.SourceKindLocal, LastPoll: at(30, 14, 20)})
	return b
}

func oneFolderBackend() *fakeBackend { return keyBackend() }

func TestGoldenStacksEmpty(t *testing.T) {
	goldenScreens(t, "stacks.empty", settingsRows, emptyHost)
}

func TestGoldenStacksOneFolder(t *testing.T) {
	goldenScreens(t, "stacks.folder", settingsRows, oneFolderBackend)
}

// The folder without a stack has its row, and the marked folder shows its detail.
func TestGoldenStacksFoldersWithAnEmptyOne(t *testing.T) {
	goldenScreens(t, "stacks.folders.empty", settingsRows, threeFoldersBackend)
}

func TestGoldenStacksFolderDetail(t *testing.T) {
	goldenScreens(t, "stacks.folder.detail", settingsRows, foldersBackend, keys("up")...)
}

func TestGoldenStacksFolderPollError(t *testing.T) {
	goldenScreens(t, "stacks.folder.pollerror", settingsRows, foldersBackend, seq(toFolders, keys("up", "up"))...)
}

func TestGoldenStacksAddFiles(t *testing.T) {
	goldenScreens(t, "stacks.add.files", settingsRows, foldersBackend, addKeys(filesDir)...)
}

func TestGoldenStacksAddClone(t *testing.T) {
	goldenScreens(t, "stacks.add.clone", settingsRows, foldersBackend, addKeys(cloneDir)...)
}

func TestGoldenStacksAddAdopt(t *testing.T) {
	goldenScreens(t, "stacks.add.adopt", settingsRows, foldersBackend, addKeys(adoptDir)...)
}

func TestGoldenStacksAddHostKey(t *testing.T) {
	goldenScreens(t, "stacks.add.hostkey", settingsRows, foldersBackend, seq(addKeys(adoptDir), keys("y"))...)
}

func TestGoldenStacksAddReject(t *testing.T) {
	goldenScreens(t, "stacks.add.reject", settingsRows, foldersBackend, addKeys(rejectDir)...)
}

func TestGoldenStacksAddDiscardQuestion(t *testing.T) {
	goldenScreens(t, "stacks.add.discard", settingsRows, settingsBackend, seq(toFolders, keys("a"), typed(stackDir), keys("esc"))...)
}

func TestGoldenStacksFetchFailed(t *testing.T) {
	goldenScreens(t, "stacks.fetchfailed", settingsRows, func() *fakeBackend {
		b := foldersBackend()
		b.errs["fetch:"+stackDir] = control.NewError(control.CodeRemote, "cannot reach git.example.test: connection refused")
		return b
	}, seq(toFolders, keys("up", "F", "y"))...)
}

func TestGoldenStacksKeyNone(t *testing.T) {
	goldenScreens(t, "stacks.key.none", settingsRows, oneFolderBackend, toKey...)
}

func TestGoldenStacksKeyShown(t *testing.T) {
	goldenScreens(t, "stacks.key.shown", settingsRows, oneFolderBackend, seq(toKey, keys("e", "right", "f2", "enter"))...)
}

func TestGoldenStacksKeyForm(t *testing.T) {
	goldenScreens(t, "stacks.key.form", settingsRows, oneFolderBackend, seq(toKey, keys("e"))...)
}

func TestGoldenStacksKeyStoreForm(t *testing.T) {
	goldenScreens(t, "stacks.key.store", settingsRows, oneFolderBackend, seq(toKey, keys("e", "f2"), typed(hiddenA))...)
}

func TestGoldenStacksKeyStoreProblem(t *testing.T) {
	goldenScreens(t, "stacks.key.problem", settingsRows, oneFolderBackend, seq(toKey, keys("e", "f2", "f2"))...)
}

func TestGoldenStacksKeyReplaceQuestion(t *testing.T) {
	goldenScreens(t, "stacks.key.replace", settingsRows, func() *fakeBackend {
		b := oneFolderBackend()
		b.key = &control.KeyResult{PublicKey: "ssh-ed25519 AAAAexamplekey qdeploy", Fingerprint: fingerprint}
		return b
	}, seq(toKey, keys("e", "f2"), typed(hiddenA), keys("f2"))...)
}
