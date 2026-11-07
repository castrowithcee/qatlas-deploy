package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

const wheelRows = 16

// fillLog adds numbered lines to a log view so that it needs scrolling.
func fillLog(v *kittui.LogView, n int) {
	for i := range n {
		v.Append(kittui.LogLine{Text: fmt.Sprintf("filler line %02d", i)})
	}
}

func TestTheWheelScrollsTheDetailTextsByNotchesAndStopsAtTheEnds(t *testing.T) {
	views := map[string]func(t *testing.T) (*Model, *int, func() (top, total, rows int)){
		"stack detail": func(t *testing.T) (*Model, *int, func() (int, int, int)) {
			m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wheelRows)
			m.mouseOn = true
			send(m, "enter")
			m.render()
			return m, &m.detail.top, func() (int, int, int) { return m.detail.top, m.detail.total, m.detail.rows }
		},
		"container detail": func(t *testing.T) (*Model, *int, func() (int, int, int)) {
			m := newTestModel(t, i18n.English, containersBackend(), wideWidth, wheelRows)
			m.mouseOn = true
			send(m, "5")
			x, y := cellOf(t, m, "demo-stack-app-1")
			clickAt(m, x, y)
			clickAt(m, x, y)
			if !m.ctv.open {
				t.Fatal("no container detail")
			}
			m.render()
			return m, &m.ctv.top, func() (int, int, int) { return m.ctv.top, m.ctv.total, m.ctv.rows }
		},
	}
	for name, setup := range views {
		t.Run(name, func(t *testing.T) {
			m, _, state := setup(t)
			if _, total, rows := state(); total <= rows+kittui.WheelTextLines {
				t.Fatalf("text of %d lines in %d rows does not scroll enough", total, rows)
			}
			zone := m.focus()
			wheelAt(m, 10, wheelRows/2+2, tea.MouseWheelDown)
			if top, _, _ := state(); top != kittui.WheelTextLines {
				t.Fatalf("one notch moved to %d, want %d", top, kittui.WheelTextLines)
			}
			wheelAt(m, 10, wheelRows/2+2, tea.MouseWheelUp)
			if top, _, _ := state(); top != 0 {
				t.Fatalf("back up: %d", top)
			}
			wheelAt(m, 10, wheelRows/2+2, tea.MouseWheelUp)
			if top, _, _ := state(); top != 0 {
				t.Fatalf("above the start: %d", top)
			}
			for range 100 {
				wheelAt(m, 10, wheelRows/2+2, tea.MouseWheelDown)
			}
			top, total, rows := state()
			if top != total-rows {
				t.Fatalf("end: top %d, total %d, rows %d", top, total, rows)
			}
			if want := fmt.Sprintf("%d/%d", total, total); m.scrollNote != want {
				t.Fatalf("note %q, want %q", m.scrollNote, want)
			}
			if m.focus() != zone {
				t.Fatalf("zone %s, want %s", m.focus(), zone)
			}
		})
	}
}

func TestTheWheelOnASettingsTextPageMovesNothingAndKeepsTheZone(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	m.mouseOn = true
	send(m, toWindow...)
	m.render()
	zone, before := m.focus(), m.render()
	wheelAt(m, 10, 6, tea.MouseWheelDown)
	if m.focus() != zone || m.render() != before {
		t.Fatalf("the wheel changed the page or the zone:\n%s", m.render())
	}
}

