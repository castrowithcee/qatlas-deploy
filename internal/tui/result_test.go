package tui

import (
	"strings"
	"testing"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

func TestResultWithoutDetailsIsAMessageLine(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	send(m, toCreds...)
	send(m, "x", "y")
	if m.modal != nil {
		t.Fatal("a result without details opened a dialog")
	}
	if m.result == "" || !strings.Contains(m.render(), m.result) {
		t.Fatalf("the message line lacks the result %q:\n%s", m.result, m.render())
	}
	send(m, "down")
	if m.result != "" {
		t.Error("a key did not clear the result")
	}
}

func TestResultWithDetailsIsACalmNotice(t *testing.T) {
	m := newSettingsModel(t, foldersBackend())
	send(m, toFolders...)
	send(m, "F", "y")
	if m.modal == nil {
		t.Fatal("no notice dialog")
	}
	screen := m.render()
	for _, bad := range []string{"The service has made the change.", "!"} {
		if strings.Contains(screen, bad) {
			t.Errorf("the notice contains %q:\n%s", bad, screen)
		}
	}
	if !strings.Contains(screen, "newer commits") {
		t.Errorf("the notice lacks the details:\n%s", screen)
	}
}

func TestFailureStaysBeforeResult(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	m.failure, m.notice, m.result = "boom", "careful", "done"
	if got := m.message(); got.Kind != kittui.MessageError {
		t.Errorf("kind = %v", got.Kind)
	}
	m.failure = ""
	if got := m.message(); got.Kind != kittui.MessageWarning {
		t.Errorf("kind = %v", got.Kind)
	}
	m.notice = ""
	if got := m.message(); got.Kind != kittui.MessageResult || got.Text != "done" {
		t.Errorf("message = %+v", got)
	}
}

// styleBefore is the last escape sequence before the first rune of text on the screen, so indentation and
// frame do not count.
func styleBefore(t *testing.T, screen, text string) string {
	t.Helper()
	i := strings.Index(screen, text)
	if i < 0 {
		t.Fatalf("no %q on the screen:\n%s", text, screen)
	}
	j := strings.LastIndex(screen[:i], "\x1b[")
	if j < 0 || strings.Contains(screen[j:i], "\n") {
		t.Fatalf("%q has no style of its own:\n%s", text, screen)
	}
	return screen[j:i]
}

func TestFinalQuestionsShowTheirConsequenceInWarnColour(t *testing.T) {
	colored := func(keys [][]string, what kittui.TextID) string {
		m := newSettingsModel(t, settingsBackend())
		m.profile = kittui.ProfileTrueColor
		m.restyle()
		for _, k := range keys {
			send(m, k...)
		}
		words := strings.Fields(m.text.Lookup(what))
		return styleBefore(t, m.render(), strings.Join(words[:min(len(words), 2)], " "))
	}
	plain := colored([][]string{toNotify, {"s"}}, textWhatNotifyTest)
	cases := []struct {
		name string
		keys [][]string
		what kittui.TextID
	}{
		{"credential", [][]string{toCreds, {"x"}}, textWhatCredDelete},
		{"channel", [][]string{toNotify, {"down", "x"}}, textWhatNotifyRemove},
		{"secret", [][]string{toSecretPick, {"down", "enter"}}, textWhatSecretDelete},
		{"restore", [][]string{toRestore, {"enter"}}, textWhatRestore},
	}
	for _, c := range cases {
		if got := colored(c.keys, c.what); got == plain || !strings.Contains(got, "\x1b[") {
			t.Errorf("%s: consequence style %q does not differ from %q", c.name, got, plain)
		}
	}
}
