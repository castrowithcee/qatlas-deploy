package tui

import kittui "github.com/castrowithcee/qatlas-kit/tui"

// The texts of the Containers area and of the container section of the stack detail. Docker names are never
// part of a catalog text; they come in as arguments and are escaped where they are drawn.
const (
	textAreaContainers kittui.TextID = "area.containers"

	textColImage    kittui.TextID = "col.image"
	textColUptime   kittui.TextID = "col.uptime"
	textColRestarts kittui.TextID = "col.restarts"

	textCtOK         kittui.TextID = "ct.state.ok"
	textCtUnhealthy  kittui.TextID = "ct.state.unhealthy"
	textCtRestarting kittui.TextID = "ct.state.restarting"
	textCtStopped    kittui.TextID = "ct.state.stopped"
	textCtStarting   kittui.TextID = "ct.state.starting"

	textCtGroupOwn      kittui.TextID = "ct.group.own"
	textCtGroupForeign  kittui.TextID = "ct.group.foreign"
	textCtGroupConflict kittui.TextID = "ct.group.conflict"
	textCtGroupSingle   kittui.TextID = "ct.group.single"
	textCtKindSingle    kittui.TextID = "ct.kind.single"

	textCtLoading     kittui.TextID = "ct.loading"
	textCtEmpty       kittui.TextID = "ct.empty"
	textCtNoneOfStack kittui.TextID = "ct.noneofstack"
	textCtNone        kittui.TextID = "ct.none"
	textCtReadOnly    kittui.TextID = "ct.readonly"

	textCtFieldGroup    kittui.TextID = "ct.field.group"
	textCtFieldState    kittui.TextID = "ct.field.state"
	textCtFieldImage    kittui.TextID = "ct.field.image"
	textCtFieldUptime   kittui.TextID = "ct.field.uptime"
	textCtFieldRestarts kittui.TextID = "ct.field.restarts"
	textCtFieldHealth   kittui.TextID = "ct.field.health"

	textCtPorts    kittui.TextID = "ct.ports"
	textCtMounts   kittui.TextID = "ct.mounts"
	textCtNetworks kittui.TextID = "ct.networks"
	textCtLabels   kittui.TextID = "ct.labels"
	textCtEnvNames kittui.TextID = "ct.envnames"

	textDetailContainers kittui.TextID = "detail.containers"

	textCtFieldDir     kittui.TextID = "ct.field.dir"
	textCtComposeFiles kittui.TextID = "ct.composefiles"
	textCtEnvFiles     kittui.TextID = "ct.envfiles"

	textKeyAdopt          kittui.TextID = "key.adopt"
	textTitleAdopt        kittui.TextID = "title.adopt"
	textAskAdoptRejected  kittui.TextID = "ask.adoptrejected"
	textHeadForeign       kittui.TextID = "head.foreign"
	textHeadWarning       kittui.TextID = "head.warning"
	textAdoptProject      kittui.TextID = "adopt.project"
	textAdoptComposeFiles kittui.TextID = "adopt.composefiles"
	textAdoptEnvFiles     kittui.TextID = "adopt.envfiles"
	textAdoptWarnProject  kittui.TextID = "adopt.warn.project"
	textAdoptWarnCompose  kittui.TextID = "adopt.warn.compose"
	textAdoptWarnEnv      kittui.TextID = "adopt.warn.env"
	textAdoptEnvImplicit  kittui.TextID = "adopt.envimplicit"
	textAdoptWarnEffect   kittui.TextID = "adopt.warn.effect"
	textAdoptNoDir        kittui.TextID = "adopt.nodir"
	textAdoptDirRelative  kittui.TextID = "adopt.dirrelative"
	textAdoptOutside      kittui.TextID = "adopt.outside"
	textAdoptStacks       kittui.TextID = "adopt.stacks"
	textAdoptNotRoot      kittui.TextID = "adopt.notroot"

	textErrDockerUnreachable kittui.TextID = "error.dockerunreachable"
	textErrDockerPermission  kittui.TextID = "error.dockerpermission"
	textErrDockerTimeout     kittui.TextID = "error.dockertimeout"
	textErrContainersSlow    kittui.TextID = "error.containersslow"

	textCtKindOwn     kittui.TextID = "ct.kind.own"
	textCtKindForeign kittui.TextID = "ct.kind.foreign"

	textAskCtStart     kittui.TextID = "ask.ctstart"
	textAskCtStop      kittui.TextID = "ask.ctstop"
	textAskCtRestart   kittui.TextID = "ask.ctrestart"
	textWhatCtStart    kittui.TextID = "what.ctstart"
	textWhatCtStop     kittui.TextID = "what.ctstop"
	textWhatCtStopOwn  kittui.TextID = "what.ctstopown"
	textWhatCtRestart  kittui.TextID = "what.ctrestart"
	textCtStateBefore  kittui.TextID = "ct.act.statebefore"
	textCtStateAfter   kittui.TextID = "ct.act.stateafter"
	textDoneCtStart    kittui.TextID = "done.ctstart"
	textDoneCtStop     kittui.TextID = "done.ctstop"
	textDoneCtRestart  kittui.TextID = "done.ctrestart"
	textDoneCtNotReady kittui.TextID = "done.ctnotready"
	textSummaryNoteCt  kittui.TextID = "summary.notect"

	textCtNotReadyExited     kittui.TextID = "ct.notready.exited"
	textCtNotReadyRestarting kittui.TextID = "ct.notready.restarting"
	textCtNotReadyNotRunning kittui.TextID = "ct.notready.notrunning"
	textCtNotReadyUnhealthy  kittui.TextID = "ct.notready.unhealthy"
	textCtNotReadyStarting   kittui.TextID = "ct.notready.starting"

	textErrCtGone        kittui.TextID = "error.ctact.gone"
	textErrCtUnreachable kittui.TextID = "error.ctact.unreachable"
	textErrCtPermission  kittui.TextID = "error.ctact.permission"
	textErrCtEngineSlow  kittui.TextID = "error.ctact.engineslow"
	textErrCtSlow        kittui.TextID = "error.ctact.slow"

	textCtLogStack    kittui.TextID = "ct.log.stack"
	textErrCtLogGone  kittui.TextID = "error.ctlog.gone"
	textErrCtLogStack kittui.TextID = "error.ctlog.stack"
	textErrCtLogSlow  kittui.TextID = "error.ctlog.slow"
)

