package tui

import (
	"maps"

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
	textStateStopped       kittui.TextID = "state.stopped"
	textStatePaused        kittui.TextID = "state.paused"
	textStatePinned        kittui.TextID = "state.pinned"
	textStateFailed        kittui.TextID = "state.failed"
	textStatePending       kittui.TextID = "state.pending"
	textStateDeployed      kittui.TextID = "state.deployed"
	textStateNotDeployed   kittui.TextID = "state.notdeployed"
	textStateNeedsDirs     kittui.TextID = "state.needsdirs"

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
	textKeyTick          kittui.TextID = "key.tick"
	textKeyUpdate        kittui.TextID = "key.update"
	textKeyDeploy        kittui.TextID = "key.deploy"
	textKeyPause         kittui.TextID = "key.pause"
	textKeyUnpause       kittui.TextID = "key.unpause"
	textKeyPin           kittui.TextID = "key.pin"
	textKeyUnpin         kittui.TextID = "key.unpin"
	textKeyResume        kittui.TextID = "key.resume"
	textKeyCheck         kittui.TextID = "key.check"
	textKeyStart         kittui.TextID = "key.stackstart"
	textKeyStop          kittui.TextID = "key.stackstop"
	textKeyRestart       kittui.TextID = "key.stackrestart"

	textAnswerYes    kittui.TextID = "answer.yes"
	textAnswerCancel kittui.TextID = "answer.cancel"
	textAnswerClose  kittui.TextID = "answer.close"
	textAnswerLog    kittui.TextID = "answer.log"

	textAskDeploy   kittui.TextID = "ask.deploy"
	textAskUpdate   kittui.TextID = "ask.update"
	textAskPause    kittui.TextID = "ask.pause"
	textAskUnpause  kittui.TextID = "ask.unpause"
	textAskPin      kittui.TextID = "ask.pin"
	textAskUnpin    kittui.TextID = "ask.unpin"
	textAskResume   kittui.TextID = "ask.resume"
	textAskCheck    kittui.TextID = "ask.check"
	textAskStart    kittui.TextID = "ask.stackstart"
	textAskStop     kittui.TextID = "ask.stackstop"
	textAskRestart  kittui.TextID = "ask.stackrestart"
	textWhatDeploy  kittui.TextID = "what.deploy"
	textWhatUpdate  kittui.TextID = "what.update"
	textWhatPause   kittui.TextID = "what.pause"
	textWhatUnpause kittui.TextID = "what.unpause"
	textWhatPin     kittui.TextID = "what.pin"
	textWhatUnpin   kittui.TextID = "what.unpin"
	textWhatResume  kittui.TextID = "what.resume"
	textWhatCheck   kittui.TextID = "what.check"
	textWhatStart   kittui.TextID = "what.stackstart"
	textWhatStop    kittui.TextID = "what.stackstop"
	textWhatRestart kittui.TextID = "what.stackrestart"

	textActionBusy         kittui.TextID = "action.busy"
	textActionNoStack      kittui.TextID = "action.nostack"
	textActionNotResumable kittui.TextID = "action.notresumable"
	textActionStopped      kittui.TextID = "action.stopped"
	textErrStackStopped    kittui.TextID = "err.stackstopped"
	textErrRunActive       kittui.TextID = "err.runactive"
	textErrStackAction     kittui.TextID = "err.stackaction"
	textAllStacks          kittui.TextID = "action.allstacks"
	textStackCount         kittui.TextID = "action.stackcount"
	textTargetsMore        kittui.TextID = "action.more"
	textBusyRun            kittui.TextID = "action.busyrun"
	textBusyShort          kittui.TextID = "action.busyshort"
	textPreviewNoUpdate    kittui.TextID = "preview.noupdate"
	textPreviewMajor       kittui.TextID = "preview.major"

	textSummaryTitle  kittui.TextID = "summary.title"
	textSummaryNote   kittui.TextID = "summary.note"
	textOutcomeOK     kittui.TextID = "outcome.ok"
	textOutcomeFailed kittui.TextID = "outcome.failed"
	textOutcomeNotRun kittui.TextID = "outcome.notrun"
	textDoneDeploy    kittui.TextID = "done.deploy"
	textDoneStep      kittui.TextID = "done.step"
	textDoneRefused   kittui.TextID = "done.refused"
	textDoneCurrent   kittui.TextID = "done.current"
	textDoneNoAnswer  kittui.TextID = "done.noanswer"
	textDonePause     kittui.TextID = "done.pause"
	textDoneUnpause   kittui.TextID = "done.unpause"
	textDonePin       kittui.TextID = "done.pin"
	textDoneUnpin     kittui.TextID = "done.unpin"
	textDoneResume    kittui.TextID = "done.resume"
	textDoneUnchanged kittui.TextID = "done.unchanged"
	textDoneCheck     kittui.TextID = "done.check"
	textDoneStart     kittui.TextID = "done.stackstart"
	textDoneStop      kittui.TextID = "done.stackstop"
	textDoneRestart   kittui.TextID = "done.stackrestart"
	textDoneNotRun    kittui.TextID = "done.notrun"
	textStepSkipped   kittui.TextID = "done.skipped"
)

