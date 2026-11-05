package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

const (
	activeRun   = "20261030T150000Z-0000000b"
	finishedRun = "20261030T140200Z-0000000c"
)

// hostile is text from a registry, GitHub or a log that tries to move the cursor, retitle the window, link and
// reorder the text.
const hostile = "evil\x1b[2J\x1b]8;;http://example.invalid\x07name\x1bP1$r\x9b31m\r\x00\u202e"

// badBytes are what must never reach the terminal; the styles of a colour profile are the only escape
// sequences that may, so a bare escape is checked without colour only.
var badBytes = []string{"\x1b[2J", "\x1b]8", "\x1bP", "\x07", "\r", "\x00", "\u202e", "\x9b"}

func noBadBytes(t *testing.T, what, screen string, profile kittui.ColorProfile) {
	t.Helper()
	bad := append([]string{"\x1b"}, badBytes...)
	if profile != kittui.ProfileNone {
		bad = badBytes
	}
	for _, bad := range bad {
		if strings.Contains(screen, bad) {
			t.Errorf("%s: %q reaches the terminal", what, bad)
		}
	}
}

func TestDetailShowsLoadingThenEachSectionOnItsOwn(t *testing.T) {
	b := demoBackend()
	b.errs["changelog:demo-stack"] = errServiceBusy()
	m := newTestModel(t, i18n.English, b, wideWidth, 40)
	_, cmd := m.Update(keyMsg("enter"))
	if screen := m.render(); strings.Count(screen, "Loading…") != 3 {
		t.Fatalf("all sections should load:\n%s", screen)
	}
	drain(m, cmd)
	screen := m.render()
	for _, want := range []string{"the service is busy", "20261030T140100Z-0123abcd", "12.0 MiB", "incomplete",
		"1.4.2 -> 1.5.0", "update available", "compose pull failed: registry unreachable", "poll", "850ms"} {
		if !strings.Contains(screen, want) {
			t.Errorf("%q missing:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "Spring release") || strings.Contains(screen, "Loading…") {
		t.Errorf("the failed section shows content or the others still load:\n%s", screen)
	}
}

func TestDetailReadsOncePerStackAndAgainOnReload(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, 40)
	send(m, "enter")
	send(m, "esc", "enter")
	if b.called("changelog") != 1 || b.called("backups") != 1 {
		t.Fatalf("calls %v", b.calls)
	}
	send(m, "r")
	if b.called("changelog") != 2 || b.called("backups") != 2 || b.called("status") != 2 {
		t.Fatalf("reload: calls %v", b.calls)
	}
	send(m, "esc", "down", "enter")
	if b.called("changelog") != 3 || m.detail.stack != "mail-relay" {
		t.Fatalf("another stack: calls %v, detail of %q", b.calls, m.detail.stack)
	}
}

func TestDetailDropsAnswersOfAStackThatWasLeft(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, 40)
	_, first := m.Update(keyMsg("enter"))
	press(m, "esc", "down")
	_, second := m.Update(keyMsg("enter"))
	drain(m, first)
	if m.detail.cl.loaded || m.detail.bk.loaded {
		t.Fatal("the answers of the first stack reached the second")
	}
	drain(m, second)
	if !m.detail.cl.loaded || m.detail.stack != "mail-relay" || m.detail.changelog.Stack != "mail-relay" {
		t.Fatalf("detail = %+v", m.detail)
	}
}

func TestDetailScrollsAndKeepsItsPlace(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), narrowWidth, 14)
	send(m, "enter")
	first := m.render()
	if !strings.Contains(first, "demo-stack") || !strings.Contains(first, "/59") {
		t.Fatalf("top:\n%s", first)
	}
	send(m, "down", "down")
	if m.detail.top != 2 {
		t.Fatalf("top %d", m.detail.top)
	}
	send(m, "end")
	last := m.render()
	if !strings.Contains(last, "secrets-env-not-ignored") || !strings.Contains(last, "59/59") {
		t.Fatalf("end:\n%s", last)
	}
	send(m, "down", "pgdown")
	if m.detail.top != m.detail.total-m.detail.rows {
		t.Fatalf("scrolled past the end: top %d of %d", m.detail.top, m.detail.total)
	}
	send(m, "home", "up")
	if m.detail.top != 0 {
		t.Fatalf("top %d", m.detail.top)
	}
}

