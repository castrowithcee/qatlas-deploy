package tui

import (
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The way to the update rules form of the first stack, and from there to the fields: mode, ceiling, major
// jump, observation, tag filters and changelog repositories.
var (
	toUpdateForm = keys("enter", "e")
	toObserve    = seq(toUpdateForm, keys("down", "down", "down"))
	toTags       = seq(toUpdateForm, keys("down", "down", "down", "down"))
	toChangelog  = seq(toTags, keys("down"))
)

func intp(n int) *int { return &n }

// updateBackend has a remote stack folder and stored settings for the first stack that hold more than the
// update rules.
func updateBackend() *fakeBackend {
	b := settingsBackend()
	b.status.Engine.Sources = []control.SourceStatus{{ID: "src1", Path: "/srv/stacks", Kind: control.SourceKindRemote, URL: "ssh://git@example.test/ops/stacks.git"}}
	for i := range b.stacks.Stacks {
		b.stacks.Stacks[i].Source = "src1"
	}
	b.stackSettings = map[string]control.StackSettings{"demo-stack": {
		Version: 1,
		Compose: &control.StackComposeSettings{Files: []string{"compose.yaml", "compose.prod.yaml"}},
		Update: &control.StackUpdateSettings{Mode: "automatic", Ceiling: "patch", Major: "direct", Observe: intp(15),
			Tags: map[string]string{"APP_VERSION": `^1\.[0-9.]+$`}, Changelog: map[string]string{"APP_VERSION": "example/app"}},
		Backup: &control.StackBackupSettings{Paths: []string{"data/db"}, Retention: &control.StackRetentionSettings{Keep: 3}},
		Check:  &control.StackCheckSettings{HTTP: &control.StackHTTPSettings{URL: "http://localhost:8080/health"}},
	}}
	return b
}

func TestUpdateFormShowsTheStoredRules(t *testing.T) {
	m := newSettingsModel(t, updateBackend())
	send(m, toUpdateForm...)
	contains(t, m, "Update rules of a stack", "demo-stack", "automatic", "patch", "direct", "15", `APP_VERSION → ^1\.[0-9.]+$`, "APP_VERSION → example/app")
}

func TestUpdateFormOfAStackWithoutSettingsShowsTheDefaults(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	send(m, toUpdateForm...)
	contains(t, m, "manual", "minor", "stepwise", "60", "none")
}

func TestUpdateRulesAreSavedAfterConfirmationAndKeepTheRest(t *testing.T) {
	b := updateBackend()
	m := newSettingsModel(t, b)
	send(m, toUpdateForm...)
	// Mode manual, ceiling major, major jump stepwise, observation off.
	send(m, "enter", "down", "enter", "down", "enter", "down", "down", "enter", "down", "enter", "up", "enter")
	send(m, "down", "backspace", "backspace", "0")
	send(m, "f2")
	contains(t, m, "Save the update rules of the stack demo-stack?", "pushed to its remote", "mode: manual, ceiling: major, major jump: stepwise", "observation: off")
	if len(b.settingsSets) != 0 {
		t.Fatal("saved before the confirmation")
	}
	send(m, "y")
	if len(b.settingsSets) != 1 {
		t.Fatalf("sets %+v", b.settingsSets)
	}
	got := b.settingsSets[0]
	old := updateBackend().stackSettings["demo-stack"]
	want := old
	want.Update = &control.StackUpdateSettings{Mode: "manual", Ceiling: "major", Major: "stepwise", Observe: intp(0),
		Tags: old.Update.Tags, Changelog: old.Update.Changelog}
	if got.Stack != "demo-stack" || !reflect.DeepEqual(got.Settings, want) {
		t.Fatalf("set %+v\nwant %+v", got.Settings.Update, want.Update)
	}
	contains(t, m, "Update rules of the stack demo-stack saved")
	if m.set.form != nil {
		t.Fatal("the form stays open after the save")
	}
}

func TestUpdateSavedWithoutChangeCommitsNothing(t *testing.T) {
	m := newSettingsModel(t, updateBackend())
	send(m, toUpdateForm...)
	send(m, "f2", "y")
	contains(t, m, "already set; nothing was committed")
}

func TestUpdateConfirmationNamesTheKindOfTheStackFolder(t *testing.T) {
	b := updateBackend()
	b.status.Engine.Sources[0].Kind = control.SourceKindLocal
	m := newSettingsModel(t, b)
	send(m, toUpdateForm...)
	send(m, "f2")
	contains(t, m, "in the local stack")
	if screen := m.render(); strings.Contains(screen, "pushed") {
		t.Fatalf("a local folder pushes nothing:\n%s", screen)
	}
}

func TestUpdateFormRefusesALocalMistakeAtTheField(t *testing.T) {
	b := updateBackend()
	m := newSettingsModel(t, b)
	send(m, toObserve...)
	send(m, "backspace", "backspace", "9", "9", "9", "9", "f2")
	contains(t, m, "whole number of minutes from 0 to 1440")
	if m.modal != nil || len(b.settingsSets) != 0 {
		t.Fatal("a refused form reached the confirmation or the service")
	}
}

func TestUpdateFormRefusesWhatTheServiceRefusesAtTheField(t *testing.T) {
	b := updateBackend()
	b.errs["settingsset:demo-stack"] = control.NewError(control.CodeInvalid, "stack settings: update.tags.APP_VERSION: not an image variable of the stack")
	m := newSettingsModel(t, b)
	send(m, toUpdateForm...)
	send(m, "f2", "y")
	contains(t, m, "update.tags.APP_VERSION: not an image")
	if m.set.form == nil || m.set.form.form.Focused() != "tags" {
		t.Fatalf("the problem is not at the tag field: %+v", m.set.form)
	}
}

func TestUpdateFormShowsOtherServiceErrorsAtTheForm(t *testing.T) {
	b := updateBackend()
	b.errs["settingsset:demo-stack"] = control.NewError(control.CodeRemote, "push rejected")
	m := newSettingsModel(t, b)
	send(m, toUpdateForm...)
	send(m, "f2", "y")
	contains(t, m, "push rejected", "Correct the form")
	if m.set.form == nil {
		t.Fatal("the form closed on a failure")
	}
}

func TestUpdateFormReadFailureIsShown(t *testing.T) {
	b := updateBackend()
	b.errs["settings:demo-stack"] = errServiceBusy()
	m := newSettingsModel(t, b)
	send(m, toUpdateForm...)
	contains(t, m, "the service is busy")
	if m.set.form != nil {
		t.Fatal("a form opened without the stored rules")
	}
}

func TestEditorShowsTheEntriesAndTheVariablesOfTheStack(t *testing.T) {
	m := newSettingsModel(t, updateBackend())
	send(m, toTags...)
	send(m, "enter")
	contains(t, m, "Tag filters", "VARIABLE", `APP_VERSION  ^1\.[0-9.]+$`, "a add", "e edit", "x remove")
	send(m, "e")
	contains(t, m, "APP_VERSION", `^1\.[0-9.]+$`)
}

func TestEditorAddsChangesAndRemovesPairs(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	send(m, toTags...)
	send(m, "enter", "a", "down")
	send(m, typed("^2")...)
	send(m, "f2")
	contains(t, m, "APP_VERSION  ^2")
	// Change the pattern of the entry.
	send(m, "e", "down", "backspace", "3")
	send(m, "f2")
	contains(t, m, "APP_VERSION  ^3")
	// Leaving the editor gives the summary to the field of the form.
	send(m, "esc")
	contains(t, m, "APP_VERSION → ^3")
	send(m, "enter", "x")
	contains(t, m, "No tag filters")
	send(m, "esc")
	contains(t, m, "none")
}

func TestEditorRefusesWhatTheRulesRefuse(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	send(m, toTags...)
	send(m, "enter", "a", "down")
	send(m, "(")
	send(m, "f2")
	contains(t, m, "Not a valid regular expression")
	send(m, "esc", "esc", "down")
	send(m, "enter", "a", "down")
	send(m, typed("nowhere")...)
	send(m, "f2")
	contains(t, m, "Use a GitHub repository as owner/name.")
	send(m, "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace")
	send(m, typed("example/app")...)
	send(m, "f2")
	contains(t, m, "APP_VERSION  example/app")
}

func TestEditorRefusesAKeyTwice(t *testing.T) {
	m := newSettingsModel(t, updateBackend())
	send(m, toTags...)
	send(m, "enter", "a", "down")
	send(m, typed("^1")...)
	send(m, "f2")
	contains(t, m, "APP_VERSION is already in the list.")
}

// A plain list of values, as the backup form will use it.
func TestEditorKeepsAPlainList(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	e := m.newListEditor(listEditorSpec{field: "paths", title: textTitleTags, valueLabel: textColPattern, valueHint: textHintPattern,
		intro: textTagsIntro, empty: textTagsEmpty}, []listEntry{{key: "data/db"}})
	if e.summary() != "data/db" {
		t.Fatalf("summary %q", e.summary())
	}
	m.openEntry(e, -1)
	e.entry.SetProblems(nil)
	if err := e.entry.SetValue("key", formText("data/files")); err != nil {
		t.Fatal(err)
	}
	m.submitEntry(e)
	if e.entry != nil || e.summary() != "data/db, data/files" {
		t.Fatalf("summary %q", e.summary())
	}
	m.openEntry(e, 0)
	_ = e.entry.SetValue("key", formText("data/db2"))
	m.submitEntry(e)
	if e.summary() != "data/db2, data/files" {
		t.Fatalf("summary %q", e.summary())
	}
	m.openEntry(e, -1)
	_ = e.entry.SetValue("key", formText("data/files"))
	m.submitEntry(e)
	if e.entry == nil {
		t.Fatal("a value twice was taken")
	}
}

func formText(s string) kittui.FormValue { return kittui.FormValue{Text: s} }
