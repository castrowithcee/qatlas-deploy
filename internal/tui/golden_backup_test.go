package tui

import (
	"fmt"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The goldens of the backup and the compose files forms: 80x24 and 120x40, in English and German.

func manyComposeFilesBackend() *fakeBackend {
	b := backupBackend()
	s := b.stackSettings["demo-stack"]
	s.Compose = &control.StackComposeSettings{}
	for i := range 17 {
		s.Compose.Files = append(s.Compose.Files, fmt.Sprintf("compose.%d.yaml", i))
	}
	b.stackSettings["demo-stack"] = s
	return b
}

func TestGoldenBackupForm(t *testing.T) {
	goldenUpdate(t, "detail.backup.form", backupBackend, toBackupForm...)
}

func TestGoldenBackupFormDefaults(t *testing.T) {
	goldenUpdate(t, "detail.backup.defaults", func() *fakeBackend {
		b := settingsBackend()
		b.stacks.Stacks[0].Volumes = []string{"app-data", "db-data"}
		return b
	}, toBackupForm...)
}

func TestGoldenBackupCommandEditor(t *testing.T) {
	goldenUpdate(t, "detail.backup.editor", backupBackend, seq(toCommand, keys("enter"))...)
}

func TestGoldenBackupFieldProblem(t *testing.T) {
	goldenUpdate(t, "detail.backup.problem", settingsBackend, seq(toBackupForm, keys("f2"))...)
}

func TestGoldenBackupKeepProblem(t *testing.T) {
	goldenUpdate(t, "detail.backup.keepproblem", backupBackend, seq(toKeep, keys("backspace", "0", "f2"))...)
}

func TestGoldenBackupServiceProblem(t *testing.T) {
	goldenUpdate(t, "detail.backup.serviceproblem", func() *fakeBackend {
		b := backupBackend()
		b.errs["settingsset:demo-stack"] = control.NewError(control.CodeInvalid, "stack settings: backup.volumes[1]: \"old volume\" is not a volume name")
		return b
	}, seq(toBackupForm, keys("f2", "y"))...)
}

func TestGoldenBackupConfirm(t *testing.T) {
	goldenUpdate(t, "detail.backup.confirm", backupBackend, seq(toBackupForm, keys("f2"))...)
}

func TestGoldenBackupDone(t *testing.T) {
	goldenUpdate(t, "detail.backup.done", backupBackend, seq(toKeep, keys("backspace", "5", "f2", "y"))...)
}

func TestGoldenComposeForm(t *testing.T) {
	goldenUpdate(t, "detail.compose.form", backupBackend, toComposeForm...)
}

func TestGoldenComposeEditor(t *testing.T) {
	goldenUpdate(t, "detail.compose.editor", backupBackend, seq(toComposeForm, keys("enter"))...)
}

func TestGoldenComposeFieldProblem(t *testing.T) {
	goldenUpdate(t, "detail.compose.problem", manyComposeFilesBackend, seq(toComposeForm, keys("f2"))...)
}

func TestGoldenComposeConfirm(t *testing.T) {
	goldenUpdate(t, "detail.compose.confirm", backupBackend, seq(toComposeForm, keys("enter", "J", "esc", "f2"))...)
}

func TestGoldenComposeDone(t *testing.T) {
	goldenUpdate(t, "detail.compose.done", backupBackend, seq(toComposeForm, keys("enter", "J", "esc", "f2", "y"))...)
}
