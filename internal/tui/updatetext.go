package tui

import kittui "github.com/castrowithcee/qatlas-kit/tui"

// The texts of the update rules form and of the list editor behind it.
const (
	textKeyUpdateRules kittui.TextID = "key.updaterules"
	textKeyApply       kittui.TextID = "key.apply"
	textKeyDone        kittui.TextID = "key.done"

	textTitleUpdate    kittui.TextID = "title.updaterules"
	textTitleTags      kittui.TextID = "title.tags"
	textTitleChangelog kittui.TextID = "title.changelog"
	textUpdateIntro    kittui.TextID = "updaterules.intro"

	textFieldUpdateMode kittui.TextID = "field.updatemode"
	textHintUpdateMode  kittui.TextID = "hint.updatemode"
	textFieldCeiling    kittui.TextID = "field.ceiling"
	textHintCeiling     kittui.TextID = "hint.ceiling"
	textFieldMajor      kittui.TextID = "field.major"
	textHintMajor       kittui.TextID = "hint.major"
	textFieldObserve    kittui.TextID = "field.observe"
	textHintObserve     kittui.TextID = "hint.observe"
	textFieldTags       kittui.TextID = "field.tags"
	textHintTags        kittui.TextID = "hint.tags"
	textFieldChangelog  kittui.TextID = "field.changelog"
	textHintChangelog   kittui.TextID = "hint.changelog"

	textCeilingPatch  kittui.TextID = "ceiling.patch"
	textCeilingMinor  kittui.TextID = "ceiling.minor"
	textCeilingMajor  kittui.TextID = "ceiling.major"
	textMajorStepwise kittui.TextID = "major.stepwise"
	textMajorDirect   kittui.TextID = "major.direct"

	textColVariable    kittui.TextID = "col.variable"
	textColPattern     kittui.TextID = "col.pattern"
	textColRepository  kittui.TextID = "col.repository"
	textHintVariable   kittui.TextID = "hint.variable"
	textHintPattern    kittui.TextID = "hint.pattern"
	textHintRepository kittui.TextID = "hint.repository"
	textTagsIntro      kittui.TextID = "tags.intro"
	textTagsEmpty      kittui.TextID = "tags.empty"
	textChangelogIntro kittui.TextID = "changelogedit.intro"
	textChangelogEmpty kittui.TextID = "changelogedit.empty"

	textProblemObserve   kittui.TextID = "problem.observe"
	textProblemVarName   kittui.TextID = "problem.varname"
	textProblemRegex     kittui.TextID = "problem.regex"
	textProblemRepo      kittui.TextID = "problem.repo"
	textProblemListTwice kittui.TextID = "problem.listtwice"

	textAskUpdateRules      kittui.TextID = "ask.updaterules"
	textWhatUpdateLocal     kittui.TextID = "what.updaterules.local"
	textWhatUpdateRemote    kittui.TextID = "what.updaterules.remote"
	textWhatUpdateUnknown   kittui.TextID = "what.updaterules.unknown"
	textDetailRuleModes     kittui.TextID = "detail.rulemodes"
	textDetailRuleObserve   kittui.TextID = "detail.ruleobserve"
	textDetailRuleTag       kittui.TextID = "detail.ruletag"
	textDetailRuleChangelog kittui.TextID = "detail.rulechangelog"
	textObserveOff          kittui.TextID = "observe.off"
	textObserveMinutes      kittui.TextID = "observe.minutes"
	textDoneUpdate          kittui.TextID = "done.updaterules"
	textDoneUpdateSame      kittui.TextID = "done.updaterulessame"
)

func updateEnglish() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textKeyUpdateRules: "update rules",
		textKeyApply:       "apply",
		textKeyDone:        "done",
		textTitleUpdate:    "Update rules of a stack",
		textTitleTags:      "Tag filters",
		textTitleChangelog: "Changelog repositories",
		textUpdateIntro:    "Rules of the stack %s. Backup, check and compose files stay as they are.",

		textFieldUpdateMode: "Mode",
		textHintUpdateMode:  "automatic updates in the maintenance window, manual only on request",
		textFieldCeiling:    "Ceiling",
		textHintCeiling:     "the largest version step taken by itself",
		textFieldMajor:      "Major jump",
		textHintMajor:       "stepwise goes one major version at a time, direct jumps to the newest",
		textFieldObserve:    "Observation (minutes)",
		textHintObserve:     "after an update a failure rolls it back; 0 is off, at most 1440",
		textFieldTags:       "Tag filters",
		textHintTags:        "per version variable a regular expression the tags must match",
		textFieldChangelog:  "Changelog repositories",
		textHintChangelog:   "per version variable the GitHub repository of its releases",

		textCeilingPatch:  "patch",
		textCeilingMinor:  "minor",
		textCeilingMajor:  "major",
		textMajorStepwise: "stepwise",
		textMajorDirect:   "direct",

		textColVariable:    "Variable",
		textColPattern:     "Pattern",
		textColRepository:  "Repository",
		textHintVariable:   "an image version variable of the stack",
		textHintPattern:    "a regular expression, e.g. ^[0-9.]+$",
		textHintRepository: "owner/name on GitHub",
		textTagsIntro:      "Only tags that match the pattern are offered as updates of the variable.",
		textTagsEmpty:      "No tag filters. {add} adds one.",
		textChangelogIntro: "The releases of this repository are shown as the changelog of the variable.",
		textChangelogEmpty: "No changelog repositories. {add} adds one.",

		textProblemObserve:   "The observation time is a whole number of minutes from 0 to %d.",
		textProblemVarName:   "A variable name has letters, digits, _, . and -, and starts with a letter or _.",
		textProblemRegex:     "Not a valid regular expression: %s",
		textProblemRepo:      "Use a GitHub repository as owner/name.",
		textProblemListTwice: "%s is already in the list.",

		textAskUpdateRules:      "Save the update rules of the stack %s?",
		textWhatUpdateLocal:     "A commit with the new rules is created in the local stack folder. The rules act on the next update.",
		textWhatUpdateRemote:    "A commit with the new rules is created in the stack repository and pushed to its remote. The rules act on the next update.",
		textWhatUpdateUnknown:   "A commit with the new rules is created in the stack repository. The rules act on the next update.",
		textDetailRuleModes:     "mode: %s, ceiling: %s, major jump: %s",
		textDetailRuleObserve:   "observation: %s",
		textDetailRuleTag:       "tag filter %s: %s",
		textDetailRuleChangelog: "changelog %s: %s",
		textObserveOff:          "off",
		textObserveMinutes:      "%d min",
		textDoneUpdate:          "Update rules of the stack %s saved.",
		textDoneUpdateSame:      "The update rules of the stack %s were already set; nothing was committed.",
	}
}

