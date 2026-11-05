package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// filterLog spans several hours; the clock of the tests stands at 18:00 on the same day.
const filterLog = `2026-10-30T11:00:00Z step check
2026-10-30T13:30:00Z info early line
2026-10-30T16:30:00Z info middle line
2026-10-30T17:50:00Z info late line
`

var filterNow = time.Date(2026, time.October, 30, 18, 0, 0, 0, time.UTC)

func filterModel(t *testing.T, b *fakeBackend, width int) *Model {
	t.Helper()
	b.setLog(finishedRun, filterLog)
	m := newTestModel(t, i18n.English, b, width, 20)
	m.now = func() time.Time { return filterNow }
	send(m, "3", "down", "enter")
	if !m.log.open || m.log.row.id != finishedRun || m.log.view.Len() != 4 {
		t.Fatalf("log = %+v, lines %d", m.log, m.log.view.Len())
	}
	return m
}

// lastFilter chooses "last <count> <unit>" in the period form. Choices wrap, so the kind and the unit are reached
// by steps to the right from what the form opens on: kind all or last, unit minutes, hours or days.
func lastFilter(m *Model, kindSteps int, count string, unitSteps int) {
	send(m, "T")
	for range kindSteps {
		send(m, "right")
	}
	send(m, "tab", "backspace", "backspace", "backspace")
	send(m, strings.Split(count, "")...)
	send(m, "tab")
	for range unitSteps {
		send(m, "right")
	}
	send(m, "f2")
}

func TestLogFilterLastHidesAndWidensWithoutReading(t *testing.T) {
	b := demoBackend()
	m := filterModel(t, b, wideWidth)
	reads := b.called("log")

	lastFilter(m, 1, "2", 1) // 2 hours: 16:00 on
	screen := m.render()
	if m.log.view.Len() != 2 || strings.Contains(screen, "early line") || !strings.Contains(screen, "middle line") ||
		!strings.Contains(screen, "late line") {
		t.Fatalf("lines %d:\n%s", m.log.view.Len(), screen)
	}

	lastFilter(m, 0, "1", 2) // 1 minute: nothing
	if m.log.view.Len() != 0 {
		t.Fatalf("lines %d, want none", m.log.view.Len())
	}

	// Widening brings the hidden lines back from the text already read.
	lastFilter(m, 0, "1", 2) // 1 day
	if m.log.view.Len() != 4 || !strings.Contains(m.render(), "early line") {
		t.Fatalf("lines %d:\n%s", m.log.view.Len(), m.render())
	}
	if got := b.called("log"); got != reads {
		t.Fatalf("%d log reads after %d", got, reads)
	}
}

func TestLogFilterRange(t *testing.T) {
	b := demoBackend()
	m := filterModel(t, b, wideWidth)
	reads := b.called("log")
	layout := "2006-01-02 15:04"
	from := time.Date(2026, time.October, 30, 13, 0, 0, 0, time.UTC).In(time.Local).Format(layout)
	until := time.Date(2026, time.October, 30, 17, 0, 0, 0, time.UTC).In(time.Local).Format(layout)
	send(m, "T", "right", "right", "tab")
	send(m, strings.Split(from, "")...)
	send(m, "tab")
	send(m, strings.Split(until, "")...)
	send(m, "f2")
	screen := m.render()
	if m.log.view.Len() != 2 || !strings.Contains(screen, "early line") || !strings.Contains(screen, "middle line") ||
		strings.Contains(screen, "late line") {
		t.Fatalf("lines %d:\n%s", m.log.view.Len(), screen)
	}
	send(m, "T", "right", "f2") // all again
	if m.log.view.Len() != 4 || b.called("log") != reads {
		t.Fatalf("lines %d, reads %d after %d", m.log.view.Len(), b.called("log"), reads)
	}
}

func TestLogFilterSurvivesRewrittenLog(t *testing.T) {
	b := demoBackend()
	b.setLog(activeRun, filterLog)
	m := newTestModel(t, i18n.English, b, wideWidth, 20)
	m.now = func() time.Time { return filterNow }
	send(m, "3", "enter")
	if !m.log.active {
		t.Fatal("the run is not active")
	}
	lastFilter(m, 1, "2", 1)
	if m.log.view.Len() != 2 {
		t.Fatalf("lines %d", m.log.view.Len())
	}
	// A rewritten log starts the view over and keeps the period.
	b.setLog(activeRun, "2026-10-30T10:00:00Z step restart\n2026-10-30T17:30:00Z info fresh line\n")
	timersOf[m].fire(t, m)
	screen := m.render()
	if got := m.log.view.Filter(); got.Count != 2 || m.log.view.Len() != 1 || !strings.Contains(screen, "fresh line") ||
		strings.Contains(screen, "restart") {
		t.Fatalf("filter %+v, lines %d:\n%s", got, m.log.view.Len(), screen)
	}
}

func TestLogFilterFollowsOnlyLinesInRange(t *testing.T) {
	b := demoBackend()
	b.setLog(activeRun, filterLog)
	m := newTestModel(t, i18n.English, b, wideWidth, 20)
	m.now = func() time.Time { return filterNow }
	send(m, "3", "enter")
	lastFilter(m, 1, "2", 1)
	b.setLog(activeRun, filterLog+"2026-10-30T15:00:00Z info stale stamp\n2026-10-30T17:59:00Z info new line\n")
	timersOf[m].fire(t, m)
	screen := m.render()
	if m.log.view.Len() != 3 || !strings.Contains(screen, "new line") || strings.Contains(screen, "stale stamp") {
		t.Fatalf("lines %d:\n%s", m.log.view.Len(), screen)
	}
}

func TestGoldenRunsLogFilter(t *testing.T) {
	for name, lang := range goldenLangs {
		for size, width := range goldenSizes {
			t.Run(name+"."+size, func(t *testing.T) {
				b := demoBackend()
				b.setLog(finishedRun, filterLog)
				m := newTestModel(t, lang, b, width, goldenRows)
				m.now = func() time.Time { return filterNow }
				send(m, "3", "down", "enter")
				lastFilter(m, 1, "2", 1)
				screen := m.render()
				checkScreen(t, screen, width, goldenRows)
				checkGolden(t, "runs.log.filter."+name+"."+size+".golden", screen+"\n")
			})
		}
	}
}
