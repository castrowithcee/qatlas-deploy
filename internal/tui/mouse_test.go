package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

func newMouseModel(t *testing.T) (*Model, *fakeBackend) {
	t.Helper()
	b := demoBackend()
	m := newActionModel(t, b)
	m.mouseOn = true
	m.render()
	return m, b
}

// cellOf is the first cell of needle on the drawn screen.
func cellOf(t *testing.T, m *Model, needle string) (x, y int) {
	t.Helper()
	for y, line := range strings.Split(m.render(), "\n") {
		if i := strings.Index(line, needle); i >= 0 {
			return len([]rune(line[:i])), y
		}
	}
	t.Fatalf("%q is not on the screen:\n%s", needle, m.render())
	return 0, 0
}

func clickAt(m *Model, x, y int) {
	_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drain(m, cmd)
	m.render()
}

func TestAClickOnATabSwitchesTheAreaLikeADigit(t *testing.T) {
	m, _ := newMouseModel(t)
	x, y := cellOf(t, m, "Runs")
	clickAt(m, x, y)
	if m.area != areaRuns {
		t.Fatalf("area = %s", m.area)
	}
}

func TestAClickOnAKeyActsLikeTheKey(t *testing.T) {
	m, b := newMouseModel(t)
	x, y := cellOf(t, m, "[?]")
	clickAt(m, x, y)
	if !m.helpOpen {
		t.Fatal("the header key did not open the help")
	}
	press(m, "esc")
	m.render()
	x, y = cellOf(t, m, "d deploy")
	clickAt(m, x, y)
	if m.modal == nil {
		t.Fatal("the footer key did not open the question")
	}
	if got := b.actions(); len(got) != 0 {
		t.Fatalf("calls = %v", got)
	}
}

func TestATargetOfAnEarlierFrameDoesNotAct(t *testing.T) {
	m, _ := newMouseModel(t)
	x, y := cellOf(t, m, "d deploy")
	send(m, "3")
	clickAt(m, x, y)
	if m.modal != nil {
		t.Fatal("a target of the Stacks frame acted in Runs")
	}
}

func TestTheMouseMapIsOffWhereTheMouseMustNotAct(t *testing.T) {
	m, _ := newMouseModel(t)
	x, y := cellOf(t, m, "Runs")

	m.mouseOn = false
	m.render()
	clickAt(m, x, y)
	if m.area != areaStacks {
		t.Error("the preference off still let a click act")
	}
	if m.View().MouseMode != tea.MouseModeNone {
		t.Error("the preference off still asks for the mouse")
	}
	m.mouseOn = true
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Error("the preference on does not ask for the mouse")
	}

	press(m, "/") // the search line captures text
	m.render()
	clickAt(m, x, y)
	if m.area != areaStacks {
		t.Error("a click acted while text was captured")
	}
	press(m, "esc")

	m.Update(tea.WindowSizeMsg{Width: minWidth - 1, Height: minHeight})
	m.render()
	clickAt(m, x, y)
	if m.area != areaStacks {
		t.Error("a click acted on the resize view")
	}
}

func TestAnOpenQuestionTakesNoClickOfTheScreenBeneath(t *testing.T) {
	m, _ := newMouseModel(t)
	x, y := cellOf(t, m, "Runs")
	press(m, "d")
	m.render()
	clickAt(m, x, y)
	if m.area != areaStacks || m.modal == nil {
		t.Fatal("a click reached the screen beneath a question")
	}
}

func TestAClickOnAZoneFocusesIt(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, 60)
	m.mouseOn = true
	press(m, "tab")
	m.render()
	if m.focus() != kittui.ZoneTabs {
		t.Fatalf("focus = %s", m.focus())
	}
	// The frame draws no zone of the content itself; the parts that do add theirs to the map.
	m.mouseMap = kittui.NewMouseMap([]kittui.Target{
		{Rect: kittui.Rect{X: 0, Y: 5, Width: 40, Height: 5}, Zone: kittui.ZoneRows},
		{Rect: kittui.Rect{X: 0, Y: 12, Width: 40, Height: 5}, Zone: kittui.ZoneDetail},
	}, true)
	m.Update(tea.MouseClickMsg{X: 2, Y: 6, Button: tea.MouseLeft})
	if m.focus() != kittui.ZoneRows {
		t.Fatalf("focus = %s", m.focus())
	}
	m.Update(tea.MouseClickMsg{X: 2, Y: 13, Button: tea.MouseLeft}) // a zone this screen does not offer
	if m.focus() != kittui.ZoneRows {
		t.Fatalf("focus = %s", m.focus())
	}
}

func TestParseKeyRoundTrips(t *testing.T) {
	for _, a := range func() []kittui.Action { k, _ := kittui.NewKeymap(keyTable(), kittui.CharsetASCII); return k.Actions() }() {
		for _, name := range a.DefaultKeys {
			if _, ok := parseKey(name); !ok {
				t.Errorf("key %q of %s does not parse", name, a.ID)
			}
		}
	}
}

func wheelAt(m *Model, x, y int, button tea.MouseButton) {
	_, cmd := m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: button})
	drain(m, cmd)
	m.render()
}

func marked(t *testing.T, l *kittui.List) string {
	t.Helper()
	id, ok := l.Selected()
	if !ok {
		t.Fatal("nothing is marked")
	}
	return string(id)
}

