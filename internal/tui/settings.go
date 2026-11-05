package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// settingsPage is one page of the Settings area; the empty page is the menu.
type settingsPage string

const (
	pageRepo   settingsPage = "repo"
	pageKey    settingsPage = "key"
	pageCreds  settingsPage = "credentials"
	pageWindow settingsPage = "window"
	pageNotify settingsPage = "notifications"
)

// settingsPages are the pages in the order of the menu, with the texts of their title and of their line in the
// menu.
var settingsPages = []struct {
	page         settingsPage
	title, about kittui.TextID
}{
	{pageRepo, textPageRepo, textAboutRepo},
	{pageKey, textPageKey, textAboutKey},
	{pageCreds, textPageCreds, textAboutCreds},
	{pageWindow, textPageWindow, textAboutWindow},
	{pageNotify, textPageNotify, textAboutNotify},
}

// settingsState is what the Settings area holds. gen separates the answers of a page that was left from those
// of the page shown.
type settingsState struct {
	menu *kittui.List
	page settingsPage
	gen  int
	// busy is set while a write is under way; the interface takes no key but quit then.
	busy bool
	form *settingsForm
	// pending is what a confirmed bind carries from the probe to the write.
	pending bind

	key    part
	keyRes control.KeyResult
	// noKey says the service holds no deploy key yet.
	noKey bool

	creds    part
	credList *kittui.List
	credData map[kittui.ItemID]control.CredentialInfo

	window    part
	windowRes control.WindowResult

	notify     part
	notifyList *kittui.List
	notifyData map[kittui.ItemID]control.NotifyChannelInfo
}

// bind is a repository the person asked to bind and the host key the server presented for it.
type bind struct {
	url, dir string
	probe    control.RepoProbeResult
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
	opFolder
	opRepoSet
	opFetch
	opKeyGenerate
	opCredSet
	opCredDelete
	opWindowSet
	opNotifyAdd
	opNotifyRemove
	opNotifyTest
	opSecretSet
	opSecretDelete
	opRestore
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
	s := &settingsState{}
	var err error
	s.menu, err = kittui.NewList(kittui.ListSpec{Columns: []kittui.Column{{Title: textColSetting}, {Title: textColAbout, Flex: true}},
		Keys: listKeys(), Search: kittui.SearchByKey, Empty: textNoValue}, keys, text)
	if err != nil {
		return nil, err
	}
	items := make([]kittui.ListItem, len(settingsPages))
	for i, p := range settingsPages {
		items[i] = kittui.ListItem{ID: kittui.ItemID(p.page), Cells: []string{text.Lookup(p.title), text.Lookup(p.about)}}
	}
	if err := s.menu.SetItems(items); err != nil {
		return nil, err
	}
	s.menu.SetFocused(true)
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
	case "":
		return s.menu
	case pageCreds:
		return s.credList
	case pageNotify:
		return s.notifyList
	}
	return nil
}

func (m *Model) settingsBusy() bool { return m.set.busy }

// openPage shows a page and reads what it shows.
func (m *Model) openPage(p settingsPage) tea.Cmd {
	m.set.page = p
	return m.loadPage(true)
}

