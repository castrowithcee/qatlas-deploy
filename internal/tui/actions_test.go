package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The stacks of the demo host in list order: demo-stack, mail-relay, old-tool, photo-album, web-shop, wiki.
// The cursor starts on demo-stack, which has an update; photo-album is paused after a failed rollback.

func newActionModel(t *testing.T, b *fakeBackend) *Model {
	t.Helper()
	return newTestModel(t, i18n.English, b, wideWidth, 60)
}

func TestEveryActionCallsTheServiceOnlyAfterConfirmation(t *testing.T) {
	for _, tt := range []struct{ key, want string }{
		{"d", "deploy:demo-stack"}, {"u", "update:demo-stack"}, {"p", "pause:demo-stack"},
		{"P", "unpause:demo-stack"}, {"i", "pin:demo-stack"}, {"I", "unpin:demo-stack"}, {"c", "refresh:*"},
	} {
		t.Run(tt.key, func(t *testing.T) {
			b := demoBackend()
			m := newActionModel(t, b)
			for _, no := range []string{"n", "esc", "enter", "x", "q", "r"} {
				send(m, tt.key)
				if m.modal == nil {
					t.Fatalf("%s opened no question", tt.key)
				}
				send(m, no)
				if (no == "x" || no == "q" || no == "r") != (m.modal != nil) {
					t.Fatalf("answer %q: question open = %v", no, m.modal != nil)
				}
				send(m, "esc")
				if m.modal != nil {
					t.Fatalf("answer %q: question stays open", no)
				}
				if got := b.actions(); len(got) != 0 {
					t.Fatalf("answer %q reached the service: %v", no, got)
				}
			}
			send(m, tt.key, "y")
			if got := b.actions(); !slices.Equal(got, []string{tt.want}) {
				t.Fatalf("calls = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResumeIsOnlyForAStackPausedAfterRollback(t *testing.T) {
	b := demoBackend()
	m := newActionModel(t, b)
	if strings.Contains(m.render(), "g resume") {
		t.Error("resume is offered for a stack that is not paused after a rollback")
	}
	send(m, "g")
	if m.modal != nil || !strings.Contains(m.render(), "demo-stack is not paused after a rollback") {
		t.Fatalf("resume was not refused:\n%s", m.render())
	}
	// A ticked stack that is not paused refuses the whole action, whatever the cursor is on.
	send(m, "space", "down", "down", "down", "g")
	if m.modal != nil || len(b.actions()) != 0 {
		t.Fatal("resume went ahead with a stack that is not paused after a rollback")
	}
	if !strings.Contains(m.render(), "g resume") {
		t.Error("resume is not offered for the stack paused after a rollback")
	}
}

func TestResumeOnTheCursorStack(t *testing.T) {
	b := demoBackend()
	m := newActionModel(t, b)
	send(m, "down", "down", "down", "g")
	if m.modal == nil {
		t.Fatalf("no question for resume:\n%s", m.render())
	}
	send(m, "y")
	if got := b.actions(); !slices.Equal(got, []string{"resume:photo-album"}) {
		t.Fatalf("calls = %v", got)
	}
	if !strings.Contains(m.render(), "pause cleared, nothing started") {
		t.Errorf("summary:\n%s", m.render())
	}
}

func TestUpdateShowsWhatChangesAndTheChangelogBeforeConfirming(t *testing.T) {
	b := demoBackend()
	m := newActionModel(t, b)
	cmd := press(m, "u")
	screen := m.render()
	for _, want := range []string{"Update demo-stack?", "APP_VERSION", "1.4.2 -> 1.5.0", "update available", "Loading"} {
		if !strings.Contains(screen, want) {
			t.Errorf("preview misses %q:\n%s", want, screen)
		}
	}
	// The yes answer exists once the changelog is in.
	press(m, "y")
	if m.modal == nil || len(b.actions()) != 0 {
		t.Fatal("update was confirmed before the changelog was read")
	}
	drain(m, cmd)
	screen = m.render()
	for _, want := range []string{"Spring release", "Adds a dark theme.", "faster start", "y yes, do it"} {
		if !strings.Contains(screen, want) {
			t.Errorf("preview misses %q:\n%s", want, screen)
		}
	}
	if len(b.actions()) != 0 {
		t.Fatal("update ran before the confirmation")
	}
	send(m, "y")
	if got := b.actions(); !slices.Equal(got, []string{"update:demo-stack"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestUpdatePreviewFlagsANewerMajorAndWorksWithoutChangelog(t *testing.T) {
	b := demoBackend()
	m := newActionModel(t, b)
	send(m, "down", "down", "down", "down", "u")
	screen := m.render()
	for _, want := range []string{"Update web-shop?", "newer major 6.0.0", "not installed automatically",
		"The stored detection shows no update"} {
		if !strings.Contains(screen, want) {
			t.Errorf("preview misses %q:\n%s", want, screen)
		}
	}
	if b.called("changelog") != 0 {
		t.Error("the changelog was read for a stack without an update")
	}
	if !strings.Contains(screen, "y yes") {
		t.Errorf("nothing to read, yet no yes answer:\n%s", screen)
	}
}

func TestUpdatePreviewShowsAFailedChangelogAndStillAllowsTheUpdate(t *testing.T) {
	b := demoBackend()
	b.errs["changelog:demo-stack"] = errServiceBusy()
	m := newActionModel(t, b)
	send(m, "u")
	if !strings.Contains(m.render(), "the service is busy") {
		t.Errorf("the failure is not shown:\n%s", m.render())
	}
	send(m, "y")
	if got := b.actions(); !slices.Equal(got, []string{"update:demo-stack"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestLeavingThePreviewDropsLateChangelogs(t *testing.T) {
	b := demoBackend()
	m := newActionModel(t, b)
	cmd := press(m, "u")
	send(m, "esc")
	drain(m, cmd)
	if m.modal != nil {
		t.Fatal("a late changelog reopened the question")
	}
	send(m, "d")
	if m.modal == nil || m.modal.kind != modalAsk {
		t.Fatal("the next question did not open")
	}
}

// tick ticks the stacks at the given rows of the list, counted from the first.
func tick(m *Model, rows ...int) {
	send(m, "home")
	at := 0
	for _, r := range rows {
		for ; at < r; at++ {
			send(m, "down")
		}
		send(m, "space")
	}
	send(m, "home")
}

func TestMultiSelectionRunsInListOrderAndContinuesAfterAFailure(t *testing.T) {
	b := demoBackend()
	b.errs["pin:mail-relay"] = control.NewError(control.CodeLocalChanges, "the stack has local changes")
	b.holds = map[string]control.HoldResult{"pin:wiki": {Stack: "wiki", Active: true, Changed: false}}
	m := newActionModel(t, b)
	send(m, "end", "space", "home", "space", "down", "space") // not in the order of the list
	send(m, "home")
	if got := m.list.Ticked(); len(got) != 3 {
		t.Fatalf("ticked = %v", got)
	}
	send(m, "i")
	if len(b.actions()) != 0 {
		t.Fatal("pin ran before the confirmation")
	}
	screen := m.render()
	for _, want := range []string{"Pin 3 stacks?", "demo-stack", "mail-relay", "wiki"} {
		if !strings.Contains(screen, want) {
			t.Errorf("question misses %q:\n%s", want, screen)
		}
	}
	// One call at a time: the answer of the first starts the second.
	cmd := press(m, "y")
	msg := cmd()
	if got := b.actions(); !slices.Equal(got, []string{"pin:demo-stack"}) {
		t.Fatalf("calls before the first answer = %v", got)
	}
	if !strings.Contains(m.render(), "pin 1/3: demo-stack") {
		t.Errorf("the header does not show the progress:\n%s", m.render())
	}
	_, next := m.Update(msg)
	drain(m, next)
	if got := b.actions(); !slices.Equal(got, []string{"pin:demo-stack", "pin:mail-relay", "pin:wiki"}) {
		t.Fatalf("calls = %v", got)
	}
	screen = m.render()
	for _, want := range []string{"pin finished: 2 ok, 1 failed", "demo-stack", "pinned", "mail-relay", "failed",
		"the service refused the request: the stack has local changes", "pinned (it already was, nothing changed)"} {
		if !strings.Contains(screen, want) {
			t.Errorf("summary misses %q:\n%s", want, screen)
		}
	}
	if m.busy() {
		t.Error("the sequence is still marked as running")
	}
	send(m, "enter")
	if m.modal != nil {
		t.Fatal("enter did not close the summary")
	}
	// The stack that failed stays ticked for another try.
	if got := m.list.Ticked(); len(got) != 1 || got[0] != "mail-relay" {
		t.Errorf("ticked after the run = %v", got)
	}
	if b.called("status") < 2 {
		t.Error("the data was not read again")
	}
}

func TestUpdateOfSeveralStacksIsOneCallPerStack(t *testing.T) {
	b := demoBackend()
	b.updateRuns = map[string]control.StackUpdateRun{
		"mail-relay": {Name: "mail-relay", Error: "the stack has local changes"},
		"wiki": {Name: "wiki", Steps: []control.UpdateStepInfo{
			{Variable: "WIKI_VERSION", From: "1.41.0", To: "1.42.0", Run: "20261031T100100Z-wiki", Result: "rolled-back", Error: "health check failed"},
			{Variable: "WIKI_VERSION", From: "1.42.0", To: "1.43.0"}}},
	}
	m := newActionModel(t, b)
	tick(m, 0, 1, 5)
	send(m, "u")
	for _, want := range []string{"Update 3 stacks?", "demo-stack", "mail-relay", "wiki"} {
		if !strings.Contains(m.render(), want) {
			t.Errorf("preview misses %q:\n%s", want, m.render())
		}
	}
	send(m, "y")
	if b.called("update.call") != 3 {
		t.Errorf("%d update calls, want one per stack", b.called("update.call"))
	}
	if got := b.actions(); !slices.Equal(got, []string{"update:demo-stack", "update:mail-relay", "update:wiki"}) {
		t.Fatalf("calls = %v", got)
	}
	screen := m.render()
	for _, want := range []string{"update finished: 1 ok, 2 failed", "APP_VERSION: 1.4.2 -> 1.5.0, succeeded",
		"refused: the stack has local changes", "WIKI_VERSION: 1.41.0 -> 1.42.0, rolled back", "health check failed",
		"WIKI_VERSION: 1.42.0 -> 1.43.0, not attempted"} {
		if !strings.Contains(screen, want) {
			t.Errorf("summary misses %q:\n%s", want, screen)
		}
	}
}

func TestASecondActionIsRefusedWhileOneRuns(t *testing.T) {
	b := demoBackend()
	m := newActionModel(t, b)
	cmd := press(m, "d", "y")
	if !m.busy() {
		t.Fatal("the action does not run")
	}
	for _, key := range []string{"p", "u", "c", "i"} {
		send(m, key)
		if m.modal != nil {
			t.Fatalf("%s opened a question while an action runs", key)
		}
		if !strings.Contains(m.render(), "Another action is still running") {
			t.Errorf("%s: no refusal shown:\n%s", key, m.render())
		}
	}
	if len(b.actions()) != 0 {
		t.Fatalf("calls = %v", b.actions())
	}
	drain(m, cmd)
	if got := b.actions(); !slices.Equal(got, []string{"deploy:demo-stack"}) {
		t.Fatalf("calls = %v", got)
	}
	// The interface stays usable while the call runs: keys move, areas change.
	m = newActionModel(t, demoBackend())
	press(m, "d", "y")
	send(m, "2")
	if m.area != areaDetail {
		t.Error("the interface does not react while an action runs")
	}
}

func TestOnlyAConfirmedKeyTriggersAnAction(t *testing.T) {
	b := demoBackend()
	m := newActionModel(t, b)
	m.Update(tickMsg{})
	m.Update(tea.MouseWheelMsg{})
	m.Update(tea.MouseClickMsg{X: 3, Y: 5})
	send(m, "u", "d", "p", "i", "g", "c", "esc", "enter", "r")
	if got := b.actions(); len(got) != 0 {
		t.Fatalf("calls = %v", got)
	}
}

func TestActionsAreOnlyOnTheStacksArea(t *testing.T) {
	b := demoBackend()
	m := newActionModel(t, b)
	for _, area := range []string{"2", "3"} {
		send(m, area, "d", "u", "p", "i", "c")
		if m.modal != nil {
			t.Fatalf("area %s opened a question", area)
		}
	}
}

func TestRegistryQueryAsksAllStacksAndReportsCounts(t *testing.T) {
	b := demoBackend()
	m := newActionModel(t, b)
	send(m, "c")
	if !strings.Contains(m.render(), "Ask the registries now about all stacks?") {
		t.Fatalf("question:\n%s", m.render())
	}
	send(m, "y")
	if got := b.actions(); !slices.Equal(got, []string{"refresh:*"}) {
		t.Fatalf("calls = %v", got)
	}
	// demo-stack has an update and photo-album a failed check.
	if !strings.Contains(m.render(), "5 stacks: 1 with an update available, 1 could not be checked") {
		t.Errorf("summary:\n%s", m.render())
	}
}

func TestDeployJumpsToTheLogOfTheRun(t *testing.T) {
	b := demoBackend()
	run := "20261031T100000Z-demo-stack"
	b.logs[run] = "2026-10-31T10:00:00Z step check\n2026-10-31T10:00:01Z info compose file is valid\n"
	m := newActionModel(t, b)
	send(m, "d", "y")
	screen := m.render()
	for _, want := range []string{"run " + run + " started at commit " + commitB[:12], "l show log"} {
		if !strings.Contains(screen, want) {
			t.Errorf("summary misses %q:\n%s", want, screen)
		}
	}
	send(m, "l")
	if m.area != areaRuns || !m.log.open || m.log.row.id != run {
		t.Fatalf("area %s, log open %v, run %q", m.area, m.log.open, m.log.row.id)
	}
	if !strings.Contains(m.render(), "compose file is valid") {
		t.Errorf("the log is not shown:\n%s", m.render())
	}
}

func TestSeveralRunsJumpToTheRunsList(t *testing.T) {
	b := demoBackend()
	m := newActionModel(t, b)
	tick(m, 0, 5)
	send(m, "u", "y", "l")
	if m.area != areaRuns || m.log.open {
		t.Fatalf("area %s, log open %v", m.area, m.log.open)
	}
}

func TestSummaryWithoutRunsOffersNoLog(t *testing.T) {
	b := demoBackend()
	m := newActionModel(t, b)
	send(m, "i", "y")
	if strings.Contains(m.render(), "show log") {
		t.Errorf("a log is offered for no run:\n%s", m.render())
	}
	send(m, "l")
	if m.area != areaStacks {
		t.Error("l left the Stacks area without a run")
	}
}

func TestServiceErrorsOfActionsAreSanitized(t *testing.T) {
	const bad = "evil\x1b[2J\x1b]8;;http://example.invalid\x07name\x1bP1$r\x9b31m\r\x00\u202e"
	for _, profile := range []kittui.ColorProfile{kittui.ProfileNone, kittui.ProfileTrueColor} {
		b := demoBackend()
		b.errs["deploy:demo-stack"] = control.NewError(control.CodeInternal, bad)
		b.updateRuns = map[string]control.StackUpdateRun{
			"mail-relay": {Name: "mail-relay", Error: bad},
			"wiki":       {Name: "wiki", Steps: []control.UpdateStepInfo{{Variable: bad, From: bad, To: bad, Result: bad, Error: bad}}},
		}
		b.changelogs["demo-stack"] = control.ChangelogResult{Stack: "demo-stack", Checked: at(30, 3, 5), Items: []control.ChangelogInfo{{
			Variable: bad, Current: bad, Target: bad, Releases: []control.ChangelogRelease{{Version: bad, Body: bad}}}}}
		m := newActionModel(t, b)
		m.profile = profile
		m.restyle()
		send(m, "d")
		noBadBytes(t, "deploy question", m.render(), profile)
		send(m, "y")
		screen := m.render()
		noBadBytes(t, "deploy failure", screen, profile)
		if !strings.Contains(screen, `evil\x1b[2J`) {
			t.Errorf("profile %d: the escaped message is not shown:\n%s", profile, screen)
		}
		send(m, "enter", "u")
		noBadBytes(t, "update preview", m.render(), profile)
		send(m, "esc")
		tick(m, 1, 5)
		send(m, "u", "y")
		noBadBytes(t, "update summary", m.render(), profile)
		send(m, "enter")
		// A refused call that comes back as a failed channel.
		b2 := demoBackend()
		b2.errs["pin:demo-stack"] = bad2()
		m = newActionModel(t, b2)
		send(m, "i", "y")
		noBadBytes(t, "pin failure", m.render(), profile)
	}
}

func bad2() error { return control.NewError(control.CodeBusy, "busy\x1b[2J\x07"+hostile) }

func TestSmallTerminalsKeepTheirSizeWithAnOpenQuestion(t *testing.T) {
	for _, width := range []int{minWidth, 44, 56, 80, 120, 200} {
		for _, rows := range []int{minHeight, 14, 24} {
			for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
				for _, keys := range [][]string{{"i"}, {"u"}, {"u", "y"}, {"space", "d", "y"}, {"c"}, {"c", "y"}} {
					m := newTestModel(t, lang, demoBackend(), width, rows)
					send(m, keys...)
					checkScreen(t, m.render(), width, rows)
					send(m, "pgdown", "end", "home", "down", "up")
					checkScreen(t, m.render(), width, rows)
				}
			}
		}
	}
}

func TestQuitStillWorksUnderAQuestion(t *testing.T) {
	m := newActionModel(t, demoBackend())
	send(m, "d")
	if cmd := press(m, "q"); isQuit(cmd) {
		t.Error("q quits under a question")
	}
	if cmd := press(m, "ctrl+c"); !isQuit(cmd) {
		t.Error("ctrl+c does not quit under a question")
	}
}

func TestHelpListsTheActions(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, 60)
	send(m, "?")
	var screen string
	for range 12 {
		screen = m.render()
		if strings.Contains(screen, "unpin") {
			return
		}
		send(m, "pgdown")
	}
	t.Errorf("the help lists no unpin key:\n%s", screen)
}

// A refresh under way when an action ends may have read before the change, so another one follows it.
func TestRefreshUnderWayIsFollowedByAnother(t *testing.T) {
	b := demoBackend()
	m := newActionModel(t, b)
	before := b.called("status")
	pending := press(m, "r")
	send(m, "i", "y")
	if b.called("status") != before {
		t.Fatal("a second refresh started beside the first")
	}
	_, next := m.Update(pending())
	if next == nil {
		t.Fatal("no refresh follows the one that was under way")
	}
	drain(m, next)
	if b.called("status") != before+2 {
		t.Errorf("status read %d times, want %d", b.called("status"), before+2)
	}
}

func TestStackActionsCallTheServiceOnlyAfterConfirmation(t *testing.T) {
	for _, tt := range []struct{ key, want string }{
		{"S", "start:demo-stack"}, {"X", "stop:demo-stack"}, {"Z", "restart:demo-stack"},
	} {
		t.Run(tt.key, func(t *testing.T) {
			b := demoBackend()
			m := newActionModel(t, b)
			send(m, tt.key)
			if m.modal == nil {
				t.Fatalf("%s opened no question", tt.key)
			}
			send(m, "n")
			if m.modal != nil || len(b.actions()) != 0 {
				t.Fatalf("cancel reached the service: %v", b.actions())
			}
			cmd := press(m, tt.key, "y")
			if len(b.actions()) != 0 {
				t.Fatalf("the call was made before the answer: %v", b.actions())
			}
			drain(m, cmd)
			if got := b.actions(); !slices.Equal(got, []string{tt.want}) {
				t.Fatalf("calls = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStopSaysTheStackStaysStopped(t *testing.T) {
	m := newActionModel(t, demoBackend())
	send(m, "X")
	screen := m.render()
	for _, want := range []string{"Stop demo-stack and keep it stopped until it is started?", "stack stays stopped until it is started"} {
		if !strings.Contains(screen, want) {
			t.Errorf("question misses %q:\n%s", want, screen)
		}
	}
}

func TestStackActionEndsAtTheFirstFailure(t *testing.T) {
	b := demoBackend()
	b.errs["stop:mail-relay"] = control.NewError(control.CodeStackAction, "compose stop failed")
	m := newActionModel(t, b)
	send(m, "space", "down", "space", "end", "space", "X", "y")
	if got := b.actions(); !slices.Equal(got, []string{"stop:demo-stack", "stop:mail-relay"}) {
		t.Fatalf("calls = %v", got)
	}
	screen := m.render()
	for _, want := range []string{"stop finished: 1 ok, 1 failed", "the stack action failed: compose stop failed", "wiki", "not run"} {
		if !strings.Contains(screen, want) {
			t.Errorf("summary misses %q:\n%s", want, screen)
		}
	}
	send(m, "esc")
	if got := m.list.Ticked(); len(got) != 2 || got[0] != "mail-relay" || got[1] != "wiki" {
		t.Errorf("ticked after the run = %v", got)
	}
}

func TestRestartOfAStoppedStackIsRefusedBeforeAsking(t *testing.T) {
	b := stoppedBackend()
	m := newActionModel(t, b)
	send(m, "space", "down", "space", "Z")
	if m.modal != nil || len(b.actions()) != 0 || !strings.Contains(m.render(), "mail-relay is stopped; restart does not apply") {
		t.Fatalf("restart was not refused:\n%s", m.render())
	}
}

func TestStackActionErrorsAreTranslated(t *testing.T) {
	for _, tt := range []struct{ code, want string }{
		{control.CodeStackStopped, "the stack is stopped; start it instead of restarting it"},
		{control.CodeRunActive, "a run of the stack is active"},
		{control.CodeStackAction, "the stack action failed: boom"},
	} {
		b := demoBackend()
		b.errs["restart:demo-stack"] = control.NewError(tt.code, "boom")
		m := newActionModel(t, b)
		send(m, "Z", "y")
		if !strings.Contains(m.render(), tt.want) {
			t.Errorf("%s: summary misses %q:\n%s", tt.code, tt.want, m.render())
		}
	}
}

func TestStoppedRanksBehindRunningRollbackPauseAndRemoved(t *testing.T) {
	since := &control.HoldInfo{Since: at(29, 16, 40)}
	for _, tt := range []struct {
		name string
		st   control.StackStatus
		want stackState
	}{
		{"stopped", control.StackStatus{Stopped: since}, stateStopped},
		{"running", control.StackStatus{Stopped: since, ActiveRun: "r"}, stateRunning},
		{"rollback", control.StackStatus{Stopped: since, Paused: &control.PauseInfo{}}, stateRollbackPause},
		{"removed", control.StackStatus{Stopped: since, Removed: true}, stateRemoved},
		{"paused", control.StackStatus{Stopped: since, OperatorPause: since}, stateStopped},
		{"pinned", control.StackStatus{Stopped: since, Pinned: since}, stateStopped},
		{"failed", control.StackStatus{Stopped: since, LastRun: "failed"}, stateStopped},
	} {
		if got := stateOf(tt.st); got != tt.want {
			t.Errorf("%s: state %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestRollbackPauseShortTextIsNotStopped(t *testing.T) {
	for lang, want := range map[i18n.Lang]string{i18n.English: "rollback", i18n.German: "Rollback"} {
		m := newTestModel(t, lang, demoBackend(), narrowWidth, goldenRows)
		if got := m.text.Lookup(textStateRollbackPauseS); got != want {
			t.Errorf("%s: short text %q, want %q", lang, got, want)
		}
	}
}
