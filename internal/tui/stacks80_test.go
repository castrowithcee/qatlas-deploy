package tui

import (
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The size of the 80-column stack list golden: a classic terminal, tall enough for every fixture row.
const (
	stacks80Width = 80
	stacks80Rows  = 24
)

// stateBackend lists every state and every availability word once.
func stateBackend() *fakeBackend {
	hold := &control.HoldInfo{Since: at(28, 9, 0)}
	st := func(name string) control.StackStatus {
		return control.StackStatus{Name: name, Deployed: commitA, LastRun: "succeeded", Findings: []control.FindingInfo{}}
	}
	running, rollback, removed := st("a-running-media-srv"), st("b-rollback-media-srv"), st("d-removed-media-srv")
	running.ActiveRun = "20261030T150000Z-0000000b"
	rollback.Paused = &control.PauseInfo{Since: at(29, 3, 12), Run: "20261029T031000Z-0000000a"}
	removed.Removed = true
	stopped, paused, pinned := st("e-stopped-media-srv"), st("f-paused-media-srv"), st("g-pinned-media-srv")
	stopped.Stopped, paused.OperatorPause, pinned.Pinned = hold, hold, hold
	broken := st("h-down-media-srv")
	broken.Failure = &control.FailureInfo{Kind: "exited", Container: "h-down-app-1", Since: at(29, 3, 12)}
	failed := st("i-failed-media-srv")
	failed.LastRun = "failed"
	pending := st("j-pending-media-srv")
	pending.Pending = commitB
	fresh := control.StackStatus{Name: "l-new-media-srv", Findings: []control.FindingInfo{}}
	dirs := st("m-dirs-media-srv")
	dirs.Findings = []control.FindingInfo{{Kind: findingDataDirMissing, Detail: "data/db"}}
	unreadable := st("c-unreadable-media")

	item := func(status, current, selected, major string) []control.UpdateInfo {
		return []control.UpdateInfo{{Variable: "V", Image: "example/app", Current: current, Selected: selected, MajorAvailable: major, Status: status}}
	}
	checked := func(name string, items []control.UpdateInfo) control.StackUpdates {
		return control.StackUpdates{Name: name, Update: "automatic", Checked: at(30, 3, 5), Items: items}
	}
	status := []control.StackStatus{running, rollback, unreadable, removed, stopped, paused, pinned, broken, failed, pending, st("k-deployed-media-srv"), fresh, dirs}
	return &fakeBackend{
		status: control.StatusResult{Version: "9.9.9", StartedAt: at(27, 8, 0), Engine: control.EngineStatus{LastPoll: at(30, 14, 30),
			Stacks: status}},
		stacks: control.StacksResult{Stacks: stackInfos(status)},
		updates: control.UpdatesResult{Stacks: []control.StackUpdates{
			checked("a-running-media-srv", item("update", "1.4.2", "1.5.0", "")),
			checked("b-rollback-media-srv", item("current", "2.0.1", "2.0.1", "")),
			checked("d-removed-media-srv", item("current", "3.1", "3.1", "4.0.0")),
			checked("e-stopped-media-srv", item("error", "1.0", "", "")),
			checked("f-paused-media-srv", item("not-comparable", "latest", "", "")),
		}},
	}
}

// stackInfos gives every stack two image versions, so the CURRENT column is as wide as on a real host.
func stackInfos(status []control.StackStatus) []control.StackInfo {
	var infos []control.StackInfo
	for _, st := range status {
		info := control.StackInfo{Name: st.Name, Update: "automatic", Images: []control.ImageInfo{
			{Service: "app", Image: "example/app", Var: "APP_VERSION", Version: "1.41.0"},
			{Service: "db", Image: "example/db", Var: "DB_VERSION", Version: "16.4.2"}}}
		if st.Name == "c-unreadable-media" {
			info = control.StackInfo{Name: st.Name, Error: "compose.yaml cannot be read"}
		}
		infos = append(infos, info)
	}
	return infos
}

func TestGoldenStacks80(t *testing.T) {
	for name, lang := range goldenLangs {
		t.Run(name, func(t *testing.T) {
			m := newTestModel(t, lang, stateBackend(), stacks80Width, stacks80Rows)
			screen := m.render()
			checkScreen(t, screen, stacks80Width, stacks80Rows)
			checkGolden(t, "stacks."+name+".80.golden", screen+"\n")
		})
	}
}

// A state or availability word cut with "~" is unreadable at 80 columns; a golden update must not let it pass.
func TestStacks80NoCutWords(t *testing.T) {
	for name, lang := range map[string]i18n.Lang{"en": i18n.English, "de": i18n.German} {
		t.Run(name, func(t *testing.T) {
			m := newTestModel(t, lang, stateBackend(), stacks80Width, stacks80Rows)
			lines := strings.Split(m.render(), "\n")
			tr := m.text
			var header string
			for _, l := range lines {
				if strings.Contains(l, strings.ToUpper(tr.Lookup(textColAvailable))) {
					header = l
					break
				}
			}
			if header == "" {
				t.Fatal("no column header found")
			}
			// Columns are found by cell, not byte: umlauts in a row would shift byte offsets.
			at := func(s, sub string) int {
				i := strings.Index(s, sub)
				if i < 0 {
					return -1
				}
				return utf8.RuneCountInString(s[:i])
			}
			var starts []int
			for _, id := range []kittui.TextID{textColStack, textColCurrent, textColAvailable, textColMode, textColState, textColLastRun, textColFindings} {
				if i := at(header, strings.ToUpper(tr.Lookup(id))); i >= 0 {
					starts = append(starts, i)
				}
			}
			sort.Ints(starts)
			cell := func(l string, id kittui.TextID) string {
				from := at(header, strings.ToUpper(tr.Lookup(id)))
				r := []rune(l)
				if from < 0 || from >= len(r) {
					return ""
				}
				to := len(r)
				for _, s := range starts {
					if s > from && s < to {
						to = s
					}
				}
				return string(r[from:to])
			}
			rows := 0
			for _, l := range lines {
				if !strings.Contains(l, "[ ]") {
					continue
				}
				rows++
				for _, id := range []kittui.TextID{textColState, textColAvailable} {
					if c := cell(l, id); strings.Contains(c, "~") {
						t.Errorf("cut word %q in %q", c, l)
					}
				}
			}
			if rows != 13 {
				t.Errorf("%d fixture rows visible, want 13", rows)
			}
		})
	}
}
