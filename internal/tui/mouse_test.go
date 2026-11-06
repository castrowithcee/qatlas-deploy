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
