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
