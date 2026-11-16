package tui

import kittui "github.com/castrowithcee/qatlas-kit/tui"

// The texts of the check form and of the removal of a stack's settings.
const (
	textKeyCheckForm          kittui.TextID = "key.checkform"
	textKeyRemoveSettings     kittui.TextID = "key.removesettings"
	textTitleCheckForm        kittui.TextID = "title.checkform"
	textTitleSettingsRemove   kittui.TextID = "title.settingsremove"
	textCheckIntro            kittui.TextID = "check.intro"
	textCheckOn               kittui.TextID = "check.on"
	textCheckOff              kittui.TextID = "check.off"
	textFieldCheckContainers  kittui.TextID = "field.checkcontainers"
	textHintCheckContainers   kittui.TextID = "hint.checkcontainers"
	textFieldCheckHTTP        kittui.TextID = "field.checkhttp"
	textHintCheckHTTP         kittui.TextID = "hint.checkhttp"
	textFieldCheckURL         kittui.TextID = "field.checkurl"
	textHintCheckURL          kittui.TextID = "hint.checkurl"
	textFieldCheckStatus      kittui.TextID = "field.checkstatus"
	textHintCheckStatus       kittui.TextID = "hint.checkstatus"
	textFieldCheckTimeout     kittui.TextID = "field.checktimeout"
	textHintCheckTimeout      kittui.TextID = "hint.checktimeout"
	textProblemCheckURLEmpty  kittui.TextID = "problem.checkurlempty"
	textProblemCheckURL       kittui.TextID = "problem.checkurl"
	textProblemCheckStatus    kittui.TextID = "problem.checkstatus"
	textProblemCheckTimeout   kittui.TextID = "problem.checktimeout"
	textAskCheckForm          kittui.TextID = "ask.checkform"
	textWhatCheckAfter        kittui.TextID = "what.checkafter"
	textDetailCheckContainers kittui.TextID = "detail.checkcontainers"
	textDetailCheckHTTP       kittui.TextID = "detail.checkhttp"
	textDetailCheckNoHTTP     kittui.TextID = "detail.checknohttp"
	textDoneCheckForm         kittui.TextID = "done.checkform"
	textDoneCheckSame         kittui.TextID = "done.checksame"
	textAskSettingsRemove     kittui.TextID = "ask.settingsremove"
	textWhatRemoveRemote      kittui.TextID = "what.removeremote"
	textWhatRemoveLocal       kittui.TextID = "what.removelocal"
	textWhatRemoveUnknown     kittui.TextID = "what.removeunknown"
	textDoneSettingsRemoved   kittui.TextID = "done.settingsremoved"
	textSettingsNone          kittui.TextID = "settings.none"
)

func checkEnglish() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textKeyCheckForm:          "checks",
		textKeyRemoveSettings:     "clear settings",
		textTitleCheckForm:        "Check of a stack",
		textTitleSettingsRemove:   "Remove the settings of a stack",
		textCheckIntro:            "Check of the stack %s. Update rules, backup and compose files stay as they are.",
		textCheckOn:               "on",
		textCheckOff:              "off",
		textFieldCheckContainers:  "Container check",
		textHintCheckContainers:   "On: the stack counts as healthy only while all its containers run.",
		textFieldCheckHTTP:        "HTTP check",
		textHintCheckHTTP:         "On: the address below must answer as expected. Off: the fields below are ignored.",
		textFieldCheckURL:         "Address (URL)",
		textHintCheckURL:          "An absolute http or https URL, for example http://localhost:8080/health.",
		textFieldCheckStatus:      "Expected status",
		textHintCheckStatus:       "The HTTP status the address must answer, 100 to 599; 200 by default.",
		textFieldCheckTimeout:     "Timeout",
		textHintCheckTimeout:      "How long to wait for the answer, from 1s to 10m, for example 10s.",
		textProblemCheckURLEmpty:  "The HTTP check is on, so it needs an address.",
		textProblemCheckURL:       "The address must be an absolute http or https URL.",
		textProblemCheckStatus:    "The status is a whole number from %d to %d.",
		textProblemCheckTimeout:   "The timeout is a duration from 1s to 10m, for example 10s or 1m30s.",
		textAskCheckForm:          "Save the check of the stack %s?",
		textWhatCheckAfter:        "The check acts on the next deployment.",
		textDetailCheckContainers: "container check: %s",
		textDetailCheckHTTP:       "HTTP check: %s answers %d within %s",
		textDetailCheckNoHTTP:     "HTTP check: off",
		textDoneCheckForm:         "Check of the stack %s saved.",
		textDoneCheckSame:         "The check of the stack %s was already set; nothing was committed.",
		textAskSettingsRemove:     "Remove all settings of the stack %s?",
		textWhatRemoveRemote:      "A commit deletes the settings file of the stack, with update rules, backup, compose files and check, and is pushed to its remote. Afterwards the stack is deployed but never updated.",
		textWhatRemoveLocal:       "A commit deletes the settings file of the stack, with update rules, backup, compose files and check, in the local stack folder. Afterwards the stack is deployed but never updated.",
		textWhatRemoveUnknown:     "A commit deletes the settings file of the stack, with all its sections. Afterwards the stack is deployed but never updated.",
		textDoneSettingsRemoved:   "Settings of the stack %s removed. The stack is deploy only now.",
		textSettingsNone:          "The stack %s has no settings. It is already deploy only; nothing was removed.",
	}
}

