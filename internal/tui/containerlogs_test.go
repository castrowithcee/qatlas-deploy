package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/i18n"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

var clogStart = time.Date(2026, time.October, 30, 17, 0, 0, 0, time.UTC)

const (
	clogApp = "demo-stack-app-1"
	clogDB  = "demo-stack-db-1"
)

func clogLine(source string, sec int, text string) control.ContainerLogLine {
	return control.ContainerLogLine{Time: clogStart.Add(time.Duration(sec) * time.Second), Source: source, Stream: "stdout", Text: text}
}

// clogBackend is containersBackend with output of the two containers of the own stack.
func clogBackend() *fakeBackend {
	b := containersBackend()
	b.ctLogLines = []control.ContainerLogLine{
		clogLine(clogApp, 0, "app started on port 8080"),
		clogLine(clogDB, 1, "database system is ready"),
		clogLine(clogApp, 2, "GET /health 200"),
		clogLine(clogDB, 3, "checkpoint complete"),
		clogLine(clogApp, 4, "token is ***"),
	}
	return b
}

// openAppLog opens the log of the app container from the Containers list.
func openAppLog(t *testing.T, b *fakeBackend) *Model {
	t.Helper()
	m := newTestModel(t, i18n.English, b, ctWide, ctWideRows)
	send(m, "5", "down", "down", "down", "down", "down", "o")
	if !m.clog.open || m.clog.name != clogApp {
		t.Fatalf("clog = %+v", m.clog)
	}
	return m
}

func TestContainerLogOpensWithOneRequest(t *testing.T) {
	b := clogBackend()
	m := openAppLog(t, b)
	if len(b.ctLogArgs) != 1 || b.ctLogArgs[0].Name != clogApp || b.ctLogArgs[0].Tail != clogTail || !b.ctLogArgs[0].Cursor.IsZero() {
		t.Fatalf("requests %+v", b.ctLogArgs)
	}
	contains(t, m, "app started on port 8080", "GET /health 200", "token is ***")
	noLeak(t, m.render(), "database system is ready")
}

func TestContainerLogFollowsFromTheCursor(t *testing.T) {
	b := clogBackend()
	m := openAppLog(t, b)
	tm := timersOf[m]
	last := b.ctLogLines[len(b.ctLogLines)-1].Time.Add(time.Nanosecond)
	if tm.waits[len(tm.waits)-1] != logInterval {
		t.Fatalf("timers %v", tm.waits)
	}
	// Timers of the Containers area come first; fire until the log's own read has run.
	b.ctLogLines = append(b.ctLogLines, clogLine(clogApp, 5, "new line"))
	before := len(b.ctLogArgs)
	fireUntil(t, m, func() bool { return len(b.ctLogArgs) > before })
	got := b.ctLogArgs[before]
	if !got.Cursor.Equal(last) || got.Tail != 0 || !got.Since.IsZero() {
		t.Fatalf("follow request %+v, want cursor %v", got, last)
	}
	if m.clog.view.Len() != 4 {
		t.Fatalf("lines %d, want 4 (3 of the app and the new one)", m.clog.view.Len())
	}
	contains(t, m, "new line")
	// Nothing new: no line twice.
	n := len(b.ctLogArgs)
	fireUntil(t, m, func() bool { return len(b.ctLogArgs) > n })
	if m.clog.view.Len() != 4 {
		t.Fatalf("lines %d after a read without news", m.clog.view.Len())
	}
}

func TestContainerLogReadsFurtherPages(t *testing.T) {
	b := clogBackend()
	b.ctLogPage = 2
	m := newTestModel(t, i18n.English, b, ctWide, ctWideRows)
	send(m, "enter", "o") // the stack detail: all five lines of both containers
	if len(b.ctLogArgs) != 3 {
		t.Fatalf("%d requests, want 3 pages: %+v", len(b.ctLogArgs), b.ctLogArgs)
	}
	if b.ctLogArgs[1].Cursor.IsZero() || b.ctLogArgs[1].Tail != 0 || !b.ctLogArgs[2].Cursor.After(b.ctLogArgs[1].Cursor) {
		t.Fatalf("pages do not continue at the cursor: %+v", b.ctLogArgs)
	}
	if m.clog.view.Len() != 5 {
		t.Fatalf("lines %d, want 5", m.clog.view.Len())
	}
}

func TestContainerLogStopsAfterClosing(t *testing.T) {
	b := clogBackend()
	m := openAppLog(t, b)
	send(m, "esc")
	if m.clog.open || m.area != areaContainers || m.ctv.open {
		t.Fatalf("open %v, area %s", m.clog.open, m.area)
	}
	if got, _ := m.ctList.Selected(); string(got) != clogApp {
		t.Fatalf("marked %q", got)
	}
	reads := len(b.ctLogArgs)
	fireAll(t, m)
	if len(b.ctLogArgs) != reads {
		t.Fatalf("%d reads after closing", len(b.ctLogArgs)-reads)
	}
	// Opened again, the old timer does not start a second chain.
	send(m, "o")
	reads = len(b.ctLogArgs)
	fireAll(t, m)
	if len(b.ctLogArgs)-reads > 2 {
		t.Fatalf("%d reads while a log is open and the area refreshes", len(b.ctLogArgs)-reads)
	}
}

func TestContainerLogStopsFollowingWhenScrolledAway(t *testing.T) {
	b := clogBackend()
	m := openAppLog(t, b)
	send(m, "up") // leaves the newest line: following is off
	reads := len(b.ctLogArgs)
	fireAll(t, m)
	if len(b.ctLogArgs) != reads {
		t.Fatalf("%d reads while not following", len(b.ctLogArgs)-reads)
	}
	b.ctLogLines = append(b.ctLogLines, clogLine(clogApp, 5, "after the pause"))
	send(m, "end") // following again, and the timer is back
	n := len(b.ctLogArgs)
	fireUntil(t, m, func() bool { return len(b.ctLogArgs) > n })
	contains(t, m, "after the pause")
}

