package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// setMouse replaces the map with the targets of the frame just drawn. The map is off while a key or text is
// captured and while the preference is off; a nil map acts on nothing.
func (m *Model) setMouse(targets []kittui.Target) {
	m.mouseMap = kittui.NewMouseMap(targets, m.mouseOn && m.input() == kittui.InputNavigation)
}

// mousePart is a widget of the workspace that takes clicks and the wheel itself: a list or a log view, with
// its place on the screen.
type mousePart struct {
	rect    kittui.Rect
	list    *kittui.List
	log     *kittui.LogView
	targets []kittui.Target
}

// listView draws list at row y of the workspace and registers it for the mouse. It is the one way a list
// is drawn, so no list is left without its targets.
func (m *Model) listView(list *kittui.List, width, height, y int) string {
	view, targets := list.View(m.styles, width, height)
	m.parts = append(m.parts, mousePart{rect: kittui.Rect{Y: y, Width: width, Height: height}, list: list, targets: targets})
	return view
}

// logPart registers the log view drawn at row y of the workspace for the wheel. Its buttons stay unregistered.
func (m *Model) logPart(view *kittui.LogView, width, height, y int) {
	m.parts = append(m.parts, mousePart{rect: kittui.Rect{Y: y, Width: width, Height: height}, log: view})
}

// partTargets are the targets of the registered parts as the screen shows them, and the parts themselves
// moved to their place on the screen.
func (m *Model) partTargets(room kittui.Rect) (targets []kittui.Target, parts []mousePart) {
	for _, p := range m.parts {
		targets = append(targets, kittui.OffsetTargets(p.targets, room.X, room.Y+p.rect.Y)...)
		p.rect.X += room.X
		p.rect.Y += room.Y
		parts = append(parts, p)
	}
	return targets, parts
}

// partAt is the part drawn on the cell.
func (m *Model) partAt(x, y int) (mousePart, bool) {
	for _, p := range slices.Backward(m.mouseParts) {
		r := p.rect
		if x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height {
			return p, true
		}
	}
	return mousePart{}, false
}

// overlayOpen reports whether a question or the period form covers the screen. Their targets are the only
// ones in the map then, and they are not acted on yet. The help is the exception, see helpMouse.
func (m *Model) overlayOpen() bool {
	return m.modal != nil || m.filteringLogView() != nil
}

// mouse acts on one mouse event. Every click is turned into the key press it stands for and takes the way of
// that key, so the mouse can never do more than a key does.
func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	res := m.mouseMap.Handle(msg)
	if !res.Handled || m.overlayOpen() {
		return nil
	}
	if m.helpOpen {
		return m.helpMouse(res)
	}
	at := msg.Mouse()
	switch {
	case res.WheelSteps != 0:
		if p, ok := m.partAt(at.X, at.Y); ok {
			switch {
			case p.list != nil:
				p.list.Wheel(res.WheelSteps)
			case p.log != nil:
				p.log.Wheel(res.WheelSteps)
			}
		}
	case res.Item != "":
		if p, ok := m.partAt(at.X, at.Y); ok && p.list != nil {
			return m.rowClick(p.list, res)
		}
	case res.Area != "":
		if i := slices.IndexFunc(areas(), func(a kittui.Area) bool { return a.ID == res.Area }); i >= 0 && i < len(areaKeys) {
			return m.pressName(areaKeys[i])
		}
	case res.Action != "":
		if key, ok := m.keyOf(res.Action); ok {
			return m.press(key)
		}
	case res.Zone != "":
		if slices.Contains(m.zones(), res.Zone) {
			m.setZone(res.Zone)
		}
	}
	return nil
}

// rowClick marks the clicked row and gives the rows the focus. A second click on the marked row opens it by
// pressing the key that opens a row, so it takes the checks of that key.
func (m *Model) rowClick(list *kittui.List, res kittui.MouseResult) tea.Cmd {
	open, _ := list.Click(res) // reads the focus as it was before this click
	if slices.Contains(m.zones(), kittui.ZoneRows) {
		m.setZone(kittui.ZoneRows)
	}
	if open == "" {
		return nil
	}
	if key, ok := m.keyOf(res.Action); ok {
		return m.press(key)
	}
	return nil
}

// helpMouse is the mouse while the help is open: only it takes clicks and the wheel. A key of the list
// closes it, as the key does.
func (m *Model) helpMouse(res kittui.MouseResult) tea.Cmd {
	if res.WheelSteps != 0 {
		m.help.Wheel(res.WheelSteps)
		return nil
	}
	if act, _ := m.help.Click(res); act != "" {
		m.helpOpen = false
	}
	return nil
}

// press runs one key press; the keyboard and the mouse both come here.
func (m *Model) press(msg tea.KeyPressMsg) tea.Cmd {
	cmd := m.key(msg)
	m.refeedFilteredLog()
	return cmd
}

func (m *Model) pressName(name string) tea.Cmd {
	if msg, ok := parseKey(name); ok {
		return m.press(msg)
	}
	return nil
}

// keyOf is a key press that triggers the action under the input focus of the screen.
func (m *Model) keyOf(id kittui.ActionID) (tea.KeyPressMsg, bool) {
	focus := m.input()
	for _, a := range m.keys.Actions() {
		if a.ID != id {
			continue
		}
		for _, name := range a.DefaultKeys {
			if msg, ok := parseKey(name); ok {
				if got, ok := m.keys.Match(msg, focus); ok && got == id {
					return msg, true
				}
			}
		}
	}
	return tea.KeyPressMsg{}, false
}

var namedKeys = map[string]rune{
	"esc": tea.KeyEscape, "enter": tea.KeyEnter, "tab": tea.KeyTab, "space": tea.KeySpace, "up": tea.KeyUp,
	"down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight, "home": tea.KeyHome, "end": tea.KeyEnd,
	"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
}

// parseKey builds the key press a key table names ("esc", "ctrl+c", "f2", "P"); false where the name does
// not come back as itself.
func parseKey(name string) (tea.KeyPressMsg, bool) {
	var key tea.Key
	rest := name
	for {
		mod, tail, found := strings.Cut(rest, "+")
		if !found || tail == "" {
			break
		}
		switch mod {
		case "ctrl":
			key.Mod |= tea.ModCtrl
		case "shift":
			key.Mod |= tea.ModShift
		case "alt":
			key.Mod |= tea.ModAlt
		default:
			return tea.KeyPressMsg{}, false
		}
		rest = tail
	}
	var n int
	runes := []rune(rest)
	switch code, named := namedKeys[rest]; {
	case named:
		key.Code = code
		if code == tea.KeySpace {
			key.Text = " "
		}
	case len(runes) == 1:
		key.Code = runes[0]
		if key.Mod&^tea.ModShift == 0 {
			key.Text = rest
		}
	default:
		if _, err := fmt.Sscanf(rest, "f%d", &n); err != nil || n < 1 || n > 63 {
			return tea.KeyPressMsg{}, false
		}
		key.Code = tea.KeyF1 + rune(n-1)
	}
	msg := tea.KeyPressMsg(key)
	return msg, kittui.KeyName(msg) == name
}
