package tui

import (
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The changes of the stack detail: a stack's secrets and the restore of one of its backups. Each opens a form
// or a choice, then a confirmation, and only the confirmation reaches the service.

const formSecret formKind = formNotifyDetail + 1

// maxSecretKey is the longest name of a secret the service accepts.
const maxSecretKey = 128

var secretKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

func validSecretKey(key string) bool { return len(key) <= maxSecretKey && secretKeyRe.MatchString(key) }

type pickKind int

const (
	pickSecret pickKind = iota
	pickBackup
)

// picker is the list a person chooses one secret or one backup from.
type picker struct {
	kind  pickKind
	stack string
	title kittui.TextID
	list  *kittui.List
	// backups holds what a backup row stands for, by its ID.
	backups map[kittui.ItemID]control.BackupInfo
}

// detailStack is the stack the detail shows.
func (m *Model) detailStack() (string, bool) {
	r, ok := m.rows[m.chosen]
	return r.name, ok
}

// detailBusy reports whether another change is under way; the interface then takes none.
func (m *Model) detailBusy() bool {
	if m.busy() || m.settingsBusy() {
		m.warn(m.text.Lookup(textActionBusy))
		return true
	}
	return false
}

// detailAction acts on a key of the detail that changes something; handled is false for any other key.
func (m *Model) detailAction(id kittui.ActionID) (cmd tea.Cmd, handled bool) {
	if m.pick != nil {
		return nil, false
	}
	switch id {
	case actAdd:
		return m.openSecretForm(), true
	case actRemove:
		return m.openSecretPick(), true
	case actRestore:
		return m.openBackupPick(), true
	case actDataDirs:
		return m.openDataDirForm(), true
	}
	return nil, false
}

func (m *Model) openSecretForm() tea.Cmd {
	stack, ok := m.detailStack()
	if !ok || m.detailBusy() {
		return nil
	}
	f := m.newSettingsForm(formSecret, textTitleSecret, []kittui.FormField{
		textField("key", textFieldSecretKey, textHintSecretKey, ""),
		maskedField("value", textFieldSecretValue, textHintSecretValue),
	})
	if f != nil {
		f.stack = stack
	}
	return m.openForm(f)
}

func (m *Model) submitSecret(f *settingsForm, key, value string) tea.Cmd {
	var ps []kittui.FormProblem
	if !validSecretKey(key) {
		ps = append(ps, m.bad("key", textProblemSecretKey))
	}
	if !validCredentialValue(value) {
		ps = append(ps, m.bad("value", textProblemSecret))
	}
	if len(ps) > 0 {
		f.problems(ps...)
		return nil
	}
	stack := f.stack
	m.ask(textTitleSecret, m.text.Lookup(textAskSecretSet, key, stack), m.text.Lookup(textWhatSecretSet), nil, false, func() tea.Cmd {
		return m.writeCall(opSecretSet, key, func(b Backend) (any, error) { return b.SecretSet(m.ctx, stack, key, value) })
	})
	return nil
}

func (m *Model) newPicker(kind pickKind, stack string, title kittui.TextID, columns []kittui.Column, empty kittui.TextID,
	items []kittui.ListItem) *picker {
	list, err := kittui.NewList(kittui.ListSpec{Columns: columns, Keys: listKeys(), Search: kittui.SearchByKey, Empty: empty},
		m.keys, m.text)
	if err == nil {
		err = list.SetItems(items)
	}
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return nil
	}
	list.SetFocused(true)
	return &picker{kind: kind, stack: stack, title: title, list: list}
}

func (m *Model) openSecretPick() tea.Cmd {
	stack, ok := m.detailStack()
	if !ok || m.detailBusy() {
		return nil
	}
	d := &m.detail
	if !d.sec.loaded || len(d.secrets.Keys) == 0 {
		m.warn(m.text.Lookup(textSecretsNoneToDelete))
		return nil
	}
	items := make([]kittui.ListItem, len(d.secrets.Keys))
	for i, k := range d.secrets.Keys {
		items[i] = kittui.ListItem{ID: kittui.ItemID(k), Cells: []string{k}}
	}
	m.pick = m.newPicker(pickSecret, stack, textTitleSecretDelete, []kittui.Column{{Title: textColName}}, textSecretsNone, items)
	return nil
}

