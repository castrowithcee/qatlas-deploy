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

	textErrDockerUnreachable kittui.TextID = "error.dockerunreachable"
	textErrDockerPermission  kittui.TextID = "error.dockerpermission"
	textErrDockerTimeout     kittui.TextID = "error.dockertimeout"
	textErrContainersSlow    kittui.TextID = "error.containersslow"

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

		textErrDockerUnreachable: "the container engine is not reachable; start it, then press {reload}",
		textErrDockerPermission:  "the service may not use the container engine; give its user access to the engine socket, then press {reload}",
		textErrDockerTimeout:     "the container engine did not answer in time; check that it is healthy, then press {reload}",
		textErrContainersSlow:    "the service did not answer in time; the next refresh tries again",

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

		textErrDockerUnreachable: "die Container-Engine ist nicht erreichbar; starte sie und drücke dann {reload}",
		textErrDockerPermission:  "der Dienst darf die Container-Engine nicht nutzen; gib seinem Benutzer Zugriff auf den Socket der Engine und drücke dann {reload}",
		textErrDockerTimeout:     "die Container-Engine hat nicht rechtzeitig geantwortet; prüfe ihren Zustand und drücke dann {reload}",
		textErrContainersSlow:    "der Dienst hat nicht rechtzeitig geantwortet; die nächste Aktualisierung versucht es erneut",

		textCtLogStack:    "alle Container",
		textErrCtLogGone:  "diesen Container gibt es nicht mehr; verlasse das Log mit {back}",
		textErrCtLogStack: "der Dienst kennt diesen Stack nicht; verlasse das Log mit {back}",
		textErrCtLogSlow:  "der Dienst hat nicht rechtzeitig geantwortet; drücke {reload}, um es erneut zu versuchen",
	}
}
