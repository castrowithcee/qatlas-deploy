package tui

import kittui "github.com/castrowithcee/qatlas-kit/tui"

// The texts of the changes in the stack detail. A secret's value is never part of a text.
const (
	textDetailSecrets        kittui.TextID = "detail.secrets"
	textSecretsNone          kittui.TextID = "secrets.none"
	textSecretsNoneToDelete  kittui.TextID = "secrets.nonetodelete"
	textBackupsNoneToRestore kittui.TextID = "backups.nonetorestore"
	textKeySecretSet         kittui.TextID = "key.secretset"
	textKeySecretDelete      kittui.TextID = "key.secretdelete"
	textKeyRestore           kittui.TextID = "key.restore"
	textKeyPick              kittui.TextID = "key.pick"
	textColBackup            kittui.TextID = "col.backup"
	textColCreated           kittui.TextID = "col.created"
	textColSize              kittui.TextID = "col.size"
	textTitleSecret          kittui.TextID = "title.secret"
	textTitleSecretDelete    kittui.TextID = "title.secretdelete"
	textTitleRestore         kittui.TextID = "title.restore"
	textFieldSecretKey       kittui.TextID = "field.secretkey"
	textHintSecretKey        kittui.TextID = "hint.secretkey"
	textFieldSecretValue     kittui.TextID = "field.secretvalue"
	textHintSecretValue      kittui.TextID = "hint.secretvalue"
	textProblemSecretKey     kittui.TextID = "problem.secretkey"
	textAskSecretSet         kittui.TextID = "ask.secretset"
	textWhatSecretSet        kittui.TextID = "what.secretset"
	textAskSecretDelete      kittui.TextID = "ask.secretdelete"
	textWhatSecretDelete     kittui.TextID = "what.secretdelete"
	textAskRestore           kittui.TextID = "ask.restore"
	textWhatRestore          kittui.TextID = "what.restore"
	textDoneSecretSet        kittui.TextID = "done.secretset"
	textDoneSecretReplaced   kittui.TextID = "done.secretreplaced"
	textDoneSecretDeleted    kittui.TextID = "done.secretdeleted"
	textSecretAppliesLater   kittui.TextID = "secret.applieslater"
	textDoneRestored         kittui.TextID = "done.restored"
	textDetailDataDirs       kittui.TextID = "detail.datadirs"
	textDataDirLine          kittui.TextID = "datadirs.line"
	textDataDirLineNoOwner   kittui.TextID = "datadirs.linenoowner"
	textDataDirsHint         kittui.TextID = "datadirs.hint"
	textDataDirsIntro        kittui.TextID = "datadirs.intro"
	textKeyDataDirs          kittui.TextID = "key.datadirs"
	textTitleDataDirs        kittui.TextID = "title.datadirs"
	textFieldDataOwner       kittui.TextID = "field.dataowner"
	textHintDataOwner        kittui.TextID = "hint.dataowner"
	textFieldDataMode        kittui.TextID = "field.datamode"
	textHintDataMode         kittui.TextID = "hint.datamode"
	textProblemDataOwner     kittui.TextID = "problem.dataowner"
	textProblemDataMode      kittui.TextID = "problem.datamode"
	textDataDirsNone         kittui.TextID = "datadirs.none"
	textDataDirsNeedRoot     kittui.TextID = "datadirs.needroot"
	textDataDirsNoFolder     kittui.TextID = "datadirs.nofolder"
	textAskDataDirs          kittui.TextID = "ask.datadirs"
	textWhatDataDirs         kittui.TextID = "what.datadirs"
	textDataDirsFolder       kittui.TextID = "datadirs.folder"
	textDataDirsOwner        kittui.TextID = "datadirs.owner"
	textDataDirsMode         kittui.TextID = "datadirs.mode"
	textDoneDataDirs         kittui.TextID = "done.datadirs"
	textDataDirsCreated      kittui.TextID = "datadirs.created"
	textDataDirsParents      kittui.TextID = "datadirs.parents"
	textDataDirsExisting     kittui.TextID = "datadirs.existing"
	textDataDirsDeployFailed kittui.TextID = "datadirs.deployfailed"
	textErrDataDirs          kittui.TextID = "err.datadirs"
	textDetailStopped        kittui.TextID = "detail.stopped"
	textStoppedSince         kittui.TextID = "stopped.since"
	textStoppedHint          kittui.TextID = "stopped.hint"
)

