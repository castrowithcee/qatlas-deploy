package tui

import (
	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The texts of the interface. Command names and the terms stack, deploy, update, pin and rollback stay
// English in every language.
const (
	textAreaStacks kittui.TextID = "area.stacks"
	textAreaDetail kittui.TextID = "area.detail"
	textAreaRuns   kittui.TextID = "area.runs"

	textKeyArea     kittui.TextID = "key.area"
	textKeyDetails  kittui.TextID = "key.details"
	textFilterKeep  kittui.TextID = "filter.keep"
	textFilterClear kittui.TextID = "filter.clear"

	textColStack     kittui.TextID = "col.stack"
	textColCurrent   kittui.TextID = "col.current"
	textColAvailable kittui.TextID = "col.available"
	textColMode      kittui.TextID = "col.mode"
	textColState     kittui.TextID = "col.state"
	textColLastRun   kittui.TextID = "col.lastrun"
	textColFindings  kittui.TextID = "col.findings"

	textModeAutomatic kittui.TextID = "mode.automatic"
	textModeManual    kittui.TextID = "mode.manual"
	textModeNone      kittui.TextID = "mode.none"

	textStateRunning       kittui.TextID = "state.running"
	textStateRollbackPause kittui.TextID = "state.rollbackpause"
	textStateUnreadable    kittui.TextID = "state.unreadable"
	textStateRemoved       kittui.TextID = "state.removed"
	textStatePaused        kittui.TextID = "state.paused"
	textStatePinned        kittui.TextID = "state.pinned"
	textStateFailed        kittui.TextID = "state.failed"
	textStatePending       kittui.TextID = "state.pending"
	textStateDeployed      kittui.TextID = "state.deployed"
	textStateNotDeployed   kittui.TextID = "state.notdeployed"

	textResultSucceeded      kittui.TextID = "result.succeeded"
	textResultFailed         kittui.TextID = "result.failed"
	textResultRolledBack     kittui.TextID = "result.rolledback"
	textResultRollbackFailed kittui.TextID = "result.rollbackfailed"

	textAvailNotChecked    kittui.TextID = "avail.notchecked"
	textAvailUpToDate      kittui.TextID = "avail.uptodate"
	textAvailMajor         kittui.TextID = "avail.major"
	textAvailError         kittui.TextID = "avail.error"
	textAvailNotComparable kittui.TextID = "avail.notcomparable"

	textFindingsNone         kittui.TextID = "findings.none"
	textFindingsErrorsOne    kittui.TextID = "findings.errors.one"
	textFindingsErrorsMany   kittui.TextID = "findings.errors.many"
	textFindingsWarningsOne  kittui.TextID = "findings.warnings.one"
	textFindingsWarningsMany kittui.TextID = "findings.warnings.many"

	textStateRollbackPauseS kittui.TextID = "state.rollbackpause.short"
	textStatePendingS       kittui.TextID = "state.pending.short"
	textStateNotDeployedS   kittui.TextID = "state.notdeployed.short"
	textAvailErrorS         kittui.TextID = "avail.error.short"

	textHeaderRefreshing  kittui.TextID = "header.refreshing"
	textHeaderWindowOpen  kittui.TextID = "header.windowopen"
	textHeaderWindowShut  kittui.TextID = "header.windowshut"
	textHeaderWindowOpenS kittui.TextID = "header.windowopen.short"
	textHeaderWindowShutS kittui.TextID = "header.windowshut.short"
	textHeaderPollFailed  kittui.TextID = "header.pollfailed"
	textHeaderPollFailedS kittui.TextID = "header.pollfailed.short"
	textStacksLoading     kittui.TextID = "stacks.loading"
	textStacksEmpty       kittui.TextID = "stacks.empty"
	textDetailNone        kittui.TextID = "detail.none"
	textErrNotRunning     kittui.TextID = "error.notrunning"
	textErrNoAccess       kittui.TextID = "error.noaccess"
	textErrService        kittui.TextID = "error.service"
	textErrChannel        kittui.TextID = "error.channel"
	textErrUnshownStacks  kittui.TextID = "error.unshownstacks"
	textNoValue           kittui.TextID = "value.none"

	textKeyStackOnly     kittui.TextID = "key.stackonly"
	textKeyAllStacks     kittui.TextID = "key.allstacks"
	textKeyLog           kittui.TextID = "key.log"
	textColRun           kittui.TextID = "col.run"
	textColStarted       kittui.TextID = "col.started"
	textColResult        kittui.TextID = "col.result"
	textColTrigger       kittui.TextID = "col.trigger"
	textColDuration      kittui.TextID = "col.duration"
	textRunsEmpty        kittui.TextID = "runs.empty"
	textRunsOnly         kittui.TextID = "runs.only"
	textRunsNoStack      kittui.TextID = "runs.nostack"
	textLogLoading       kittui.TextID = "log.loading"
	textDetailVersions   kittui.TextID = "detail.versions"
	textDetailChangelog  kittui.TextID = "detail.changelog"
	textDetailBackups    kittui.TextID = "detail.backups"
	textDetailRuns       kittui.TextID = "detail.runs"
	textDetailLoading    kittui.TextID = "detail.loading"
	textDetailNoImages   kittui.TextID = "detail.noimages"
	textItemUpdate       kittui.TextID = "item.update"
	textItemMajor        kittui.TextID = "item.major"
	textChangelogNever   kittui.TextID = "changelog.never"
	textChangelogNone    kittui.TextID = "changelog.none"
	textChangelogNoNotes kittui.TextID = "changelog.nonotes"
	textChangelogSource  kittui.TextID = "changelog.source"
	textChangelogLink    kittui.TextID = "changelog.link"
	textChangelogOmitted kittui.TextID = "changelog.omitted"
	textChangelogMore    kittui.TextID = "changelog.more"
	textBackupsNone      kittui.TextID = "backups.none"
	textBackupIncomplete kittui.TextID = "backups.incomplete"
)

func catalog() i18n.Catalogs[kittui.TextID] {
	return i18n.Catalogs[kittui.TextID]{
		i18n.English: {
			textAreaStacks: "Stacks",
			textAreaDetail: "Stack detail",
			textAreaRuns:   "Runs",

			textKeyArea:     "area",
			textKeyDetails:  "details",
			textFilterKeep:  "keep filter",
			textFilterClear: "clear filter",

			textColStack:     "stack",
			textColCurrent:   "current",
			textColAvailable: "available",
			textColMode:      "mode",
			textColState:     "state",
			textColLastRun:   "last run",
			textColFindings:  "findings",

			textModeAutomatic: "automatic",
			textModeManual:    "manual",
			textModeNone:      "none",

			textStateRunning:        "running",
			textStateRollbackPause:  "paused after rollback",
			textStateUnreadable:     "unreadable",
			textStateRemoved:        "removed",
			textStatePaused:         "paused",
			textStatePinned:         "pinned",
			textStateFailed:         "failed",
			textStatePending:        "pending",
			textStateDeployed:       "deployed",
			textStateNotDeployed:    "not deployed",
			textStateRollbackPauseS: "stopped",
			textStatePendingS:       "pending",
			textStateNotDeployedS:   "new",

			textResultSucceeded:      "succeeded",
			textResultFailed:         "failed",
			textResultRolledBack:     "rolled back",
			textResultRollbackFailed: "rollback failed",

			textAvailNotChecked:    "unchecked",
			textAvailUpToDate:      "up to date",
			textAvailMajor:         "%s (major)",
			textAvailError:         "check failed",
			textAvailNotComparable: "not comparable",
			textAvailErrorS:        "error",

			textFindingsNone:         "none",
			textFindingsErrorsOne:    "%d error",
			textFindingsErrorsMany:   "%d errors",
			textFindingsWarningsOne:  "%d warning",
			textFindingsWarningsMany: "%d warnings",

			textHeaderRefreshing:  "refreshing",
			textHeaderWindowOpen:  "maintenance window open",
			textHeaderWindowShut:  "maintenance window closed",
			textHeaderWindowOpenS: "window open",
			textHeaderWindowShutS: "window closed",
			textHeaderPollFailed:  "repository poll failed",
			textHeaderPollFailedS: "poll failed",
			textStacksLoading:     "Loading the stacks…",
			textStacksEmpty:       "No stacks. Bind the stacks repository with 'sudo qdeploy repo set <ssh-url>', or add a stack to it.",
			textDetailNone:        "No stack chosen. Choose one in Stacks and press {enter}.",
			textErrNotRunning:     "the qdeploy service is not running; start it, then press {reload}",
			textErrNoAccess:       "only root may use the qdeploy service; quit and run qdeploy with sudo",
			textErrService:        "the service refused the request: %s",
			textErrChannel:        "cannot talk to the service: %s",
			textErrUnshownStacks:  "the stack list could not be shown: %s",
			textNoValue:           "-",

			textKeyStackOnly:     "this stack",
			textKeyAllStacks:     "all stacks",
			textKeyLog:           "log",
			textColRun:           "run",
			textColStarted:       "started",
			textColResult:        "result",
			textColTrigger:       "trigger",
			textColDuration:      "duration",
			textRunsEmpty:        "No runs recorded.",
			textRunsOnly:         "Runs of %s only. {stackonly} shows all stacks.",
			textRunsNoStack:      "Choose a stack in Stacks first.",
			textLogLoading:       "Loading the log…",
			textDetailVersions:   "Image versions",
			textDetailChangelog:  "Changelog",
			textDetailBackups:    "Backups",
			textDetailRuns:       "Last runs",
			textDetailLoading:    "Loading…",
			textDetailNoImages:   "No image version variables.",
			textItemUpdate:       "update available",
			textItemMajor:        "newer major %s",
			textChangelogNever:   "Never checked for updates, so there are no release notes.",
			textChangelogNone:    "No update is available, so there are no release notes.",
			textChangelogNoNotes: "No release notes.",
			textChangelogSource:  "source %s",
			textChangelogLink:    "more at %s",
			textChangelogOmitted: "%d more releases left out",
			textChangelogMore:    "… %d more lines",
			textBackupsNone:      "No backups.",
			textBackupIncomplete: "incomplete",
		},
		i18n.German: {
			textAreaStacks: "Stacks",
			textAreaDetail: "Stack-Details",
			textAreaRuns:   "Läufe",

			textKeyArea:     "Bereich",
			textKeyDetails:  "Details",
			textFilterKeep:  "Filter behalten",
			textFilterClear: "Filter leeren",

			textColStack:     "Stack",
			textColCurrent:   "Version",
			textColAvailable: "verfügbar",
			textColMode:      "Modus",
			textColState:     "Zustand",
			textColLastRun:   "letzter Lauf",
			textColFindings:  "Befunde",

			textModeAutomatic: "automatisch",
			textModeManual:    "manuell",
			textModeNone:      "keiner",

			textStateRunning:        "läuft",
			textStateRollbackPause:  "pausiert nach Rollback",
			textStateUnreadable:     "unlesbar",
			textStateRemoved:        "entfernt",
			textStatePaused:         "pausiert",
			textStatePinned:         "gepinnt",
			textStateFailed:         "fehlgeschlagen",
			textStatePending:        "ausstehend",
			textStateDeployed:       "deployt",
			textStateNotDeployed:    "nicht deployt",
			textStateRollbackPauseS: "gestoppt",
			textStatePendingS:       "offen",
			textStateNotDeployedS:   "neu",

			textResultSucceeded:      "erfolgreich",
			textResultFailed:         "fehlgeschlagen",
			textResultRolledBack:     "zurückgerollt",
			textResultRollbackFailed: "Rollback fehlgeschlagen",

			textAvailNotChecked:    "ungeprüft",
			textAvailUpToDate:      "aktuell",
			textAvailMajor:         "%s (Major)",
			textAvailError:         "Prüfung fehlgeschlagen",
			textAvailNotComparable: "nicht vergleichbar",
			textAvailErrorS:        "Fehler",

			textFindingsNone:         "keine",
			textFindingsErrorsOne:    "%d Fehler",
			textFindingsErrorsMany:   "%d Fehler",
			textFindingsWarningsOne:  "%d Warnung",
			textFindingsWarningsMany: "%d Warnungen",

			textHeaderRefreshing:  "wird aktualisiert",
			textHeaderWindowOpen:  "Wartungsfenster offen",
			textHeaderWindowShut:  "Wartungsfenster zu",
			textHeaderWindowOpenS: "Fenster offen",
			textHeaderWindowShutS: "Fenster zu",
			textHeaderPollFailed:  "Abruf des Repositorys fehlgeschlagen",
			textHeaderPollFailedS: "Abruf fehlgeschlagen",
			textStacksLoading:     "Stacks werden geladen…",
			textStacksEmpty:       "Keine Stacks. Binde das Stacks-Repository mit 'sudo qdeploy repo set <ssh-url>' an oder lege dort einen Stack an.",
			textDetailNone:        "Kein Stack gewählt. Wähle einen unter Stacks und drücke {enter}.",
			textErrNotRunning:     "der qdeploy-Dienst läuft nicht; starte ihn und drücke dann {reload}",
			textErrNoAccess:       "nur root darf den qdeploy-Dienst nutzen; beende und starte qdeploy mit sudo",
			textErrService:        "der Dienst hat die Anfrage abgelehnt: %s",
			textErrChannel:        "keine Verbindung zum Dienst: %s",
			textErrUnshownStacks:  "die Stack-Liste ließ sich nicht anzeigen: %s",
			textNoValue:           "-",

			textKeyStackOnly:     "dieser Stack",
			textKeyAllStacks:     "alle Stacks",
			textKeyLog:           "Log",
			textColRun:           "Lauf",
			textColStarted:       "Start",
			textColResult:        "Ergebnis",
			textColTrigger:       "Auslöser",
			textColDuration:      "Dauer",
			textRunsEmpty:        "Keine Läufe aufgezeichnet.",
			textRunsOnly:         "Nur Läufe von %s. {stackonly} zeigt alle Stacks.",
			textRunsNoStack:      "Wähle zuerst einen Stack unter Stacks.",
			textLogLoading:       "Log wird geladen…",
			textDetailVersions:   "Image-Versionen",
			textDetailChangelog:  "Changelog",
			textDetailBackups:    "Backups",
			textDetailRuns:       "Letzte Läufe",
			textDetailLoading:    "Wird geladen…",
			textDetailNoImages:   "Keine Image-Versionsvariablen.",
			textItemUpdate:       "Update verfügbar",
			textItemMajor:        "neuere Major-Version %s",
			textChangelogNever:   "Nie auf Updates geprüft, daher gibt es keine Release-Notes.",
			textChangelogNone:    "Kein Update verfügbar, daher gibt es keine Release-Notes.",
			textChangelogNoNotes: "Keine Release-Notes.",
			textChangelogSource:  "Quelle %s",
			textChangelogLink:    "mehr unter %s",
			textChangelogOmitted: "%d weitere Releases ausgelassen",
			textChangelogMore:    "… %d weitere Zeilen",
			textBackupsNone:      "Keine Backups.",
			textBackupIncomplete: "unvollständig",
		},
	}
}
