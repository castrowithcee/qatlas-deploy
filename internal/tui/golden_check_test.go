package tui

import (
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The goldens of the check form and of the removal of a stack's settings: 80x24 and 120x40, in English and
// German.

func TestGoldenCheckForm(t *testing.T) {
	goldenUpdate(t, "detail.check.form", updateBackend, toCheckForm...)
}

func TestGoldenCheckFormDefaults(t *testing.T) {
	goldenUpdate(t, "detail.check.defaults", settingsBackend, toCheckForm...)
}

func TestGoldenCheckFieldProblem(t *testing.T) {
	goldenUpdate(t, "detail.check.problem", updateBackend, seq(toStatus, backspaces(3), keys("9", "9", "9", "f2"))...)
}

func TestGoldenCheckServiceProblem(t *testing.T) {
	goldenUpdate(t, "detail.check.serviceproblem", func() *fakeBackend {
		b := updateBackend()
		b.errs["settingsset:demo-stack"] = control.NewError(control.CodeInvalid, "stack settings: check.http.url: must be an absolute http or https URL (got \"x\")")
		return b
	}, seq(toCheckForm, keys("f2", "y"))...)
}

func TestGoldenCheckConfirm(t *testing.T) {
	goldenUpdate(t, "detail.check.confirm", updateBackend, seq(toCheckForm, keys("f2"))...)
}

func TestGoldenCheckConfirmLocal(t *testing.T) {
	goldenUpdate(t, "detail.check.confirmlocal", func() *fakeBackend {
		b := updateBackend()
		b.status.Engine.Sources[0].Kind = control.SourceKindLocal
		return b
	}, seq(toCheckForm, keys("f2"))...)
}

func TestGoldenSettingsRemoveAsk(t *testing.T) {
	goldenUpdate(t, "detail.settings.removeask", updateBackend, keys("enter", "E")...)
}

func TestGoldenSettingsRemoveNone(t *testing.T) {
	goldenUpdate(t, "detail.settings.removenone", settingsBackend, keys("enter", "E", "y")...)
}