func detailEnglish() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textDetailSecrets:        "Secrets",
		textSecretsNone:          "No secrets.",
		textSecretsNoneToDelete:  "This stack has no secrets to delete.",
		textBackupsNoneToRestore: "There is no older backup to restore.",
		textKeySecretSet:         "set secret",
		textKeySecretDelete:      "delete secret",
		textKeyRestore:           "restore backup",
		textKeyPick:              "choose",
		textColBackup:            "backup",
		textColCreated:           "created",
		textColSize:              "size",
		textTitleSecret:          "Set a secret",
		textTitleSecretDelete:    "Delete a secret",
		textTitleRestore:         "Restore a backup",
		textFieldSecretKey:       "Name",
		textHintSecretKey:        "letters, digits, _ . -; starts with a letter or _",
		textFieldSecretValue:     "Value",
		textHintSecretValue:      "hidden; 4 to 8192 characters",
		textProblemSecretKey:     "A name has up to 128 characters: letters, digits, _ . -, starting with a letter or _.",
		textAskSecretSet:         "Set the secret %s of the stack %s?",
		textWhatSecretSet:        "The service stores the value in the stack's secrets file and never shows it again. A secret of that name is replaced. The value applies at the next deployment.",
		textAskSecretDelete:      "Delete the secret %s of the stack %s?",
		textWhatSecretDelete:     "The value cannot be recovered. The change applies at the next deployment.",
		textAskRestore:           "Restore the backup %s of the stack %s?",
		textWhatRestore:          "The stack is stopped and its current data is overwritten with the backup. The stack is started again afterwards. This cannot be undone.",
		textDoneSecretSet:        "Secret %s set for the stack %s.",
		textDoneSecretReplaced:   "Secret %s of the stack %s replaced.",
		textDoneSecretDeleted:    "Secret %s of the stack %s deleted.",
		textSecretAppliesLater:   "The change applies at the next deployment.",
		textDoneRestored:         "Backup %s restored for the stack %s.",
		textDetailDataDirs:       "Missing data directories",
		textDataDirLine:          "%s  owner %s",
		textDataDirLineNoOwner:   "%s  owner unknown",
		textDataDirsHint:         "The stack waits until these directories exist. {datadirs} creates them with an owner and mode you confirm, then deploys the stack.",
		textDetailStopped:        "Stopped",
		textStoppedSince:         "Stopped since %s",
		textStoppedHint:          "The stack stays stopped: commits, the maintenance window and updates leave it out. {stackstart} starts it and lifts the mark.",
		textDataDirsIntro:        "Missing below %s:",
		textKeyDataDirs:          "create data directories",
		textTitleDataDirs:        "Create data directories",
		textFieldDataOwner:       "Owner",
		textHintDataOwner:        "UID:GID of the user the container runs as, for example 1000:1000",
		textFieldDataMode:        "Mode",
		textHintDataMode:         "octal permission bits, for example 0750",
		textProblemDataOwner:     "The owner must be UID:GID with decimal numbers, for example 1000:1000.",
		textProblemDataMode:      "The mode must be octal permission bits such as 0750; setuid, setgid and sticky are not allowed.",
		textDataDirsNone:         "This stack has no missing data directories.",
		textDataDirsNeedRoot:     "Creating data directories needs root; start the interface with sudo.",
		textDataDirsNoFolder:     "The stacks folder is not known yet; reload and try again.",
		textAskDataDirs:          "Create the missing data directories of the stack %s and deploy it?",
		textWhatDataDirs:         "Only the directories below are created, with this owner and mode. Existing directories stay as they are. Then a run of the stack is started.",
		textDataDirsFolder:       "stack folder: %s",
		textDataDirsOwner:        "owner: %s",
		textDataDirsMode:         "mode: %s",
		textDoneDataDirs:         "Data directories created for the stack %s.",
		textDataDirsCreated:      "created: %s",
		textDataDirsParents:      "parent directories: %s",
		textDataDirsExisting:     "already existing: %s",
		textDataDirsDeployFailed: "The run could not be started: %s",
		textErrDataDirs:          "Cannot create the data directories: %s",
	}
}

