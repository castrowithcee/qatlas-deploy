package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

// The values of the mouse field; the file stores a boolean.
const (
	mouseChoiceOn  = "on"
	mouseChoiceOff = "off"
)

// generalForm is the form of the General page. It holds finite choices only, so nothing is typed.
func (m *Model) generalForm() *settingsForm {
	p := m.prefs
	if p.Theme == "" {
		p.Theme = m.theme
	}
	if p.Start == "" {
		p.Start = string(areaStacks)
	}
	if p.Language == "" {
		p.Language = uiprefs.LanguageEnglish
	}
	var themes []kittui.Choice
	for _, name := range m.themes.Names() {
		// A theme name is a proper name; the catalog has no text for it, so the name stands as it is.
		themes = append(themes, choice(name, kittui.TextID(name)))
	}
	var starts []kittui.Choice
	for _, a := range areas() {
		starts = append(starts, choice(string(a.ID), a.Title))
	}
	mouse := mouseChoiceOn
	if !p.MouseEnabled {
		mouse = mouseChoiceOff
	}
	pick := func(id string, label, hint kittui.TextID, choices []kittui.Choice, value string) kittui.FormField {
		return kittui.FormField{ID: kittui.FieldID(id), Kind: kittui.FieldChoice, Label: label, Hint: hint, Choices: choices,
			Value: kittui.FormValue{Choice: kittui.ChoiceID(value)}}
	}
	return m.newSettingsForm(formGeneral, textPageGeneral, []kittui.FormField{
		pick("theme", textFieldTheme, textHintTheme, themes, p.Theme),
		pick("start", textFieldStartArea, textHintStartArea, starts, p.Start),
		pick("language", textFieldLanguage, textHintLanguage, []kittui.Choice{choice(uiprefs.LanguageEnglish, textLangEnglish),
			choice(uiprefs.LanguageGerman, textLangGerman), choice(uiprefs.LanguageSystem, textLangSystem)}, p.Language),
		pick("mouse", textFieldMouse, textHintMouse, []kittui.Choice{choice(mouseChoiceOn, textMouseOn),
			choice(mouseChoiceOff, textMouseOff)}, mouse),
	})
}

// submitGeneral writes the chosen values with the rest of the preferences unchanged. It reaches no service, so
// it neither waits for nor blocks a call of the service. Nothing of the running state changes unless the
// write succeeded.
func (m *Model) submitGeneral(f *settingsForm) tea.Cmd {
	t := m.text
	if m.savePrefs == nil {
		f.problem(t.Lookup(textGeneralCannotSave), t.Lookup(textFormFix))
		return nil
	}
	v := f.form.Values()
	next := m.prefs
	next.Theme, next.Start = string(v["theme"].Choice), string(v["start"].Choice)
	next.Language, next.MouseEnabled = string(v["language"].Choice), v["mouse"].Choice == mouseChoiceOn
	if err := m.savePrefs(next); err != nil {
		f.problem(t.Lookup(textGeneralSaveFailed, err.Error()), t.Lookup(textFormRetry))
		return nil
	}
	return m.applyPrefs(next)
}

// applyPrefs makes saved preferences effective at once, except the start area, which only the next start reads.
func (m *Model) applyPrefs(next uiprefs.Prefs) tea.Cmd {
	var cmds []tea.Cmd
	startChanged := next.Start != m.prefs.Start
	if lang := uiprefs.Language(next.Language); lang != m.lang {
		cmds = append(cmds, m.relocalize(lang))
	}
	if next.Theme != m.theme {
		m.theme = next.Theme
		m.restyle()
		if m.themes.AsksTerminalColors(m.theme, m.profile) {
			cmds = append(cmds, tea.RequestForegroundColor, tea.RequestBackgroundColor)
		}
	}
	m.mouseOn = next.MouseEnabled
	m.prefs = next
	m.set.form = m.generalForm()
	m.notice = ""
	m.result = m.text.Lookup(textGeneralSaved)
	if startChanged {
		m.result = m.text.Lookup(textGeneralSavedStart)
	}
	return tea.Batch(cmds...)
}

// listMemory is the state of a list that outlives a rebuild.
type listMemory struct {
	selected kittui.ItemID
	ticked   []kittui.ItemID
	query    string
}

func remember(l *kittui.List) listMemory {
	sel, _ := l.Selected()
	return listMemory{selected: sel, ticked: l.Ticked(), query: l.Query()}
}

// restore puts the state back once the rows are set again; what no longer exists is dropped.
func (mem listMemory) restore(l *kittui.List) {
	l.SetFilter(mem.query)
	_ = l.SetTicked(mem.ticked)
	if mem.selected != "" {
		l.Select(mem.selected)
	}
}

// relocalize switches the interface to lang. Every component that holds the translator is built again and
// filled from what was read before, keeping cursor, ticks and filter of the lists. The returned command reads
// again what cannot be rebuilt from memory.
func (m *Model) relocalize(lang i18n.Lang) tea.Cmd {
	text, err := kittui.NewTranslator(lang, catalog(), m.keys)
	var set *settingsState
	var stacks, runs, containers *kittui.List
	if err == nil {
		set, err = newSettings(m.keys, text)
	}
	if err == nil {
		stacks, err = m.newStackList(text)
	}
	if err == nil {
		runs, err = m.newRunsList(text)
	}
	if err == nil {
		containers, err = m.newContainerList(text)
	}
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return nil
	}
	s := m.set
	old := []**kittui.List{&m.list, &m.runsList, &m.ctList, &s.menu, &s.credList, &s.notifyList}
	fresh := []*kittui.List{stacks, runs, containers, set.menu, set.credList, set.notifyList}
	mem := make([]listMemory, len(old))
	for i, l := range old {
		mem[i] = remember(*l)
		*l = fresh[i]
	}
	m.lang, m.text, m.frame = lang, text, kittui.NewFrame(m.keys, text)
	if m.loaded {
		m.fillStacks()
	}
	if m.ct.loaded {
		m.fillContainers()
	}
	m.setCredentials(s.credRead)
	m.setChannels(s.notifyRead)
	for i, l := range fresh {
		mem[i].restore(l)
	}
	// A picker keeps its rows only inside its list; the detail opens it again.
	m.pick = nil
	return m.relocalizeLogs()
}

// relocalizeLogs builds the open log views again. A run log is filled from the text it holds; a container log
// is read again from the service, as the view alone holds its lines.
func (m *Model) relocalizeLogs() tea.Cmd {
	if l := &m.log; l.open && l.view != nil {
		if view, err := m.newLogView(l.view.Filter()); err == nil {
			l.view = view
			m.appendLog(l.consumed)
		}
	}
	l := &m.clog
	if !l.open || l.view == nil {
		return nil
	}
	view, err := m.newLogView(l.view.Filter())
	if err != nil {
		m.closeContainerLog()
		return nil
	}
	*l = containerLog{open: true, area: l.area, name: l.name, stack: l.stack, view: view, gen: l.gen + 1}
	return m.readContainerLog()
}
