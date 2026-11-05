package tui

import (
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The texts of the stack check in the stack detail. Reasons, actions and findings come from the service in
// English.
const (
	textDetailCapabilities kittui.TextID = "detail.capabilities"
	textCapNotChecked      kittui.TextID = "cap.notchecked"
	textCapWorks           kittui.TextID = "cap.works"
	textCapNotPossible     kittui.TextID = "cap.notpossible"
	textCapAction          kittui.TextID = "cap.action"
	textCapDeploy          kittui.TextID = "cap.deploy"
	textCapUpdate          kittui.TextID = "cap.update"
	textCapRollback        kittui.TextID = "cap.rollback"
	textCapBackup          kittui.TextID = "cap.backup"
	textCapRedact          kittui.TextID = "cap.redact"
	textCapError           kittui.TextID = "cap.error"
	textCapWarning         kittui.TextID = "cap.warning"
)

var capabilityTexts = map[string]kittui.TextID{
	"deploy": textCapDeploy, "update": textCapUpdate, "rollback": textCapRollback, "backup": textCapBackup, "redact": textCapRedact,
}

func capabilitiesEnglish() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textDetailCapabilities: "Functions",
		textCapNotChecked:      "Not checked.",
		textCapWorks:           "works",
		textCapNotPossible:     "not possible",
		textCapAction:          "To do: %s",
		textCapDeploy:          "Deploy",
		textCapUpdate:          "Update",
		textCapRollback:        "Rollback with data",
		textCapBackup:          "Backup",
		textCapRedact:          "Redact secrets",
		textCapError:           "error",
		textCapWarning:         "warning",
	}
}

func capabilitiesGerman() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textDetailCapabilities: "Funktionen",
		textCapNotChecked:      "Nicht geprüft.",
		textCapWorks:           "geht",
		textCapNotPossible:     "geht nicht",
		textCapAction:          "Zu tun: %s",
		textCapDeploy:          "Deploy",
		textCapUpdate:          "Update",
		textCapRollback:        "Rollback mit Daten",
		textCapBackup:          "Backup",
		textCapRedact:          "Geheimnisse schwärzen",
		textCapError:           "Fehler",
		textCapWarning:         "Warnung",
	}
}

// capabilityLines are the functions of the stack's check, each with whether it works, why, and what to do,
// and then its findings.
func (m *Model) capabilityLines(stack string, width int) []string {
	t := m.text
	var rep *control.StackReport
	for i := range m.snap.check.Stacks {
		if m.snap.check.Stacks[i].Stack == stack {
			rep = &m.snap.check.Stacks[i]
		}
	}
	if rep == nil {
		return []string{m.line(detailIndent, kittui.StyleHint, t.Lookup(textCapNotChecked), width)}
	}
	var out []string
	action := func(text string) {
		if text != "" {
			out = append(out, m.wrapped(detailIndent+2, kittui.StyleText, t.Lookup(textCapAction, text), width)...)
		}
	}
	for _, f := range rep.Functions {
		name := f.Function
		if id, ok := capabilityTexts[name]; ok {
			name = t.Lookup(id)
		}
		word, tone := t.Lookup(textCapNotPossible), kittui.StyleWarn
		if f.Works {
			word, tone = t.Lookup(textCapWorks), kittui.StyleOK
		}
		if id, ok := modeTexts[f.Mode]; ok {
			word += ", " + t.Lookup(id)
		}
		out = append(out, m.pair(detailIndent, name, word, tone, width))
		out = append(out, m.wrapped(detailIndent+2, kittui.StyleHint, f.Reason, width)...)
		action(f.Action)
	}
	for _, f := range rep.Findings {
		word, tone := t.Lookup(textCapWarning), kittui.StyleWarn
		if f.Severity == "error" {
			word, tone = t.Lookup(textCapError), kittui.StyleFail
		}
		out = append(out, m.pair(detailIndent, f.Rule, word, tone, width))
		out = append(out, m.wrapped(detailIndent+2, kittui.StyleHint, f.Cause, width)...)
		action(f.Fix)
	}
	return out
}