func detailGerman() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textDetailSecrets:        "Geheimnisse",
		textSecretsNone:          "Keine Geheimnisse.",
		textSecretsNoneToDelete:  "Dieser Stack hat keine Geheimnisse zum Löschen.",
		textBackupsNoneToRestore: "Es gibt kein älteres Backup zum Wiederherstellen.",
		textKeySecretSet:         "Geheimnis setzen",
		textKeySecretDelete:      "Geheimnis löschen",
		textKeyRestore:           "Backup einspielen",
		textKeyPick:              "wählen",
		textColBackup:            "Backup",
		textColCreated:           "erstellt",
		textColSize:              "Größe",
		textTitleSecret:          "Geheimnis setzen",
		textTitleSecretDelete:    "Geheimnis löschen",
		textTitleRestore:         "Backup einspielen",
		textFieldSecretKey:       "Name",
		textHintSecretKey:        "Buchstaben, Ziffern, _ . -; beginnt mit Buchstabe oder _",
		textFieldSecretValue:     "Wert",
		textHintSecretValue:      "verborgen; 4 bis 8192 Zeichen",
		textProblemSecretKey:     "Ein Name hat bis zu 128 Zeichen: Buchstaben, Ziffern, _ . -, beginnend mit Buchstabe oder _.",
		textAskSecretSet:         "Das Geheimnis %s des Stacks %s setzen?",
		textWhatSecretSet:        "Der Dienst speichert den Wert in der Secrets-Datei des Stacks und zeigt ihn nie wieder. Ein Geheimnis gleichen Namens wird ersetzt. Der Wert gilt ab dem nächsten Deployment.",
		textAskSecretDelete:      "Das Geheimnis %s des Stacks %s löschen?",
		textWhatSecretDelete:     "Der Wert ist nicht wiederherstellbar. Die Änderung gilt ab dem nächsten Deployment.",
		textAskRestore:           "Das Backup %s des Stacks %s einspielen?",
		textWhatRestore:          "Der Stack wird gestoppt und seine aktuellen Daten werden mit dem Backup überschrieben. Danach wird der Stack wieder gestartet. Das lässt sich nicht rückgängig machen.",
		textDoneSecretSet:        "Geheimnis %s für den Stack %s gesetzt.",
		textDoneSecretReplaced:   "Geheimnis %s des Stacks %s ersetzt.",
		textDoneSecretDeleted:    "Geheimnis %s des Stacks %s gelöscht.",
		textSecretAppliesLater:   "Die Änderung gilt ab dem nächsten Deployment.",
		textDoneRestored:         "Backup %s für den Stack %s eingespielt.",
		textDetailDataDirs:       "Fehlende Datenordner",
		textDataDirLine:          "%s  Besitzer %s",
		textDataDirLineNoOwner:   "%s  Besitzer unbekannt",
		textDataDirsHint:         "Der Stack wartet, bis diese Ordner existieren. {datadirs} legt sie mit dem bestätigten Besitzer und Modus an und rollt den Stack danach aus.",
		textDetailStopped:        "Gestoppt",
		textStoppedSince:         "Gestoppt seit %s",
		textStoppedHint:          "Der Stack bleibt gestoppt: Commits, das Wartungsfenster und Updates lassen ihn aus. {stackstart} startet ihn und hebt die Markierung auf.",
		textDataDirsIntro:        "Fehlend unter %s:",
		textKeyDataDirs:          "Datenordner anlegen",
		textTitleDataDirs:        "Datenordner anlegen",
		textFieldDataOwner:       "Besitzer",
		textHintDataOwner:        "UID:GID des Benutzers, als der der Container läuft, z. B. 1000:1000",
		textFieldDataMode:        "Modus",
		textHintDataMode:         "oktale Zugriffsrechte, z. B. 0750",
		textProblemDataOwner:     "Der Besitzer muss UID:GID mit Dezimalzahlen sein, z. B. 1000:1000.",
		textProblemDataMode:      "Der Modus muss aus oktalen Zugriffsrechten wie 0750 bestehen; setuid, setgid und Sticky-Bit sind nicht erlaubt.",
		textDataDirsNone:         "Diesem Stack fehlen keine Datenordner.",
		textDataDirsNeedRoot:     "Datenordner anzulegen braucht root; die Oberfläche muss mit sudo gestartet werden.",
		textDataDirsNoFolder:     "Der Stacks-Ordner ist noch nicht bekannt; neu laden und erneut versuchen.",
		textAskDataDirs:          "Die fehlenden Datenordner des Stacks %s anlegen und ihn ausrollen?",
		textWhatDataDirs:         "Es werden nur die unten genannten Ordner mit diesem Besitzer und Modus angelegt. Vorhandene Ordner bleiben unverändert. Danach wird ein Lauf des Stacks gestartet.",
		textDataDirsFolder:       "Stack-Ordner: %s",
		textDataDirsOwner:        "Besitzer: %s",
		textDataDirsMode:         "Modus: %s",
		textDoneDataDirs:         "Datenordner für den Stack %s angelegt.",
		textDataDirsCreated:      "angelegt: %s",
		textDataDirsParents:      "Elternordner: %s",
		textDataDirsExisting:     "bereits vorhanden: %s",
		textDataDirsDeployFailed: "Der Lauf konnte nicht gestartet werden: %s",
		textErrDataDirs:          "Die Datenordner konnten nicht angelegt werden: %s",
	}
}
