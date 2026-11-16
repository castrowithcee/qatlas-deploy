package tui

import kittui "github.com/castrowithcee/qatlas-kit/tui"

// The texts of giving a stack folder a remote. No text names a secret value or key material.
const (
	textKeyRemote           kittui.TextID = "key.remote"
	textAnswerWithRemote    kittui.TextID = "answer.withremote"
	textAnswerPush          kittui.TextID = "answer.push"
	textTitleRemote         kittui.TextID = "title.remote"
	textHintRemoteURL       kittui.TextID = "hint.remoteurl"
	textFolderHasRemote     kittui.TextID = "folder.hasremote"
	textRemoteNoKey         kittui.TextID = "remote.nokey"
	textRemoteNoKeyNext     kittui.TextID = "remote.nokeynext"
	textDetailKeyWrite      kittui.TextID = "detail.keywrite"
	textAskRemoteEmpty      kittui.TextID = "ask.remoteempty"
	textWhatRemoteEmpty     kittui.TextID = "what.remoteempty"
	textAskRemoteEqual      kittui.TextID = "ask.remoteequal"
	textWhatRemoteEqual     kittui.TextID = "what.remoteequal"
	textAskRemoteDifferent  kittui.TextID = "ask.remotedifferent"
	textWhatRemoteDifferent kittui.TextID = "what.remotedifferent"
	textAskRemoteSecrets    kittui.TextID = "ask.remotesecrets"
	textWhatRemoteSecrets   kittui.TextID = "what.remotesecrets"
	textAskRemoteNoCommit   kittui.TextID = "ask.remotenocommit"
	textWhatRemoteNoCommit  kittui.TextID = "what.remotenocommit"
	textDetailBranch        kittui.TextID = "detail.branch"
	textHeadPaths           kittui.TextID = "head.paths"
	textMorePaths           kittui.TextID = "more.paths"
	textHeadSecrets         kittui.TextID = "head.secrets"
	textSecretEntry         kittui.TextID = "secret.entry"
	textDoneRemote          kittui.TextID = "done.remote"
	textDetailPushed        kittui.TextID = "detail.pushed"
	textDetailState         kittui.TextID = "detail.state"
)

func remoteEnglish() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textKeyRemote:           "add remote",
		textAnswerWithRemote:    "with a remote",
		textAnswerPush:          "put and push",
		textTitleRemote:         "Add a remote",
		textHintRemoteURL:       "address of the repository for the marked stack folder; it is compared before anything changes",
		textFolderHasRemote:     "This stack folder runs against a remote already.",
		textRemoteNoKey:         "%s has no deploy key yet.",
		textRemoteNoKeyNext:     "Store or generate it in this view and enter the public key on the git platform with write access, then add the remote again. Nothing was changed.",
		textDetailKeyWrite:      "The public key of the deploy key needs write access to the repository.",
		textAskRemoteEmpty:      "Push the history of %s to %s (branch %s)?",
		textWhatRemoteEmpty:     "The repository has no commit yet. Yes pushes the history of the folder to it, and the folder then runs against the remote.",
		textAskRemoteEqual:      "Take over the history of %s from %s?",
		textWhatRemoteEqual:     "The repository holds the same files as the folder. Yes makes the folder follow it; the local history is kept in the repository and nothing is pushed.",
		textAskRemoteDifferent:  "Put the state of the host in %s as one commit on branch %s of %s and push it?",
		textWhatRemoteDifferent: "The files differ. Yes adds one commit with the files of the host on top of the remote history and pushes it; the remote history is kept and never overwritten.",
		textAskRemoteSecrets:    "The commits of %s to push to %s hold %d secret-like files.",
		textWhatRemoteSecrets:   "Nothing was changed and nothing is pushed. There is no way around this: remove the files from the history of the folder yourself, then add the remote again.",
		textAskRemoteNoCommit:   "The repository at %s has no commit; %s cannot be taken over with it.",
		textWhatRemoteNoCommit:  "Take the folder over without an address first, then add the remote on its row. Nothing was changed.",
		textDetailBranch:        "branch: %s",
		textHeadPaths:           "Paths that differ (%d)",
		textMorePaths:           "... and %d more",
		textHeadSecrets:         "Secret-like files (%d)",
		textSecretEntry:         "commit %s: %s (%s)",
		textDoneRemote:          "%s runs against %s now.",
		textDetailPushed:        "pushed: %s",
		textDetailState:         "remote: %s",
	}
}

func remoteGerman() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textKeyRemote:           "Remote hinzufügen",
		textAnswerWithRemote:    "mit Remote",
		textAnswerPush:          "setzen und pushen",
		textTitleRemote:         "Remote hinzufügen",
		textHintRemoteURL:       "Adresse des Repositorys für den markierten Stack-Ordner; sie wird verglichen, bevor sich etwas ändert",
		textFolderHasRemote:     "Dieser Stack-Ordner läuft schon gegen ein Remote.",
		textRemoteNoKey:         "%s hat noch keinen Deploy-Schlüssel.",
		textRemoteNoKeyNext:     "Hinterlege oder erzeuge ihn in dieser Ansicht und trage den öffentlichen Schlüssel mit Schreibrecht bei der Git-Plattform ein, dann füge das Remote erneut hinzu. Es wurde nichts geändert.",
		textDetailKeyWrite:      "Der öffentliche Teil des Deploy-Schlüssels braucht Schreibrecht auf das Repository.",
		textAskRemoteEmpty:      "Die Historie von %s nach %s (Branch %s) pushen?",
		textWhatRemoteEmpty:     "Das Repository hat noch keinen Commit. Ja pusht die Historie des Ordners dorthin, danach läuft der Ordner gegen das Remote.",
		textAskRemoteEqual:      "Die Historie von %s aus %s übernehmen?",
		textWhatRemoteEqual:     "Das Repository enthält dieselben Dateien wie der Ordner. Ja lässt den Ordner ihm folgen; die lokale Historie bleibt im Repository erhalten, gepusht wird nichts.",
		textAskRemoteDifferent:  "Den Stand des Hosts in %s als einen Commit auf Branch %s von %s setzen und pushen?",
		textWhatRemoteDifferent: "Die Dateien unterscheiden sich. Ja fügt einen Commit mit den Dateien des Hosts auf die Historie des Remotes und pusht ihn; die Historie des Remotes bleibt erhalten und wird nie überschrieben.",
		textAskRemoteSecrets:    "Die zu pushenden Commits von %s nach %s enthalten %d geheimnisartige Dateien.",
		textWhatRemoteSecrets:   "Es wurde nichts geändert und nichts gepusht. Das lässt sich nicht übersteuern: Entferne die Dateien selbst aus der Historie des Ordners, dann füge das Remote erneut hinzu.",
		textAskRemoteNoCommit:   "Das Repository unter %s hat keinen Commit; %s lässt sich damit nicht übernehmen.",
		textWhatRemoteNoCommit:  "Übernimm den Ordner zuerst ohne Adresse und füge das Remote dann auf seiner Zeile hinzu. Es wurde nichts geändert.",
		textDetailBranch:        "Branch: %s",
		textHeadPaths:           "Abweichende Pfade (%d)",
		textMorePaths:           "... und %d weitere",
		textHeadSecrets:         "Geheimnisartige Dateien (%d)",
		textSecretEntry:         "Commit %s: %s (%s)",
		textDoneRemote:          "%s läuft jetzt gegen %s.",
		textDetailPushed:        "gepusht: %s",
		textDetailState:         "Remote: %s",
	}
}
