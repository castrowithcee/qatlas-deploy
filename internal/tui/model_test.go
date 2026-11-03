package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

func noRepoError() error {
	return control.NewError(control.CodeNoRepo, "no stacks repository is bound")
}

func TestCatalogsAreComplete(t *testing.T) {
	if err := kittui.CheckCatalogs(catalog()); err != nil {
		t.Fatal(err)
	}
	keys, err := kittui.NewKeymap(keyTable(), kittui.CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
		if _, err := kittui.NewTranslator(lang, catalog(), keys); err != nil {
			t.Errorf("%s: %v", lang, err)
		}
	}
	if err := kittui.CheckAreas(areas()); err != nil {
		t.Fatal(err)
	}
}

func TestPrefsSchemaAcceptsTheAreas(t *testing.T) {
	schema, err := PrefsSchema()
	if err != nil {
		t.Fatal(err)
	}
	if schema.DefaultArea != string(areaStacks) || len(schema.Areas) != len(areas()) || len(schema.Themes) == 0 {
		t.Fatalf("schema = %+v", schema)
	}
}

func TestRowsShowTheMostPressingState(t *testing.T) {
	rows := map[string]stackRow{}
	for _, r := range buildRows(demoBackend().snapshot()) {
		rows[r.name] = r
	}
	for name, want := range map[string]stackState{
		"demo-stack": stateDeployed, "mail-relay": statePending, "old-tool": stateRemoved,
		"photo-album": stateRollbackPause, "web-shop": stateRunning, "wiki": statePinned,
	} {
		if rows[name].state != want {
			t.Errorf("%s: state %d, want %d", name, rows[name].state, want)
		}
	}
	if r := rows["demo-stack"]; r.avail != availUpdate || r.available[0] != "1.5.0" || r.lastResult != "succeeded" || r.lastAt != at(30, 14, 2) || r.warnings != 1 {
		t.Errorf("demo-stack = %+v", r)
	}
	if r := rows["web-shop"]; r.avail != availMajor || strings.Join(r.current, ",") != "5.2.0,16.4" || r.lastResult != "rolled-back" {
		t.Errorf("web-shop = %+v", r)
	}
	if r := rows["photo-album"]; r.avail != availError || r.errors != 1 {
		t.Errorf("photo-album = %+v", r)
	}
	if r := rows["wiki"]; r.avail != availNotChecked || r.warnings != 2 || r.mode != "none" {
		t.Errorf("wiki = %+v", r)
	}
}

func TestUnreadableStackIsMarked(t *testing.T) {
	b := demoBackend()
	b.stacks.Stacks[0].Error = "compose.yaml: cannot parse"
	for _, r := range buildRows(b.snapshot()) {
		if r.name == "demo-stack" && r.state != stateUnreadable {
			t.Fatalf("state %d, want unreadable", r.state)
		}
	}
}

// snapshot is what one refresh reads from the fake.
func (f *fakeBackend) snapshot() snapshot {
	return snapshot{status: f.status, stacks: f.stacks, check: f.check, updates: f.updates, runs: f.runs}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestKeysMoveBetweenAreasAndQuit(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	press(m, "down", "enter")
	if m.area != areaDetail || m.rows[m.chosen].name != "mail-relay" {
		t.Fatalf("enter: area %s, chosen %q", m.area, m.chosen)
	}
	press(m, "esc")
	if m.area != areaStacks {
		t.Fatalf("esc: area %s", m.area)
	}
	press(m, "3")
	if m.area != areaRuns || !strings.Contains(m.render(), "Runs recorded: 6") {
		t.Fatalf("3: area %s\n%s", m.area, m.render())
	}
	press(m, "1")
	if m.area != areaStacks {
		t.Fatalf("1: area %s", m.area)
	}
	if !isQuit(press(m, "q")) || !isQuit(press(m, "ctrl+c")) {
		t.Fatal("q and ctrl+c do not quit")
	}
}

func TestFilterTakesLettersAsText(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	if cmd := press(m, "/", "w", "i", "q"); isQuit(cmd) {
		t.Fatal("q quit while a filter was typed")
	}
	if m.list.Query() != "wiq" {
		t.Fatalf("query %q", m.list.Query())
	}
	press(m, "esc")
	press(m, "/", "w", "i", "k", "enter")
	if id, ok := m.list.Selected(); !ok || m.rows[id].name != "wiki" {
		t.Fatalf("selected %q", id)
	}
	screen := m.render()
	if strings.Contains(screen, "demo-stack") || !strings.Contains(screen, "1/1 (6 total)") {
		t.Fatalf("filtered screen:\n%s", screen)
	}
	press(m, "esc")
	if m.list.Query() != "" {
		t.Fatalf("esc kept the filter %q", m.list.Query())
	}
}

func TestReloadReadsAgainWithoutRefreshingRegistries(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, goldenRows)
	cmd := press(m, "r")
	if cmd == nil || press(m, "r") != nil {
		t.Fatal("reload did not start exactly one refresh")
	}
	if !strings.Contains(m.render(), "refreshing") {
		t.Fatal("the header does not say that a refresh runs")
	}
	m.Update(cmd())
	if b.called("status") != 2 || b.called("updates") != 2 || b.called("refresh") != 0 {
		t.Fatalf("calls %v", b.calls)
	}
	for _, name := range []string{"log", "changelog", "window"} {
		if b.called(name) != 0 {
			t.Errorf("the overview called %s", name)
		}
	}
}

