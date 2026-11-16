package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// settingsPage is one page of the Settings area, shown under its sub-tab.
type settingsPage string

const (
	pageGeneral settingsPage = "general"
	pageCreds   settingsPage = "credentials"
	pageWindow  settingsPage = "window"
	pageNotify  settingsPage = "notifications"
)

type settingsEntry struct {
	page  settingsPage
	title kittui.TextID
}

// settingsPages are the pages in the order of the sub-tabs, with the text of their title.
var settingsPages = []settingsEntry{
	{pageGeneral, textPageGeneral},
	{pageCreds, textPageCreds},
	{pageWindow, textPageWindow},
	{pageNotify, textPageNotify},
}

// settingsTabs are the sub-tabs of the Settings area.
func settingsTabs() []kittui.Tab {
	tabs := make([]kittui.Tab, len(settingsPages))
	for i, p := range settingsPages {
		tabs[i] = kittui.Tab{ID: kittui.TabID(p.page), Title: p.title}
	}
	return tabs
}

// settingsState is what the Settings area holds. page is the sub-tab shown; it stays while the area is left.
// gen separates the answers of a page that was left from those of the page shown.
type settingsState struct {
	page settingsPage
	gen  int
	// busy is set while a write is under way; the interface takes no key but quit then.
	busy bool
	form *settingsForm

	// folderData are the bound stack folders by path; add is the one being added, nil while none is.
	folderData map[string]control.SourceStatus
	add        *addFlow

	// keyOpen says the Stacks area shows the deploy key of the folder keyFor.
	keyOpen bool
	key     part
	keyFor  string
	keyRes  control.KeyResult
	// noKey says the service holds no deploy key yet.
	noKey bool

	creds    part
	credList *kittui.List
	credData map[kittui.ItemID]control.CredentialInfo
	credRead []control.CredentialInfo

	window    part
	windowRes control.WindowResult

	notify     part
	notifyList *kittui.List
	notifyData map[kittui.ItemID]control.NotifyChannelInfo
	notifyRead []control.NotifyChannelInfo
}

// settingsLoadMsg carries the answer of a read of one page.
type settingsLoadMsg struct {
	gen  int
	page settingsPage
	res  any
	err  error
}

// settingOp tells which write an answer belongs to.
type settingOp int

const (
	opProbe settingOp = iota
	opInspect
	opKeyCheck
	opFolderAdd
	opSourceRemove
	opFetch
	opKeyGenerate
	opKeySet
	opCredSet
	opCredDelete
	opWindowSet
	opNotifyAdd
	opNotifyRemove
	opNotifyTest
	opSecretSet
	opSecretDelete
	opRestore
	opDataDirs
	opCompare
	opRemote
	opStackSettings
	opStackSettingsSet
	opStackSettingsBackup
	opStackSettingsCompose
)

// settingMsg carries the answer of a write. A write answers with the name it concerned, never with a value.
type settingMsg struct {
	gen  int
	op   settingOp
	name string
	res  any
	err  error
}

func newSettings(keys *kittui.Keymap, text kittui.Translator) (*settingsState, error) {
	s := &settingsState{page: pageGeneral}
	var err error
	s.credList, err = kittui.NewList(kittui.ListSpec{Columns: []kittui.Column{{Title: textColName}, {Title: textColModified, Flex: true}},
		Keys: listKeys(), Search: kittui.SearchByKey, Empty: textCredsEmpty}, keys, text)
	if err != nil {
		return nil, err
	}
	s.credList.SetFocused(true)
	s.notifyList, err = kittui.NewList(kittui.ListSpec{Columns: []kittui.Column{{Title: textColName}, {Title: textColType},
		{Title: textColEvents, Optional: true}, {Title: textColTarget, Flex: true}},
		Keys: listKeys(), Search: kittui.SearchByKey, Empty: textNotifyEmpty}, keys, text)
	if err != nil {
		return nil, err
	}
	s.notifyList.SetFocused(true)
	return s, nil
}

// activeList is the list of the page shown, nil for a page without one.
func (s *settingsState) activeList() *kittui.List {
	switch s.page {
	case pageCreds:
		return s.credList
	case pageNotify:
		return s.notifyList
	}
	return nil
}

