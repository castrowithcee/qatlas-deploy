package tui

import kittui "github.com/castrowithcee/qatlas-kit/tui"

// The texts of the backup and the compose files forms.
const (
	textKeyBackupForm            kittui.TextID = "key.backupform"
	textKeyComposeForm           kittui.TextID = "key.composeform"
	textKeyMoveUp                kittui.TextID = "key.moveup"
	textKeyMoveDown              kittui.TextID = "key.movedown"
	textTitleBackup              kittui.TextID = "title.backup"
	textTitleCompose             kittui.TextID = "title.compose"
	textTitleBackupPaths         kittui.TextID = "title.backuppaths"
	textTitleBackupCommand       kittui.TextID = "title.backupcommand"
	textTitleComposeFiles        kittui.TextID = "title.composefiles"
	textBackupIntro              kittui.TextID = "backup.intro"
	textComposeIntro             kittui.TextID = "compose.intro"
	textFieldBackupPaths         kittui.TextID = "field.backuppaths"
	textHintBackupPaths          kittui.TextID = "hint.backuppaths"
	textFieldBackupVolumes       kittui.TextID = "field.backupvolumes"
	textHintBackupVolumes        kittui.TextID = "hint.backupvolumes"
	textHintBackupVolumesMissing kittui.TextID = "hint.backupvolumesmissing"
	textVolumesNone              kittui.TextID = "volumes.none"
	textFieldBackupCommand       kittui.TextID = "field.backupcommand"
	textHintBackupCommand        kittui.TextID = "hint.backupcommand"
	textFieldKeep                kittui.TextID = "field.keep"
	textHintKeep                 kittui.TextID = "hint.keep"
	textFieldMaxAge              kittui.TextID = "field.maxage"
	textHintMaxAge               kittui.TextID = "hint.maxage"
	textFieldComposeFiles        kittui.TextID = "field.composefiles"
	textHintComposeFiles         kittui.TextID = "hint.composefiles"
	textColPath                  kittui.TextID = "col.path"
	textColArgument              kittui.TextID = "col.argument"
	textColFile                  kittui.TextID = "col.file"
	textHintPath                 kittui.TextID = "hint.path"
	textHintArgument             kittui.TextID = "hint.argument"
	textHintFile                 kittui.TextID = "hint.file"
	textPathsIntro               kittui.TextID = "paths.intro"
	textPathsEmpty               kittui.TextID = "paths.empty"
	textCommandIntro             kittui.TextID = "command.intro"
	textCommandEmpty             kittui.TextID = "command.empty"
	textComposeFilesIntro        kittui.TextID = "composefiles.intro"
	textComposeFilesEmpty        kittui.TextID = "composefiles.empty"
	textProblemPath              kittui.TextID = "problem.path"
	textProblemComposeDash       kittui.TextID = "problem.composedash"
	textProblemComposeMax        kittui.TextID = "problem.composemax"
	textProblemBackupNeeded      kittui.TextID = "problem.backupneeded"
	textProblemVolume            kittui.TextID = "problem.volume"
	textProblemCommand           kittui.TextID = "problem.command"
	textProblemKeep              kittui.TextID = "problem.keep"
	textProblemMaxAge            kittui.TextID = "problem.maxage"
	textAskBackup                kittui.TextID = "ask.backup"
	textAskCompose               kittui.TextID = "ask.compose"
	textWhatSettingsLocal        kittui.TextID = "what.settings.local"
	textWhatSettingsRemote       kittui.TextID = "what.settings.remote"
	textWhatSettingsUnknown      kittui.TextID = "what.settings.unknown"
	textWhatBackupAfter          kittui.TextID = "what.backupafter"
	textWhatComposeAfter         kittui.TextID = "what.composeafter"
	textDetailBackupPaths        kittui.TextID = "detail.backuppaths"
	textDetailBackupVolumes      kittui.TextID = "detail.backupvolumes"
	textDetailBackupCommand      kittui.TextID = "detail.backupcommand"
	textDetailKeep               kittui.TextID = "detail.keep"
	textDetailKeepDefault        kittui.TextID = "detail.keepdefault"
	textDetailMaxAge             kittui.TextID = "detail.maxage"
	textDetailComposeFile        kittui.TextID = "detail.composefile"
	textDetailComposeDefault     kittui.TextID = "detail.composedefault"
	textDoneBackup               kittui.TextID = "done.backup"
	textDoneBackupSame           kittui.TextID = "done.backupsame"
	textDoneCompose              kittui.TextID = "done.compose"
	textDoneComposeSame          kittui.TextID = "done.composesame"
)

