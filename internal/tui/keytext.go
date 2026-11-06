package tui

import kittui "github.com/castrowithcee/qatlas-kit/tui"

// The texts of the deploy key form. No text names key material.
const (
	textKeySetKey        kittui.TextID = "key.setkey"
	textTitleKey         kittui.TextID = "title.key"
	textTitleKeyStore    kittui.TextID = "title.keystore"
	textFieldKeyFolder   kittui.TextID = "field.keyfolder"
	textHintKeyFolder    kittui.TextID = "hint.keyfolder"
	textFieldKeyMode     kittui.TextID = "field.keymode"
	textHintKeyMode      kittui.TextID = "hint.keymode"
	textKeyModeStore     kittui.TextID = "key.modestore"
	textKeyModeGenerate  kittui.TextID = "key.modegenerate"
	textFieldKeyBlock    kittui.TextID = "field.keyblock"
	textHintKeyBlock     kittui.TextID = "hint.keyblock"
	textFieldKeyPath     kittui.TextID = "field.keypath"
	textHintKeyPath      kittui.TextID = "hint.keypath"
	textProblemKeyNone   kittui.TextID = "problem.keynone"
	textProblemKeyBoth   kittui.TextID = "problem.keyboth"
	textProblemKeyFile   kittui.TextID = "problem.keyfile"
	textProblemKeyTooBig kittui.TextID = "problem.keytoobig"
	textDoneKeySet       kittui.TextID = "done.keyset"
	textAskKeyReplace    kittui.TextID = "ask.keyreplace"
	textWhatKeyReplace   kittui.TextID = "what.keyreplace"
	textDetailPublicKey  kittui.TextID = "detail.publickey"
	textDetailKeyFolder  kittui.TextID = "detail.keyfolder"
)

func keyEnglish() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textKeySetKey:        "set key",
		textTitleKey:         "Deploy key",
		textTitleKeyStore:    "Store the deploy key",
		textFieldKeyFolder:   "Stack folder",
		textHintKeyFolder:    "the folder the key is for; left and right choose",
		textFieldKeyMode:     "Key",
		textHintKeyMode:      "store a key you have, or let the service create a new one",
		textKeyModeStore:     "store an existing key",
		textKeyModeGenerate:  "generate a new key",
		textFieldKeyBlock:    "Private key",
		textHintKeyBlock:     "paste the OpenSSH private key without passphrase; it is never shown",
		textFieldKeyPath:     "Key file",
		textHintKeyPath:      "or the path of a key file; leave empty when you paste the key",
		textProblemKeyNone:   "Paste the private key or give the path of a key file.",
		textProblemKeyBoth:   "Give either the pasted key or a key file, not both.",
		textProblemKeyFile:   "The key file cannot be read; check the path and the permissions.",
		textProblemKeyTooBig: "The key is larger than 16 KiB.",
		textDoneKeySet:       "The deploy key is stored.",
		textAskKeyReplace:    "Replace the existing deploy key?",
		textWhatKeyReplace:   "The old key stops working at once. The git platform refuses this host until the new public key is entered there. This cannot be undone.",
		textDetailPublicKey:  "public key: %s",
		textDetailKeyFolder:  "stack folder: %s",
	}
}

func keyGerman() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textKeySetKey:        "Schlüssel setzen",
		textTitleKey:         "Deploy-Schlüssel",
		textTitleKeyStore:    "Deploy-Schlüssel hinterlegen",
		textFieldKeyFolder:   "Stack-Ordner",
		textHintKeyFolder:    "der Ordner, für den der Schlüssel gilt; links und rechts wählen",
		textFieldKeyMode:     "Schlüssel",
		textHintKeyMode:      "einen vorhandenen Schlüssel hinterlegen oder vom Dienst einen neuen erzeugen lassen",
		textKeyModeStore:     "vorhandenen Schlüssel hinterlegen",
		textKeyModeGenerate:  "neuen Schlüssel erzeugen",
		textFieldKeyBlock:    "Privater Schlüssel",
		textHintKeyBlock:     "den OpenSSH-Schlüssel ohne Passphrase einfügen; er wird nie angezeigt",
		textFieldKeyPath:     "Schlüsseldatei",
		textHintKeyPath:      "oder der Pfad einer Schlüsseldatei; leer lassen, wenn der Schlüssel eingefügt wird",
		textProblemKeyNone:   "Füge den privaten Schlüssel ein oder gib den Pfad einer Schlüsseldatei an.",
		textProblemKeyBoth:   "Gib entweder den eingefügten Schlüssel oder eine Schlüsseldatei an, nicht beides.",
		textProblemKeyFile:   "Die Schlüsseldatei lässt sich nicht lesen; prüfe Pfad und Rechte.",
		textProblemKeyTooBig: "Der Schlüssel ist größer als 16 KiB.",
		textDoneKeySet:       "Der Deploy-Schlüssel ist hinterlegt.",
		textAskKeyReplace:    "Den vorhandenen Deploy-Schlüssel ersetzen?",
		textWhatKeyReplace:   "Der alte Schlüssel funktioniert sofort nicht mehr. Die Git-Plattform weist diesen Host ab, bis der neue öffentliche Schlüssel dort eingetragen ist. Das lässt sich nicht rückgängig machen.",
		textDetailPublicKey:  "öffentlicher Schlüssel: %s",
		textDetailKeyFolder:  "Stack-Ordner: %s",
	}
}
