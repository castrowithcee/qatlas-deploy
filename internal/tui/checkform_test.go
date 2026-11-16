package tui

import (
	"reflect"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The ways to the check form of the first stack and from there to its fields: container check, HTTP check,
// address, status and timeout.
var (
	toCheckForm = keys("enter", "k")
	toHTTP      = seq(toCheckForm, keys("down"))
	toURL       = seq(toCheckForm, keys("down", "down"))
	toStatus    = seq(toCheckForm, keys("down", "down", "down"))
	toTimeout   = seq(toCheckForm, keys("down", "down", "down", "down"))
)

func backspaces(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "backspace"
	}
	return out
}

func TestCheckFormShowsTheStoredValues(t *testing.T) {
	b := updateBackend()
	s := b.stackSettings["demo-stack"]
	f := false
	s.Check = &control.StackCheckSettings{Containers: &f, HTTP: &control.StackHTTPSettings{URL: "http://localhost:8080/health", Status: 204, Timeout: "30s"}}
	b.stackSettings["demo-stack"] = s
	m := newSettingsModel(t, b)
	send(m, toCheckForm...)
	contains(t, m, "Check of a stack", "demo-stack", "Container check", "off", "HTTP check", "on", "http://localhost:8080/health", "204", "30s")
}

func TestCheckFormOfAStackWithoutSettingsShowsTheDefaults(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	send(m, toCheckForm...)
	contains(t, m, "Container check", "on", "200", "10s")
}

func TestCheckSavedAsShownCommitsNothingNew(t *testing.T) {
	b := updateBackend()
	m := newSettingsModel(t, b)
	send(m, toCheckForm...)
	send(m, "f2")
	contains(t, m, "Save the check of the stack demo-stack?", "pushed to its remote", "container check: on",
		"HTTP check: http://localhost:8080/health answers 200")
	if len(b.settingsSets) != 0 {
		t.Fatal("saved before the confirmation")
	}
	send(m, "y")
	if len(b.settingsSets) != 1 {
		t.Fatalf("sets %+v", b.settingsSets)
	}
	if want := updateBackend().stackSettings["demo-stack"]; !reflect.DeepEqual(b.settingsSets[0].Settings, want) {
		t.Fatalf("set %+v\nwant %+v", b.settingsSets[0].Settings, want)
	}
	contains(t, m, "was already set", "nothing was committed")
}

func TestCheckSavesTheChosenValuesAndLeavesTheOtherSections(t *testing.T) {
	b := updateBackend()
	m := newSettingsModel(t, b)
	send(m, toCheckForm...)
	send(m, "right") // container check off
	send(m, "down", "down")
	send(m, backspaces(40)...)
	send(m, typed("https://example.test/up")...)
	send(m, "down", "backspace", "backspace", "backspace", "2", "0", "4", "down", "backspace", "backspace", "backspace", "3", "0", "s")
	send(m, "f2", "y")
	if len(b.settingsSets) != 1 {
		t.Fatalf("sets %+v", b.settingsSets)
	}
	off := false
	want := updateBackend().stackSettings["demo-stack"]
	want.Check = &control.StackCheckSettings{Containers: &off, HTTP: &control.StackHTTPSettings{URL: "https://example.test/up", Status: 204, Timeout: "30s"}}
	if got := b.settingsSets[0].Settings; !reflect.DeepEqual(got, want) {
		t.Fatalf("set %+v %+v\nwant %+v %+v", *got.Check.Containers, *got.Check.HTTP, *want.Check.Containers, *want.Check.HTTP)
	}
	contains(t, m, "Check of the stack demo-stack saved")
	if m.set.form != nil {
		t.Fatal("the form stays open after the save")
	}
}

func TestCheckWithTheHTTPCheckOffDropsTheProbe(t *testing.T) {
	b := updateBackend()
	m := newSettingsModel(t, b)
	send(m, toHTTP...)
	send(m, "right", "f2")
	contains(t, m, "HTTP check: off")
	send(m, "y")
	if got := b.settingsSets[0].Settings; got.Check != nil || got.Backup == nil {
		t.Fatalf("set %+v", got)
	}
}

func TestCheckRefusesLocalErrorsAtTheField(t *testing.T) {
	for name, tc := range map[string]struct {
		to    []string
		edit  []string
		want  string
		field string
	}{
		"status low":   {toStatus, append(backspaces(3), "9", "9"), "whole number from 100 to 599", "status"},
		"status high":  {toStatus, append(backspaces(3), "6", "0", "0"), "whole number from 100 to 599", "status"},
		"status text":  {toStatus, append(backspaces(3), "x"), "whole number from 100 to 599", "status"},
		"timeout low":  {toTimeout, append(backspaces(3), "5", "0", "0", "m", "s"), "duration from 1s to 10m", "timeout"},
		"timeout high": {toTimeout, append(backspaces(3), "1", "1", "m"), "duration from 1s to 10m", "timeout"},
		"timeout text": {toTimeout, append(backspaces(3), "s", "o", "o", "n"), "duration from 1s to 10m", "timeout"},
		"url empty":    {toURL, backspaces(40), "needs an address", "url"},
		"url relative": {toURL, append(backspaces(40), typed("/health")...), "absolute http or https URL", "url"},
		"url scheme":   {toURL, append(backspaces(40), typed("ftp://example.test/")...), "absolute http or https URL", "url"},
	} {
		t.Run(name, func(t *testing.T) {
			b := updateBackend()
			m := newSettingsModel(t, b)
			send(m, tc.to...)
			send(m, tc.edit...)
			send(m, "f2")
			contains(t, m, tc.want)
			if m.modal != nil || len(b.settingsSets) != 0 || string(m.set.form.form.Focused()) != tc.field {
				t.Fatalf("a refused form reached the confirmation or the service, or the problem is not at %s", tc.field)
			}
		})
	}
}