func catalog() i18n.Catalogs[kittui.TextID] {
	c := baseCatalog()
	maps.Copy(c[i18n.English], settingsEnglish())
	maps.Copy(c[i18n.German], settingsGerman())
	maps.Copy(c[i18n.English], detailEnglish())
	maps.Copy(c[i18n.German], detailGerman())
	maps.Copy(c[i18n.English], containerEnglish())
	maps.Copy(c[i18n.German], containerGerman())
	return c
}

func baseCatalog() i18n.Catalogs[kittui.TextID] {
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
			textStateNeedsDirs:      "needs data dirs",
			textStateRemoved:        "removed",
			textStateStopped:        "stopped",
			textStatePaused:         "paused",
			textStatePinned:         "pinned",
			textStateFailed:         "failed",
			textStatePending:        "pending",
			textStateDeployed:       "deployed",
			textStateNotDeployed:    "not deployed",
			textStateRollbackPauseS: "rollback",
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

			textKeyTick:    "tick",
			textKeyUpdate:  "update",
			textKeyDeploy:  "deploy",
			textKeyPause:   "pause",
			textKeyUnpause: "unpause",
			textKeyPin:     "pin",
			textKeyUnpin:   "unpin",
			textKeyResume:  "resume",
			textKeyCheck:   "check registries",
			textKeyStart:   "start",
			textKeyStop:    "stop",
			textKeyRestart: "restart",

			textAnswerYes:    "yes, do it",
			textAnswerCancel: "cancel",
			textAnswerClose:  "close",
			textAnswerLog:    "show log",

			textAskDeploy:   "Deploy %s?",
			textAskUpdate:   "Update %s?",
			textAskPause:    "Pause %s?",
			textAskUnpause:  "Unpause %s?",
			textAskPin:      "Pin %s?",
			textAskUnpin:    "Unpin %s?",
			textAskResume:   "Resume %s?",
			textAskCheck:    "Ask the registries now about %s?",
			textAskStart:    "Start %s?",
			textAskStop:     "Stop %s and keep it stopped until it is started?",
			textAskRestart:  "Restart %s?",
			textWhatDeploy:  "Starts a run at once: it stops the stack, backs up its data, pulls, starts and checks it, and rolls back if a step fails.",
			textWhatUpdate:  "Each stack is backed up, committed and deployed step by step, one stack after another; a failed step is rolled back. The versions shown are from the last check, the service asks the registries again.",
			textWhatPause:   "The maintenance window starts no run of the stack until you unpause it. Manual runs are not affected.",
			textWhatUnpause: "The maintenance window may run the stack again.",
			textWhatPin:     "The maintenance window and update leave the image versions of the stack as they are until you unpin it.",
			textWhatUnpin:   "The maintenance window and update may change the image versions of the stack again.",
			textWhatResume:  "Clears the pause a failed rollback left on the stack and starts nothing; the stack runs again on a new commit or a deploy.",
			textWhatCheck:   "Asks the registries for newer image versions now. This can take minutes and changes nothing in the stacks.",
			textWhatStart:   "Starts the existing containers of the stack, removes the stopped mark and checks the stack. It creates no containers; a stack that was never deployed needs a deploy.",
			textWhatStop:    "Stops the containers of the stack without removing them. The stack stays stopped until it is started: commits, the maintenance window and updates leave it out.",
			textWhatRestart: "Restarts the containers of the stack and checks the stack. A stack stopped through qdeploy is refused; start it instead.",

			textActionBusy:         "Another action is still running. Wait until it has finished.",
			textActionNoStack:      "Choose a stack first.",
			textActionNotResumable: "%s is not paused after a rollback; resume applies only to such a stack.",
			textActionStopped:      "%s is stopped; restart does not apply. Start it first.",
			textErrStackStopped:    "the stack is stopped; start it instead of restarting it",
			textErrRunActive:       "a run of the stack is active; try again when it has finished",
			textErrStackAction:     "the stack action failed: %s",
			textAllStacks:          "all stacks",
			textStackCount:         "%d stacks",
			textTargetsMore:        "… and %d more",
			textBusyRun:            "%s %d/%d: %s",
			textBusyShort:          "%s %d/%d",
			textPreviewNoUpdate:    "The stored detection shows no update. The service asks the registries again once you confirm.",
			textPreviewMajor:       "A newer major version is not installed automatically.",

			textSummaryTitle:  "%s finished: %d ok, %d failed",
			textSummaryNote:   "The result per stack.",
			textOutcomeOK:     "ok",
			textOutcomeFailed: "failed",
			textOutcomeNotRun: "not run",
			textDoneDeploy:    "run %s started at commit %s",
			textDoneStep:      "%s: %s -> %s, %s",
			textDoneRefused:   "refused: %s",
			textDoneCurrent:   "nothing to update, the stack is current",
			textDoneNoAnswer:  "the service gave no answer for this stack",
			textDonePause:     "paused",
			textDoneUnpause:   "no longer paused",
			textDonePin:       "pinned",
			textDoneUnpin:     "no longer pinned",
			textDoneResume:    "pause cleared, nothing started",
			textDoneUnchanged: "%s (it already was, nothing changed)",
			textDoneCheck:     "%d stacks: %d with an update available, %d could not be checked",
			textDoneStart:     "started, the stopped mark is removed",
			textDoneStop:      "stopped, it stays stopped until it is started",
			textDoneRestart:   "restarted",
			textDoneNotRun:    "an earlier stack failed",
			textStepSkipped:   "not attempted",
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
			textStateNeedsDirs:      "Datenordner fehlen",
			textStateRemoved:        "entfernt",
			textStateStopped:        "gestoppt",
			textStatePaused:         "pausiert",
			textStatePinned:         "gepinnt",
			textStateFailed:         "fehlgeschlagen",
			textStatePending:        "ausstehend",
			textStateDeployed:       "deployt",
			textStateNotDeployed:    "nicht deployt",
			textStateRollbackPauseS: "Rollback",
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

			textKeyTick:    "markieren",
			textKeyUpdate:  "update",
			textKeyDeploy:  "deploy",
			textKeyPause:   "pause",
			textKeyUnpause: "unpause",
			textKeyPin:     "pin",
			textKeyUnpin:   "unpin",
			textKeyResume:  "resume",
			textKeyCheck:   "Registries prüfen",
			textKeyStart:   "start",
			textKeyStop:    "stop",
			textKeyRestart: "restart",

			textAnswerYes:    "ja, ausführen",
			textAnswerCancel: "abbrechen",
			textAnswerClose:  "schließen",
			textAnswerLog:    "Log zeigen",

			textAskDeploy:   "Deploy von %s starten?",
			textAskUpdate:   "Update von %s ausführen?",
			textAskPause:    "Pause für %s setzen?",
			textAskUnpause:  "Pause von %s aufheben (unpause)?",
			textAskPin:      "%s pinnen?",
			textAskUnpin:    "Pin von %s aufheben (unpin)?",
			textAskResume:   "Pause von %s nach dem Rollback aufheben (resume)?",
			textAskCheck:    "Die Registries jetzt für %s abfragen?",
			textAskStart:    "%s starten (start)?",
			textAskStop:     "%s stoppen (stop) und gestoppt lassen, bis er gestartet wird?",
			textAskRestart:  "%s neu starten (restart)?",
			textWhatDeploy:  "Startet sofort einen Lauf: er stoppt den Stack, sichert seine Daten, lädt, startet und prüft ihn und macht bei einem fehlgeschlagenen Schritt einen Rollback.",
			textWhatUpdate:  "Jeder Stack wird gesichert, per Commit geändert und Schritt für Schritt ausgerollt, ein Stack nach dem anderen; ein fehlgeschlagener Schritt wird zurückgerollt. Die gezeigten Versionen stammen aus der letzten Prüfung, der Dienst fragt die Registries erneut ab.",
			textWhatPause:   "Das Wartungsfenster startet keinen Lauf des Stacks, bis du die Pause aufhebst. Manuelle Läufe bleiben möglich.",
			textWhatUnpause: "Das Wartungsfenster darf den Stack wieder ausführen.",
			textWhatPin:     "Wartungsfenster und update lassen die Image-Versionen des Stacks unverändert, bis du den Pin aufhebst.",
			textWhatUnpin:   "Wartungsfenster und update dürfen die Image-Versionen des Stacks wieder ändern.",
			textWhatResume:  "Hebt die Pause auf, die ein fehlgeschlagener Rollback hinterlassen hat, und startet nichts; der Stack läuft bei einem neuen Commit oder per deploy wieder.",
			textWhatCheck:   "Fragt die Registries jetzt nach neueren Image-Versionen. Das kann Minuten dauern und ändert nichts an den Stacks.",
			textWhatStart:   "Startet die vorhandenen Container des Stacks, hebt die Markierung „gestoppt“ auf und prüft den Stack. Es entstehen keine neuen Container; ein nie ausgerollter Stack braucht ein deploy.",
			textWhatStop:    "Stoppt die Container des Stacks, ohne sie zu entfernen. Der Stack bleibt gestoppt, bis er gestartet wird: Commits, das Wartungsfenster und Updates lassen ihn aus.",
			textWhatRestart: "Startet die Container des Stacks neu und prüft den Stack. Ein über qdeploy gestoppter Stack wird abgelehnt; starte ihn stattdessen.",

			textActionBusy:         "Eine andere Aktion läuft noch. Warte, bis sie fertig ist.",
			textActionNoStack:      "Wähle zuerst einen Stack.",
			textActionNotResumable: "%s ist nicht nach einem Rollback pausiert; resume gilt nur für einen solchen Stack.",
			textActionStopped:      "%s ist gestoppt; restart gilt dafür nicht. Starte ihn zuerst.",
			textErrStackStopped:    "der Stack ist gestoppt; starte ihn, statt ihn neu zu starten",
			textErrRunActive:       "ein Lauf des Stacks ist aktiv; versuche es nach seinem Ende erneut",
			textErrStackAction:     "die Stack-Aktion ist fehlgeschlagen: %s",
			textAllStacks:          "alle Stacks",
			textStackCount:         "%d Stacks",
			textTargetsMore:        "… und %d weitere",
			textBusyRun:            "%s %d/%d: %s",
			textBusyShort:          "%s %d/%d",
			textPreviewNoUpdate:    "Die gespeicherte Prüfung zeigt kein Update. Der Dienst fragt die Registries nach deiner Bestätigung erneut ab.",
			textPreviewMajor:       "Eine neuere Major-Version wird nicht automatisch installiert.",

			textSummaryTitle:  "%s beendet: %d ok, %d fehlgeschlagen",
			textSummaryNote:   "Das Ergebnis je Stack.",
			textOutcomeOK:     "ok",
			textOutcomeFailed: "fehlgeschlagen",
			textOutcomeNotRun: "nicht ausgeführt",
			textDoneDeploy:    "Lauf %s bei Commit %s gestartet",
			textDoneStep:      "%s: %s -> %s, %s",
			textDoneRefused:   "abgelehnt: %s",
			textDoneCurrent:   "nichts zu aktualisieren, der Stack ist aktuell",
			textDoneNoAnswer:  "der Dienst hat für diesen Stack nichts geantwortet",
			textDonePause:     "pausiert",
			textDoneUnpause:   "nicht mehr pausiert",
			textDonePin:       "gepinnt",
			textDoneUnpin:     "nicht mehr gepinnt",
			textDoneResume:    "Pause aufgehoben, nichts gestartet",
			textDoneUnchanged: "%s (war bereits so, nichts geändert)",
			textDoneCheck:     "%d Stacks: %d mit verfügbarem Update, %d nicht prüfbar",
			textDoneStart:     "gestartet, die Markierung „gestoppt“ ist aufgehoben",
			textDoneStop:      "gestoppt, bleibt gestoppt, bis er gestartet wird",
			textDoneRestart:   "neu gestartet",
			textDoneNotRun:    "ein früherer Stack ist fehlgeschlagen",
			textStepSkipped:   "nicht versucht",
		},
	}
}