func checkGerman() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textKeyCheckForm:          "Prüfung",
		textKeyRemoveSettings:     "Einstellungen entfernen",
		textTitleCheckForm:        "Prüfung eines Stacks",
		textTitleSettingsRemove:   "Einstellungen eines Stacks entfernen",
		textCheckIntro:            "Prüfung des Stacks %s. Update-Regeln, Backup und Compose-Dateien bleiben unverändert.",
		textCheckOn:               "an",
		textCheckOff:              "aus",
		textFieldCheckContainers:  "Container-Prüfung",
		textHintCheckContainers:   "An: der Stack gilt nur als gesund, solange alle seine Container laufen.",
		textFieldCheckHTTP:        "HTTP-Prüfung",
		textHintCheckHTTP:         "An: die Adresse unten muss wie erwartet antworten. Aus: die Felder darunter bleiben unbeachtet.",
		textFieldCheckURL:         "Adresse (URL)",
		textHintCheckURL:          "Eine absolute http- oder https-URL, zum Beispiel http://localhost:8080/health.",
		textFieldCheckStatus:      "Erwarteter Status",
		textHintCheckStatus:       "Der HTTP-Status, mit dem die Adresse antworten muss, 100 bis 599; Standard 200.",
		textFieldCheckTimeout:     "Zeitlimit",
		textHintCheckTimeout:      "Wie lange auf die Antwort gewartet wird, von 1s bis 10m, zum Beispiel 10s.",
		textProblemCheckURLEmpty:  "Die HTTP-Prüfung ist an und braucht deshalb eine Adresse.",
		textProblemCheckURL:       "Die Adresse muss eine absolute http- oder https-URL sein.",
		textProblemCheckStatus:    "Der Status ist eine ganze Zahl von %d bis %d.",
		textProblemCheckTimeout:   "Das Zeitlimit ist eine Dauer von 1s bis 10m, zum Beispiel 10s oder 1m30s.",
		textAskCheckForm:          "Die Prüfung des Stacks %s speichern?",
		textWhatCheckAfter:        "Die Prüfung wirkt beim nächsten Deploy.",
		textDetailCheckContainers: "Container-Prüfung: %s",
		textDetailCheckHTTP:       "HTTP-Prüfung: %s antwortet mit %d innerhalb von %s",
		textDetailCheckNoHTTP:     "HTTP-Prüfung: aus",
		textDoneCheckForm:         "Prüfung des Stacks %s gespeichert.",
		textDoneCheckSame:         "Die Prüfung des Stacks %s war schon gesetzt; es wurde nichts committet.",
		textAskSettingsRemove:     "Alle Einstellungen des Stacks %s entfernen?",
		textWhatRemoveRemote:      "Ein Commit löscht die Einstellungsdatei des Stacks mit Update-Regeln, Backup, Compose-Dateien und Prüfung und wird zum Remote gepusht. Danach wird der Stack nur noch deployt und nie aktualisiert.",
		textWhatRemoveLocal:       "Ein Commit löscht die Einstellungsdatei des Stacks mit Update-Regeln, Backup, Compose-Dateien und Prüfung im lokalen Stack-Ordner. Danach wird der Stack nur noch deployt und nie aktualisiert.",
		textWhatRemoveUnknown:     "Ein Commit löscht die Einstellungsdatei des Stacks mit allen Abschnitten. Danach wird der Stack nur noch deployt und nie aktualisiert.",
		textDoneSettingsRemoved:   "Einstellungen des Stacks %s entfernt. Der Stack ist jetzt nur Deploy.",
		textSettingsNone:          "Der Stack %s hat keine Einstellungen. Er ist schon nur Deploy; es wurde nichts entfernt.",
	}
}