func (m *Model) settingsBusy() bool { return m.set.busy }

// showPage fills the page shown: the General page is its form, the others read what they show.
func (m *Model) showPage(force bool) tea.Cmd {
	if m.set.page == pageGeneral {
		if m.set.form == nil {
			return m.openForm(m.generalForm())
		}
		return nil
	}
	return m.loadPage(force)
}

// selectTab shows the page of a sub-tab and gives the focus to the sub-tab line. Unsaved changes of the General
// form are asked about first; the focus moves only once they are answered.
func (m *Model) selectTab(id kittui.TabID) tea.Cmd {
	page := settingsPage(id)
	if !slices.ContainsFunc(settingsPages, func(e settingsEntry) bool { return e.page == page }) {
		return nil
	}
	return m.leaveGeneral(func() tea.Cmd {
		m.setZone(kittui.ZoneSubtabs)
		if page == m.set.page {
			return nil
		}
		m.set.page, m.set.form = page, nil
		return m.showPage(true)
	})
}

// stepTab shows the sub-tab step places after the one shown, with wrap-around.
func (m *Model) stepTab(step int) tea.Cmd {
	for _, a := range areas() {
		if a.ID == areaSettings {
			return m.selectTab(kittui.StepTab(a, kittui.TabID(m.set.page), step))
		}
	}
	return nil
}

// leaveGeneral runs leave at once, or, while the General form holds unsaved changes, after the question about
// discarding them; the form then starts again from the saved values.
func (m *Model) leaveGeneral(leave func() tea.Cmd) tea.Cmd {
	f := m.set.form
	if f == nil || f.kind != formGeneral || !f.form.Changed() {
		return leave()
	}
	m.ask(textTitleDiscard, m.text.Lookup(textAskDiscard), m.text.Lookup(textWhatDiscard), nil, true, func() tea.Cmd {
		if m.set.form == f {
			m.set.form = m.generalForm()
		}
		return leave()
	})
	return nil
}

// loadPage reads what the page shows. The folder page shows the status the refresh reads.
func (m *Model) loadPage(force bool) tea.Cmd {
	s := m.set
	ctx, b, page := m.ctx, m.backend, s.page
	read := func(p *part, fn func() (any, error)) tea.Cmd {
		if p.loading || p.loaded && !force {
			return nil
		}
		p.loading = true
		s.gen++
		gen := s.gen
		return func() tea.Msg {
			res, err := fn()
			return settingsLoadMsg{gen: gen, page: page, res: res, err: err}
		}
	}
	switch page {
	case pageCreds:
		return read(&s.creds, func() (any, error) { return b.Credentials(ctx) })
	case pageWindow:
		return read(&s.window, func() (any, error) { return b.Window(ctx) })
	case pageNotify:
		return read(&s.notify, func() (any, error) { return b.Notifications(ctx) })
	}
	return nil
}

// pagePart is the read state of a page, nil for a page that reads nothing of its own.
func (s *settingsState) pagePart(p settingsPage) *part {
	switch p {
	case pageCreds:
		return &s.creds
	case pageWindow:
		return &s.window
	case pageNotify:
		return &s.notify
	}
	return nil
}

func (m *Model) applySettingsLoad(msg settingsLoadMsg) {
	s := m.set
	p := s.pagePart(msg.page)
	if p == nil || msg.page != s.page {
		if p != nil {
			p.loading = false
		}
		return
	}
	p.loading = false
	p.err = msg.err
	p.loaded = msg.err == nil
	switch res := msg.res.(type) {
	case control.CredentialListResult:
		if msg.err == nil {
			m.setCredentials(res.Credentials)
		}
	case control.WindowResult:
		if msg.err == nil {
			s.windowRes = res
		}
	case control.NotifyListResult:
		if msg.err == nil {
			m.setChannels(res.Channels)
		}
	}
}

