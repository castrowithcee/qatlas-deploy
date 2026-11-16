package tui

import (
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

func newSubtabModel(t *testing.T) *Model {
	t.Helper()
	m := newTestModel(t, i18n.English, settingsBackend(), wideWidth, goldenRows)
	m.mouseOn = true
	return m
}

func TestArrowsAloneLeadThroughTheLinesAndTheContent(t *testing.T) {
	m := newSubtabModel(t)
	m.setZone(kittui.ZoneTabs)
	send(m, "4") // the digit shows the area on its first page, the form
	wantZone(t, m, kittui.ZoneForm)
	send(m, "up")
	wantZone(t, m, kittui.ZoneSubtabs)
	send(m, "up")
	wantZone(t, m, kittui.ZoneTabs)
	send(m, "up")
	wantZone(t, m, kittui.ZoneTabs)
	send(m, "down")
	wantZone(t, m, kittui.ZoneSubtabs)
	// The arrows walk the sub-tabs with wrap-around, and each page is shown at once.
	for _, want := range []settingsPage{pageCreds, pageWindow, pageNotify, pageGeneral} {
		send(m, "right")
		if m.set.page != want {
			t.Fatalf("page %q, want %q", m.set.page, want)
		}
		wantZone(t, m, kittui.ZoneSubtabs)
	}
	send(m, "left")
	if m.set.page != pageNotify {
		t.Fatalf("page %q after left", m.set.page)
	}
	send(m, "down")
	wantZone(t, m, kittui.ZoneRows)
	send(m, "down", "up")
	wantZone(t, m, kittui.ZoneRows)
	send(m, "up")
	wantZone(t, m, kittui.ZoneSubtabs)
	send(m, "up")
	wantZone(t, m, kittui.ZoneTabs)
	// Left and right on the tab line still switch the area.
	send(m, "right")
	if m.area != areaContainers {
		t.Fatalf("area %s", m.area)
	}
}

func TestEnterAndTabLeadFromTheSubtabLineIntoThePage(t *testing.T) {
	for _, key := range []string{"enter", "down", "tab"} {
		m := newSubtabModel(t)
		send(m, toSubtabs...)
		send(m, "right", key)
		wantZone(t, m, kittui.ZoneRows)
		send(m, "shift+tab")
		wantZone(t, m, kittui.ZoneSubtabs)
	}
}

func TestTheSubtabIsKeptWhileTheAreaIsLeft(t *testing.T) {
	m := newSubtabModel(t)
	send(m, toCreds...)
	send(m, "1", "4")
	if m.set.page != pageCreds {
		t.Fatalf("page %q", m.set.page)
	}
	contains(t, m, "registry.docker.io")
}

func TestUnsavedGeneralChangesAreAskedAboutBeforeTheFocusLeavesTheForm(t *testing.T) {
	m := newSubtabModel(t)
	send(m, toGeneral...)
	send(m, "right") // a new theme
	if !m.set.form.form.Changed() {
		t.Fatal("the form is unchanged")
	}
	for _, key := range []string{"up", "esc"} {
		send(m, key)
		if m.modal == nil {
			t.Fatalf("%s left a changed form without a question", key)
		}
		send(m, "n")
		wantZone(t, m, kittui.ZoneForm)
		if !m.set.form.form.Changed() {
			t.Fatal("cancelling dropped the changes")
		}
	}
	send(m, "up", "y")
	wantZone(t, m, kittui.ZoneSubtabs)
	if m.set.form == nil || m.set.form.form.Changed() {
		t.Fatal("the discarded changes are still in the form")
	}
}

func TestUnsavedGeneralChangesAreAskedAboutBeforeAnotherSubtab(t *testing.T) {
	m := newSubtabModel(t)
	send(m, toGeneral...)
	send(m, "right")
	x, y := cellOf(t, m, "Credentials")
	clickAt(m, x, y)
	if m.modal == nil || m.set.page != pageGeneral {
		t.Fatalf("a click changed the page without a question: page %q", m.set.page)
	}
	send(m, "n")
	if m.set.page != pageGeneral || m.set.form == nil || !m.set.form.form.Changed() {
		t.Fatal("cancelling did not keep the form")
	}
	wantZone(t, m, kittui.ZoneForm)
	clickAt(m, x, y)
	send(m, "y")
	if m.set.page != pageCreds || m.set.form != nil {
		t.Fatalf("page %q", m.set.page)
	}
	wantZone(t, m, kittui.ZoneSubtabs)

	// The arrows ask the same where the sub-tab line holds the focus over a changed form.
	m = newSubtabModel(t)
	send(m, toGeneral...)
	send(m, "right")
	m.setZone(kittui.ZoneSubtabs)
	send(m, "right")
	if m.modal == nil || m.set.page != pageGeneral {
		t.Fatalf("an arrow changed the page without a question: page %q", m.set.page)
	}
	send(m, "n")
	if m.set.page != pageGeneral || !m.set.form.form.Changed() {
		t.Fatal("cancelling did not keep the form")
	}
	send(m, "right", "y")
	if m.set.page != pageCreds {
		t.Fatalf("page %q", m.set.page)
	}
}

func TestUnsavedGeneralChangesAreAskedAboutBeforeAnotherArea(t *testing.T) {
	m := newSubtabModel(t)
	send(m, toGeneral...)
	send(m, "right")
	x, y := cellOf(t, m, "Runs")
	clickAt(m, x, y)
	if m.modal == nil || m.area != areaSettings {
		t.Fatalf("a click left the area without a question: area %s", m.area)
	}
	send(m, "n")
	if m.area != areaSettings || m.set.form == nil || !m.set.form.form.Changed() {
		t.Fatal("cancelling did not keep the form")
	}
	clickAt(m, x, y)
	send(m, "y")
	if m.area != areaRuns {
		t.Fatalf("area %s", m.area)
	}
	// The General form does not show in the other areas.
	if m.set.form != nil {
		t.Fatal("the General form stayed open outside its area")
	}
}

func TestAnUnchangedGeneralFormIsLeftWithoutAQuestion(t *testing.T) {
	m := newSubtabModel(t)
	send(m, toGeneral...)
	x, y := cellOf(t, m, "Runs")
	clickAt(m, x, y)
	if m.modal != nil || m.area != areaRuns || m.set.form != nil {
		t.Fatalf("area %s, modal %v", m.area, m.modal)
	}
}

func TestEveryPageOfAnotherSubtabKeepsItsActions(t *testing.T) {
	b := settingsBackend()
	m := newSubtabModel(t)
	m.backend = b
	send(m, toNotify...)
	send(m, "s", "y")
	send(m, "enter")
	if got := b.actions(); len(got) != 1 || got[0] != "notifytest:ops-hook" {
		t.Fatalf("calls %v", got)
	}
}
