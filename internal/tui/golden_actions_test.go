package tui

import (
	"github.com/castrowithcee/qatlas-deploy/internal/control"

	"testing"
)

func TestGoldenUpdateConfirmation(t *testing.T) {
	goldenScreens(t, "update.confirm", detailRows, demoBackend, "u")
}

func TestGoldenPinConfirmation(t *testing.T) {
	goldenScreens(t, "pin.confirm", 20, demoBackend, "i")
}

func TestGoldenPinConfirmationInASmallTerminal(t *testing.T) {
	goldenScreens(t, "pin.confirm.small", goldenRows, demoBackend, "i")
}

func TestGoldenTickedStacks(t *testing.T) {
	goldenScreens(t, "stacks.ticked", goldenRows, demoBackend, "space", "down", "down", "down", "down", "down", "space", "up")
}

func TestGoldenMultiActionSummary(t *testing.T) {
	goldenScreens(t, "pin.summary", 24, func() *fakeBackend {
		b := demoBackend()
		b.errs["pin:mail-relay"] = control.NewError(control.CodeLocalChanges, "the stack has local changes")
		return b
	}, "space", "down", "space", "end", "space", "i", "y")
}

// stoppedBackend is the demo host with mail-relay stopped through qdeploy.
func stoppedBackend() *fakeBackend {
	b := demoBackend()
	b.status.Engine.Stacks[1].Stopped = &control.HoldInfo{Since: at(29, 16, 40)}
	return b
}

func TestGoldenStopConfirmation(t *testing.T) {
	goldenScreens(t, "stop.confirm", 20, stoppedBackend, "X")
}

func TestGoldenStartConfirmation(t *testing.T) {
	goldenScreens(t, "start.confirm", 20, stoppedBackend, "down", "S")
}

func TestGoldenRestartConfirmation(t *testing.T) {
	goldenScreens(t, "restart.confirm", 20, stoppedBackend, "Z")
}

func TestGoldenStoppedStack(t *testing.T) {
	goldenScreens(t, "stacks.stopped", goldenRows, stoppedBackend)
}

func TestGoldenStoppedStackDetail(t *testing.T) {
	goldenScreens(t, "detail.stopped", detailRows, stoppedBackend, "down", "enter")
}

func TestGoldenStopSummaryEndsAtTheFirstFailure(t *testing.T) {
	goldenScreens(t, "stop.summary", 24, func() *fakeBackend {
		b := demoBackend()
		b.errs["stop:mail-relay"] = control.NewError(control.CodeStackAction, "compose stop failed")
		return b
	}, "space", "down", "space", "end", "space", "X", "y")
}