func (m *Model) setCredentials(list []control.CredentialInfo) {
	s := m.set
	s.credRead = list
	s.credData = map[kittui.ItemID]control.CredentialInfo{}
	items := make([]kittui.ListItem, len(list))
	for i, c := range list {
		id := kittui.ItemID(c.Name)
		s.credData[id] = c
		items[i] = kittui.ListItem{ID: id, Cells: []string{c.Name, c.Modified.In(m.loc).Format(modifiedLayout)}}
	}
	if err := s.credList.SetItems(items); err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
	}
}

// modifiedLayout is the time a credential was stored.
const modifiedLayout = "2006-01-02 15:04"

func (m *Model) setChannels(list []control.NotifyChannelInfo) {
	s := m.set
	s.notifyRead = list
	s.notifyData = map[kittui.ItemID]control.NotifyChannelInfo{}
	items := make([]kittui.ListItem, len(list))
	for i, c := range list {
		id := kittui.ItemID(c.Name)
		s.notifyData[id] = c
		events := m.text.Lookup(textEventsAll)
		if len(c.Events) > 0 {
			events = strings.Join(c.Events, ",")
		}
		items[i] = kittui.ListItem{ID: id, Cells: []string{c.Name, c.Type, events, channelTarget(c, m.text.Lookup(textNoValue))}}
	}
	if err := s.notifyList.SetItems(items); err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
	}
}

// channelTarget is where a channel sends to, without any secret part.
func channelTarget(c control.NotifyChannelInfo, none string) string {
	switch c.Type {
	case control.NotifySMTP:
		target := c.Host
		if c.Port > 0 {
			target = fmt.Sprintf("%s:%d", c.Host, c.Port)
		}
		if len(c.To) > 0 {
			target += " -> " + strings.Join(c.To, ",")
		}
		return target
	case control.NotifyTelegram:
		return "chat " + c.ChatID
	}
	return none
}

// selectedCredential is the credential under the cursor.
func (m *Model) selectedCredential() (control.CredentialInfo, bool) {
	id, ok := m.set.credList.Selected()
	c, found := m.set.credData[id]
	return c, ok && found
}

func (m *Model) selectedChannel() (control.NotifyChannelInfo, bool) {
	id, ok := m.set.notifyList.Selected()
	c, found := m.set.notifyData[id]
	return c, ok && found
}

// settingsKey acts on the key of an action of the Settings area; handled is false for a key that is none.
func (m *Model) settingsAction(id kittui.ActionID) (cmd tea.Cmd, handled bool) {
	s := m.set
	switch {
	case id == actAdd && s.page == pageCreds:
		return m.openForm(m.credentialForm()), true
	case id == actRemove && s.page == pageCreds:
		return m.askCredentialDelete(), true
	case id == actEdit && s.page == pageWindow:
		return m.openForm(m.windowForm()), true
	case id == actAdd && s.page == pageNotify:
		return m.openForm(m.notifyBasicsForm()), true
	case id == actRemove && s.page == pageNotify:
		return m.askNotifyRemove(), true
	case id == actTest && s.page == pageNotify:
		return m.askNotifyTest(), true
	}
	return nil, false
}

// call runs a write off the event loop and answers with a settingMsg. The caller shows the busy mark.
func (m *Model) writeCall(op settingOp, name string, fn func(b Backend) (any, error)) tea.Cmd {
	s := m.set
	s.busy = true
	s.gen++
	gen, b := s.gen, m.backend
	return func() tea.Msg {
		res, err := fn(b)
		return settingMsg{gen: gen, op: op, name: name, res: res, err: err}
	}
}

