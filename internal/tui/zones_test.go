package tui

import (
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

func wantZone(t *testing.T, m *Model, want kittui.FocusZone) {
	t.Helper()
	if got := m.focus(); got != want {
		t.Fatalf("zone %s, want %s", got, want)
	}
}

// Tab walks the zones of a list, a text view and a log with wrap-around, and shift+tab walks back.
func TestTabCyclesTheZonesOfEachKindOfView(t *testing.T) {
	views := map[string]struct {
		model   func(t *testing.T) *Model
		content kittui.FocusZone
	}{
		"list": {func(t *testing.T) *Model { return newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows) }, kittui.ZoneRows},
		"text": {func(t *testing.T) *Model {
			m := newTestModel(t, i18n.English, demoBackend(), wideWidth, detailRows)
			send(m, "enter")
			return m
		}, kittui.ZoneDetail},
		"run log": {func(t *testing.T) *Model {
			m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
			send(m, "3", "down", "enter")
			return m
		}, kittui.ZoneRows},
		"container log": {func(t *testing.T) *Model { return openAppLog(t, clogBackend()) }, kittui.ZoneRows},
	}
	for name, v := range views {
		t.Run(name, func(t *testing.T) {
			m := v.model(t)
			wantZone(t, m, v.content)
			send(m, "tab")
			wantZone(t, m, kittui.ZoneTabs)
			send(m, "tab")
			wantZone(t, m, v.content)
			send(m, "shift+tab")
			wantZone(t, m, kittui.ZoneTabs)
			send(m, "shift+tab")
			wantZone(t, m, v.content)
		})
	}
}

func TestLeftAndRightStepThroughAllAreasWithWrapAround(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	send(m, "tab")
	for _, want := range []kittui.AreaID{areaDetail, areaRuns, areaSettings, areaContainers, areaStacks} {
		send(m, "right")
		if m.area != want {
			t.Fatalf("right: area %s, want %s", m.area, want)
		}
		wantZone(t, m, kittui.ZoneTabs)
	}
	send(m, "left")
	if m.area != areaContainers {
		t.Fatalf("left from the first area: %s", m.area)
	}
}

func TestLeftAndRightLeaveAnOpenLog(t *testing.T) {
	m := openAppLog(t, clogBackend())
	send(m, "tab", "right")
	if m.area != areaStacks || m.clog.open {
		t.Fatalf("area %s, log open %v", m.area, m.clog.open)
	}
	m = openAppLog(t, clogBackend())
	send(m, "tab", "left")
	if m.area != areaSettings || m.clog.open {
		t.Fatalf("area %s, log open %v", m.area, m.clog.open)
	}
	// The log of a whole stack opens from the stack detail.
	m = newTestModel(t, i18n.English, clogBackend(), ctWide, ctWideRows)
	send(m, "enter", "o")
	if !m.clog.open || m.clog.stack == "" {
		t.Fatalf("no stack log: %+v", m.clog)
	}
	send(m, "tab", "right")
	if m.area != areaRuns || m.clog.open {
		t.Fatalf("area %s, log open %v", m.area, m.clog.open)
	}
}

func TestEnterAndDownInTheTabLineDoNotOpenARow(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	send(m, "tab", "enter")
	if m.area != areaStacks {
		t.Fatalf("enter opened a row: area %s", m.area)
	}
	wantZone(t, m, kittui.ZoneRows)
	send(m, "tab", "down")
	wantZone(t, m, kittui.ZoneRows)
	if m.area != areaStacks {
		t.Fatalf("area %s", m.area)
	}
}

func TestDigitsLeadToTheContentZone(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, detailRows)
	send(m, "tab", "2")
	if m.area != areaDetail {
		t.Fatalf("area %s", m.area)
	}
	wantZone(t, m, kittui.ZoneDetail)
	send(m, "tab", "1")
	wantZone(t, m, kittui.ZoneRows)
}

func TestTabStaysTheFieldKeyInForms(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, detailRows)
	send(m, "enter", "a")
	if m.set.form == nil {
		t.Fatal("no form")
	}
	before := m.area
	send(m, "tab")
	if m.area != before || m.set.form == nil {
		t.Fatalf("tab left the form: area %s", m.area)
	}
	if got := m.zones(); len(got) != 1 || got[0] != kittui.ZoneForm {
		t.Fatalf("zones %v", got)
	}
}

func TestTabIsTextInASearch(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	send(m, "/", "tab")
	wantZone(t, m, kittui.ZoneRows)
}

func TestSettingsTextPageHasADetailZone(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	send(m, "4")
	wantZone(t, m, kittui.ZoneRows)
	send(m, "down", "down", "enter")
	if m.set.page == "" || m.set.activeList() != nil {
		t.Fatalf("page %q is not a text page", m.set.page)
	}
	wantZone(t, m, kittui.ZoneDetail)
}

func TestFocusedTabLine(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	send(m, "tab")
	screen := m.render()
	checkScreen(t, screen, wideWidth, goldenRows)
	checkGolden(t, "tabs.focused.en.wide.golden", screen+"\n")
}
