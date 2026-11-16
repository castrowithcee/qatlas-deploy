package tui

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The ways to the backup and the compose files form of the first stack, and from there to the fields: data
// directories, volumes, command, keep and maximum age.
var (
	toBackupForm  = keys("enter", "b")
	toVolumes     = seq(toBackupForm, keys("down"))
	toCommand     = seq(toBackupForm, keys("down", "down"))
	toKeep        = seq(toBackupForm, keys("down", "down", "down"))
	toMaxAge      = seq(toBackupForm, keys("down", "down", "down", "down"))
	toComposeForm = keys("enter", "C")
)

// backupBackend has stored settings that hold a backup with a volume the compose files no longer have, and
// compose files, next to the other sections.
func backupBackend() *fakeBackend {
	b := updateBackend()
	b.stacks.Stacks[0].Volumes = []string{"app-data", "db-data"}
	s := b.stackSettings["demo-stack"]
	s.Backup = &control.StackBackupSettings{Paths: []string{"data/db"}, Volumes: []string{"app-data", "old-volume"},
		Command: []string{"pg_dump", "--clean"}, Retention: &control.StackRetentionSettings{Keep: 3, MaxAgeDays: 30}}
	b.stackSettings["demo-stack"] = s
	return b
}

func TestBackupFormShowsTheStoredValues(t *testing.T) {
	m := newSettingsModel(t, backupBackend())
	send(m, toBackupForm...)
	contains(t, m, "Backup of a stack", "demo-stack", "data/db", "app-data", "old-volume (!)", "db-data", "pg_dump, --clean", "3", "30")
}

func TestBackupFormOfAStackWithoutSettingsShowsTheDefaults(t *testing.T) {
	b := settingsBackend()
	b.stacks.Stacks[0].Volumes = []string{"app-data"}
	m := newSettingsModel(t, b)
	send(m, toBackupForm...)
	contains(t, m, "[ ] app-data", "7")
}

func TestBackupFormOfAStackWithoutVolumesSaysSo(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	send(m, toBackupForm...)
	contains(t, m, "none in the compose files")
}

func TestBackupSavedAsShownKeepsTheUnknownVolumeAndTheRest(t *testing.T) {
	b := backupBackend()
	m := newSettingsModel(t, b)
	send(m, toBackupForm...)
	send(m, "f2")
	contains(t, m, "Save the backup of the stack demo-stack?", "pushed to its remote", "data directories: data/db",
		"volumes: app-data, old-volume", "command: pg_dump --clean", "keep the newest 3", "remove after 30 days")
	if len(b.settingsSets) != 0 {
		t.Fatal("saved before the confirmation")
	}
	send(m, "y")
	if len(b.settingsSets) != 1 {
		t.Fatalf("sets %+v", b.settingsSets)
	}
	if want := backupBackend().stackSettings["demo-stack"]; !reflect.DeepEqual(b.settingsSets[0].Settings, want) {
		t.Fatalf("set %+v\nwant %+v", b.settingsSets[0].Settings, want)
	}
	contains(t, m, "was already set", "nothing was committed")
}

func TestBackupSavesTheChosenValuesAndLeavesTheOtherSections(t *testing.T) {
	b := backupBackend()
	m := newSettingsModel(t, b)
	send(m, toVolumes...)
	// Take old-volume out, tick db-data.
	send(m, "right", "space", "right", "space")
	contains(t, m, "[x] app-data", "[ ] old-volume (!)", "[x] db-data")
	send(m, "down", "down", "backspace", "5", "down", "backspace", "backspace", "9")
	send(m, "f2", "y")
	if len(b.settingsSets) != 1 {
		t.Fatalf("sets %+v", b.settingsSets)
	}
	got := b.settingsSets[0].Settings
	old := backupBackend().stackSettings["demo-stack"]
	want := old
	want.Backup = &control.StackBackupSettings{Paths: []string{"data/db"}, Volumes: []string{"app-data", "db-data"},
		Command: []string{"pg_dump", "--clean"}, Retention: &control.StackRetentionSettings{Keep: 5, MaxAgeDays: 9}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("set %+v\nwant %+v", got.Backup, want.Backup)
	}
	contains(t, m, "Backup of the stack demo-stack saved")
	if m.set.form != nil {
		t.Fatal("the form stays open after the save")
	}
}

