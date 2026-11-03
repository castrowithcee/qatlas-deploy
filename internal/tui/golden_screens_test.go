package tui

import (
	"testing"
)

// The heights of the detail goldens: tall enough for the whole detail, and short enough to scroll.
const (
	detailRows   = 40
	scrolledRows = 16
)

var goldenSizes = map[string]int{"narrow": narrowWidth, "wide": wideWidth}

func goldenScreens(t *testing.T, prefix string, rows int, setup func() *fakeBackend, keys ...string) {
	t.Helper()
	for name, lang := range goldenLangs {
		for size, width := range goldenSizes {
			t.Run(name+"."+size, func(t *testing.T) {
				m := newTestModel(t, lang, setup(), width, rows)
				send(m, keys...)
				screen := m.render()
				checkScreen(t, screen, width, rows)
				noLeak(t, screen, hiddenA, hiddenB)
				checkGolden(t, prefix+"."+name+"."+size+".golden", screen+"\n")
			})
		}
	}
}

func TestGoldenDetail(t *testing.T) {
	goldenScreens(t, "detail", detailRows, demoBackend, "enter")
}

// One failing section shows its error and leaves the others as they are.
func TestGoldenDetailWithError(t *testing.T) {
	goldenScreens(t, "detail.error", detailRows, func() *fakeBackend {
		b := demoBackend()
		b.errs["backups:demo-stack"] = errServiceBusy()
		return b
	}, "enter")
}

func TestGoldenDetailScrolled(t *testing.T) {
	goldenScreens(t, "detail.scrolled", scrolledRows, demoBackend, "enter", "pgdown")
}

func TestGoldenRunsList(t *testing.T) {
	goldenScreens(t, "runs.list", goldenRows, demoBackend, "3")
}

func TestGoldenRunsFinishedLog(t *testing.T) {
	goldenScreens(t, "runs.log", goldenRows, demoBackend, "3", "down", "enter")
}

func TestGoldenRunsFollowing(t *testing.T) {
	goldenScreens(t, "runs.follow", goldenRows, demoBackend, "3", "enter")
}

func TestGoldenRunsOfOneStack(t *testing.T) {
	goldenScreens(t, "runs.stack", goldenRows, demoBackend, "1", "enter", "3", "f")
}