func TestDetailWithoutUpdatesOrImagesSaysSo(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, 40)
	send(m, "/", "w", "i", "k", "enter", "enter")
	screen := m.render()
	for _, want := range []string{"unchecked", "Never checked for updates", "No backups.", "wiki"} {
		if !strings.Contains(screen, want) {
			t.Errorf("%q missing:\n%s", want, screen)
		}
	}
}

func TestDetailShowsTheFunctionsOfTheCheck(t *testing.T) {
	for lang, want := range map[i18n.Lang][]string{
		i18n.English: {"Functions", "Update  not possible, none", "updates are not switched on", "To do: Switch on updates", "tracked-data  warning"},
		i18n.German:  {"Funktionen", "Update  geht nicht, keiner", "updates are not switched on", "Zu tun: Switch on updates", "tracked-data  Warnung"},
	} {
		m := newTestModel(t, lang, demoBackend(), wideWidth, 90)
		send(m, "/", "w", "i", "k", "enter", "enter")
		screen := m.render()
		for _, w := range want {
			if !strings.Contains(screen, w) {
				t.Errorf("%s: %q missing:\n%s", lang, w, screen)
			}
		}
	}
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, 90)
	send(m, "down", "enter")
	if screen := m.render(); !strings.Contains(screen, "Not checked.") {
		t.Errorf("a stack without a report:\n%s", screen)
	}
}

func TestDetailEscapesTextFromTheOutside(t *testing.T) {
	b := demoBackend()
	b.updates.Stacks[0].Items[0].Variable = hostile
	b.updates.Stacks[0].Items[0].Image = hostile
	b.updates.Stacks[0].Error = hostile
	b.changelogs["demo-stack"] = control.ChangelogResult{Stack: "demo-stack", Checked: at(30, 3, 5), Items: []control.ChangelogInfo{{
		Variable: hostile, Current: hostile, Target: hostile, Source: hostile, Link: hostile, Reason: hostile,
		Releases: []control.ChangelogRelease{{Version: hostile, Name: hostile, Body: hostile + "\n" + hostile}}}}}
	b.backups["demo-stack"] = control.BackupListResult{Backups: []control.BackupInfo{{ID: hostile, Created: at(30, 1, 1)}}}
	b.runs.Runs[1].Error, b.runs.Runs[1].Trigger = hostile, hostile
	b.check.Stacks[0].Functions[0].Reason, b.check.Stacks[0].Findings[0].Fix = hostile, hostile
	b.errs["backups:mail-relay"] = errServiceBusy()
	for _, profile := range []kittui.ColorProfile{kittui.ProfileNone, kittui.ProfileTrueColor} {
		for _, width := range []int{narrowWidth, wideWidth} {
			m := newTestModel(t, i18n.English, b, width, 120)
			m.profile = profile
			m.restyle()
			send(m, "enter")
			screen := m.render()
			noBadBytes(t, "detail", screen, profile)
			if !strings.Contains(screen, `evil\x1b[2J`) {
				t.Errorf("the escaped text is not shown:\n%s", screen)
			}
		}
	}
}

func TestRunsListsActiveAndRecordedRunsNewestFirst(t *testing.T) {
	b := demoBackend()
	// An active run is in the status only; it is recorded when it ends.
	b.runs.Runs = b.runs.Runs[1:]
	rows := runRows(b.snapshot())
	if len(rows) != 7 || rows[0].id != activeRun || !rows[0].active || rows[0].stack != "web-shop" ||
		rows[0].started != at(30, 15, 0) {
		t.Fatalf("rows = %+v", rows[0])
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].active || rows[i].started.After(rows[i-1].started) {
			t.Errorf("row %d out of order: %+v", i, rows[i])
		}
	}
}