func TestAClickOnARowMarksItAndASecondClickOpensItOnlyWithFocusedRows(t *testing.T) {
	m, _ := newMouseModel(t)
	x, y := cellOf(t, m, "photo-album")
	clickAt(m, x, y)
	if got := marked(t, m.list); got != "photo-album" || m.area != areaStacks {
		t.Fatalf("marked %q in %s", got, m.area)
	}
	clickAt(m, x, y)
	if m.area != areaDetail || m.chosen != "photo-album" {
		t.Fatalf("area %s, chosen %q", m.area, m.chosen)
	}

	// With the tab line focused the click marks the row and takes the focus, and opens nothing.
	send(m, "1", "tab")
	m.render()
	if m.focus() != kittui.ZoneTabs {
		t.Fatalf("focus = %s", m.focus())
	}
	x, y = cellOf(t, m, "wiki")
	clickAt(m, x, y)
	if marked(t, m.list) != "wiki" || m.area != areaStacks || m.focus() != kittui.ZoneRows {
		t.Fatalf("marked %q, area %s, focus %s", marked(t, m.list), m.area, m.focus())
	}
}

func TestAClickOnAMarkedRowOpensWhatEnterOpens(t *testing.T) {
	keys, _ := newMouseModel(t)
	send(keys, "down", "down", "enter")

	m, _ := newMouseModel(t)
	x, y := cellOf(t, m, "old-tool")
	clickAt(m, x, y)
	clickAt(m, x, y)
	if m.area != keys.area || m.chosen != keys.chosen {
		t.Fatalf("the click led to %s/%q, enter to %s/%q", m.area, m.chosen, keys.area, keys.chosen)
	}
}

func TestTheWheelMovesTheSelectionAndStopsAtTheEnds(t *testing.T) {
	m, _ := newMouseModel(t)
	x, y := cellOf(t, m, "mail-relay")
	wheelAt(m, x, y, tea.MouseWheelDown)
	if got := marked(t, m.list); got != "mail-relay" {
		t.Fatalf("marked %q", got)
	}
	for range 20 {
		wheelAt(m, x, y, tea.MouseWheelDown)
	}
	if got := marked(t, m.list); got != "wiki" {
		t.Fatalf("marked %q at the end", got)
	}
	for range 20 {
		wheelAt(m, x, y, tea.MouseWheelUp)
	}
	if got := marked(t, m.list); got != "demo-stack" {
		t.Fatalf("marked %q at the start", got)
	}
}

func TestTheRunsListTakesTheRowUnderThePointer(t *testing.T) {
	m, _ := newMouseModel(t)
	send(m, "3")
	x, y := cellOf(t, m, "photo-album")
	clickAt(m, x, y)
	if got := marked(t, m.runsList); got != "20261029T031000Z-0000000a" {
		t.Fatalf("marked %q", got)
	}
	wheelAt(m, x, y, tea.MouseWheelDown)
	if got := marked(t, m.runsList); got != "20261029T020000Z-00000010" {
		t.Fatalf("marked %q after the wheel", got)
	}
}

func TestTheSettingsMenuAndPageListTakeTheRowUnderThePointer(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	m.mouseOn = true
	send(m, toMenu...)
	x, y := cellOf(t, m, "Credentials")
	clickAt(m, x, y)
	if got := marked(t, m.set.menu); got != string(pageCreds) {
		t.Fatalf("marked %q", got)
	}
	clickAt(m, x, y) // the marked row opens as enter does
	if m.set.page != pageCreds {
		t.Fatalf("page %q", m.set.page)
	}
	m.render()
	x, y = cellOf(t, m, "github.com")
	clickAt(m, x, y) // the page list lies two rows below the top of the workspace
	if got := marked(t, m.set.credList); !strings.Contains(got, "github.com") {
		t.Fatalf("marked %q", got)
	}
}

func TestTheContainersListIsClickable(t *testing.T) {
	m := newActionModel(t, containersBackend())
	m.mouseOn = true
	send(m, "5")
	x, y := cellOf(t, m, "example-cache")
	clickAt(m, x, y)
	if got := marked(t, m.ctList); !strings.Contains(got, "example-cache") {
		t.Fatalf("marked %q", got)
	}
	clickAt(m, x, y)
	if !m.ctv.open {
		t.Fatal("the second click did not open the container")
	}
}

func TestOnlyTheHelpTakesClicksAndTheWheelWhileItIsOpen(t *testing.T) {
	m, _ := newMouseModel(t)
	send(m, "?")
	if !m.helpOpen {
		t.Fatal("the help is not open")
	}
	x, y := cellOf(t, m, "Runs") // a tab of the screen beneath, if the help leaves it in sight
	clickAt(m, x, y)
	if m.area != areaStacks || !m.helpOpen {
		t.Fatalf("a click reached the screen beneath the help: area %s, open %v", m.area, m.helpOpen)
	}
	before := m.render()
	for range 3 {
		wheelAt(m, 5, m.height/2, tea.MouseWheelDown)
	}
	if m.render() == before {
		t.Fatal("the wheel did not scroll the help")
	}
	if marked(t, m.list) != "demo-stack" {
		t.Fatal("the wheel reached the list beneath the help")
	}
	x, y = cellOf(t, m, "esc")
	clickAt(m, x, y)
	if m.helpOpen {
		t.Fatal("the close key of the help did not close it")
	}
}

func TestAStaleRowAndTheWheelOfAnOpenQuestionDoNotAct(t *testing.T) {
	m, _ := newMouseModel(t)
	x, y := cellOf(t, m, "photo-album")
	send(m, "5") // Containers shows nothing at that cell that takes a click
	clickAt(m, x, y)
	if m.area != areaContainers || marked(t, m.list) != "demo-stack" {
		t.Fatalf("area %s, marked %q", m.area, marked(t, m.list))
	}
	send(m, "1", "d")
	if m.modal == nil {
		t.Fatal("no question")
	}
	wheelAt(m, x, y, tea.MouseWheelDown)
	if marked(t, m.list) != "demo-stack" || m.modal == nil {
		t.Fatal("the wheel acted beneath a question")
	}
}
