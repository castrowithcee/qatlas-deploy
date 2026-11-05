package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-kit/i18n"
)

func TestContainerFailureRanksBehindTheOperatorMarksAndBeforeTheRunResult(t *testing.T) {
	hold := &control.HoldInfo{Since: at(29, 16, 40)}
	fail := &control.FailureInfo{Kind: "exited", Container: "app-1", Since: at(30, 1, 0)}
	for _, tt := range []struct {
		name string
		st   control.StackStatus
		want stackState
	}{
		{"failure", control.StackStatus{Failure: fail}, stateContainerFailed},
		{"no field", control.StackStatus{LastRun: "succeeded", Deployed: commitA}, stateDeployed},
		{"running", control.StackStatus{Failure: fail, ActiveRun: "r"}, stateRunning},
		{"rollback", control.StackStatus{Failure: fail, Paused: &control.PauseInfo{}}, stateRollbackPause},
		{"removed", control.StackStatus{Failure: fail, Removed: true}, stateRemoved},
		{"stopped", control.StackStatus{Failure: fail, Stopped: hold}, stateStopped},
		{"paused", control.StackStatus{Failure: fail, OperatorPause: hold}, statePaused},
		{"pinned", control.StackStatus{Failure: fail, Pinned: hold}, statePinned},
		{"run failed", control.StackStatus{Failure: fail, LastRun: "failed"}, stateContainerFailed},
		{"pending", control.StackStatus{Failure: fail, Pending: commitB}, stateContainerFailed},
	} {
		if got := stateOf(tt.st); got != tt.want {
			t.Errorf("%s: state %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestContainerFailureIsNotCountedAsAnError(t *testing.T) {
	b := failedContainerBackend()
	rows := buildRows(snapshot{status: b.status})
	for _, r := range rows {
		if r.name == "mail-relay" && (r.errors != 0 || r.state != stateContainerFailed) {
			t.Fatalf("row = %+v", r)
		}
	}
	// Without the structured field the finding alone changes nothing about the state.
	b.status.Engine.Stacks[1].Failure = nil
	for _, r := range buildRows(snapshot{status: b.status}) {
		if r.name == "mail-relay" && r.state == stateContainerFailed {
			t.Fatalf("state from the finding text: %+v", r)
		}
	}
}

func TestDetailShowsTheFailureKindContainerAndStart(t *testing.T) {
	for _, tt := range []struct {
		lang i18n.Lang
		kind string
		want string
	}{
		{i18n.English, "exited", "Failure"},
		{i18n.English, "unhealthy", "unhealthy: mail-relay-relay-1, since 10-30 11:05"},
		{i18n.English, "restart-loop", "restart loop: mail-relay-relay-1, since 10-30 11:05"},
		{i18n.English, "missing", "missing: mail-relay-relay-1, since 10-30 11:05"},
		{i18n.German, "exited", "beendet: mail-relay-relay-1, seit 10-30 11:05"},
		{i18n.German, "unhealthy", "nicht gesund: mail-relay-relay-1, seit 10-30 11:05"},
		{i18n.German, "restart-loop", "Neustartschleife: mail-relay-relay-1, seit 10-30 11:05"},
		{i18n.German, "missing", "fehlt: mail-relay-relay-1, seit 10-30 11:05"},
	} {
		b := failedContainerBackend()
		b.status.Engine.Stacks[1].Failure.Kind = tt.kind
		m := newTestModel(t, tt.lang, b, wideWidth, detailRows)
		send(m, "down", "enter")
		if got := m.render(); !strings.Contains(got, tt.want) {
			t.Errorf("%s %s: no %q in\n%s", tt.lang, tt.kind, tt.want, got)
		}
	}
}

func TestDetailWithoutFailureHasNoFailureLine(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, detailRows)
	send(m, "down", "enter")
	if got := m.render(); strings.Contains(got, "Failure") {
		t.Fatalf("failure line without a failure:\n%s", got)
	}
}

func TestFailureContainerNameIsEscapedAndShortened(t *testing.T) {
	b := failedContainerBackend()
	b.status.Engine.Stacks[1].Failure.Container = "app\x1b[31m-1" + strings.Repeat("x", 300)
	m := newTestModel(t, i18n.English, b, narrowWidth, detailRows)
	send(m, "down", "enter")
	got := m.render()
	if strings.Contains(got, "\x1b[31m") || strings.Contains(got, strings.Repeat("x", 200)) {
		t.Fatalf("unescaped or unshortened name:\n%s", got)
	}
}

func TestSelectedContainerFailedEventIsSentWithTheChannel(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	hidden := []string{hiddenA, hiddenB}
	send(m, toNotify...)
	send(m, "a")
	sendHidden(t, m, hidden, typed("deploy-hook")...)
	// The events are listed in the order of the notifier; container-failed is the fifth.
	sendHidden(t, m, hidden, "down", "down", "right", "right", "right", "right", "space", "f2")
	sendHidden(t, m, hidden, typed(hiddenA)...)
	sendHidden(t, m, hidden, "down")
	sendHidden(t, m, hidden, typed(hiddenB)...)
	sendHidden(t, m, hidden, "f2", "y")
	if len(b.adds) != 1 || !slices.Equal(b.adds[0].Events, []string{control.NotifyEventContainerFailed}) {
		t.Fatalf("adds = %+v", b.adds)
	}
}