func TestRunsCanBeLimitedToTheChosenStack(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	send(m, "3", "f")
	if m.runsOnly || !strings.Contains(m.render(), "wiki") {
		t.Fatal("the limit was set without a chosen stack")
	}
	if !strings.Contains(m.render(), "Choose a stack in Stacks first.") {
		t.Fatalf("no notice:\n%s", m.render())
	}
	send(m, "1", "enter", "3", "f")
	screen := m.render()
	if !m.runsOnly || strings.Contains(screen, "wiki") || !strings.Contains(screen, "Runs of demo-stack only. f shows all stacks.") {
		t.Fatalf("limited:\n%s", screen)
	}
	send(m, "f")
	if m.runsOnly || !strings.Contains(m.render(), "wiki") {
		t.Fatal("the limit stayed")
	}
}

func TestRunsSurviveTheRefreshWithTheirSelection(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, goldenRows)
	send(m, "3", "down", "down")
	id, _ := m.runsList.Selected()
	load(t, m)
	if now, _ := m.runsList.Selected(); now != id {
		t.Fatalf("selection %q became %q", id, now)
	}
}

// followed opens the log of the active run, with its log as the service has it so far.
func followed(t *testing.T, lang i18n.Lang, b *fakeBackend) *Model {
	t.Helper()
	m := newTestModel(t, lang, b, wideWidth, 20)
	send(m, "3", "enter")
	if !m.log.open || m.log.row.id != activeRun || !m.log.active {
		t.Fatalf("log = %+v", m.log)
	}
	return m
}

func TestFollowingAppendsOnlyWhatIsNew(t *testing.T) {
	b := demoBackend()
	m := followed(t, i18n.English, b)
	rec := timersOf[m]
	if m.log.view.Len() != 6 || len(rec.fns) != 1 || rec.waits[0] != logInterval {
		t.Fatalf("lines %d, timers %v", m.log.view.Len(), rec.waits)
	}
	// Nothing new: nothing appended.
	rec.fire(t, m)
	if m.log.view.Len() != 6 || len(rec.fns) != 1 {
		t.Fatalf("lines %d, timers %d", m.log.view.Len(), len(rec.fns))
	}
	// New lines arrive, the last one only half written.
	b.setLog(activeRun, demoActiveLog+"2026-10-30T15:00:40Z info up -d finished\n2026-10-30T15:00:41Z step ver")
	rec.fire(t, m)
	if m.log.view.Len() != 7 || strings.Contains(m.render(), "step ver") {
		t.Fatalf("lines %d, the half line is shown:\n%s", m.log.view.Len(), m.render())
	}
	// Its end arrives.
	b.setLog(activeRun, demoActiveLog+"2026-10-30T15:00:40Z info up -d finished\n2026-10-30T15:00:41Z step verify\n")
	rec.fire(t, m)
	screen := m.render()
	if m.log.view.Len() != 8 || strings.Count(screen, "up -d finished") != 1 || strings.Count(screen, "verify") != 1 {
		t.Fatalf("lines %d:\n%s", m.log.view.Len(), screen)
	}
	if !m.log.view.Following() || !strings.Contains(screen, "following") {
		t.Fatalf("not following:\n%s", screen)
	}
}