func containerEnglish() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textAreaContainers: "Containers",

		textColImage:    "image",
		textColUptime:   "uptime",
		textColRestarts: "restarts",

		textCtOK:         "ok",
		textCtUnhealthy:  "unhealthy",
		textCtRestarting: "restarting",
		textCtStopped:    "stopped",
		textCtStarting:   "starting",

		textCtGroupOwn:      "own stack %s",
		textCtGroupForeign:  "foreign project %s",
		textCtGroupConflict: "foreign project %s [conflict: name of a stack, other folder]",
		textCtGroupSingle:   "single containers",
		textCtKindSingle:    "single container",

		textCtLoading:     "Reading the containers…",
		textCtEmpty:       "No containers on this host.",
		textCtNoneOfStack: "No containers of this stack.",
		textCtNone:        "none",
		textCtReadOnly:    "read-only",

		textCtFieldGroup:    "group",
		textCtFieldState:    "state",
		textCtFieldImage:    "image",
		textCtFieldUptime:   "uptime",
		textCtFieldRestarts: "restarts",
		textCtFieldHealth:   "health",

		textCtPorts:    "Ports",
		textCtMounts:   "Mounts",
		textCtNetworks: "Networks",
		textCtLabels:   "Labels",
		textCtEnvNames: "Environment variables (names only)",

		textDetailContainers: "Containers",

		textCtFieldDir:     "folder",
		textCtComposeFiles: "Compose files",
		textCtEnvFiles:     "Env files",

		textKeyAdopt:          "adopt",
		textTitleAdopt:        "Adopt a foreign project",
		textAskAdoptRejected:  "The project %s cannot be taken over.",
		textHeadForeign:       "Foreign project",
		textHeadWarning:       "Warning",
		textAdoptProject:      "project: %s",
		textAdoptComposeFiles: "compose files (labels): %s",
		textAdoptEnvFiles:     "env files (labels): %s",
		textAdoptWarnProject:  "project name: the running containers belong to %s, a deploy would use %s",
		textAdoptWarnCompose:  "compose files: the running containers use %s, a deploy would pass %s",
		textAdoptEnvImplicit:  "none; Compose reads the file .env of the folder if it exists",
		textAdoptWarnEnv:      "env files: the running containers use %s, a deploy would pass %s",
		textAdoptWarnEffect:   "The first deploy would create new containers instead of taking over the running ones.",
		textAdoptNoDir:        "the containers carry no working directory, so there is no folder to take over",
		textAdoptDirRelative:  "the working directory %s is not an absolute path",
		textAdoptOutside:      "%s lies outside the working directory %s; a stack folder holds all files of its stack",
		textAdoptStacks:       "the folder must hold exactly one stack, the inspection found %d",
		textAdoptNotRoot:      "the compose file must lie directly in the folder, not in its subfolder %s",

		textErrDockerUnreachable: "the container engine is not reachable; start it, then press {reload}",
		textErrDockerPermission:  "the service may not use the container engine; give its user access to the engine socket, then press {reload}",
		textErrDockerTimeout:     "the container engine did not answer in time; check that it is healthy, then press {reload}",
		textErrContainersSlow:    "the service did not answer in time; the next refresh tries again",

		textCtKindOwn:     "own stack %s",
		textCtKindForeign: "foreign project %s",

		textAskCtStart:     "Start container %s (%s)?",
		textAskCtStop:      "Stop container %s (%s)?",
		textAskCtRestart:   "Restart container %s (%s)?",
		textWhatCtStart:    "Starts the container and waits up to a minute until it runs and, if it has a health check, is healthy. A container that already runs is left as it is.",
		textWhatCtStop:     "Signals the container to end and kills it after %d seconds. Whatever it serves is unavailable until it is started again. A container that is already stopped is left as it is.",
		textWhatCtStopOwn:  "Signals the container to end and kills it after %d seconds. Whatever it serves is unavailable until it is started again. The stack is not marked as stopped: a run of the stack (deploy, update or the maintenance window) can start the container again.",
		textWhatCtRestart:  "Stops the container as stop does, starts it again and waits up to a minute until it runs and, if it has a health check, is healthy. Whatever it serves is unavailable meanwhile.",
		textCtStateBefore:  "State: %s",
		textCtStateAfter:   "State afterwards: %s",
		textDoneCtStart:    "started",
		textDoneCtStop:     "stopped",
		textDoneCtRestart:  "restarted",
		textDoneCtNotReady: "%s, but the container is not ready: %s",
		textSummaryNoteCt:  "The result for the container.",

		textCtNotReadyExited:     "it exited",
		textCtNotReadyRestarting: "it keeps restarting",
		textCtNotReadyNotRunning: "it is not running",
		textCtNotReadyUnhealthy:  "its health check fails",
		textCtNotReadyStarting:   "its health check has not passed yet",

		textErrCtGone:        "this container no longer exists",
		textErrCtUnreachable: "the container engine is not reachable; start it and try again",
		textErrCtPermission:  "the service may not use the container engine; give its user access to the engine socket",
		textErrCtEngineSlow:  "the container engine did not answer in time; check the state of the container before you try again",
		textErrCtSlow:        "the service did not answer in time; check the state of the container before you try again",

		textCtLogStack:    "all containers",
		textErrCtLogGone:  "this container no longer exists; press {back} to leave the log",
		textErrCtLogStack: "this stack is not known to the service; press {back} to leave the log",
		textErrCtLogSlow:  "the service did not answer in time; press {reload} to try again",
	}
}

