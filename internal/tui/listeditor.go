package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The editor of a FieldList: the kit's list field only shows a summary and leaves the editing to the
// application. The editor adds, changes and removes the entries of one list, plain values or pairs of a key
// and a value. It lives inside the form that owns the field and writes its summary back when it closes.

// listEntry is one entry; a plain value is kept in key.
type listEntry struct{ key, value string }

// listEditorSpec says what a list holds and how its entries read.
type listEditorSpec struct {
	// field is the FieldList of the form the editor belongs to.
	field kittui.FieldID
	title kittui.TextID
	// pair makes entries of a key and a value; else an entry is one value.
	pair bool
	// keyLabel and valueLabel title the columns and the fields of an entry; valueLabel is used for a plain value.
	keyLabel, keyHint     kittui.TextID
	valueLabel, valueHint kittui.TextID
	intro, empty          kittui.TextID
	// choices are the keys a pair can take, shown as a choice; none asks for the key as text.
	choices []string
	// check returns the problems of one entry, by field "key" or "value".
	check func(m *Model, e listEntry) []kittui.FormProblem
}

type listEditor struct {
	spec    listEditorSpec
	entries []listEntry
	list    *kittui.List
	// entry is the open form of one entry; editing is the index it changes, -1 for a new one.
	entry   *kittui.Form
	editing int
}

func (m *Model) newListEditor(spec listEditorSpec, entries []listEntry) *listEditor {
	cols := []kittui.Column{{Title: spec.valueLabel}}
	if spec.pair {
		cols = []kittui.Column{{Title: spec.keyLabel}, {Title: spec.valueLabel, Flex: true}}
	}
	list, err := kittui.NewList(kittui.ListSpec{Columns: cols, Keys: listKeys(), Search: kittui.SearchByKey, Empty: spec.empty}, m.keys, m.text)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return nil
	}
	list.SetFocused(true)
	e := &listEditor{spec: spec, list: list, editing: -1}
	e.set(entries)
	return e
}

// set replaces the entries; a list of pairs stays in the order of its keys.
func (e *listEditor) set(entries []listEntry) {
	e.entries = slices.Clone(entries)
	if e.spec.pair {
		slices.SortFunc(e.entries, func(a, b listEntry) int { return strings.Compare(a.key, b.key) })
	}
	items := make([]kittui.ListItem, len(e.entries))
	for i, en := range e.entries {
		cells := []string{en.key}
		if e.spec.pair {
			cells = []string{en.key, en.value}
		}
		items[i] = kittui.ListItem{ID: kittui.ItemID(strconv.Itoa(i)), Cells: cells}
	}
	// The items are built here and fit the columns, so the kit has nothing to refuse.
	_ = e.list.SetItems(items)
}

// summary is what the field of the form shows.
func (e *listEditor) summary() string {
	parts := make([]string, len(e.entries))
	for i, en := range e.entries {
		parts[i] = en.key
		if e.spec.pair {
			parts[i] = en.key + " → " + en.value
		}
	}
	return strings.Join(parts, ", ")
}

// input is the input focus of the editor.
func (e *listEditor) input() kittui.InputFocus {
	if e.entry != nil {
		return e.entry.Input()
	}
	return e.list.Input()
}

// entryForm builds the form of one entry, with the values of the entry it changes.
func (m *Model) entryForm(e *listEditor, en listEntry) *kittui.Form {
	s := e.spec
	var fields []kittui.FormField
	switch {
	case !s.pair:
		fields = []kittui.FormField{textField("key", s.valueLabel, s.valueHint, en.key)}
	default:
		key := textField("key", s.keyLabel, s.keyHint, en.key)
		if len(s.choices) > 0 {
			// A name stands for its own text: the translator returns an ID it has no text for as it is.
			var cs []kittui.Choice
			for _, c := range s.choices {
				cs = append(cs, kittui.Choice{ID: kittui.ChoiceID(c), Label: kittui.TextID(c)})
			}
			first := s.choices[0]
			if en.key != "" {
				first = en.key
			}
			key = kittui.FormField{ID: "key", Kind: kittui.FieldChoice, Label: s.keyLabel, Hint: s.keyHint, Choices: cs,
				Value: kittui.FormValue{Choice: kittui.ChoiceID(first)}}
		}
		fields = []kittui.FormField{key, textField("value", s.valueLabel, s.valueHint, en.value)}
	}
	form, err := kittui.NewForm(kittui.FormSpec{Fields: fields, Keys: formKeys(), Buttons: []kittui.FormButton{
		{Action: actSave, Label: textKeyApply, Primary: true}, {Action: actBack, Label: kittui.TextKeyDiscard}}}, m.keys, m.text)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return nil
	}
	return form
}