// applySetting takes the answer of a write. A failure goes under the field of an open form, else to the
// message line; a success closes the form and says what was done.
func (m *Model) applySetting(msg settingMsg) tea.Cmd {
	s := m.set
	if msg.gen != s.gen || !s.busy {
		return nil
	}
	s.busy = false
	if msg.err != nil {
		return m.settingFailed(msg)
	}
	t := m.text
	var cmds []tea.Cmd
	done := func(text string, details ...string) {
		if s.form == nil || s.form.kind != formGeneral {
			s.form = nil
		}
		m.tell(textSettingDoneTitle, text, details)
	}
	switch res := msg.res.(type) {
	case inspected:
		s.form, s.add = nil, res.flow
		m.showInspection(res.flow)
		return nil
	case control.RepoProbeResult:
		if s.add == nil {
			return nil
		}
		s.form, s.add.probe = nil, res
		m.askHostKey()
		return nil
	case keyChecked:
		return m.keyDone(res)
	case control.SourceCompareResult:
		m.showComparison(res)
		return nil
	case control.SourceRemoteResult:
		s.add = nil
		details := []string{t.Lookup(textDetailState, res.State), t.Lookup(textDetailBranch, res.Branch), t.Lookup(textDetailHead, short(res.Head)),
			t.Lookup(textDetailPushed, yesNo(t, res.Pushed))}
		done(t.Lookup(textDoneRemote, res.Path, res.URL), details...)
		cmds = append(cmds, m.reloadNow())
	case listChanged:
		s.add = nil
		m.failure = t.Lookup(kittui.TextError, t.Lookup(textFolderListChanged, res.path))
		return nil
	case control.RepoSetResult:
		s.add = nil
		done(t.Lookup(textDoneBound, res.Dir), t.Lookup(textDetailHead, short(res.Head)), t.Lookup(textDetailCloned, yesNo(t, res.Cloned)))
		cmds = append(cmds, m.reloadNow(), m.rereadForeign())
	case control.SourceAddResult:
		s.add = nil
		details := []string{t.Lookup(textDetailKind, res.Kind), t.Lookup(textDetailHead, short(res.Head))}
		if res.Files > 0 {
			details = append(details, t.Lookup(textDetailFiles, res.Files))
		}
		done(t.Lookup(textDoneFolderAdded, res.Path), details...)
		cmds = append(cmds, m.reloadNow(), m.rereadForeign())
	case control.SourceRemoveResult:
		done(t.Lookup(textDoneReleased, res.Path))
		cmds = append(cmds, m.reloadNow())
	case control.RepoFetchResult:
		m.fetchDone(res, msg.name, done)
		cmds = append(cmds, m.reloadNow())
	case control.KeyResult:
		// The page shows the key of its folder only; the key of another stack folder is shown in the answer alone.
		if s.keyOpen && msg.name == s.keyFor {
			s.key.loaded, s.noKey, s.keyRes = true, false, res
		}
		text := textDoneKeySet
		if msg.op == opKeyGenerate {
			text = textDoneGenerated
		}
		done(t.Lookup(text), t.Lookup(textDetailFingerprint, res.Fingerprint),
			t.Lookup(textDetailPublicKey, kittui.Escape(strings.TrimSpace(res.PublicKey))))
	case keyExists:
		m.ask(textTitleKey, t.Lookup(textAskKeyReplace), t.Lookup(textWhatKeyReplace), res.details, true, res.replace)
		return nil
	case control.WindowResult:
		s.window.loaded, s.windowRes = true, res
		done(t.Lookup(textDoneWindow))
		cmds = append(cmds, m.reloadNow())
	case control.NotifyChannelInfo:
		done(t.Lookup(textDoneAdded, res.Name))
		cmds = append(cmds, m.loadPage(true))
	case control.NotifyTestResult:
		done(t.Lookup(textDoneTested, res.Name))
	case control.SecretResult:
		text := t.Lookup(textDoneSecretSet, res.Key, res.Stack)
		switch {
		case msg.op == opSecretDelete:
			text = t.Lookup(textDoneSecretDeleted, res.Key, res.Stack)
		case res.Replaced:
			text = t.Lookup(textDoneSecretReplaced, res.Key, res.Stack)
		}
		done(text, t.Lookup(textSecretAppliesLater))
		cmds = append(cmds, m.readSecrets())
	case control.StackSettingsResult:
		switch msg.op {
		case opStackSettingsBackup:
			cmds = append(cmds, m.backupFormFor(res))
		case opStackSettingsCompose:
			cmds = append(cmds, m.composeFormFor(res))
		default:
			cmds = append(cmds, m.updateFormFor(res))
		}
	case control.StackSettingsSetResult:
		cmds = append(cmds, m.settingsSaved(res, done))
	case dataDirsResult:
		cmds = append(cmds, m.dataDirsDone(res, done))
	case control.BackupRestoreResult:
		done(t.Lookup(textDoneRestored, res.Backup.ID, res.Backup.Stack))
		cmds = append(cmds, m.reloadNow(), m.readBackups())
	default:
		switch msg.op {
		case opCredSet:
			done(t.Lookup(textDoneStored, msg.name))
		case opCredDelete:
			done(t.Lookup(textDoneDeleted, msg.name))
		case opNotifyRemove:
			done(t.Lookup(textDoneRemoved, msg.name))
		}
		cmds = append(cmds, m.loadPage(true))
	}
	return tea.Batch(cmds...)
}

