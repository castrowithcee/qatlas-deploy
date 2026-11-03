package tui

import (
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The ways to a change in the detail of the first stack.
var (
	toSecretForm = keys("enter", "a")
	toSecretPick = keys("enter", "x")
	toRestore    = keys("enter", "R")
)

func secretFormKeys() []string {
	return seq(toSecretForm, typed("SERVICE_TOKEN"), keys("down"), typed(hiddenA))
}

const (
	olderBackup  = "20261023T031000Z-4567cdef"
	newestBackup = "20261030T140100Z-0123abcd"
	brokenBackup = "20261016T031000Z-89abcdef"
)

func TestDetailListsTheKeysOfTheSecretsButNoValue(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	send(m, "enter")
	contains(t, m, "Secrets", "API_SIGNING_KEY", "DB_PASSWORD")
}

func TestSecretIsSetAfterConfirmationAndNeverShown(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	hidden := []string{hiddenA}
	send(m, toSecretForm...)
	sendHidden(t, m, hidden, typed("SERVICE_TOKEN")...)
	sendHidden(t, m, hidden, "down")
	sendHidden(t, m, hidden, typed(hiddenA)...)
	contains(t, m, "************************")
	sendHidden(t, m, hidden, "f2")
	contains(t, m, "SERVICE_TOKEN", "demo-stack")
	if len(b.secretSets) != 0 {
		t.Fatal("stored before the confirmation")
	}
	// Cancel is the default: enter changes nothing and keeps the form.
	sendHidden(t, m, hidden, "enter")
	if len(b.secretSets) != 0 || m.set.form == nil {
		t.Fatalf("sets %v, form %v", b.secretSets, m.set.form)
	}
	sendHidden(t, m, hidden, "f2", "y")
	if want := []control.SecretSetArgs{{Stack: "demo-stack", Key: "SERVICE_TOKEN", Value: hiddenA}}; !slices.Equal(b.secretSets, want) {
		t.Fatalf("sets %v", b.secretSets)
	}
	contains(t, m, "Secret SERVICE_TOKEN set for the stack demo-stack")
	sendHidden(t, m, hidden, "enter")
	if m.set.form != nil {
		t.Fatal("the form stays open after the change")
	}
	contains(t, m, "SERVICE_TOKEN", "API_SIGNING_KEY")
}

func TestSecretFormRefusesWhatTheServiceWouldRefuse(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toSecretForm...)
	send(m, typed("1bad name")...)
	send(m, "down")
	send(m, typed("abc")...)
	send(m, "f2")
	contains(t, m, "A name has up to 128 characters", "A value of 4 to 8192 characters")
	if m.modal != nil || len(b.actions()) != 0 {
		t.Fatal("a refused form reached the confirmation or the service")
	}
}

func TestSecretFailureIsShownWithoutTheValue(t *testing.T) {
	b := settingsBackend()
	b.errs["secretset:demo-stack/SERVICE_TOKEN"] = control.NewError(control.CodeKeyExists, "the key stands in .env")
	m := newSettingsModel(t, b)
	hidden := []string{hiddenA}
	send(m, secretFormKeys()...)
	sendHidden(t, m, hidden, "f2", "y")
	contains(t, m, "the key stands in .env")
	if m.set.form == nil {
		t.Fatal("the form was closed by a failure")
	}
}

func TestSecretIsDeletedAfterConfirmation(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toSecretPick...)
	contains(t, m, "Delete a secret", "API_SIGNING_KEY", "DB_PASSWORD")
	send(m, "down", "enter")
	contains(t, m, "DB_PASSWORD", "demo-stack")
	send(m, "n")
	if slices.Contains(b.actions(), "secretdelete:demo-stack/DB_PASSWORD") {
		t.Fatal("deleted without the confirmation")
	}
	send(m, "x", "down", "enter", "y")
	if got := b.actions(); !slices.Contains(got, "secretdelete:demo-stack/DB_PASSWORD") {
		t.Fatalf("calls = %v", got)
	}
	send(m, "enter")
	if m.render(); slices.Contains(m.detail.secrets.Keys, "DB_PASSWORD") {
		t.Fatalf("keys = %v", m.detail.secrets.Keys)
	}
}

func TestSecretChoiceIsLeftWithEscape(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	send(m, toSecretPick...)
	send(m, "esc")
	if m.pick != nil || m.area != areaDetail {
		t.Fatalf("pick %v area %s", m.pick, m.area)
	}
	send(m, "x", "1")
	if m.pick != nil || m.area != areaStacks {
		t.Fatal("the choice stays open after leaving the area")
	}
}

func TestBackupRestoreOffersOnlyOlderCompleteBackups(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toRestore...)
	contains(t, m, "Restore a backup", olderBackup)
	screen := m.render()
	for _, id := range []string{newestBackup, brokenBackup} {
		if contains2(screen, id) {
			t.Fatalf("%s is offered:\n%s", id, screen)
		}
	}
}

func contains2(screen, want string) bool {
	for i := 0; i+len(want) <= len(screen); i++ {
		if screen[i:i+len(want)] == want {
			return true
		}
	}
	return false
}

func TestBackupIsRestoredAfterConfirmation(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toRestore...)
	send(m, "enter")
	contains(t, m, olderBackup, "demo-stack", "overwritten")
	send(m, "enter")
	if len(b.restores) != 0 {
		t.Fatal("restored by the default answer")
	}
	send(m, "R", "enter", "y")
	if want := []control.BackupRestoreArgs{{Stack: "demo-stack", ID: olderBackup, Confirm: true}}; !slices.Equal(b.restores, want) {
		t.Fatalf("restores %v", b.restores)
	}
	contains(t, m, "restored")
}

func TestBackupRestoreFailureIsShown(t *testing.T) {
	b := settingsBackend()
	b.errs["restore:demo-stack/"+olderBackup] = control.NewError(control.CodeRunActive, "a run is active")
	m := newSettingsModel(t, b)
	send(m, toRestore...)
	send(m, "enter", "y")
	contains(t, m, "a run is active")
}

func TestRestoreWithoutAnOlderBackupSaysSo(t *testing.T) {
	b := settingsBackend()
	b.backups["demo-stack"] = control.BackupListResult{Backups: b.backups["demo-stack"].Backups[:1]}
	m := newSettingsModel(t, b)
	send(m, toRestore...)
	contains(t, m, "no older backup")
	if m.pick != nil {
		t.Fatal("a choice without rows was opened")
	}
}

func TestDetailChangesKeysDoNothingInStacks(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, "a", "x", "R")
	if m.modal != nil || m.pick != nil || m.set.form != nil || len(b.actions()) != 0 {
		t.Fatal("a detail key acted in the Stacks area")
	}
}
