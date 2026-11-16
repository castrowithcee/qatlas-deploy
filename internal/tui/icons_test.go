package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

// allIcons are the glyphs of every set; none has none.
func allIcons() map[string]iconSet {
	return map[string]iconSet{uiprefs.IconsUnicode: iconsFor(uiprefs.IconsUnicode), uiprefs.IconsNerdFont: iconsFor(uiprefs.IconsNerdFont)}
}

func TestIconSetsPassTheKitChecks(t *testing.T) {
	for name, set := range allIcons() {
		if set.logo == "" || len(set.areas) != len(areas()) {
			t.Fatalf("%s: logo %q and %d area icons, want a logo and %d", name, set.logo, len(set.areas), len(areas()))
		}
		spec := kittui.FrameSpec{Product: productName, Icon: set.logo, Areas: areasWith(set)}
		if err := kittui.CheckFrame(spec); err != nil {
			t.Errorf("%s: CheckFrame: %v", name, err)
		}
		if err := kittui.CheckAreas(areasWith(set)); err != nil {
			t.Errorf("%s: CheckAreas: %v", name, err)
		}
		for id, glyph := range set.areas {
			if err := kittui.CheckIcon(glyph); err != nil {
				t.Errorf("%s: area %s: %v", name, id, err)
			}
		}
	}
}

func TestIconSetsAreDistinct(t *testing.T) {
	for name, set := range allIcons() {
		seen := map[string]bool{set.logo: true}
		for id, glyph := range set.areas {
			if seen[glyph] {
				t.Errorf("%s: the icon of %s is used twice", name, id)
			}
			seen[glyph] = true
		}
	}
}

// iconsOn reports whether the screen shows a glyph of any set.
func iconsOn(screen string) bool {
	for _, set := range allIcons() {
		if strings.Contains(screen, set.logo) {
			return true
		}
		for _, g := range set.areas {
			if strings.Contains(screen, g) {
				return true
			}
		}
	}
	return false
}

func TestIconsShowBeforeTheNamesInUnicode(t *testing.T) {
	for name, set := range allIcons() {
		m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
		m.prefs.Icons = name
		screen := m.render()
		for _, want := range []string{set.logo + " " + productName, "1 " + set.areas[areaStacks] + " Stacks",
			"3 " + set.areas[areaRuns] + " Runs", "5 " + set.areas[areaContainers] + " Containers"} {
			if !strings.Contains(screen, want) {
				t.Errorf("%s: the screen lacks %q:\n%s", name, want, screen)
			}
		}
		checkScreen(t, screen, wideWidth, goldenRows)
	}
}

func TestNoIconsWithNoneOrWithoutUnicode(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, goldenRows)
	m.prefs.Icons = uiprefs.IconsNone
	if screen := m.render(); iconsOn(screen) || !strings.Contains(screen, "1 Stacks") {
		t.Fatalf("none shows icons or loses the names:\n%s", screen)
	}
	for _, name := range []string{uiprefs.IconsUnicode, uiprefs.IconsNerdFont} {
		m := newTestModelIn(t, i18n.English, demoBackend(), wideWidth, goldenRows, kittui.CharsetASCII)
		m.prefs.Icons = name
		if screen := m.render(); iconsOn(screen) || !strings.Contains(screen, "1 Stacks") {
			t.Fatalf("ASCII with %s shows icons or loses the names:\n%s", name, screen)
		}
	}
}

func TestGeneralIconsFieldSavesAndTakesEffectAtOnce(t *testing.T) {
	s := &savedPrefs{}
	m := generalModel(t, s)
	m.prefs.Icons = uiprefs.IconsUnicode
	send(m, toGeneral...)
	contains(t, m, "Icons", "Unicode")
	if !strings.Contains(m.render(), unicodeIcons.areas[areaStacks]+" Stacks") {
		t.Fatalf("the unicode icons are not shown before the change:\n%s", m.render())
	}
	send(m, "down", "down", "down", "down", "right") // nerd font
	send(m, save...)
	if len(s.calls) != 1 || s.calls[0].Icons != uiprefs.IconsNerdFont {
		t.Fatalf("saved %+v", s.calls)
	}
	if m.prefs.Icons != uiprefs.IconsNerdFont || !strings.Contains(m.render(), nerdFontIcons.areas[areaStacks]+" Stacks") {
		t.Fatalf("the nerd font icons are not shown at once:\n%s", m.render())
	}
	send(m, "down", "down", "down", "down", "right") // none
	send(m, save...)
	if len(s.calls) != 2 || s.calls[1].Icons != uiprefs.IconsNone || iconsOn(m.render()) {
		t.Fatalf("none is not saved or shown: %+v\n%s", s.calls, m.render())
	}
}

func TestGeneralIconsFieldInGerman(t *testing.T) {
	m := newTestModel(t, i18n.German, settingsBackend(), wideWidth, 40)
	send(m, toGeneral...)
	contains(t, m, "Symbole", "Unicode")
}

// goldenWith renders the screens of one language and size with the preference and the character set.
func goldenWith(t *testing.T, name, icons string, charset kittui.CharsetMode) {
	t.Helper()
	for size, width := range goldenSizes {
		t.Run(size, func(t *testing.T) {
			m := newTestModelIn(t, i18n.English, demoBackend(), width, goldenRows, charset)
			m.prefs.Icons = icons
			screen := m.render()
			checkScreen(t, screen, width, goldenRows)
			checkGolden(t, name+"."+size+".golden", screen+"\n")
			if (icons == uiprefs.IconsNone || charset == kittui.CharsetASCII) && iconsOn(screen) {
				t.Errorf("the screen shows icons:\n%s", screen)
			}
		})
	}
}

func TestGoldenStacksIconsNone(t *testing.T) {
	goldenWith(t, "stacks.en.icons-none", uiprefs.IconsNone, kittui.CharsetUnicode)
}

func TestGoldenStacksASCII(t *testing.T) {
	goldenWith(t, "stacks.en.ascii", uiprefs.IconsUnicode, kittui.CharsetASCII)
}

func TestGoldenStacksNerdFont(t *testing.T) {
	goldenWith(t, "stacks.en.icons-nerdfont", uiprefs.IconsNerdFont, kittui.CharsetUnicode)
}
