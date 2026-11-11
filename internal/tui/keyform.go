package tui

import (
	"errors"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

const (
	// formKeyChoice asks for the stack folder and whether to store or generate; formKeyStore asks for the key.
	formKeyChoice formKind = formDataDirs + 1
	formKeyStore  formKind = formKeyChoice + 1
	formGeneral   formKind = formKeyStore + 1
)

// maxKeyBytes is the largest private key the service takes. The terminal interface may not import the
// credential store, so the limit stands here as with the credential values.
const maxKeyBytes = 16 << 10

// The values of the mode choice.
const (
	keyModeStore    = "store"
	keyModeGenerate = "generate"
)

// keyExists is the answer of a write that found a key in the stack folder and sent nothing: replace sends it
// again with the replacement the person confirmed.
type keyExists struct {
	details []string
	replace func() tea.Cmd
}

// keySources are the stack folders the form can choose from, in the order they were bound.
func (m *Model) keySources() []control.SourceStatus { return m.snap.status.Engine.Sources }

// keyDir is the Dir to send for a chosen folder: empty for the first folder, as the commands do without --dir.
func (m *Model) keyDir(chosen string) string {
	if src := m.keySources(); len(src) == 0 || chosen == src[0].Path {
		return ""
	}
	return chosen
}

// openKeyForm asks for the key; the folder is chosen only on the page of the default key, as the page of one
// folder is for that folder alone.
func (m *Model) openKeyForm() tea.Cmd {
	var fields []kittui.FormField
	if src := m.keySources(); len(src) > 1 && m.set.keyFor == "" {
		choices := make([]kittui.Choice, len(src))
		for i, s := range src {
			// A path is its own label: the translator returns an unknown text as it is.
			choices[i] = kittui.Choice{ID: kittui.ChoiceID(s.Path), Label: kittui.TextID(s.Path)}
		}
		fields = append(fields, kittui.FormField{ID: "folder", Kind: kittui.FieldChoice, Label: textFieldKeyFolder,
			Hint: textHintKeyFolder, Choices: choices})
	}
	fields = append(fields, kittui.FormField{ID: "mode", Kind: kittui.FieldChoice, Label: textFieldKeyMode, Hint: textHintKeyMode,
		Choices: []kittui.Choice{choice(keyModeStore, textKeyModeStore), choice(keyModeGenerate, textKeyModeGenerate)}})
	return m.openForm(m.newSettingsForm(formKeyChoice, textTitleKey, fields))
}

func (m *Model) submitKeyChoice(f *settingsForm, v map[kittui.FieldID]kittui.FormValue) tea.Cmd {
	dir := m.keyDir(string(v["folder"].Choice))
	label := string(v["folder"].Choice)
	switch {
	case m.set.keyFor != "":
		dir, label = m.set.keyFor, m.set.keyFor
	case label == "":
		label = m.firstSourcePath()
	}
	if v["mode"].Choice == keyModeGenerate {
		return m.keyWrite(f, dir, label, "", true)
	}
	next := m.newSettingsForm(formKeyStore, textTitleKeyStore, []kittui.FormField{
		{ID: "key", Kind: kittui.FieldMaskedBlock, Label: textFieldKeyBlock, Hint: textHintKeyBlock},
		textField("path", textFieldKeyPath, textHintKeyPath, ""),
	})
	if next != nil {
		next.stackDir, next.keyLabel = dir, label
	}
	return m.openForm(next)
}

func (m *Model) firstSourcePath() string {
	if src := m.keySources(); len(src) > 0 {
		return src[0].Path
	}
	return ""
}

func (m *Model) submitKeyStore(f *settingsForm, v map[kittui.FieldID]kittui.FormValue) tea.Cmd {
	pasted, path := v["key"].Text, strings.TrimSpace(v["path"].Text)
	key := pasted
	switch {
	case strings.TrimSpace(pasted) == "" && path == "":
		f.problems(m.bad("key", textProblemKeyNone))
		return nil
	case strings.TrimSpace(pasted) != "" && path != "":
		f.problems(m.bad("path", textProblemKeyBoth))
		return nil
	case path != "":
		text, cause := readKeyFile(path)
		if cause != "" {
			f.problems(m.bad("path", cause))
			return nil
		}
		key = text
	}
	if len(key) > maxKeyBytes {
		f.problems(m.bad(map[bool]string{true: "path", false: "key"}[path != ""], textProblemKeyTooBig))
		return nil
	}
	return m.keyWrite(f, f.stackDir, f.keyLabel, key, false)
}

// readKeyFile reads a key file for the form; cause is the text of the problem, never anything of the file.
func readKeyFile(path string) (text string, cause kittui.TextID) {
	file, err := os.Open(path)
	if err != nil {
		return "", textProblemKeyFile
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxKeyBytes+1))
	if err != nil {
		return "", textProblemKeyFile
	}
	if len(raw) > maxKeyBytes {
		return "", textProblemKeyTooBig
	}
	return string(raw), ""
}

// keyWrite stores the key or generates one. The key of the folder is asked for first, so that the question about
// replacing it comes before anything is sent; a key that appeared in between is refused by the service and
// shown at the form.
func (m *Model) keyWrite(f *settingsForm, dir, label, key string, generate bool) tea.Cmd {
	op := opKeySet
	if generate {
		op = opKeyGenerate
	}
	send := func(b Backend, replace bool) (control.KeyResult, error) {
		if generate {
			return b.KeyGenerate(m.ctx, dir, replace)
		}
		return b.KeySet(m.ctx, dir, key, replace)
	}
	var details []string
	if label != "" {
		details = []string{m.text.Lookup(textDetailKeyFolder, label)}
	}
	return m.writeCall(op, dir, func(b Backend) (any, error) {
		_, err := b.KeyShow(m.ctx, dir)
		var remote *control.Error
		switch {
		case err == nil:
			return keyExists{details: details, replace: func() tea.Cmd {
				return m.writeCall(op, dir, func(b Backend) (any, error) { return send(b, true) })
			}}, nil
		case !errors.As(err, &remote) || remote.Code != control.CodeNotFound:
			return nil, err
		}
		return send(b, false)
	})
}

// keyProblem places a refused key at the field it came from: the pasted block, or the path when that was empty.
func (m *Model) keyProblem(f *settingsForm, msg settingMsg) (kittui.FormProblem, bool) {
	var remote *control.Error
	if f.kind != formKeyStore || msg.op != opKeySet || !errors.As(msg.err, &remote) || remote.Code != control.CodeInvalid {
		return kittui.FormProblem{}, false
	}
	field := "path"
	if strings.TrimSpace(f.form.Values()["key"].Text) != "" {
		field = "key"
	}
	return kittui.FormProblem{Field: kittui.FieldID(field), Cause: m.serviceText(msg.err), NextStep: m.text.Lookup(textFormFix)}, true
}