// openEntry opens the form of the entry at index, or of a new one for -1.
func (m *Model) openEntry(e *listEditor, index int) {
	var en listEntry
	if index >= 0 {
		en = e.entries[index]
	}
	if form := m.entryForm(e, en); form != nil {
		e.entry, e.editing = form, index
	}
}

// editorKey handles a key of the open editor of a form.
func (m *Model) editorKey(f *settingsForm, msg tea.KeyPressMsg) tea.Cmd {
	e := f.editor
	if e.entry != nil {
		act, handled := e.entry.Update(msg)
		switch {
		case act == actSave && handled:
			m.submitEntry(e)
		case act == actBack:
			e.entry = nil
		default:
			if id, ok := m.keys.Match(msg, e.entry.Input()); ok && id == actForceQuit {
				return tea.Quit
			}
		}
		return nil
	}
	open, handled := e.list.Update(msg)
	if open != "" {
		if i, err := strconv.Atoi(string(open)); err == nil && i < len(e.entries) {
			m.openEntry(e, i)
		}
		return nil
	}
	if handled {
		return nil
	}
	id, ok := m.keys.Match(msg, e.list.Input())
	if !ok {
		return nil
	}
	switch id {
	case actForceQuit:
		return tea.Quit
	case actBack:
		m.closeEditor(f)
	case actAdd:
		m.openEntry(e, -1)
	case actEdit:
		if sel, ok := e.list.Selected(); ok {
			if i, err := strconv.Atoi(string(sel)); err == nil && i < len(e.entries) {
				m.openEntry(e, i)
			}
		}
	case actRemove:
		if sel, ok := e.list.Selected(); ok {
			if i, err := strconv.Atoi(string(sel)); err == nil && i < len(e.entries) {
				e.set(slices.Delete(slices.Clone(e.entries), i, i+1))
			}
		}
	}
	return nil
}

// submitEntry checks the entry of the open form and, when it holds up, takes it into the list.
func (m *Model) submitEntry(e *listEditor) {
	v := e.entry.Values()
	var en listEntry
	var ps []kittui.FormProblem
	if e.spec.pair {
		en.key = string(v["key"].Choice)
		if en.key == "" {
			en.key = strings.TrimSpace(v["key"].Text)
		}
		en.value = strings.TrimSpace(v["value"].Text)
	} else {
		en.key = strings.TrimSpace(v["key"].Text)
	}
	if en.key == "" {
		ps = append(ps, m.bad("key", textProblemRequired))
	}
	if e.spec.pair && en.value == "" {
		ps = append(ps, m.bad("value", textProblemRequired))
	}
	for i, other := range e.entries {
		if i != e.editing && other.key == en.key && en.key != "" {
			ps = append(ps, m.bad("key", textProblemListTwice, en.key))
		}
	}
	if len(ps) == 0 && e.spec.check != nil {
		ps = e.spec.check(m, en)
	}
	if len(ps) > 0 {
		e.entry.SetProblems(ps)
		return
	}
	entries := slices.Clone(e.entries)
	if e.editing >= 0 {
		entries[e.editing] = en
	} else {
		entries = append(entries, en)
	}
	e.set(entries)
	e.entry = nil
}

// openEditor shows the editor of the list field the form has the focus in; ok is false for any other field.
func (m *Model) openEditor(f *settingsForm) bool {
	e := f.lists[f.form.Focused()]
	if e == nil {
		return false
	}
	f.editor = e
	return true
}

// closeEditor ends the editor and gives the summary to the field.
func (m *Model) closeEditor(f *settingsForm) {
	e := f.editor
	f.editor = nil
	if e != nil {
		// The summary is plain text without control characters, so the form takes it.
		_ = f.form.SetValue(e.spec.field, kittui.FormValue{Text: e.summary()})
	}
}

// editorView draws the editor under its heading.
func (m *Model) editorView(e *listEditor, width, height int) string {
	head := []string{m.heading(e.spec.title, width)}
	if e.entry != nil {
		head = append(head, "")
		view, _ := e.entry.View(m.styles, width, max(height-len(head), 1))
		return strings.Join(head, "\n") + "\n" + view
	}
	head = append(head, m.wrapped(detailMargin, kittui.StyleHint, m.text.Lookup(e.spec.intro), width)...)
	head = append(head, "")
	view, _ := e.list.View(m.styles, width, max(height-len(head), 1))
	return strings.Join(head, "\n") + "\n" + view
}

// editorHints are the keys of the open editor.
func (m *Model) editorHints(e *listEditor) []kittui.Hint {
	if e.entry != nil {
		return append(e.entry.Hints(), kittui.Hint{Action: actBack, Label: textKeyCancelForm})
	}
	return []kittui.Hint{{Action: actAdd, Label: textKeyAdd, Priority: 3}, {Action: actEdit, Label: textKeyEdit, Priority: 3},
		{Action: actRemove, Label: textKeyRemove, Priority: 3}, {Action: actBack, Label: textKeyDone, Priority: 1}}
}
