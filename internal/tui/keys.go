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
	// actStackOnly limits the Runs area to the stack chosen in Stacks, and lifts the limit again.
	actStackOnly kittui.ActionID = "stackonly"
	// actToggle ticks a stack in the Stacks list.
	actToggle kittui.ActionID = "toggle"
	// The actions on stacks; each opens a confirmation first.
	actUpdate  kittui.ActionID = "update"
	actDeploy  kittui.ActionID = "deploy"
	actPause   kittui.ActionID = "pause"
	actUnpause kittui.ActionID = "unpause"
	actPin     kittui.ActionID = "pin"
	actUnpin   kittui.ActionID = "unpin"
	actResume  kittui.ActionID = "resume"
	actCheck   kittui.ActionID = "check"
	// The actions on the containers of a whole stack.
	actStackStart   kittui.ActionID = "stackstart"
	actStackStop    kittui.ActionID = "stackstop"
	actStackRestart kittui.ActionID = "stackrestart"
	// The answers of a question, and of a result that started runs.
	actYes kittui.ActionID = "yes"
	// The keys of the Settings area and of its forms.
	actNext   kittui.ActionID = "next"
	actPrev   kittui.ActionID = "prev"
	actSave   kittui.ActionID = "save"
	actFetch  kittui.ActionID = "fetch"
	actAdd    kittui.ActionID = "add"
	actRemove kittui.ActionID = "remove"
	actEdit   kittui.ActionID = "edit"
	actTest   kittui.ActionID = "test"
	actNo     kittui.ActionID = "no"
	// actRestore opens the choice of a backup in the stack detail.
	actRestore kittui.ActionID = "restore"
	actShowLog kittui.ActionID = "showlog"
	// actDataDirs opens the creation of the missing data directories in the stack detail.
	actDataDirs kittui.ActionID = "datadirs"
	// actContainerLogs opens the output of a container, or of a whole stack in the stack detail.
	actContainerLogs kittui.ActionID = "containerlogs"
)

// areaKeys are the keys of actArea, one per area in the order of the tab line.
var areaKeys = []string{"1", "2", "3", "4", "5"}

func keyTable() []kittui.Action {
	act := func(id kittui.ActionID, level kittui.KeyLevel, help kittui.TextID, keys ...string) kittui.Action {
		return kittui.Action{ID: id, Level: level, DefaultKeys: keys, Help: help}
	}
	table := []kittui.Action{
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
		act(actStackOnly, kittui.LevelLetter, textKeyStackOnly, "f"),
		act(actToggle, kittui.LevelMove, textKeyTick, "space"),
		act(actUpdate, kittui.LevelLetter, textKeyUpdate, "u"),
		act(actDeploy, kittui.LevelLetter, textKeyDeploy, "d"),
		act(actPause, kittui.LevelLetter, textKeyPause, "p"),
		act(actUnpause, kittui.LevelLetter, textKeyUnpause, "P"),
		act(actPin, kittui.LevelLetter, textKeyPin, "i"),
		act(actUnpin, kittui.LevelLetter, textKeyUnpin, "I"),
		act(actResume, kittui.LevelLetter, textKeyResume, "g"),
		act(actCheck, kittui.LevelLetter, textKeyCheck, "c"),
		act(actStackStart, kittui.LevelLetter, textKeyStart, "S"),
		act(actStackStop, kittui.LevelLetter, textKeyStop, "X"),
		act(actStackRestart, kittui.LevelLetter, textKeyRestart, "Z"),
		act(actNext, kittui.LevelMove, textKeyNext, "tab"),
		act(actPrev, kittui.LevelMove, textKeyPrev, "shift+tab"),
		act(actSave, kittui.LevelGlobal, textKeySave, "f2"),
		act(actFetch, kittui.LevelLetter, textKeyFetch, "F"),
		act(actAdd, kittui.LevelLetter, textKeyAdd, "a"),
		act(actRemove, kittui.LevelLetter, textKeyRemove, "x"),
		act(actEdit, kittui.LevelLetter, textKeyEdit, "e"),
		act(actTest, kittui.LevelLetter, textKeyTest, "s"),
		act(actRestore, kittui.LevelLetter, textKeyRestore, "R"),
		act(actDataDirs, kittui.LevelLetter, textKeyDataDirs, "m"),
		act(actContainerLogs, kittui.LevelLetter, textKeyLog, "o"),
		act(actYes, kittui.LevelConfirm, textAnswerYes, "y"),
		act(actNo, kittui.LevelConfirm, textAnswerCancel, "n", "esc"),
		act(actShowLog, kittui.LevelConfirm, textAnswerLog, "l"),
	}
	return append(table, kittui.LogViewActions()...)
}

func logKeys() kittui.LogViewKeys {
	return kittui.LogViewKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actFirst,
		Last: actLast, Search: actSearch, Enter: actEnter, Back: actBack, Follow: kittui.ActionLogFollow,
		NextMatch: kittui.ActionLogNextMatch, PrevMatch: kittui.ActionLogPrevMatch, Wrap: kittui.ActionLogWrap,
		Timestamps: kittui.ActionLogTimestamps,
		Filter:     kittui.ActionLogFilter, FilterForm: formKeys()}
}

func listKeys() kittui.ListKeys {
	return kittui.ListKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actFirst,
		Last: actLast, Left: actLeft, Right: actRight, Enter: actEnter, Search: actSearch, Back: actBack}
}

// stackListKeys are the keys of the Stacks list, which ticks stacks.
func stackListKeys() kittui.ListKeys {
	k := listKeys()
	k.Toggle = actToggle
	return k
}

func helpKeys() kittui.HelpKeys {
	return kittui.HelpKeys{Left: actLeft, Right: actRight, Up: actUp, Down: actDown, PageUp: actPageUp,
		PageDown: actPageDown, First: actFirst, Last: actLast, Back: actBack, Help: actHelp}
}

// formKeys are the actions of the key table a form reads.
func formKeys() kittui.FormKeys {
	return kittui.FormKeys{Up: actUp, Down: actDown, Next: actNext, Prev: actPrev, PageUp: actPageUp,
		PageDown: actPageDown, Left: actLeft, Right: actRight, First: actFirst, Last: actLast, Enter: actEnter,
		Toggle: actToggle, Save: actSave, Back: actBack}
}
