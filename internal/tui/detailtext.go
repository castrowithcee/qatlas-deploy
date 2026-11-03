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
	}
}