// settingFailed shows why a write failed. The service words contain no stored value.
func (m *Model) settingFailed(msg settingMsg) tea.Cmd {
	s := m.set
	text := m.serviceText(msg.err)
	var host hostError
	if errors.As(msg.err, &host) {
		text = m.text.Lookup(textErrHost, host.err.Error())
	}
	var kept keptChanges
	if errors.As(msg.err, &kept) {
		text = m.text.Lookup(textKeptChanges, text, kept.changed)
	}
	if msg.op == opFetch {
		text = m.text.Lookup(textFetchFailed, msg.name, text)
	}
	if s.form != nil && s.form.kind != formGeneral {
		if p, ok := m.keyProblem(s.form, msg); ok {
			s.form.problems(p)
			return nil
		}
		if msg.op == opStackSettingsSet {
			if p, ok := m.settingsProblems(s.form.kind, msg.err); ok {
				s.form.problems(p)
				return nil
			}
		}
		s.form.problem(text, m.text.Lookup(textFormRetry))
		return nil
	}
	m.failure = m.text.Lookup(kittui.TextError, text)
	return nil
}

func short(commit string) string { return commit[:min(len(commit), maxShownCommit)] }

func yesNo(t kittui.Translator, v bool) string {
	if v {
		return t.Lookup(textYes)
	}
	return t.Lookup(textNo)
}

// ask opens the confirmation of a change. Cancel is the default answer; a destructive change also marks the
// yes answer.
func (m *Model) ask(title kittui.TextID, question, consequence string, details []string, destructive bool, yes func() tea.Cmd) {
	if m.settingsBusy() {
		m.warn(m.text.Lookup(textActionBusy))
		return
	}
	spec := kittui.DialogSpec{Title: title, Question: question, Consequence: consequence, Details: dialogLines(details),
		Answers: []kittui.Answer{{Action: actYes, Label: textAnswerYes}, {Action: actNo, Label: textAnswerCancel}},
		Default: actNo, Keys: kittui.DialogKeys{Enter: actEnter}}
	if destructive {
		spec.Danger, spec.Irreversible = actYes, true
	}
	dialog, err := kittui.NewDialog(spec, m.keys, m.text)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return
	}
	m.modal = &modal{kind: modalAsk, dialog: dialog, yes: yes}
}

// askSheet opens a question that fills the workspace, for one with more to read than a dialog holds; body
// gives its lines for a width. Without yes it only tells, and its one answer closes it.
func (m *Model) askSheet(title kittui.TextID, question, consequence string, body func(int) []string, answers []kittui.Answer, yes func() tea.Cmd) {
	if m.settingsBusy() {
		m.warn(m.text.Lookup(textActionBusy))
		return
	}
	spec := kittui.DialogSpec{Title: title, Question: question, Consequence: consequence, Answers: answers, Default: actNo,
		Keys: kittui.DialogKeys{Enter: actEnter}}
	if yes == nil {
		spec.Notice, spec.Details = true, dialogLines([]string{consequence})
		spec.Answers = []kittui.Answer{{Action: actNo, Label: textAnswerClose}}
	}
	dialog, err := kittui.NewDialog(spec, m.keys, m.text)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return
	}
	m.modal = &modal{kind: modalSheet, dialog: dialog, yes: yes, question: question, consequence: consequence, body: body}
}