func TestBackupEditsTheListsOfPathsAndCommand(t *testing.T) {
	b := backupBackend()
	m := newSettingsModel(t, b)
	send(m, toBackupForm...)
	send(m, "enter", "a")
	send(m, typed("data/files")...)
	send(m, "f2", "esc")
	contains(t, m, "data/db, data/files")
	send(m, "down", "down", "enter", "a")
	send(m, typed("--clean")...)
	send(m, "f2", "esc")
	contains(t, m, "pg_dump, --clean, --clean")
	send(m, "f2", "y")
	got := b.settingsSets[0].Settings.Backup
	if !slices.Equal(got.Paths, []string{"data/db", "data/files"}) || !slices.Equal(got.Command, []string{"pg_dump", "--clean", "--clean"}) {
		t.Fatalf("set %+v", got)
	}
}

func TestBackupWithoutPathVolumeOrCommandIsRefusedAtTheField(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toBackupForm...)
	send(m, "f2")
	contains(t, m, "needs at least one data directory, volume or command")
	if m.modal != nil || len(b.settingsSets) != 0 || m.set.form.form.Focused() != "paths" {
		t.Fatal("a refused form reached the confirmation or the service")
	}
}

func TestBackupRetentionBelowOneIsRefusedAtTheField(t *testing.T) {
	for name, tc := range map[string]struct {
		to   []string
		want string
	}{
		"keep":   {toKeep, "Keep is a whole number of at least 1"},
		"maxage": {toMaxAge, "maximum age is a whole number of days"},
	} {
		t.Run(name, func(t *testing.T) {
			b := backupBackend()
			m := newSettingsModel(t, b)
			send(m, tc.to...)
			send(m, "backspace", "backspace", "0", "f2")
			contains(t, m, tc.want)
			if m.modal != nil || len(b.settingsSets) != 0 {
				t.Fatal("a refused form reached the confirmation or the service")
			}
		})
	}
}

func TestBackupRefusesAStoredPathOutsideTheStackFolder(t *testing.T) {
	b := backupBackend()
	s := b.stackSettings["demo-stack"]
	s.Backup.Paths = []string{"../data"}
	b.stackSettings["demo-stack"] = s
	m := newSettingsModel(t, b)
	send(m, toBackupForm...)
	send(m, "f2")
	contains(t, m, "../data", "clean path relative to the stack folder")
	if m.modal != nil {
		t.Fatal("a refused form reached the confirmation")
	}
}

func TestBackupRefusesWhatTheServiceRefusesAtTheField(t *testing.T) {
	for msg, field := range map[string]string{
		"stack settings: backup.volumes[1]: \"x\" is not a volume name":         "volumes",
		"stack settings: backup.command[0]: the program must not be empty":      "command",
		"stack settings: backup.retention.keep: must be at least 1":             "keep",
		"stack settings: backup.retention.max_age_days: must be at least 1":     "maxage",
		"stack settings: backup.paths[0]: absolute paths are not allowed":       "paths",
		"stack settings: backup: needs at least one of paths, volumes, command": "paths",
	} {
		b := backupBackend()
		b.errs["settingsset:demo-stack"] = control.NewError(control.CodeInvalid, msg)
		m := newSettingsModel(t, b)
		send(m, toBackupForm...)
		send(m, "f2", "y")
		contains(t, m, "the service refused the request")
		if m.set.form == nil || string(m.set.form.form.Focused()) != field {
			t.Fatalf("%s: the problem is not at the %s field", msg, field)
		}
	}
}

func TestComposeFormShowsTheStoredFiles(t *testing.T) {
	m := newSettingsModel(t, backupBackend())
	send(m, toComposeForm...)
	contains(t, m, "Compose files of a stack", "demo-stack", "compose.yaml, compose.prod.yaml")
}

