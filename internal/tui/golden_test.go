package tui

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// The sizes of the goldens.
const (
	narrowWidth = 56
	wideWidth   = 120
	goldenRows  = 14
)

// newTestModel builds a model on b in plain ASCII without colour, sized and loaded once.
func newTestModel(t *testing.T, lang i18n.Lang, b Backend, width, height int) *Model {
	t.Helper()
	m, err := newModel(config{ctx: context.Background(), backend: b, lang: lang, theme: kittui.ThemeTerminal,
		profile: kittui.ProfileNone, charset: kittui.CharsetASCII, loc: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	load(t, m)
	return m
}

// load runs one refresh synchronously.
func load(t *testing.T, m *Model) {
	t.Helper()
	cmd := m.reload()
	if cmd == nil {
		t.Fatal("no refresh started")
	}
	m.Update(cmd())
}

func press(m *Model, keys ...string) tea.Cmd {
	var last tea.Cmd
	for _, k := range keys {
		_, last = m.Update(keyMsg(k))
	}
	return last
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	golden := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("screen differs from %s:\ngot:\n%s\nwant:\n%s", golden, got, want)
	}
}

// checkScreen holds a screen to the size of the terminal: every line exactly as wide, no line more.
func checkScreen(t *testing.T, screen string, width, height int) {
	t.Helper()
	lines := strings.Split(screen, "\n")
	if len(lines) != height {
		t.Errorf("%d lines, want %d", len(lines), height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != width {
			t.Errorf("line %d is %d cells, want %d: %q", i, w, width, l)
		}
	}
}

var goldenLangs = map[string]i18n.Lang{"en": i18n.English, "de": i18n.German}

// The goldens fix what a person sees, byte for byte. Run with -update to rewrite them.
func TestGoldenStacks(t *testing.T) {
	for name, lang := range goldenLangs {
		for size, width := range map[string]int{"narrow": narrowWidth, "wide": wideWidth} {
			t.Run(name+"."+size, func(t *testing.T) {
				m := newTestModel(t, lang, demoBackend(), width, goldenRows)
				screen := m.render()
				checkScreen(t, screen, width, goldenRows)
				checkGolden(t, "stacks."+name+"."+size+".golden", screen+"\n")
			})
		}
	}
}

func TestGoldenAreas(t *testing.T) {
	for name, lang := range goldenLangs {
		for _, tt := range []struct {
			area string
			keys []string
		}{
			{"detail", []string{"enter"}},
			{"detail.none", []string{"2"}},
			{"runs", []string{"3"}},
		} {
			t.Run(name+"."+tt.area, func(t *testing.T) {
				m := newTestModel(t, lang, demoBackend(), narrowWidth, goldenRows)
				press(m, tt.keys...)
				screen := m.render()
				checkScreen(t, screen, narrowWidth, goldenRows)
				checkGolden(t, tt.area+"."+name+".golden", screen+"\n")
			})
		}
	}
}

func TestGoldenWithoutRepository(t *testing.T) {
	b := &fakeBackend{status: demoBackend().status}
	b.status.Engine.Stacks = nil
	b.stacksErr = noRepoError()
	for name, lang := range goldenLangs {
		t.Run(name, func(t *testing.T) {
			m := newTestModel(t, lang, b, narrowWidth, goldenRows)
			screen := m.render()
			checkScreen(t, screen, narrowWidth, goldenRows)
			checkGolden(t, "empty."+name+".golden", screen+"\n")
		})
	}
}