func updateGerman() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textKeyUpdateRules: "Update-Regeln",
		textKeyApply:       "übernehmen",
		textKeyDone:        "fertig",
		textTitleUpdate:    "Update-Regeln eines Stacks",
		textTitleTags:      "Tag-Filter",
		textTitleChangelog: "Changelog-Repositories",
		textUpdateIntro:    "Regeln des Stacks %s. Backup, Prüfung und Compose-Dateien bleiben unverändert.",

		textFieldUpdateMode: "Modus",
		textHintUpdateMode:  "automatisch aktualisiert im Wartungsfenster, manuell nur auf Anforderung",
		textFieldCeiling:    "Obergrenze",
		textHintCeiling:     "der größte Versionssprung, der von selbst erfolgt",
		textFieldMajor:      "Major-Sprung",
		textHintMajor:       "stepwise geht eine Major-Version nach der anderen, direct springt zur neuesten",
		textFieldObserve:    "Beobachtung (Minuten)",
		textHintObserve:     "ein Ausfall nach einem Update setzt es zurück; 0 ist aus, höchstens 1440",
		textFieldTags:       "Tag-Filter",
		textHintTags:        "je Versionsvariable ein regulärer Ausdruck, den die Tags erfüllen müssen",
		textFieldChangelog:  "Changelog-Repositories",
		textHintChangelog:   "je Versionsvariable das GitHub-Repository seiner Releases",

		textCeilingPatch:  "patch",
		textCeilingMinor:  "minor",
		textCeilingMajor:  "major",
		textMajorStepwise: "stepwise",
		textMajorDirect:   "direct",

		textColVariable:    "Variable",
		textColPattern:     "Muster",
		textColRepository:  "Repository",
		textHintVariable:   "eine Image-Versionsvariable des Stacks",
		textHintPattern:    "ein regulärer Ausdruck, z. B. ^[0-9.]+$",
		textHintRepository: "owner/name auf GitHub",
		textTagsIntro:      "Nur Tags, die zum Muster passen, werden als Update der Variable angeboten.",
		textTagsEmpty:      "Keine Tag-Filter. {add} fügt einen hinzu.",
		textChangelogIntro: "Die Releases dieses Repositorys werden als Changelog der Variable gezeigt.",
		textChangelogEmpty: "Keine Changelog-Repositories. {add} fügt eines hinzu.",

		textProblemObserve:   "Die Beobachtungszeit ist eine ganze Zahl von Minuten von 0 bis %d.",
		textProblemVarName:   "Ein Variablenname hat Buchstaben, Ziffern, _, . und - und beginnt mit einem Buchstaben oder _.",
		textProblemRegex:     "Kein gültiger regulärer Ausdruck: %s",
		textProblemRepo:      "Gib ein GitHub-Repository als owner/name an.",
		textProblemListTwice: "%s steht schon in der Liste.",

		textAskUpdateRules:      "Die Update-Regeln des Stacks %s speichern?",
		textWhatUpdateLocal:     "Im lokalen Stack-Ordner entsteht ein Commit mit den neuen Regeln. Die Regeln wirken beim nächsten Update.",
		textWhatUpdateRemote:    "Im Stack-Repository entsteht ein Commit mit den neuen Regeln und wird zum Remote gepusht. Die Regeln wirken beim nächsten Update.",
		textWhatUpdateUnknown:   "Im Stack-Repository entsteht ein Commit mit den neuen Regeln. Die Regeln wirken beim nächsten Update.",
		textDetailRuleModes:     "Modus: %s, Obergrenze: %s, Major-Sprung: %s",
		textDetailRuleObserve:   "Beobachtung: %s",
		textDetailRuleTag:       "Tag-Filter %s: %s",
		textDetailRuleChangelog: "Changelog %s: %s",
		textObserveOff:          "aus",
		textObserveMinutes:      "%d Min.",
		textDoneUpdate:          "Update-Regeln des Stacks %s gespeichert.",
		textDoneUpdateSame:      "Die Update-Regeln des Stacks %s waren schon gesetzt; es wurde nichts committet.",
	}
}