func TestTheWheelScrollsTheRunLogAndTheContainerLog(t *testing.T) {
	run := func(t *testing.T) (*Model, *kittui.LogView) {
		m := followed(t, i18n.English, demoBackend())
		m.mouseOn = true
		fillLog(m.log.view, 60)
		m.render()
		return m, m.log.view
	}
	ct := func(t *testing.T) (*Model, *kittui.LogView) {
		m := openAppLog(t, clogBackend())
		m.mouseOn = true
		fillLog(m.clog.view, 100)
		m.render()
		return m, m.clog.view
	}
	for name, setup := range map[string]func(*testing.T) (*Model, *kittui.LogView){"run log": run, "container log": ct} {
		t.Run(name, func(t *testing.T) {
			m, v := setup(t)
			if !v.Following() {
				t.Fatal("not following")
			}
			before := m.render()
			wheelAt(m, 10, 8, tea.MouseWheelUp)
			if v.Following() || m.render() == before {
				t.Fatalf("the wheel did not scroll the log:\n%s", m.render())
			}
			zone := m.focus()
			for range 200 {
				wheelAt(m, 10, 8, tea.MouseWheelDown)
			}
			if !v.Following() || m.focus() != zone {
				t.Fatalf("following %v, zone %s", v.Following(), m.focus())
			}
		})
	}
}

func TestAClickOnALogButtonActsLikeItsKey(t *testing.T) {
	logs := map[string]func(t *testing.T) (*Model, *kittui.LogView){
		"run log": func(t *testing.T) (*Model, *kittui.LogView) {
			m := followed(t, i18n.English, demoBackend())
			return m, m.log.view
		},
		"container log": func(t *testing.T) (*Model, *kittui.LogView) {
			m := openAppLog(t, clogBackend())
			return m, m.clog.view
		},
	}
	for name, setup := range logs {
		t.Run(name, func(t *testing.T) {
			m, v := setup(t)
			m.mouseOn = true
			fillLog(v, 80)
			m.render()
			wheelAt(m, 10, 8, tea.MouseWheelUp)
			if v.Following() {
				t.Fatal("still following")
			}
			x, y := cellOf(t, m, "L follow")
			clickAt(m, x, y)
			if !v.Following() {
				t.Fatal("the follow button did not resume following")
			}
			x, y = cellOf(t, m, "w wrap")
			clickAt(m, x, y)
			if !strings.Contains(m.render(), "wrap lines on") {
				t.Fatalf("the wrap button did nothing:\n%s", m.render())
			}
			x, y = cellOf(t, m, "/ search")
			clickAt(m, x, y)
			if !v.Searching() || m.input() != kittui.InputText {
				t.Fatal("the search button did not open the search like its key")
			}
		})
	}
}

func TestATargetOfAnEarlierLogFrameDoesNothing(t *testing.T) {
	m := followed(t, i18n.English, demoBackend())
	m.mouseOn = true
	fillLog(m.log.view, 60)
	m.render()
	x, y := cellOf(t, m, "w wrap")
	send(m, "esc") // the log closes; the frame on screen is the list of runs
	clickAt(m, x, y)
	if m.log.open {
		t.Fatal("the click reopened the log")
	}
	wheelAt(m, x, y, tea.MouseWheelDown)
	if m.log.open {
		t.Fatal("the wheel reopened the log")
	}
}

func TestTheWheelUnderAnOpenQuestionScrollsNoLog(t *testing.T) {
	m := followed(t, i18n.English, demoBackend())
	m.mouseOn = true
	fillLog(m.log.view, 60)
	m.render()
	m.ask(textAnswerYes, "Remove the thing?", "It is gone.", nil, false, func() tea.Cmd { return nil })
	m.render()
	wheelAt(m, 10, 8, tea.MouseWheelUp)
	if !m.log.view.Following() {
		t.Fatal("the wheel scrolled the run log beneath a question")
	}

	m = newTestModel(t, i18n.English, demoBackend(), wideWidth, wheelRows)
	m.mouseOn = true
	send(m, "enter")
	m.render()
	m.ask(textAnswerYes, "Remove the thing?", "It is gone.", nil, false, func() tea.Cmd { return nil })
	m.render()
	wheelAt(m, 10, 8, tea.MouseWheelDown)
	if m.detail.top != 0 {
		t.Fatalf("the wheel scrolled the detail beneath a question: %d", m.detail.top)
	}
}