// loadPage reads what the page shows. The repository page shows the status the refresh reads.
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
	case pageKey:
		return read(&s.key, func() (any, error) { return b.KeyShow(ctx) })
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
	case pageKey:
		return &s.key
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
	var remote *control.Error
	switch res := msg.res.(type) {
	case control.KeyResult:
		s.noKey = false
		if errors.As(msg.err, &remote) && remote.Code == control.CodeNotFound {
			// No key yet is a state of the page, not a failure.
			s.noKey, p.err, p.loaded, s.keyRes = true, nil, true, control.KeyResult{}
		} else if msg.err == nil {
			s.keyRes = res
		}
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
	case id == actBack:
		if s.page != "" {
			s.page = ""
			return nil, true
		}
		return m.enter(areaStacks), true
	case s.page == "" && id == actEnter:
		if item, ok := s.menu.Selected(); ok {
			return m.openPage(settingsPage(item)), true
		}
		return nil, true
	case id == actBind && s.page == pageRepo:
		return m.openForm(m.repoForm()), true
	case id == actFetch && s.page == pageRepo:
		return m.askFetch(), true
	case id == actGenerate && s.page == pageKey:
		return m.askGenerate(), true
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
		s.form = nil
		m.tell(textSettingDoneTitle, text, details)
	}
	switch res := msg.res.(type) {
	case folderReady:
		return m.probeRepo()
	case control.RepoProbeResult:
		s.pending.probe = res
		m.askBind()
		return nil
	case control.RepoSetResult:
		done(t.Lookup(textDoneBound), t.Lookup(textDetailHead, short(res.Head)), t.Lookup(textDetailCloned, yesNo(t, res.Cloned)))
		cmds = append(cmds, m.reloadNow())
	case control.RepoFetchResult:
		text := t.Lookup(textDoneFetchedCurrent)
		if res.UpdatesAvailable {
			text = t.Lookup(textDoneFetchedNew)
		}
		done(text, t.Lookup(textDetailHead, short(res.Head)), t.Lookup(textDetailRemote, short(res.RemoteHead)))
		cmds = append(cmds, m.reloadNow())
	case control.KeyResult:
		s.key.loaded, s.noKey, s.keyRes = true, false, res
		done(t.Lookup(textDoneGenerated), res.Fingerprint)
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
	if s.form != nil {
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

func (m *Model) askFetch() tea.Cmd {
	m.ask(textPageRepo, m.text.Lookup(textAskFetch), m.text.Lookup(textWhatFetch), nil, false, func() tea.Cmd {
		return m.writeCall(opFetch, "", func(b Backend) (any, error) { return b.RepoFetch(m.ctx) })
	})
	return nil
}

func (m *Model) askGenerate() tea.Cmd {
	s := m.set
	if !s.key.loaded {
		return nil
	}
	if !s.noKey {
		m.warn(m.text.Lookup(textKeyExists))
		return nil
	}
	m.ask(textPageKey, m.text.Lookup(textAskGenerate), m.text.Lookup(textWhatGenerate), nil, false, func() tea.Cmd {
		return m.writeCall(opKeyGenerate, "", func(b Backend) (any, error) { return b.KeyGenerate(m.ctx) })
	})
	return nil
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

// askBind asks whether the host key the server presented is the one to trust, then binds. A key the service
// pinned before is not asked again, as the command does.
func (m *Model) askBind() {
	t, p := m.text, m.set.pending
	target := p.probe.Host
	if p.probe.Port != "22" && p.probe.Port != "" {
		target += ":" + p.probe.Port
	}
	details := []string{t.Lookup(textDetailHost, target), t.Lookup(textDetailKeyType, p.probe.KeyType),
		t.Lookup(textDetailFingerprint, p.probe.Fingerprint), t.Lookup(textDetailDir, p.dir)}
	question, consequence := t.Lookup(textAskBind, p.url), t.Lookup(textWhatBind)
	if p.probe.Pinned {
		consequence = t.Lookup(textWhatBindPinned)
	}
	m.ask(textPageRepo, question, consequence, details, false, func() tea.Cmd {
		return m.writeCall(opRepoSet, "", func(b Backend) (any, error) {
			return b.RepoSet(m.ctx, control.RepoSetArgs{URL: p.url, Dir: p.dir, Fingerprint: p.probe.Fingerprint})
		})
	})
}

// settingsMark is the status mark while a write is under way.
func (m *Model) settingsMark() kittui.StatusMark {
	return kittui.StatusMark{Kind: kittui.StatusBusy, Text: m.text.Lookup(textHeaderWorking)}
}

// settingsFooter is the key line of the Settings area.
func (m *Model) settingsFooter(common, nav []kittui.Hint) []kittui.Hint {
	s := m.set
	if s.form != nil {
		return append(s.form.form.Hints(), kittui.Hint{Action: actBack, Label: textKeyCancelForm})
	}
	back := kittui.Hint{Action: actBack, Label: kittui.TextKeyBack, Priority: 1}
	var own []kittui.Hint
	switch s.page {
	case "":
		return append(append([]kittui.Hint{{Action: actEnter, Label: textKeyOpen}}, common...), nav...)
	case pageRepo:
		own = []kittui.Hint{{Action: actBind, Label: textKeyBind, Priority: 3}, {Action: actFetch, Label: textKeyFetch, Priority: 3}}
	case pageKey:
		if s.noKey {
			own = []kittui.Hint{{Action: actGenerate, Label: textKeyGenerate, Priority: 3}}
		}
	case pageCreds:
		own = []kittui.Hint{{Action: actAdd, Label: textKeyAdd, Priority: 3}, {Action: actRemove, Label: textKeyRemove, Priority: 3}}
	case pageWindow:
		own = []kittui.Hint{{Action: actEdit, Label: textKeyEdit, Priority: 3}}
	case pageNotify:
		own = []kittui.Hint{{Action: actAdd, Label: textKeyAdd, Priority: 3}, {Action: actTest, Label: textKeyTest, Priority: 3},
			{Action: actRemove, Label: textKeyRemove, Priority: 3}}
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
	if s.page == "" {
		view, _ := s.menu.View(m.styles, width, height)
		return view
	}
	title := m.heading(pageTitle(s.page), width)
	if list := s.activeList(); list != nil {
		lines := []string{title, ""}
		room := max(height-len(lines), 1)
		if p := s.pagePart(s.page); p != nil && (!p.loaded || p.err != nil) {
			return strings.Join(append(lines, m.partLines(*p, width)...), "\n")
		}
		view, _ := list.View(m.styles, width, room)
		return strings.Join(lines, "\n") + "\n" + view
	}
	lines := append([]string{title, ""}, m.pageLines(width)...)
	return strings.Join(lines[:min(len(lines), height)], "\n")
}

func pageTitle(p settingsPage) kittui.TextID {
	for _, e := range settingsPages {
		if e.page == p {
			return e.title
		}
	}
	return textAreaSettings
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
func (m *Model) pageLines(width int) []string {
	switch m.set.page {
	case pageRepo:
		return m.repoLines(width)
	case pageKey:
		return m.keyLines(width)
	case pageWindow:
		return m.windowLines(width)
	}
	return nil
}

func (m *Model) repoLines(width int) []string {
	t, e := m.text, m.snap.status.Engine
	none := t.Lookup(textNoValue)
	if m.snap.noRepo || !m.loaded {
		return m.wrapped(detailIndent, kittui.StyleHint, t.Lookup(textRepoNone), width)
	}
	val := func(v string) string {
		if v == "" {
			return none
		}
		return short(v)
	}
	lines := []string{
		m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailHead, val(e.Head)), width),
		m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailRemote, val(e.RemoteHead)), width),
		m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailStacks, len(e.Stacks)), width),
	}
	if e.PollError != "" {
		lines = append(lines, m.wrapped(detailIndent, kittui.StyleFail, t.Lookup(textDetailPollError, e.PollError), width)...)
	}
	return append(lines, "", m.line(detailIndent, kittui.StyleHint, t.Lookup(textRepoRebind), width))
}