// restorable are the backups a restore is offered for: all but the newest, which the command line never
// chooses for the operator either, and none without valid metadata.
func restorable(list []control.BackupInfo) []control.BackupInfo {
	var out []control.BackupInfo
	for i, b := range list {
		if i > 0 && !b.Incomplete {
			out = append(out, b)
		}
	}
	return out
}

func (m *Model) openBackupPick() tea.Cmd {
	stack, ok := m.detailStack()
	if !ok || m.detailBusy() {
		return nil
	}
	d := &m.detail
	choices := restorable(d.backups.Backups)
	if !d.bk.loaded || len(choices) == 0 {
		m.warn(m.text.Lookup(textBackupsNoneToRestore))
		return nil
	}
	items := make([]kittui.ListItem, len(choices))
	byID := map[kittui.ItemID]control.BackupInfo{}
	for i, b := range choices {
		id := kittui.ItemID(b.ID)
		byID[id] = b
		items[i] = kittui.ListItem{ID: id, Cells: []string{b.ID, b.Created.In(m.loc).Format(lastRunLayout), humanSize(b.Size)}}
	}
	p := m.newPicker(pickBackup, stack, textTitleRestore, []kittui.Column{{Title: textColBackup}, {Title: textColCreated, Flex: true},
		{Title: textColSize}}, textBackupsNone, items)
	if p != nil {
		p.backups = byID
	}
	m.pick = p
	return nil
}

// picked takes the row a person chose and asks for the confirmation of the change it stands for.
func (m *Model) picked(id kittui.ItemID) tea.Cmd {
	p := m.pick
	if p == nil || id == "" {
		return nil
	}
	m.pick = nil
	stack, key := p.stack, string(id)
	switch p.kind {
	case pickSecret:
		m.ask(textTitleSecretDelete, m.text.Lookup(textAskSecretDelete, key, stack), m.text.Lookup(textWhatSecretDelete), nil, true, func() tea.Cmd {
			return m.writeCall(opSecretDelete, key, func(b Backend) (any, error) { return b.SecretDelete(m.ctx, stack, key) })
		})
	case pickBackup:
		b := p.backups[id]
		details := []string{b.Created.In(m.loc).Format(lastRunLayout), humanSize(b.Size)}
		m.ask(textTitleRestore, m.text.Lookup(textAskRestore, key, stack), m.text.Lookup(textWhatRestore), details, true, func() tea.Cmd {
			return m.writeCall(opRestore, key, func(b Backend) (any, error) { return b.BackupRestore(m.ctx, stack, key) })
		})
	}
	return nil
}

// pickView draws the choice under its heading.
func (m *Model) pickView(width, height int) string {
	p := m.pick
	head := m.heading(p.title, width) + "\n\n"
	view, _ := p.list.View(m.styles, width, max(height-2, 1))
	return head + view
}

// formView draws the open form of the Settings area or of the detail.
func (m *Model) formView(width, height int) string {
	m.scrollNote = ""
	f := m.set.form
	head := []string{m.heading(f.title, width), ""}
	if f.kind == formDataDirs {
		head = append(head, m.formDataDirLines(f, width)...)
	}
	view, _ := f.form.View(m.styles, width, max(height-len(head), 1))
	return strings.Join(head, "\n") + "\n" + view
}

// detailHints are the keys of the detail that change something.
func (m *Model) detailHints() []kittui.Hint {
	var hints []kittui.Hint
	if stack, ok := m.detailStack(); ok {
		if _, found := m.dataDirFinding(stack); found {
			hints = append(hints, kittui.Hint{Action: actDataDirs, Label: textKeyDataDirs, Priority: 2})
		}
	}
	return append(hints,
		kittui.Hint{Action: actAdd, Label: textKeySecretSet, Priority: 3},
		kittui.Hint{Action: actRemove, Label: textKeySecretDelete, Priority: 3},
		kittui.Hint{Action: actRestore, Label: textKeyRestore, Priority: 3})
}