func TestFollowingStopsWhenTheRunEnds(t *testing.T) {
	b := demoBackend()
	m := followed(t, i18n.English, b)
	rec := timersOf[m]
	statuses := b.called("status")
	// The run ends and writes its last line without a line break.
	b.status.Engine.Stacks[4].ActiveRun = ""
	b.setLog(activeRun, demoActiveLog+"2026-10-30T15:00:50Z info all services healthy")
	rec.fire(t, m)
	if m.log.active || m.log.view.Len() != 7 || len(rec.fns) != 0 {
		t.Fatalf("active %v, lines %d, timers %d", m.log.active, m.log.view.Len(), len(rec.fns))
	}
	if !strings.Contains(m.render(), "all services healthy") {
		t.Fatalf("last line missing:\n%s", m.render())
	}
	// The end leads to a refresh of the whole screen, and nothing polls on.
	if got := b.called("status") - statuses; got != 2 {
		t.Fatalf("%d status reads after the end, want the poll and the refresh", got)
	}
	logs := b.called("log")
	send(m, "r")
	if b.called("log") != logs+1 {
		t.Fatal("reload did not read the log once")
	}
	if len(rec.fns) != 0 {
		t.Fatal("a timer runs for a finished run")
	}
}

func TestFollowingIsBounded(t *testing.T) {
	b := demoBackend()
	m := followed(t, i18n.English, b)
	rec := timersOf[m]
	// A timer fired while a read is under way starts nothing.
	m.log.busy = true
	_, cmd := m.Update(logTickMsg{gen: m.log.gen})
	if cmd != nil {
		t.Fatal("a second read started")
	}
	m.log.busy = false

	// Left to another area, the timer fires and ends the chain.
	press(m, "1")
	logs := b.called("log")
	rec.fire(t, m)
	if b.called("log") != logs || len(rec.fns) != 0 || m.log.pending {
		t.Fatalf("polled outside the Runs area: logs %d, timers %d", b.called("log")-logs, len(rec.fns))
	}
	// Back in the Runs area it follows again, with one timer.
	press(m, "3")
	press(m, "3")
	if len(rec.fns) != 1 {
		t.Fatalf("%d timers", len(rec.fns))
	}
	// The 30 s refresh is not slowed or doubled.
	_, cmd = m.Update(tickMsg{})
	if cmd == nil || !m.loading {
		t.Fatal("the refresh did not start")
	}
	// A tick of an earlier log does nothing.
	m.log.busy = false
	if _, cmd := m.Update(logTickMsg{gen: m.log.gen - 1}); cmd != nil {
		t.Fatal("an old timer read")
	}
}

func TestFinishedRunIsReadOnceAndNotFollowed(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, 20)
	send(m, "3", "down", "enter")
	rec := timersOf[m]
	if m.log.row.id != finishedRun || m.log.active || len(rec.fns) != 0 || m.log.view.Len() != 9 {
		t.Fatalf("log = %+v, lines %d", m.log, m.log.view.Len())
	}
	if b.called("log") != 1 {
		t.Fatalf("%d reads", b.called("log"))
	}
}

func TestRewrittenLogStartsOverWithoutDuplicates(t *testing.T) {
	b := demoBackend()
	m := followed(t, i18n.English, b)
	b.setLog(activeRun, "2026-10-30T15:00:00Z step restart\n2026-10-30T15:00:01Z info again\n")
	timersOf[m].fire(t, m)
	if m.log.view.Len() != 2 || strings.Contains(m.render(), "compose file") {
		t.Fatalf("lines %d:\n%s", m.log.view.Len(), m.render())
	}
}

func TestLogErrorIsShownAndStopsFollowing(t *testing.T) {
	b := demoBackend()
	m := followed(t, i18n.English, b)
	b.errs["log:"+activeRun] = errServiceBusy()
	timersOf[m].fire(t, m)
	screen := m.render()
	if !strings.Contains(screen, "the service is busy") || !strings.Contains(screen, "info pull example/shop") || len(timersOf[m].fns) != 0 {
		t.Fatalf("timers %d:\n%s", len(timersOf[m].fns), screen)
	}
	// A run without a log says so.
	b2 := demoBackend()
	delete(b2.logs, finishedRun)
	m = newTestModel(t, i18n.English, b2, wideWidth, 20)
	send(m, "3", "down", "enter")
	if !strings.Contains(m.render(), "no such run") {
		t.Fatalf("screen:\n%s", m.render())
	}
}

