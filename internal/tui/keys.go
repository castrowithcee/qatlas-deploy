package tui

import kittui "github.com/castrowithcee/qatlas-kit/tui"

// The actions of the key table.
const (
	actHelp      kittui.ActionID = "help"
	actBack      kittui.ActionID = "back"
	actForceQuit kittui.ActionID = "forcequit"
	actQuit      kittui.ActionID = "quit"
	actReload    kittui.ActionID = "reload"
	actUp        kittui.ActionID = "up"
	actDown      kittui.ActionID = "down"
	actLeft      kittui.ActionID = "left"
	actRight     kittui.ActionID = "right"
	actFirst     kittui.ActionID = "first"
	actLast      kittui.ActionID = "last"
	actPageUp    kittui.ActionID = "pageup"
	actPageDown  kittui.ActionID = "pagedown"
	actEnter     kittui.ActionID = "enter"
	actSearch    kittui.ActionID = "search"
	actArea      kittui.ActionID = "area"
)

// areaKeys are the keys of actArea, one per area in the order of the tab line.
var areaKeys = []string{"1", "2", "3"}

func keyTable() []kittui.Action {
	act := func(id kittui.ActionID, level kittui.KeyLevel, help kittui.TextID, keys ...string) kittui.Action {
		return kittui.Action{ID: id, Level: level, DefaultKeys: keys, Help: help}
	}
	return []kittui.Action{
		act(actHelp, kittui.LevelGlobal, kittui.TextKeyHelp, "?"),
		act(actBack, kittui.LevelGlobal, kittui.TextKeyBack, "esc"),
		act(actForceQuit, kittui.LevelGlobal, kittui.TextKeyQuit, "ctrl+c"),
		act(actQuit, kittui.LevelLetter, kittui.TextKeyQuit, "q"),
		act(actReload, kittui.LevelLetter, kittui.TextKeyReload, "r"),
		act(actUp, kittui.LevelMove, kittui.TextKeyUp, "up"),
		act(actDown, kittui.LevelMove, kittui.TextKeyDown, "down"),
		act(actLeft, kittui.LevelMove, kittui.TextKeyLeft, "left"),
		act(actRight, kittui.LevelMove, kittui.TextKeyRight, "right"),
		act(actFirst, kittui.LevelMove, kittui.TextKeyFirst, "home"),
		act(actLast, kittui.LevelMove, kittui.TextKeyLast, "end"),
		act(actPageUp, kittui.LevelMove, kittui.TextKeyPageUp, "pgup"),
		act(actPageDown, kittui.LevelMove, kittui.TextKeyPageDown, "pgdown"),
		act(actEnter, kittui.LevelMove, kittui.TextKeyEnter, "enter"),
		act(actSearch, kittui.LevelMove, kittui.TextKeySearch, "/"),
		act(actArea, kittui.LevelMove, textKeyArea, areaKeys...),
	}
}

func listKeys() kittui.ListKeys {
	return kittui.ListKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actFirst,
		Last: actLast, Left: actLeft, Right: actRight, Enter: actEnter, Search: actSearch, Back: actBack}
}

func helpKeys() kittui.HelpKeys {
	return kittui.HelpKeys{Left: actLeft, Right: actRight, Up: actUp, Down: actDown, PageUp: actPageUp,
		PageDown: actPageDown, First: actFirst, Last: actLast, Back: actBack, Help: actHelp}
}
