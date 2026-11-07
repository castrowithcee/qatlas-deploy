package tui

import (
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

// savedPrefs records what the page saved and answers with err.
type savedPrefs struct {
	calls []uiprefs.Prefs
	err   error
}

func (s *savedPrefs) save(p uiprefs.Prefs) error {
	s.calls = append(s.calls, p)
	return s.err
}

// generalModel is a model whose preferences hold values the page does not edit, so a save shows it keeps them.
func generalModel(t *testing.T, s *savedPrefs) *Model {
	t.Helper()
	m := newSettingsModel(t, settingsBackend())
	m.prefs = uiprefs.Prefs{Theme: kittui.ThemeTerminal, Start: string(areaRuns), Icons: uiprefs.IconsNerdFont,
		Language: uiprefs.LanguageEnglish, MouseEnabled: true, Keys: map[kittui.ActionID]string{"reload": "x"}}
	m.mouseOn = true
	if s != nil {
		m.savePrefs = s.save
	}
	return m
}

// The fields in focus order; right and left change a choice, f2 saves.
var (
	toLanguage = keys("down", "down")
	save       = keys("f2")
)

func TestGeneralPageIsFirstAndShowsTheLoadedValues(t *testing.T) {
	m := generalModel(t, &savedPrefs{})
	m.prefs.MouseEnabled = false
	send(m, toMenu...)
	if first, _ := m.set.menu.Selected(); first != "general" {
		t.Fatalf("the first page is %q", first)
	}
	send(m, "enter")
	contains(t, m, "General", "Theme", "Start area", "Language", "Mouse", "terminal", "Runs", "English", "off",
		"the colors of the interface")
	send(m, "down", "down", "down")
	contains(t, m, "hold Shift to select text")
}

func TestGeneralSaveWritesTheChosenValuesAndKeepsTheRest(t *testing.T) {
	s := &savedPrefs{}
	m := generalModel(t, s)
	old := m.prefs
	themes := m.themes.Names()
	send(m, toGeneral...)
	send(m, "right")         // theme
	send(m, toLanguage...)   // language
	send(m, "right", "down") // deutsch, mouse
	send(m, "right")         // off
	send(m, save...)
	if len(s.calls) != 1 {
		t.Fatalf("%d saves", len(s.calls))
	}
	got := s.calls[0]
	want := old
	want.Theme = themes[(slices.Index(themes, old.Theme)+1)%len(themes)]
	want.Language, want.MouseEnabled = uiprefs.LanguageGerman, false
	if got.Theme != want.Theme || got.Start != want.Start || got.Icons != want.Icons || got.Language != want.Language ||
		got.MouseEnabled != want.MouseEnabled || !maps.Equal(got.Keys, want.Keys) {
		t.Fatalf("saved %+v, want %+v", got, want)
	}
	if m.theme != want.Theme || m.mouseOn || m.lang != i18n.German || m.prefs.Start != old.Start {
		t.Fatalf("theme %q mouse %v lang %v", m.theme, m.mouseOn, m.lang)
	}
	// The form and the message are in the new language in the next frame.
	contains(t, m, "Allgemein", "Sprache", "Maus", "Deutsch", "Die Einstellungen sind gespeichert.")
	send(m, "esc")
	contains(t, m, "Allgemein", "Stacks-Repository")
}

func TestGeneralLanguageRebuildsTheListsAndKeepsTheirState(t *testing.T) {
	m := generalModel(t, &savedPrefs{})
	send(m, " ", "down", "down")
	ticked, selected := m.list.Ticked(), mustSelected(t, m.list)
	if len(ticked) != 1 {
		t.Fatalf("ticked %v", ticked)
	}
	send(m, toGeneral...)
	send(m, toLanguage...)
	send(m, "right")
	send(m, save...)
	send(m, "esc", "1")
	if !slices.Equal(m.list.Ticked(), ticked) || mustSelected(t, m.list) != selected {
		t.Fatalf("ticked %v selected %q, before %v %q", m.list.Ticked(), mustSelected(t, m.list), ticked, selected)
	}
	contains(t, m, "Bereich", "LETZTER LAUF")
}

func mustSelected(t *testing.T, l *kittui.List) kittui.ItemID {
	t.Helper()
	id, ok := l.Selected()
	if !ok {
		t.Fatal("no row is selected")
	}
	return id
}

func TestGeneralMouseOffTakesEffect(t *testing.T) {
	m := generalModel(t, &savedPrefs{})
	send(m, toGeneral...)
	send(m, "down", "down", "down", "right")
	send(m, save...)
	if m.mouseOn {
		t.Fatal("the mouse is still on")
	}
}

func TestGeneralWriteErrorChangesNothing(t *testing.T) {
	s := &savedPrefs{err: errors.New("disk full in the example")}
	m := generalModel(t, s)
	old := m.prefs
	send(m, toGeneral...)
	send(m, "right", "down", "right", "down", "right", "down", "right")
	send(m, save...)
	contains(t, m, "could not be saved", "disk full in the example")
	if m.lang != i18n.English || m.theme != kittui.ThemeTerminal || !m.mouseOn || m.prefs.Language != old.Language ||
		m.prefs.Start != old.Start || m.prefs.Theme != old.Theme || !m.prefs.MouseEnabled {
		t.Fatalf("the running state changed: lang %v theme %q mouse %v prefs %+v", m.lang, m.theme, m.mouseOn, m.prefs)
	}
}

func TestGeneralWithoutCallbackSaysItCannotSave(t *testing.T) {
	m := generalModel(t, nil)
	send(m, toGeneral...)
	send(m, "down", "down", "down", "right")
	send(m, save...)
	contains(t, m, "cannot be saved here")
	if !m.mouseOn {
		t.Fatal("the mouse changed without a save")
	}
}

func TestGeneralSavesDuringAServiceCall(t *testing.T) {
	s := &savedPrefs{}
	m := generalModel(t, s)
	send(m, toGeneral...)
	m.set.busy = true
	send(m, "down", "down", "down", "right")
	send(m, save...)
	if len(s.calls) != 1 || s.calls[0].MouseEnabled || m.mouseOn {
		t.Fatalf("saves %v, mouse %v", s.calls, m.mouseOn)
	}
	if !m.set.busy {
		t.Fatal("the save ended the call")
	}
}

func TestGeneralDiscardReturnsToTheMenu(t *testing.T) {
	m := generalModel(t, &savedPrefs{})
	send(m, toGeneral...)
	send(m, "esc")
	if m.set.page != "" || m.set.form != nil {
		t.Fatalf("page %q form %v", m.set.page, m.set.form)
	}
}