func backupEnglish() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textKeyBackupForm:            "backup",
		textKeyComposeForm:           "compose",
		textKeyMoveUp:                "move up",
		textKeyMoveDown:              "move down",
		textTitleBackup:              "Backup of a stack",
		textTitleCompose:             "Compose files of a stack",
		textTitleBackupPaths:         "Data directories",
		textTitleBackupCommand:       "Backup command",
		textTitleComposeFiles:        "Compose files",
		textBackupIntro:              "Backup of the stack %s. Update rules, check and compose files stay as they are.",
		textComposeIntro:             "Compose files of the stack %s. Update rules, backup and check stay as they are.",
		textFieldBackupPaths:         "Data directories",
		textHintBackupPaths:          "folders below the stack folder that are saved",
		textFieldBackupVolumes:       "Volumes",
		textHintBackupVolumes:        "named volumes of the compose files; space ticks",
		textHintBackupVolumesMissing: "(!) is saved but not in the compose files; space ticks",
		textVolumesNone:              "none in the compose files",
		textFieldBackupCommand:       "Command",
		textHintBackupCommand:        "program and arguments, run on the host without a shell",
		textFieldKeep:                "Keep (number)",
		textHintKeep:                 "the newest backups kept, at least 1; empty keeps 7 unless an age is set",
		textFieldMaxAge:              "Maximum age (days)",
		textHintMaxAge:               "older backups are removed, at least 1; empty sets no age limit",
		textFieldComposeFiles:        "Files",
		textHintComposeFiles:         "in merge order, relative to the stack folder; empty uses the files Compose finds itself",
		textColPath:                  "Path",
		textColArgument:              "Argument",
		textColFile:                  "File",
		textHintPath:                 "relative to the stack folder, e.g. data/db",
		textHintArgument:             "one argument; the first is the program",
		textHintFile:                 "relative to the stack folder, e.g. compose.yaml",
		textPathsIntro:               "These folders are packed into every backup of the stack.",
		textPathsEmpty:               "No data directories. {add} adds one.",
		textCommandIntro:             "The program and its arguments, in order. {moveup} and {movedown} move the entry.",
		textCommandEmpty:             "No command. {add} adds the program.",
		textComposeFilesIntro:        "Compose merges the files in this order; a later file overrides an earlier one. {moveup} and {movedown} move the entry.",
		textComposeFilesEmpty:        "No files. {add} adds one.",
		textProblemPath:              "Use a clean path relative to the stack folder, without .. or a backslash.",
		textProblemComposeDash:       "A file name must not start with a dash.",
		textProblemComposeMax:        "A stack has at most %d compose files.",
		textProblemBackupNeeded:      "A backup needs at least one data directory, volume or command.",
		textProblemVolume:            "Not a volume name: letters, digits, _, . and -, starting with a letter or digit.",
		textProblemCommand:           "The program, the first argument, must not be empty.",
		textProblemKeep:              "Keep is a whole number of at least 1, or empty.",
		textProblemMaxAge:            "The maximum age is a whole number of days, at least 1, or empty.",
		textAskBackup:                "Save the backup of the stack %s?",
		textAskCompose:               "Save the compose files of the stack %s?",
		textWhatSettingsLocal:        "A commit with the new settings is created in the local stack folder.",
		textWhatSettingsRemote:       "A commit with the new settings is created in the stack repository and pushed to its remote.",
		textWhatSettingsUnknown:      "A commit with the new settings is created in the stack repository.",
		textWhatBackupAfter:          "The backup acts on the next update.",
		textWhatComposeAfter:         "The files act on the next deployment.",
		textDetailBackupPaths:        "data directories: %s",
		textDetailBackupVolumes:      "volumes: %s",
		textDetailBackupCommand:      "command: %s",
		textDetailKeep:               "keep the newest %d",
		textDetailKeepDefault:        "keep the newest %d (default)",
		textDetailMaxAge:             "remove after %d days",
		textDetailComposeFile:        "%d. %s",
		textDetailComposeDefault:     "no list: the files Compose finds itself",
		textDoneBackup:               "Backup of the stack %s saved.",
		textDoneBackupSame:           "The backup of the stack %s was already set; nothing was committed.",
		textDoneCompose:              "Compose files of the stack %s saved.",
		textDoneComposeSame:          "The compose files of the stack %s were already set; nothing was committed.",
	}
}