// tell reports the result of a change: a calm notice dialog when there are details, else the message line.
func (m *Model) tell(title kittui.TextID, text string, details []string) {
	if len(details) == 0 {
		m.result = text
		return
	}
	dialog, err := kittui.NewDialog(kittui.DialogSpec{Title: title, Question: text, Notice: true,
		Details: dialogLines(details),
		Answers: []kittui.Answer{{Action: actNo, Label: textAnswerClose}}, Default: actNo,
		Keys: kittui.DialogKeys{Enter: actEnter}}, m.keys, m.text)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return
	}
	m.modal = &modal{kind: modalAsk, dialog: dialog}
}

func (m *Model) askCredentialDelete() tea.Cmd {
	c, ok := m.selectedCredential()
	if !ok {
		m.warn(m.text.Lookup(textChooseOne))
		return nil
	}
	m.ask(textPageCreds, m.text.Lookup(textAskCredDelete, c.Name), m.text.Lookup(textWhatCredDelete), nil, true, func() tea.Cmd {
		return m.writeCall(opCredDelete, c.Name, func(b Backend) (any, error) { return nil, b.CredentialDelete(m.ctx, c.Name) })
	})
	return nil
}

func (m *Model) askNotifyRemove() tea.Cmd {
	c, ok := m.selectedChannel()
	if !ok {
		m.warn(m.text.Lookup(textChooseOne))
		return nil
	}
	m.ask(textPageNotify, m.text.Lookup(textAskNotifyRemove, c.Name), m.text.Lookup(textWhatNotifyRemove), nil, true, func() tea.Cmd {
		return m.writeCall(opNotifyRemove, c.Name, func(b Backend) (any, error) { return nil, b.NotifyRemove(m.ctx, c.Name) })
	})
	return nil
}

func (m *Model) askNotifyTest() tea.Cmd {
	c, ok := m.selectedChannel()
	if !ok {
		m.warn(m.text.Lookup(textChooseOne))
		return nil
	}
	m.ask(textPageNotify, m.text.Lookup(textAskNotifyTest, c.Name), m.text.Lookup(textWhatNotifyTest), nil, false, func() tea.Cmd {
		return m.writeCall(opNotifyTest, c.Name, func(b Backend) (any, error) { return b.NotifyTest(m.ctx, c.Name) })
	})
	return nil
}

// settingsMark is the status mark while a write is under way.
func (m *Model) settingsMark() kittui.StatusMark {
	return kittui.StatusMark{Kind: kittui.StatusBusy, Text: m.text.Lookup(textHeaderWorking)}
}

// settingsFooter is the key line of the Settings area.
func (m *Model) settingsFooter(common, nav []kittui.Hint) []kittui.Hint {
	s := m.set
	if s.form != nil && m.formFocused() {
		return append(s.form.form.Hints(), kittui.Hint{Action: actBack, Label: textKeyCancelForm})
	}
	back := kittui.Hint{Action: actBack, Label: kittui.TextKeyBack, Priority: 1}
	var own []kittui.Hint
	if m.focus() == kittui.ZoneSubtabs {
		own = append(own, kittui.Hint{Action: actLeft, Label: textKeyPrevPage, Priority: 3},
			kittui.Hint{Action: actRight, Label: textKeyNextPage, Priority: 3})
	}
	switch s.page {
	case pageCreds:
		own = append(own, kittui.Hint{Action: actAdd, Label: textKeyAdd, Priority: 3}, kittui.Hint{Action: actRemove, Label: textKeyRemove, Priority: 3})
	case pageWindow:
		own = append(own, kittui.Hint{Action: actEdit, Label: textKeyEdit, Priority: 3})
	case pageNotify:
		own = append(own, kittui.Hint{Action: actAdd, Label: textKeyAdd, Priority: 3}, kittui.Hint{Action: actTest, Label: textKeyTest, Priority: 3},
			kittui.Hint{Action: actRemove, Label: textKeyRemove, Priority: 3})
	}
	hints := append(own, back)
	if s.activeList() != nil {
		hints = append(hints, nav...)
	}
	return append(hints, common...)
}