func TestLogKeys(t *testing.T) {
	m := followed(t, i18n.English, demoBackend())
	// Scrolling up pauses, L resumes.
	send(m, "up")
	if m.log.view.Following() {
		t.Fatal("up kept following")
	}
	send(m, "L")
	if !m.log.view.Following() {
		t.Fatal("L did not resume following")
	}
	// Letters typed into the search are text.
	if cmd := press(m, "/", "q", "u"); isQuit(cmd) {
		t.Fatal("q quit while a search was typed")
	}
	if m.log.view.Query() != "qu" || m.input() != kittui.InputText {
		t.Fatalf("query %q", m.log.view.Query())
	}
	press(m, "esc")
	if m.log.view.Query() != "" || !m.log.open {
		t.Fatal("esc did not clear the search first")
	}
	// Esc leaves the log, then the area.
	press(m, "esc")
	if m.log.open || m.area != areaRuns {
		t.Fatalf("open %v, area %s", m.log.open, m.area)
	}
	press(m, "esc")
	if m.area != areaStacks {
		t.Fatalf("area %s", m.area)
	}
	send(m, "3", "enter")
	if !isQuit(press(m, "q")) {
		t.Fatal("q does not quit in the log")
	}
}

func TestLogLinesAreSplitIntoTimeKindAndText(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, 20)
	l := m.logLine("2026-10-30T15:00:12Z info backup done\r")
	if l.Source != "info" || l.Text != "backup done" || !l.Time.Equal(at(30, 15, 0).Add(12e9)) {
		t.Fatalf("line = %+v", l)
	}
	for _, raw := range []string{"plain text", "not-a-time info text", "2026-10-30T15:00:12Z", "2026-10-30T15:00:12Z onlykind"} {
		if l := m.logLine(raw); l.Source != "" && l.Text == "" || l.Time.IsZero() && l.Text != raw {
			t.Errorf("%q became %+v", raw, l)
		}
	}
}

func TestLogTextIsEscaped(t *testing.T) {
	for _, profile := range []kittui.ColorProfile{kittui.ProfileNone, kittui.ProfileTrueColor} {
		b := demoBackend()
		b.setLog(activeRun, "2026-10-30T15:00:00Z info "+hostile+"\n"+hostile+"\n2026-10-30T15:00:02Z "+hostile+" x\n")
		b.runs.Runs[0].Stack = hostile
		b.status.Engine.Stacks[4].Name = hostile
		m := newTestModel(t, i18n.English, b, wideWidth, 20)
		m.profile = profile
		m.restyle()
		send(m, "3")
		noBadBytes(t, "runs list", m.render(), profile)
		send(m, "enter", "t", "w")
		screen := m.render()
		noBadBytes(t, "log", screen, profile)
		if !strings.Contains(screen, `evil\x1b[2J`) {
			t.Errorf("profile %d: the escaped line is not shown:\n%s", profile, screen)
		}
		send(m, "/", "\x1b", "enter")
		noBadBytes(t, "log search", m.render(), profile)
	}
}

func TestOverviewOnlyReads(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, 30)
	send(m, "enter", "r", "esc", "3", "enter", "L", "r", "esc", "esc")
	for name := range b.calls {
		switch name {
		case "status", "stacks", "check", "updates", "runs", "log", "changelog", "backups", "secrets":
		default:
			t.Errorf("unexpected call %s", name)
		}
	}
	if b.called("refresh") != 0 {
		t.Error("the registries were asked")
	}
}

func TestNarrowAndWideLayoutsKeepTheirWidth(t *testing.T) {
	for _, width := range []int{minWidth, 44, 56, 80, 120, 200} {
		for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
			m := newTestModel(t, lang, demoBackend(), width, 24)
			for _, keys := range [][]string{{"enter"}, {"3"}, {"3", "enter"}, {"3", "down", "enter", "t"}, {"3", "enter", "w", "/", "a"}} {
				send(m, keys...)
				checkScreen(t, m.render(), width, 24)
				m = newTestModel(t, lang, demoBackend(), width, 24)
			}
		}
	}
}
