package tui

import (
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// startedModel is a model as the terminal interface opens it: the focus is still on the tab line.
func startedModel(t *testing.T, b Backend, width, height int) *Model {
	t.Helper()
	m := newTestModel(t, i18n.English, b, width, height)
	m.setZone(kittui.ZoneTabs)
	return m
}

func TestTheInterfaceStartsOnTheTabLine(t *testing.T) {
	m, err := newModel(config{backend: demoBackend(), lang: i18n.English, start: areaRuns})
	if err != nil {
		t.Fatal(err)
	}
	wantZone(t, m, kittui.ZoneTabs)
	if m.area != areaRuns {
		t.Fatalf("area %s", m.area)
	}
	m = startedModel(t, demoBackend(), wideWidth, goldenRows)
	send(m, "right")
	if m.area != areaDetail {
		t.Fatalf("right after the start: area %s", m.area)
	}
	wantZone(t, m, kittui.ZoneTabs)
}

func TestUpOnTheTabLineLeavesTheListAlone(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	send(m, "down", "down")
	before, _ := m.list.Selected()
	m.setZone(kittui.ZoneTabs)
	send(m, "up")
	wantZone(t, m, kittui.ZoneTabs)
	if after, _ := m.list.Selected(); after != before {
		t.Fatalf("up on the tab line moved the list from %s to %s", before, after)
	}
}

func TestDownAndEnterLeadFromTheTabLineIntoEveryKindOfView(t *testing.T) {
	for _, key := range []string{"down", "enter"} {
		for name, v := range map[string]struct {
			keys []string
			zone kittui.FocusZone
		}{
			"list": {nil, kittui.ZoneRows},
			"text": {[]string{"right"}, kittui.ZoneDetail},
			"log":  {[]string{"right", "right"}, kittui.ZoneRows},
		} {
			m := startedModel(t, demoBackend(), wideWidth, detailRows)
			send(m, v.keys...)
			if name == "log" {
				send(m, "down", "enter", "tab") // open a run log, back to the tab line
			}
			wantZone(t, m, kittui.ZoneTabs)
			send(m, key)
			wantZone(t, m, v.zone)
		}
	}
}

func TestUpInTheFirstRowLeadsToTheTabLine(t *testing.T) {
	for name, keys := range map[string][]string{
		"stacks":        {"1"},
		"runs":          {"3"},
		"containers":    {"5"},
		"settings menu": {"4"},
	} {
		t.Run(name, func(t *testing.T) {
			m := newTestModel(t, i18n.English, containersBackend(), ctWide, ctWideRows)
			send(m, keys...)
			wantZone(t, m, kittui.ZoneRows)
			send(m, "down")
			wantZone(t, m, kittui.ZoneRows)
			send(m, "up")
			wantZone(t, m, kittui.ZoneRows) // only the cursor moved
			// The Containers list has group rows above the first container.
			for i := 0; i < 3 && m.focus() != kittui.ZoneTabs; i++ {
				send(m, "up")
			}
			wantZone(t, m, kittui.ZoneTabs)
		})
	}
}

func TestUpInASettingsListLeadsToTheTabLine(t *testing.T) {
	m := newTestModel(t, i18n.English, settingsBackend(), wideWidth, goldenRows)
	send(m, toCreds...)
	if m.set.page != pageCreds {
		t.Fatalf("page %q", m.set.page)
	}
	wantZone(t, m, kittui.ZoneRows)
	send(m, "up")
	wantZone(t, m, kittui.ZoneTabs)
	if m.set.page != pageCreds {
		t.Fatalf("page %q", m.set.page)
	}
}

func TestUpInAnEmptyListLeadsToTheTabLine(t *testing.T) {
	b := containersBackend()
	b.containers = control.ContainerListResult{}
	m := newTestModel(t, i18n.English, b, ctWide, ctWideRows)
	send(m, "5", "up")
	wantZone(t, m, kittui.ZoneTabs)
}

func TestUpAtTheTopOfATextLeadsToTheTabLine(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	send(m, "enter")
	wantZone(t, m, kittui.ZoneDetail)
	send(m, "down")
	if m.detail.top == 0 {
		t.Fatal("the detail did not scroll")
	}
	send(m, "up")
	wantZone(t, m, kittui.ZoneDetail)
	send(m, "up")
	wantZone(t, m, kittui.ZoneTabs)

	m = newTestModel(t, i18n.English, containersBackend(), ctWide, goldenRows)
	send(m, "5", "enter")
	if !m.ctv.open {
		t.Fatal("no container view")
	}
	send(m, "down")
	if m.ctv.top == 0 {
		t.Fatal("the view did not scroll")
	}
	send(m, "up")
	wantZone(t, m, kittui.ZoneDetail)
	send(m, "up")
	wantZone(t, m, kittui.ZoneTabs)
	if !m.ctv.open {
		t.Fatal("the container view closed")
	}
}

func TestUpInASettingsTextPageLeadsToTheTabLine(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	send(m, "4", "down", "down", "enter")
	wantZone(t, m, kittui.ZoneDetail)
	send(m, "up")
	wantZone(t, m, kittui.ZoneTabs)
	if m.set.page == "" {
		t.Fatal("the page closed")
	}
}

func TestEscInAnAreaListLeadsToTheTabLineAndStaysInTheArea(t *testing.T) {
	for name, area := range map[string]struct {
		key  string
		area kittui.AreaID
	}{"stacks": {"1", areaStacks}, "runs": {"3", areaRuns}, "containers": {"5", areaContainers}, "settings": {"4", areaSettings}} {
		t.Run(name, func(t *testing.T) {
			m := newTestModel(t, i18n.English, containersBackend(), ctWide, ctWideRows)
			send(m, area.key, "esc")
			wantZone(t, m, kittui.ZoneTabs)
			if m.area != area.area {
				t.Fatalf("area %s", m.area)
			}
			send(m, "esc")
			wantZone(t, m, kittui.ZoneTabs)
			if m.area != area.area {
				t.Fatalf("esc on the tab line: area %s", m.area)
			}
		})
	}
}

func TestEscClearsAFilterBeforeItLeadsToTheTabLine(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	send(m, "3", "/", "x", "enter")
	wantZone(t, m, kittui.ZoneRows)
	send(m, "esc")
	wantZone(t, m, kittui.ZoneRows)
	send(m, "esc")
	wantZone(t, m, kittui.ZoneTabs)
	if m.area != areaRuns {
		t.Fatalf("area %s", m.area)
	}
}

func TestDrillDownLevelsKeepTheirEsc(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	send(m, "enter", "esc")
	if m.area != areaStacks {
		t.Fatalf("detail: area %s", m.area)
	}
	send(m, "3", "enter")
	if !m.log.open {
		t.Fatal("no run log")
	}
	send(m, "up")
	wantZone(t, m, kittui.ZoneRows)
	send(m, "esc")
	if m.log.open || m.area != areaRuns {
		t.Fatalf("log open %v, area %s", m.log.open, m.area)
	}
}