// settingsView draws the Settings area in width by height cells.
func (m *Model) settingsView(width, height int) string {
	s := m.set
	m.scrollNote = ""
	if s.form != nil {
		return m.formView(width, height)
	}
	if list := s.activeList(); list != nil {
		if p := s.pagePart(s.page); p != nil && (!p.loaded || p.err != nil) {
			lines := m.partLines(*p, width)
			return strings.Join(lines[:min(len(lines), height)], "\n")
		}
		return m.listView(list, width, height, 0)
	}
	lines := m.pageLines(width, height)
	return strings.Join(lines[:min(len(lines), height)], "\n")
}

// partLines say why a page shows nothing yet.
func (m *Model) partLines(p part, width int) []string {
	switch {
	case p.err != nil:
		return m.wrapped(detailIndent, kittui.StyleFail, m.errorText(p.err), width)
	case p.loading:
		return []string{m.line(detailIndent, kittui.StyleHint, m.text.Lookup(textDetailLoading), width)}
	}
	return nil
}

// pageLines are the lines of the pages that are text.
func (m *Model) pageLines(width, height int) []string {
	switch m.set.page {
	case pageWindow:
		return m.windowLines(width, height)
	}
	return nil
}

func (m *Model) windowLines(width, height int) []string {
	s, t := m.set, m.text
	if !s.window.loaded || s.window.err != nil {
		return m.partLines(s.window, width)
	}
	w := s.windowRes
	state := t.Lookup(textHeaderWindowShut)
	if w.Open {
		state = t.Lookup(textHeaderWindowOpen)
	}
	return m.framed(t.Lookup(textPageWindow), "", width, height, func(width int) []string {
		lines := []string{
			m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailDays, m.dayList(w.Days)), width),
			m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailStart, w.Start), width),
			m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailDuration, w.Duration), width),
			m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailZone, w.Zone), width),
			m.line(detailIndent, kittui.StyleText, state, width),
		}
		if !w.NextOpening.IsZero() {
			lines = append(lines, m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailNext, m.stamp(w.NextOpening)), width))
		}
		if !w.LastRun.IsZero() {
			lines = append(lines, m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailLastRun, m.stamp(w.LastRun)), width))
		}
		return lines
	})
}

// settingsTyped gives a key to the list of the page that has the text focus.
func (m *Model) settingsTyped(msg tea.KeyPressMsg) (cmd tea.Cmd, handled bool) {
	if l := m.set.activeList(); l != nil && m.set.form == nil {
		// The lists of the pages open nothing.
		_, handled := l.Update(msg)
		return nil, handled
	}
	return nil, false
}

// settingsInput is the input focus of the Settings area.
func (m *Model) settingsInput() kittui.InputFocus {
	if f := m.set.form; f != nil {
		if f.kind == formGeneral && m.focus() != kittui.ZoneForm {
			return kittui.InputNavigation
		}
		return f.input()
	}
	if l := m.set.activeList(); l != nil {
		return l.Input()
	}
	return kittui.InputNavigation
}

// settingsPosition is the position text of the header.
func (m *Model) settingsPosition() string {
	if l := m.set.activeList(); l != nil && m.set.form == nil {
		return l.Position()
	}
	return ""
}

var weekdayTexts = [...]kittui.TextID{textWeekdaySun, textWeekdayMon, textWeekdayTue, textWeekdayWed, textWeekdayThu, textWeekdayFri, textWeekdaySat}

var dayWords = map[string]time.Weekday{"mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday, "sun": time.Sunday}

// stamp formats a time with the weekday in the UI language.
func (m *Model) stamp(t time.Time) string {
	return m.text.Lookup(weekdayTexts[t.Weekday()]) + " " + t.Format("2006-01-02 15:04")
}

// dayList shows command day words in the UI language, lower case; an unknown word stays as given.
func (m *Model) dayList(days []string) string {
	out := make([]string, len(days))
	for i, d := range days {
		out[i] = d
		if wd, ok := dayWords[d]; ok {
			out[i] = strings.ToLower(m.text.Lookup(weekdayTexts[wd]))
		}
	}
	return strings.Join(out, ",")
}