func TestCheckIgnoresTheProbeFieldsWhileTheProbeIsOff(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toStatus...)
	send(m, backspaces(3)...)
	send(m, "f2")
	contains(t, m, "HTTP check: off")
	send(m, "y")
	if len(b.settingsSets) != 1 || b.settingsSets[0].Settings.Check != nil {
		t.Fatalf("sets %+v", b.settingsSets)
	}
}

func TestCheckRefusesWhatTheServiceRefusesAtTheField(t *testing.T) {
	for msg, field := range map[string]string{
		"stack settings: check.http.url: must be an absolute http or https URL (got \"x\")":    "url",
		"stack settings: check.http.status: must be between 100 and 599 (got 7)":               "status",
		"stack settings: check.http.timeout: must be a duration between 1s and 10m (got \"\")": "timeout",
		"stack settings: check.containers: not a boolean":                                      "containers",
	} {
		b := updateBackend()
		b.errs["settingsset:demo-stack"] = control.NewError(control.CodeInvalid, msg)
		m := newSettingsModel(t, b)
		send(m, toCheckForm...)
		send(m, "f2", "y")
		contains(t, m, "the service refused the request", "check.")
		if m.set.form == nil || string(m.set.form.form.Focused()) != field {
			t.Fatalf("%s: the problem is not at the %s field", msg, field)
		}
	}
}

func TestCheckShowsAnotherServiceRefusalAboveTheForm(t *testing.T) {
	b := updateBackend()
	b.errs["settingsset:demo-stack"] = control.NewError(control.CodeInvalid, "stack settings: update.mode: unknown")
	m := newSettingsModel(t, b)
	send(m, toCheckForm...)
	send(m, "f2", "y")
	if m.set.form == nil || m.set.form.form.Focused() != "containers" {
		t.Fatal("an unrelated refusal moved to a field")
	}
	contains(t, m, "update.mode")
}

func TestDetailOffersTheCheckFormAndTheRemoval(t *testing.T) {
	m := newSettingsModel(t, updateBackend())
	send(m, "enter")
	contains(t, m, "k checks", "E clear settings")
}

func TestRemovingTheSettingsCallsTheServiceOnlyAfterTheQuestion(t *testing.T) {
	b := updateBackend()
	m := newSettingsModel(t, b)
	send(m, "enter", "E")
	contains(t, m, "Remove all settings of the stack demo-stack?", "deletes the settings file", "Afterwards")
	if m.modal == nil || m.modal.dialog == nil {
		t.Fatal("no question")
	}
	if len(b.settingsRemoves) != 0 {
		t.Fatal("removed before the answer")
	}
	send(m, "y")
	if !reflect.DeepEqual(b.settingsRemoves, []string{"demo-stack"}) {
		t.Fatalf("removes %v", b.settingsRemoves)
	}
	if _, ok := b.stackSettings["demo-stack"]; ok {
		t.Fatal("the settings are still there")
	}
	contains(t, m, "Settings of the stack demo-stack removed", "deploy only")
}

func TestCancellingTheRemovalChangesNothing(t *testing.T) {
	for _, no := range []string{"n", "esc", "enter"} {
		b := updateBackend()
		m := newSettingsModel(t, b)
		send(m, "enter", "E", no)
		if m.modal != nil || len(b.settingsRemoves) != 0 || len(b.stackSettings) != 1 {
			t.Fatalf("%s: modal %v, removes %v", no, m.modal, b.settingsRemoves)
		}
	}
}

func TestRemovingNoSettingsSaysSoPlainly(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, "enter", "E", "y")
	contains(t, m, "The stack demo-stack has no settings", "already deploy only")
	if len(b.settingsRemoves) != 0 || m.modal != nil {
		t.Fatalf("removes %v", b.settingsRemoves)
	}
}

func TestAClickOnTheHintsOpensTheCheckFormAndTheRemovalLikeTheKeys(t *testing.T) {
	b := updateBackend()
	m := newSettingsModel(t, b)
	m.mouseOn = true
	send(m, "enter")
	x, y := cellOf(t, m, "k checks")
	clickAt(m, x, y)
	if m.set.form == nil || m.set.form.kind != formCheck {
		t.Fatal("the click did not open the check form")
	}
	send(m, "esc")
	m.render()
	x, y = cellOf(t, m, "E clear settings")
	clickAt(m, x, y)
	if m.modal == nil {
		t.Fatal("the click did not open the question")
	}
	if len(b.settingsRemoves) != 0 {
		t.Fatal("the click removed without the question")
	}
}