func TestComposeFilesAreMovedAndSavedAfterConfirmation(t *testing.T) {
	b := backupBackend()
	m := newSettingsModel(t, b)
	send(m, toComposeForm...)
	send(m, "enter", "J", "esc")
	contains(t, m, "compose.prod.yaml, compose.yaml")
	send(m, "f2")
	contains(t, m, "Save the compose files of the stack demo-stack?", "1. compose.prod.yaml", "2. compose.yaml", "next deployment")
	if len(b.settingsSets) != 0 {
		t.Fatal("saved before the confirmation")
	}
	send(m, "y")
	if len(b.settingsSets) != 1 {
		t.Fatalf("sets %+v", b.settingsSets)
	}
	want := backupBackend().stackSettings["demo-stack"]
	want.Compose = &control.StackComposeSettings{Files: []string{"compose.prod.yaml", "compose.yaml"}}
	if got := b.settingsSets[0].Settings; !reflect.DeepEqual(got, want) {
		t.Fatalf("set %+v\nwant %+v", got, want)
	}
	contains(t, m, "Compose files of the stack demo-stack saved")
}

func TestComposeFilesTakeTheSectionOutWhenEmptied(t *testing.T) {
	b := backupBackend()
	m := newSettingsModel(t, b)
	send(m, toComposeForm...)
	send(m, "enter", "x", "x", "esc", "f2")
	contains(t, m, "the files Compose finds itself")
	send(m, "y")
	if got := b.settingsSets[0].Settings; got.Compose != nil || got.Backup == nil {
		t.Fatalf("set %+v", got)
	}
}

func TestComposeMoreThanSixteenFilesAreRefusedAtTheField(t *testing.T) {
	b := backupBackend()
	s := b.stackSettings["demo-stack"]
	s.Compose.Files = nil
	for i := range 17 {
		s.Compose.Files = append(s.Compose.Files, fmt.Sprintf("compose.%d.yaml", i))
	}
	b.stackSettings["demo-stack"] = s
	m := newSettingsModel(t, b)
	send(m, toComposeForm...)
	send(m, "f2")
	contains(t, m, "at most 16 compose files")
	if m.modal != nil || len(b.settingsSets) != 0 || m.set.form.form.Focused() != "files" {
		t.Fatal("a refused form reached the confirmation or the service")
	}
}

func TestComposeRefusesAFileOnTheEntryForm(t *testing.T) {
	m := newSettingsModel(t, backupBackend())
	send(m, toComposeForm...)
	send(m, "enter", "a")
	send(m, typed("-bad.yaml")...)
	send(m, "f2")
	contains(t, m, "must not start with a dash")
	send(m, "esc")
	send(m, "a")
	send(m, typed("../x.yaml")...)
	send(m, "f2")
	contains(t, m, "clean path relative to the stack folder")
}

func TestComposeRefusesWhatTheServiceRefusesAtTheField(t *testing.T) {
	b := backupBackend()
	b.errs["settingsset:demo-stack"] = control.NewError(control.CodeInvalid, "stack settings: compose.files[1]: \"compose.prod.yaml\" is listed twice")
	m := newSettingsModel(t, b)
	send(m, toComposeForm...)
	send(m, "f2", "y")
	contains(t, m, "compose.files[1]")
	if m.set.form == nil || m.set.form.form.Focused() != "files" {
		t.Fatal("the problem is not at the files field")
	}
}

func TestOrderedListMovesAnEntryAndKeepsTheCursorOnIt(t *testing.T) {
	m := newSettingsModel(t, backupBackend())
	e := m.newListEditor(listEditorSpec{field: "files", title: textTitleComposeFiles, valueLabel: textColFile, valueHint: textHintFile,
		intro: textComposeFilesIntro, empty: textComposeFilesEmpty, ordered: true}, plainEntries([]string{"a", "b", "c"}))
	e.move(0, 1)
	e.move(1, 1)
	if got := e.summary(); got != "b, c, a" {
		t.Fatalf("summary %q", got)
	}
	if sel, _ := e.list.Selected(); sel != "2" {
		t.Fatalf("cursor on %q", sel)
	}
	e.move(2, 1)
	e.move(0, -1)
	if got := e.summary(); got != "b, c, a" {
		t.Fatalf("a move past the end changed the list: %q", got)
	}
}

func TestDetailOffersTheBackupAndComposeForms(t *testing.T) {
	m := newSettingsModel(t, backupBackend())
	send(m, "enter")
	contains(t, m, "b backup", "C compose")
}
