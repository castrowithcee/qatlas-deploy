package tui

import "testing"

// The goldens of the changes in the stack detail. The masked value of the secret form is typed in; the goldens
// show it hidden, and goldenScreens fails if it shows.

const detailEditRows = 24

func TestGoldenDetailSecretForm(t *testing.T) {
	goldenScreens(t, "detail.secret.form", detailEditRows, settingsBackend, secretFormKeys()...)
}

func TestGoldenDetailSecretFormProblem(t *testing.T) {
	goldenScreens(t, "detail.secret.problem", detailEditRows+6, settingsBackend, seq(toSecretForm, keys("f2"))...)
}

func TestGoldenDetailSecretQuestion(t *testing.T) {
	goldenScreens(t, "detail.secret.confirm", detailEditRows, settingsBackend, seq(secretFormKeys(), keys("f2"))...)
}

func TestGoldenDetailSecretDone(t *testing.T) {
	goldenScreens(t, "detail.secret.done", detailEditRows, settingsBackend, seq(secretFormKeys(), keys("f2", "y"))...)
}

func TestGoldenDetailSecretChoice(t *testing.T) {
	goldenScreens(t, "detail.secret.pick", detailEditRows, settingsBackend, toSecretPick...)
}

func TestGoldenDetailSecretDeleteQuestion(t *testing.T) {
	goldenScreens(t, "detail.secret.delete", detailEditRows, settingsBackend, seq(toSecretPick, keys("down", "enter"))...)
}

func TestGoldenDetailRestoreChoice(t *testing.T) {
	goldenScreens(t, "detail.restore.pick", detailEditRows, settingsBackend, toRestore...)
}

func TestGoldenDetailRestoreQuestion(t *testing.T) {
	goldenScreens(t, "detail.restore.confirm", detailEditRows, settingsBackend, seq(toRestore, keys("enter"))...)
}

func TestGoldenDetailRestoreDone(t *testing.T) {
	goldenScreens(t, "detail.restore.done", detailEditRows, settingsBackend, seq(toRestore, keys("enter", "y"))...)
}

// asRoot makes new models run as root and record their creations instead of touching the disk.
func asRoot(t *testing.T) *dirMaker {
	t.Helper()
	oldEuid, oldMake := defaultEuid, defaultMakeDirs
	d := &dirMaker{}
	defaultEuid, defaultMakeDirs = func() int { return 0 }, d.make
	t.Cleanup(func() { defaultEuid, defaultMakeDirs = oldEuid, oldMake })
	return d
}

func TestGoldenDetailDataDirs(t *testing.T) {
	goldenScreens(t, "detail.datadirs", detailEditRows, dataDirBackend, "enter")
}

func TestGoldenDetailDataDirsForm(t *testing.T) {
	asRoot(t)
	goldenScreens(t, "detail.datadirs.form", detailEditRows, dataDirBackend, toDataDirs...)
}

func TestGoldenDetailDataDirsProblem(t *testing.T) {
	asRoot(t)
	goldenScreens(t, "detail.datadirs.problem", detailEditRows+6, dataDirBackend,
		seq(toDataDirs, keys("backspace", "backspace", "x", "f2"))...)
}

func TestGoldenDetailDataDirsQuestion(t *testing.T) {
	asRoot(t)
	goldenScreens(t, "detail.datadirs.confirm", detailEditRows, dataDirBackend, seq(toDataDirs, keys("f2"))...)
}

func TestGoldenDetailDataDirsDone(t *testing.T) {
	asRoot(t)
	goldenScreens(t, "detail.datadirs.done", detailEditRows, dataDirBackend, seq(toDataDirs, keys("f2", "y"))...)
}