func TestTickRefreshes(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, goldenRows)
	_, cmd := m.Update(tickMsg{})
	if cmd == nil || !m.loading {
		t.Fatal("a tick started no refresh")
	}
}

func TestFailedRefreshKeepsTheRows(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, goldenRows)
	b.statusErr = fmt.Errorf("dial: %w", client.ErrNotRunning)
	load(t, m)
	screen := m.render()
	if !strings.Contains(screen, "demo-stack") || !strings.Contains(screen, "error: the qdeploy service is not running; start it, then press r") {
		t.Fatalf("screen:\n%s", screen)
	}
	b.statusErr = nil
	load(t, m)
	if strings.Contains(m.render(), "error:") {
		t.Fatal("a good refresh kept the error")
	}
}

func TestErrorTexts(t *testing.T) {
	m := newTestModel(t, i18n.German, demoBackend(), wideWidth, goldenRows)
	for err, want := range map[error]string{
		client.ErrNoAccess:                         "nur root",
		control.ErrNotRoot():                       "nur root",
		control.NewError(control.CodeBusy, "busy"): "abgelehnt: busy",
		fmt.Errorf("broken pipe"):                  "keine Verbindung zum Dienst: broken pipe",
	} {
		if got := m.errorText(err); !strings.HasPrefix(got, "Fehler: ") || !strings.Contains(got, want) {
			t.Errorf("%v: %q", err, got)
		}
	}
}

func TestHelpOpensAndCloses(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, 30)
	press(m, "?")
	if !m.helpOpen || !strings.Contains(m.render(), "Help") {
		t.Fatalf("help not shown:\n%s", m.render())
	}
	press(m, "esc")
	if m.helpOpen || m.area != areaStacks {
		t.Fatal("esc did not close the help")
	}
}

func TestSmallTerminalAsksForMoreRoom(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), 30, 8)
	screen := m.render()
	if !strings.Contains(screen, "Resize terminal") || !strings.Contains(screen, "Have 30x8") {
		t.Fatalf("screen:\n%s", screen)
	}
}

// Text from the service never reaches the terminal as a control sequence, in any colour profile.
func TestServiceTextIsEscaped(t *testing.T) {
	const hostile = "evil\x1b[2J\x1b]8;;http://example.invalid\x07name‮"
	b := demoBackend()
	b.status.Engine.Stacks[0].Name = hostile
	b.stacks.Stacks[0].Name = hostile
	b.stacks.Stacks[0].Images[0].Version = "1.0\x1b[31m"
	b.updates.Stacks[0].Name = hostile
	b.updates.Stacks[0].Items[0].Selected = "2.0\r\n"
	b.status.Engine.Stacks[1].Name = hostile + "\x00"
	b.statusErr = nil
	for _, profile := range []kittui.ColorProfile{kittui.ProfileNone, kittui.ProfileTrueColor} {
		m := newTestModel(t, i18n.English, b, wideWidth, goldenRows)
		m.profile = profile
		m.restyle()
		for _, keys := range [][]string{nil, {"/", "e", "v", "i", "l", "enter", "enter"}} {
			press(m, keys...)
			screen := m.render()
			for _, bad := range []string{"\x1b[2J", "\x1b]8", "\x07", "\r", "\x00", "‮", "\x1b[31m"} {
				if strings.Contains(screen, bad) {
					t.Errorf("profile %d, keys %v: %q reaches the terminal", profile, keys, bad)
				}
			}
			if !strings.Contains(screen, `evil\x1b[2J`) {
				t.Errorf("profile %d, keys %v: the escaped name is not shown:\n%s", profile, keys, screen)
			}
		}
	}
}

func TestStateWordsAreTranslated(t *testing.T) {
	// At 120 cells the German row takes the short state words; 160 cells hold the long ones.
	m := newTestModel(t, i18n.German, demoBackend(), 160, goldenRows)
	screen := m.render()
	for _, want := range []string{"pausiert nach Rollback", "läuft", "gepinnt", "entfernt", "ausstehend", "deployt", "Wartungsfenster zu"} {
		if !strings.Contains(screen, want) {
			t.Errorf("%q missing:\n%s", want, screen)
		}
	}
}