func backupGerman() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textKeyBackupForm:            "Backup",
		textKeyComposeForm:           "Compose",
		textKeyMoveUp:                "nach oben",
		textKeyMoveDown:              "nach unten",
		textTitleBackup:              "Backup eines Stacks",
		textTitleCompose:             "Compose-Dateien eines Stacks",
		textTitleBackupPaths:         "Datenordner",
		textTitleBackupCommand:       "Backup-Befehl",
		textTitleComposeFiles:        "Compose-Dateien",
		textBackupIntro:              "Backup des Stacks %s. Update-Regeln, Prüfung und Compose-Dateien bleiben unverändert.",
		textComposeIntro:             "Compose-Dateien des Stacks %s. Update-Regeln, Backup und Prüfung bleiben unverändert.",
		textFieldBackupPaths:         "Datenordner",
		textHintBackupPaths:          "Ordner unterhalb des Stack-Ordners, die gesichert werden",
		textFieldBackupVolumes:       "Volumes",
		textHintBackupVolumes:        "benannte Volumes der Compose-Dateien; Leertaste hakt ab",
		textHintBackupVolumesMissing: "(!) ist gespeichert, steht aber nicht in den Compose-Dateien; Leertaste hakt ab",
		textVolumesNone:              "keine in den Compose-Dateien",
		textFieldBackupCommand:       "Befehl",
		textHintBackupCommand:        "Programm und Argumente, auf dem Host ohne Shell ausgeführt",
		textFieldKeep:                "Aufbewahren (Anzahl)",
		textHintKeep:                 "so viele neueste Backups bleiben, mindestens 1; leer behält 7, wenn kein Alter gesetzt ist",
		textFieldMaxAge:              "Höchstalter (Tage)",
		textHintMaxAge:               "ältere Backups werden entfernt, mindestens 1; leer setzt keine Altersgrenze",
		textFieldComposeFiles:        "Dateien",
		textHintComposeFiles:         "in Merge-Reihenfolge, relativ zum Stack-Ordner; leer nutzt die Dateien, die Compose selbst findet",
		textColPath:                  "Pfad",
		textColArgument:              "Argument",
		textColFile:                  "Datei",
		textHintPath:                 "relativ zum Stack-Ordner, z. B. data/db",
		textHintArgument:             "ein Argument; das erste ist das Programm",
		textHintFile:                 "relativ zum Stack-Ordner, z. B. compose.yaml",
		textPathsIntro:               "Diese Ordner werden in jedes Backup des Stacks gepackt.",
		textPathsEmpty:               "Keine Datenordner. {add} fügt einen hinzu.",
		textCommandIntro:             "Das Programm und seine Argumente, der Reihe nach. {moveup} und {movedown} verschieben den Eintrag.",
		textCommandEmpty:             "Kein Befehl. {add} fügt das Programm hinzu.",
		textComposeFilesIntro:        "Compose führt die Dateien in dieser Reihenfolge zusammen; eine spätere überschreibt eine frühere. {moveup} und {movedown} verschieben den Eintrag.",
		textComposeFilesEmpty:        "Keine Dateien. {add} fügt eine hinzu.",
		textProblemPath:              "Nutze einen sauberen Pfad relativ zum Stack-Ordner, ohne .. und ohne Backslash.",
		textProblemComposeDash:       "Ein Dateiname darf nicht mit einem Bindestrich beginnen.",
		textProblemComposeMax:        "Ein Stack hat höchstens %d Compose-Dateien.",
		textProblemBackupNeeded:      "Ein Backup braucht mindestens einen Datenordner, ein Volume oder einen Befehl.",
		textProblemVolume:            "Kein Volume-Name: Buchstaben, Ziffern, _, . und -, beginnend mit Buchstabe oder Ziffer.",
		textProblemCommand:           "Das Programm, das erste Argument, darf nicht leer sein.",
		textProblemKeep:              "Die Anzahl ist eine ganze Zahl ab 1 oder leer.",
		textProblemMaxAge:            "Das Höchstalter ist eine ganze Zahl von Tagen ab 1 oder leer.",
		textAskBackup:                "Das Backup des Stacks %s speichern?",
		textAskCompose:               "Die Compose-Dateien des Stacks %s speichern?",
		textWhatSettingsLocal:        "Im lokalen Stack-Ordner entsteht ein Commit mit den neuen Einstellungen.",
		textWhatSettingsRemote:       "Im Stack-Repository entsteht ein Commit mit den neuen Einstellungen und wird zum Remote gepusht.",
		textWhatSettingsUnknown:      "Im Stack-Repository entsteht ein Commit mit den neuen Einstellungen.",
		textWhatBackupAfter:          "Das Backup wirkt beim nächsten Update.",
		textWhatComposeAfter:         "Die Dateien wirken beim nächsten Deploy.",
		textDetailBackupPaths:        "Datenordner: %s",
		textDetailBackupVolumes:      "Volumes: %s",
		textDetailBackupCommand:      "Befehl: %s",
		textDetailKeep:               "die neuesten %d behalten",
		textDetailKeepDefault:        "die neuesten %d behalten (Standard)",
		textDetailMaxAge:             "nach %d Tagen entfernen",
		textDetailComposeFile:        "%d. %s",
		textDetailComposeDefault:     "keine Liste: die Dateien, die Compose selbst findet",
		textDoneBackup:               "Backup des Stacks %s gespeichert.",
		textDoneBackupSame:           "Das Backup des Stacks %s war schon gesetzt; es wurde nichts committet.",
		textDoneCompose:              "Compose-Dateien des Stacks %s gespeichert.",
		textDoneComposeSame:          "Die Compose-Dateien des Stacks %s waren schon gesetzt; es wurde nichts committet.",
	}
}
