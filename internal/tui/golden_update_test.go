package tui

import (
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The goldens of the update rules form: 80x24 and 120x40, in English and German.

var goldenUpdateRows = map[string]int{"narrow": 24, "wide": 40}

func goldenUpdate(t *testing.T, prefix string, setup func() *fakeBackend, keys ...string) {
	t.Helper()
	for name, lang := range goldenLangs {
		for size, width := range goldenSizes {
			t.Run(name+"."+size, func(t *testing.T) {
				rows := goldenUpdateRows[size]
				m := newTestModel(t, lang, setup(), width, rows)
				send(m, keys...)
				screen := m.render()
				checkScreen(t, screen, width, rows)
				checkGolden(t, prefix+"."+name+"."+size+".golden", screen+"\n")
			})
		}
	}
}

func TestGoldenUpdateForm(t *testing.T) {
	goldenUpdate(t, "detail.update.form", updateBackend, toUpdateForm...)
}

func TestGoldenUpdateFormDefaults(t *testing.T) {
	goldenUpdate(t, "detail.update.defaults", settingsBackend, toUpdateForm...)
}

func TestGoldenUpdateEditor(t *testing.T) {
	goldenUpdate(t, "detail.update.editor", updateBackend, seq(toTags, keys("enter"))...)
}

func TestGoldenUpdateEditorEntry(t *testing.T) {
	goldenUpdate(t, "detail.update.entry", updateBackend, seq(toTags, keys("enter", "e"))...)
}

func TestGoldenUpdateEditorEntryProblem(t *testing.T) {
	goldenUpdate(t, "detail.update.entryproblem", settingsBackend, seq(toChangelog, keys("enter", "a", "down"), typed("nowhere"), keys("f2"))...)
}

func TestGoldenUpdateFieldProblem(t *testing.T) {
	goldenUpdate(t, "detail.update.problem", updateBackend, seq(toObserve, keys("backspace", "backspace", "9", "9", "9", "9", "f2"))...)
}

func TestGoldenUpdateServiceProblem(t *testing.T) {
	goldenUpdate(t, "detail.update.serviceproblem", func() *fakeBackend {
		b := updateBackend()
		b.errs["settingsset:demo-stack"] = control.NewError(control.CodeInvalid, "stack settings: update.tags.APP_VERSION: not an image variable of the stack")
		return b
	}, seq(toUpdateForm, keys("f2", "y"))...)
}

func TestGoldenUpdateConfirm(t *testing.T) {
	goldenUpdate(t, "detail.update.confirm", updateBackend, seq(toUpdateForm, keys("f2"))...)
}

func TestGoldenUpdateConfirmLocal(t *testing.T) {
	goldenUpdate(t, "detail.update.confirmlocal", func() *fakeBackend {
		b := updateBackend()
		b.status.Engine.Sources[0].Kind = control.SourceKindLocal
		return b
	}, seq(toUpdateForm, keys("f2"))...)
}

func TestGoldenUpdateDone(t *testing.T) {
	goldenUpdate(t, "detail.update.done", updateBackend, seq(toObserve, keys("backspace", "backspace", "3", "0", "f2", "y"))...)
}