func (m *Model) keyLines(width int) []string {
	s, t := m.set, m.text
	if !s.key.loaded || s.key.err != nil {
		return m.partLines(s.key, width)
	}
	if s.noKey {
		return m.wrapped(detailIndent, kittui.StyleHint, t.Lookup(textKeyNone), width)
	}
	lines := []string{m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailFingerprint, s.keyRes.Fingerprint), width), ""}
	lines = append(lines, m.wrapped(detailIndent, kittui.StyleHint, t.Lookup(textKeyCopy), width)...)
	// The key is written out whole and broken only where the screen is too narrow, so it can be copied.
	for _, l := range kittui.Wrap(kittui.Escape(strings.TrimSpace(s.keyRes.PublicKey)), max(width-detailIndent, 1)) {
		lines = append(lines, strings.Repeat(" ", detailIndent)+m.styles.Render(kittui.StyleActive, l))
	}
	return lines
}

func (m *Model) windowLines(width int) []string {
	s, t := m.set, m.text
	if !s.window.loaded || s.window.err != nil {
		return m.partLines(s.window, width)
	}
	w := s.windowRes
	layout := "Mon 2006-01-02 15:04"
	state := t.Lookup(textHeaderWindowShut)
	if w.Open {
		state = t.Lookup(textHeaderWindowOpen)
	}
	lines := []string{
		m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailDays, strings.Join(w.Days, ",")), width),
		m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailStart, w.Start), width),
		m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailDuration, w.Duration), width),
		m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailZone, w.Zone), width),
		m.line(detailIndent, kittui.StyleText, state, width),
	}
	if !w.NextOpening.IsZero() {
		lines = append(lines, m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailNext, w.NextOpening.Format(layout)), width))
	}
	if !w.LastRun.IsZero() {
		lines = append(lines, m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailLastRun, w.LastRun.Format(layout)), width))
	}
	return lines
}

// settingsTyped gives a key to the list of the page that has the text focus.
func (m *Model) settingsTyped(msg tea.KeyPressMsg) (cmd tea.Cmd, handled bool) {
	if l := m.set.activeList(); l != nil && m.set.form == nil {
		open, handled := l.Update(msg)
		return m.settingsOpen(open), handled
	}
	return nil, false
}

// settingsOpen opens the page the menu chose; the lists of the pages open nothing.
func (m *Model) settingsOpen(id kittui.ItemID) tea.Cmd {
	if id == "" || m.set.page != "" {
		return nil
	}
	return m.openPage(settingsPage(id))
}

// settingsInput is the input focus of the Settings area.
func (m *Model) settingsInput() kittui.InputFocus {
	if m.set.form != nil {
		return m.set.form.form.Input()
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
