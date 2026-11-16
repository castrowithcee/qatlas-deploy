package tui

import (
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

// iconSet holds the glyphs of one value of the icons preference. The frame itself leaves them out in an ASCII
// terminal and when a glyph fails the kit's width check.
type iconSet struct {
	logo  string
	areas map[kittui.AreaID]string
}

var (
	unicodeIcons = iconSet{logo: "◆", areas: map[kittui.AreaID]string{
		areaStacks: "▤", areaDetail: "▣", areaRuns: "▶", areaSettings: "⚙", areaContainers: "▦",
	}}
	// The nerd font glyphs are Font Awesome code points of the patched fonts.
	nerdFontIcons = iconSet{logo: "", areas: map[kittui.AreaID]string{
		areaStacks: "", areaDetail: "", areaRuns: "", areaSettings: "", areaContainers: "",
	}}
)

// iconsFor returns the glyphs of the preference value; unknown values fall back to unicode, only none gives
// no icons.
func iconsFor(value string) iconSet {
	switch value {
	case uiprefs.IconsNerdFont:
		return nerdFontIcons
	case uiprefs.IconsNone:
		return iconSet{}
	}
	return unicodeIcons
}

// areasWith is areas() with the icons of the set.
func areasWith(set iconSet) []kittui.Area {
	as := areas()
	for i := range as {
		as[i].Icon = set.areas[as[i].ID]
	}
	return as
}
