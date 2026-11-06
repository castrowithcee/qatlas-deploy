package tui

import (
	"fmt"
	"strings"
	"testing"
)

func formOpen(m *Model) bool { return m.set.form != nil }

func TestFormTabRunsOverFieldsThenButtons(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	send(m, toRepo...)
	send(m, "b")
	if _, on := m.set.form.form.FocusedButton(); on {
		t.Fatal("a button has the focus at the start")
	}
	var seen []string
	for range 4 {
		send(m, "tab")
		if act, on := m.set.form.form.FocusedButton(); on {
			seen = append(seen, string(act))
		}
	}
	if len(seen) < 2 || seen[0] != string(actSave) || seen[1] != string(actBack) {
		t.Fatalf("buttons reached in order %v", seen)
	}
	contains(t, m, "save", "discard")
}

func TestEnterOnSaveSavesAndEnterOnDiscardClosesAnUnchangedForm(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	send(m, toRepo...)
	send(m, "b", "tab", "tab")
	send(m, "enter") // Save with no values: refused with a problem, the form stays.
	if !formOpen(m) || m.modal != nil {
		t.Fatal("save of an empty form")
	}
	contains(t, m, "SSH address is required")
	// Tab wraps from Save to Discard on the next press.
	for {
		if act, on := m.set.form.form.FocusedButton(); on && act == actBack {
			break
		}
		send(m, "tab")
	}
	send(m, "enter")
	if formOpen(m) || m.modal != nil {
		t.Fatalf("an unchanged form stays: form %v, question %v", m.set.form, m.modal)
	}
}

func TestDiscardOfAChangedFormAsksAndCancelKeepsEverything(t *testing.T) {
	for _, how := range []string{"esc", "button"} {
		t.Run(how, func(t *testing.T) {
			m := newSettingsModel(t, settingsBackend())
			send(m, secretFormKeysFromStart()...)
			form := m.set.form
			if how == "esc" {
				send(m, "esc")
			} else {
				for {
					if act, on := form.form.FocusedButton(); on && act == actBack {
						break
					}
					send(m, "tab")
				}
				send(m, "enter")
			}
			if m.modal == nil || m.set.form != form {
				t.Fatalf("question %v, form kept %v", m.modal, m.set.form == form)
			}
			screen := m.render()
			noLeak(t, screen, hiddenA)
			if !strings.Contains(screen, "lost") {
				t.Fatalf("the question does not say the changes are lost:\n%s", screen)
			}
			values := form.form.Values()
			focus := form.form.Focused()
			send(m, "enter") // Cancel is the default.
			if m.modal != nil || m.set.form != form || !equalValues(values, form.form.Values()) || focus != form.form.Focused() {
				t.Fatal("cancel changed the form")
			}
			send(m, "esc", "n")
			if m.set.form != form {
				t.Fatal("no closed the form")
			}
			send(m, "esc", "y")
			if formOpen(m) {
				t.Fatal("yes keeps the form")
			}
		})
	}
}

func secretFormKeysFromStart() []string {
	return seq(toSecretForm, typed("SERVICE_TOKEN"), keys("down"), typed(hiddenA))
}

func TestEscOfAnUnchangedFormClosesWithoutAsking(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	send(m, toRepo...)
	send(m, "b", "esc")
	if formOpen(m) || m.modal != nil {
		t.Fatal("unchanged form asked or stayed")
	}
}

func equalValues[K comparable, V any](a, b map[K]V) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || fmt.Sprint(v) != fmt.Sprint(w) {
			return false
		}
	}
	return true
}
