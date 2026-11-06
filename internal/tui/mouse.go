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

// overlayOpen reports whether a question, the help or the period form covers the screen. Their targets are
// the only ones in the map then, and they are not acted on yet.
func (m *Model) overlayOpen() bool {
	return m.modal != nil || m.helpOpen || m.filteringLogView() != nil
}

// mouse acts on one mouse event. Every click is turned into the key press it stands for and takes the way of
// that key, so the mouse can never do more than a key does.
func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	res := m.mouseMap.Handle(msg)
	if !res.Handled || m.overlayOpen() {
		return nil
	}
	switch {
	case res.Area != "":
		if i := slices.IndexFunc(areas(), func(a kittui.Area) bool { return a.ID == res.Area }); i >= 0 && i < len(areaKeys) {
			return m.pressName(areaKeys[i])
		}
	case res.Action != "":
		if key, ok := m.keyOf(res.Action); ok {
			return m.press(key)
		}
	case res.Item != "", res.WheelSteps != 0:
		// Hook: a row of a list and the wheel are wired where the part that drew them is.
	case res.Zone != "":
		if slices.Contains(m.zones(), res.Zone) {
			m.setZone(res.Zone)
		}
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