func containerGerman() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textAreaContainers: "Container",

		textColImage:    "Image",
		textColUptime:   "Laufzeit",
		textColRestarts: "Neustarts",

		textCtOK:         "ok",
		textCtUnhealthy:  "ungesund",
		textCtRestarting: "Neustart",
		textCtStopped:    "gestoppt",
		textCtStarting:   "startet",

		textCtGroupOwn:      "eigener Stack %s",
		textCtGroupForeign:  "fremdes Projekt %s",
		textCtGroupConflict: "fremdes Projekt %s [Konflikt: Name eines Stacks, anderer Ordner]",
		textCtGroupSingle:   "einzelne Container",
		textCtKindSingle:    "einzelner Container",

		textCtLoading:     "Container werden gelesen…",
		textCtEmpty:       "Keine Container auf diesem Host.",
		textCtNoneOfStack: "Keine Container dieses Stacks.",
		textCtNone:        "keine",
		textCtReadOnly:    "nur lesend",

		textCtFieldGroup:    "Gruppe",
		textCtFieldState:    "Zustand",
		textCtFieldImage:    "Image",
		textCtFieldUptime:   "Laufzeit",
		textCtFieldRestarts: "Neustarts",
		textCtFieldHealth:   "Gesundheit",

		textCtPorts:    "Ports",
		textCtMounts:   "Mounts",
		textCtNetworks: "Netze",
		textCtLabels:   "Labels",
		textCtEnvNames: "Umgebungsvariablen (nur Namen)",

		textDetailContainers: "Container",

		textCtFieldDir:     "Ordner",
		textCtComposeFiles: "Compose-Dateien",
		textCtEnvFiles:     "Env-Dateien",

		textKeyAdopt:          "übernehmen",
		textTitleAdopt:        "Fremdes Projekt übernehmen",
		textAskAdoptRejected:  "Das Projekt %s kann nicht übernommen werden.",
		textHeadForeign:       "Fremdes Projekt",
		textHeadWarning:       "Warnung",
		textAdoptProject:      "Projekt: %s",
		textAdoptComposeFiles: "Compose-Dateien (Labels): %s",
		textAdoptEnvFiles:     "Env-Dateien (Labels): %s",
		textAdoptWarnProject:  "Projektname: Die laufenden Container gehören zu %s, ein Deploy nähme %s",
		textAdoptWarnCompose:  "Compose-Dateien: Die laufenden Container nutzen %s, ein Deploy übergäbe %s",
		textAdoptEnvImplicit:  "keine; Compose liest die Datei .env des Ordners, wenn sie existiert",
		textAdoptWarnEnv:      "Env-Dateien: Die laufenden Container nutzen %s, ein Deploy übergäbe %s",
		textAdoptWarnEffect:   "Der erste Deploy legte neue Container an, statt die laufenden zu übernehmen.",
		textAdoptNoDir:        "die Container tragen kein Arbeitsverzeichnis, es gibt also keinen Ordner zum Übernehmen",
		textAdoptDirRelative:  "das Arbeitsverzeichnis %s ist kein absoluter Pfad",
		textAdoptOutside:      "%s liegt außerhalb des Arbeitsverzeichnisses %s; ein Stack-Ordner enthält alle Dateien seines Stacks",
		textAdoptStacks:       "der Ordner muss genau einen Stack enthalten, die Inspektion fand %d",
		textAdoptNotRoot:      "die Compose-Datei muss direkt im Ordner liegen, nicht im Unterordner %s",

		textErrDockerUnreachable: "die Container-Engine ist nicht erreichbar; starte sie und drücke dann {reload}",
		textErrDockerPermission:  "der Dienst darf die Container-Engine nicht nutzen; gib seinem Benutzer Zugriff auf den Socket der Engine und drücke dann {reload}",
		textErrDockerTimeout:     "die Container-Engine hat nicht rechtzeitig geantwortet; prüfe ihren Zustand und drücke dann {reload}",
		textErrContainersSlow:    "der Dienst hat nicht rechtzeitig geantwortet; die nächste Aktualisierung versucht es erneut",

		textCtKindOwn:     "eigener Stack %s",
		textCtKindForeign: "fremdes Projekt %s",

		textAskCtStart:     "Container %s (%s) starten (start)?",
		textAskCtStop:      "Container %s (%s) stoppen (stop)?",
		textAskCtRestart:   "Container %s (%s) neu starten (restart)?",
		textWhatCtStart:    "Startet den Container und wartet bis zu eine Minute, bis er läuft und, falls er einen Health-Check hat, gesund ist. Ein Container, der schon läuft, bleibt unverändert.",
		textWhatCtStop:     "Signalisiert dem Container das Ende und beendet ihn nach %d Sekunden gewaltsam. Was er bereitstellt, ist nicht erreichbar, bis er wieder gestartet wird. Ein bereits gestoppter Container bleibt unverändert.",
		textWhatCtStopOwn:  "Signalisiert dem Container das Ende und beendet ihn nach %d Sekunden gewaltsam. Was er bereitstellt, ist nicht erreichbar, bis er wieder gestartet wird. Der Stack wird nicht als gestoppt markiert: ein Lauf des Stacks (deploy, update oder das Wartungsfenster) kann den Container wieder starten.",
		textWhatCtRestart:  "Stoppt den Container wie stop, startet ihn wieder und wartet bis zu eine Minute, bis er läuft und, falls er einen Health-Check hat, gesund ist. Was er bereitstellt, ist solange nicht erreichbar.",
		textCtStateBefore:  "Zustand: %s",
		textCtStateAfter:   "Zustand danach: %s",
		textDoneCtStart:    "gestartet",
		textDoneCtStop:     "gestoppt",
		textDoneCtRestart:  "neu gestartet",
		textDoneCtNotReady: "%s, aber der Container ist nicht bereit: %s",
		textSummaryNoteCt:  "Das Ergebnis für den Container.",

		textCtNotReadyExited:     "er wurde beendet",
		textCtNotReadyRestarting: "er startet dauernd neu",
		textCtNotReadyNotRunning: "er läuft nicht",
		textCtNotReadyUnhealthy:  "sein Health-Check schlägt fehl",
		textCtNotReadyStarting:   "sein Health-Check ist noch nicht bestanden",

		textErrCtGone:        "diesen Container gibt es nicht mehr",
		textErrCtUnreachable: "die Container-Engine ist nicht erreichbar; starte sie und versuche es erneut",
		textErrCtPermission:  "der Dienst darf die Container-Engine nicht nutzen; gib seinem Benutzer Zugriff auf den Socket der Engine",
		textErrCtEngineSlow:  "die Container-Engine hat nicht rechtzeitig geantwortet; prüfe den Zustand des Containers, bevor du es erneut versuchst",
		textErrCtSlow:        "der Dienst hat nicht rechtzeitig geantwortet; prüfe den Zustand des Containers, bevor du es erneut versuchst",

		textCtLogStack:    "alle Container",
		textErrCtLogGone:  "diesen Container gibt es nicht mehr; verlasse das Log mit {back}",
		textErrCtLogStack: "der Dienst kennt diesen Stack nicht; verlasse das Log mit {back}",
		textErrCtLogSlow:  "der Dienst hat nicht rechtzeitig geantwortet; drücke {reload}, um es erneut zu versuchen",
	}
}