func TestContainerLogFilterReloadsWithTheBounds(t *testing.T) {
	b := clogBackend()
	b.ctLogLines = []control.ContainerLogLine{
		{Time: filterNow.Add(-4 * time.Hour), Source: clogApp, Text: "early line"},
		{Time: filterNow.Add(-90 * time.Minute), Source: clogApp, Text: "middle line"},
		{Time: filterNow.Add(-10 * time.Minute), Source: clogApp, Text: "late line"},
	}
	m := newTestModel(t, i18n.English, b, ctWide, ctWideRows)
	m.now = func() time.Time { return filterNow }
	send(m, "5", "down", "down", "down", "down", "down", "o")
	if m.clog.view.Len() != 3 {
		t.Fatalf("lines %d", m.clog.view.Len())
	}
	lastFilter(m, 1, "2", 1) // 2 hours
	got := b.ctLogArgs[len(b.ctLogArgs)-1]
	if !got.Since.Equal(filterNow.Add(-2*time.Hour)) || !got.Until.IsZero() || got.Tail != clogTail || !got.Cursor.IsZero() {
		t.Fatalf("request %+v", got)
	}
	screen := m.render()
	if m.clog.view.Len() != 2 || strings.Contains(screen, "early line") || !strings.Contains(screen, "middle line") {
		t.Fatalf("lines %d:\n%s", m.clog.view.Len(), screen)
	}
}

func TestContainerLogErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"gone":        {control.NewError(control.CodeNotFound, "no such container"), "no longer exists"},
		"unreachable": {control.NewError(control.CodeDockerUnreachable, "engine message that is not shown"), "container engine is not reachable"},
		"timeout":     {context.DeadlineExceeded, "did not answer in time"},
	} {
		t.Run(name, func(t *testing.T) {
			b := clogBackend()
			b.errs["containerlogs:"+clogApp] = tc.err
			m := openAppLog(t, b)
			contains(t, m, tc.want)
			noLeak(t, m.render(), "engine message that is not shown")
			reads := b.called("containerlogs")
			fireAll(t, m)
			if b.called("containerlogs") != reads {
				t.Fatal("the log reads on after an error")
			}
			// The reload key tries again.
			delete(b.errs, "containerlogs:"+clogApp)
			send(m, "r")
			contains(t, m, "app started on port 8080")
			noLeak(t, m.render(), tc.want)
		})
	}
}

func TestStackLogShowsTheSourceOfEveryLine(t *testing.T) {
	b := clogBackend()
	m := newTestModel(t, i18n.English, b, ctWide, ctWideRows)
	send(m, "enter", "o")
	if len(b.ctLogArgs) != 1 || b.ctLogArgs[0].Stack != "demo-stack" || b.ctLogArgs[0].Name != "" {
		t.Fatalf("requests %+v", b.ctLogArgs)
	}
	var app, db bool
	for _, l := range strings.Split(m.render(), "\n") {
		app = app || strings.Contains(l, clogApp) && strings.Contains(l, "GET /health 200")
		db = db || strings.Contains(l, clogDB) && strings.Contains(l, "checkpoint complete")
	}
	if !app || !db {
		t.Fatalf("the lines do not name their container:\n%s", m.render())
	}
	send(m, "esc")
	if m.clog.open || m.area != areaDetail || chosenStack(m) != "demo-stack" {
		t.Fatalf("open %v, area %s", m.clog.open, m.area)
	}
}

func TestContainerLogKeyIsListedInTheFooter(t *testing.T) {
	m := newTestModel(t, i18n.English, clogBackend(), ctWide, ctWideRows)
	send(m, "5")
	contains(t, m, "o log")
	send(m, "enter")
	contains(t, m, "o log")
	send(m, "esc", "1", "enter")
	contains(t, m, "o log")
}

func TestContainerLogDoesNotOpenOnAGroupRow(t *testing.T) {
	b := clogBackend()
	m := newTestModel(t, i18n.English, b, ctWide, ctWideRows)
	send(m, "5", "home", "o")
	if m.clog.open || len(b.ctLogArgs) != 0 {
		t.Fatalf("a log opened on %v", m.clog)
	}
}

func TestGoldenContainerLog(t *testing.T) {
	goldenContainers(t, "containers.log", clogBackend, "5", "down", "down", "down", "down", "down", "o")
}

func TestGoldenStackLog(t *testing.T) {
	goldenContainers(t, "detail.log", clogBackend, "enter", "o")
}

func TestGoldenContainerLogError(t *testing.T) {
	goldenContainers(t, "containers.log.error", func() *fakeBackend {
		b := clogBackend()
		b.errs["containerlogs:"+clogApp] = control.NewError(control.CodeDockerUnreachable, "engine message that is not shown")
		return b
	}, "5", "down", "down", "down", "down", "down", "o")
}

func chosenStack(m *Model) string {
	name, _ := m.detailStack()
	return name
}

// fireAll fires the timers that run now, not those they set.
func fireAll(t *testing.T, m *Model) {
	t.Helper()
	for n := len(timersOf[m].fns); n > 0; n-- {
		timersOf[m].fire(t, m)
	}
}

// fireUntil fires timers in turn until done; the Containers area keeps setting its own, so it is bounded.
func fireUntil(t *testing.T, m *Model, done func() bool) {
	t.Helper()
	for i := 0; i < 10 && !done(); i++ {
		timersOf[m].fire(t, m)
	}
	if !done() {
		t.Fatal("the timers did not lead to the expected read")
	}
}
