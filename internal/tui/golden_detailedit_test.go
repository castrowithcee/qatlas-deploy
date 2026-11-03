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
