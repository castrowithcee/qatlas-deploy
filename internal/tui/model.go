package tui

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/datadir"
	"github.com/castrowithcee/qatlas-deploy/internal/helptopics"
)

// defaultEuid and defaultMakeDirs are what a new model uses; tests replace them for the models they cannot reach.
var (
	defaultEuid     = os.Geteuid
	defaultMakeDirs = datadir.Create
)

// The areas of the tab line, in its order.
const (
	areaStacks kittui.AreaID = "stacks"
	areaDetail kittui.AreaID = "detail"
	areaRuns   kittui.AreaID = "runs"
	// areaSettings holds what a host needs to operate: the repository, the deploy key, credentials, the
	// maintenance window and the notifications.
	areaSettings kittui.AreaID = "settings"
)

func areas() []kittui.Area {
	return []kittui.Area{
		{ID: areaStacks, Title: textAreaStacks},
		{ID: areaDetail, Title: textAreaDetail},
		{ID: areaRuns, Title: textAreaRuns},
		{ID: areaSettings, Title: textAreaSettings},
	}
}

// refreshInterval is how often the interface reads the service again on its own; the reload key reads at once.
const refreshInterval = 30 * time.Second

// The smallest terminal the frame is drawn in.
const (
	minWidth  = 40
	minHeight = 10
)

// productName stands at the left of the header.
const productName = "qdeploy"

// config is what a Model is built from.
type config struct {
	ctx      context.Context
	backend  Backend
	folders  Folders
	lang     i18n.Lang
	theme    string
	profile  kittui.ColorProfile
	charset  kittui.CharsetMode
	start    kittui.AreaID
	loc      *time.Location
	warnings []string
}

// tickMsg starts a periodic refresh.
type tickMsg struct{}

// Model is the state of the interface. An action reaches the service only after the person confirmed it; the
// periodic refresh and the mouse never trigger one.
type Model struct {
	ctx     context.Context
	backend Backend
	folders Folders
	keys    *kittui.Keymap
	text    kittui.Translator
	frame   *kittui.Frame
	loc     *time.Location

	themes          *kittui.ThemeSet
	theme           string
	profile         kittui.ColorProfile
	charset         kittui.CharsetMode
	terminal        kittui.TerminalColors
	haveFG, haveBG  bool
	styles          kittui.Styles
	width, height   int
	area            kittui.AreaID
	list            *kittui.List
	tones           map[string]kittui.TextStyle
	rows            map[kittui.ItemID]stackRow
	snap            snapshot
	loaded, loading bool
	// failure is the error of the last refresh; notice a warning that stays until the next key.
	failure, notice string
	// result is the success of a change without details; it stays until the next key.
	result string
	// chosen is the stack the detail area shows.
	chosen kittui.ItemID
	detail detailState
	// pick is the choice of a secret or a backup the detail shows.
	pick *picker
	// scrollNote is the position in the detail, empty while it fits.
	scrollNote string
	runsList   *kittui.List
	runTones   map[string]kittui.TextStyle
	runByID    map[kittui.ItemID]runRow
	runData    []runRow
	// runsOnly limits the Runs area to the chosen stack.
	runsOnly bool
	log      runLog
	// modal is the question or result that takes every key; run the sequence of calls under way. stale is set
	// when a refresh must follow the one under way.
	set              *settingsState
	modal            *modal
	run              *running
	modalGen, runGen int
	stale            bool
	// after starts a timer; tests replace it.
	after func(time.Duration, func(time.Time) tea.Msg) tea.Cmd
	// euid and makeDirs are the user the interface runs as and the creation of data directories; tests replace them.
	euid     func() int
	makeDirs func(datadir.Request) (datadir.Result, error)
	help     *kittui.Help
	helpOpen bool
}

func newModel(c config) (*Model, error) {
	keys, err := kittui.NewKeymap(keyTable(), c.charset)
	if err != nil {
		return nil, err
	}
	text, err := kittui.NewTranslator(c.lang, catalog(), keys)
	if err != nil {
		return nil, err
	}
	themes, err := kittui.NewThemeSet(nil)
	if err != nil {
		return nil, err
	}
	m := &Model{ctx: c.ctx, backend: c.backend, folders: c.folders, keys: keys, text: text, frame: kittui.NewFrame(keys, text),
		loc: c.loc, themes: themes, theme: c.theme, profile: c.profile, charset: c.charset, area: areaStacks,
		tones: map[string]kittui.TextStyle{}, rows: map[kittui.ItemID]stackRow{},
		runTones: map[string]kittui.TextStyle{}, runByID: map[kittui.ItemID]runRow{}, after: tea.Tick,
		euid: defaultEuid, makeDirs: defaultMakeDirs}
	if m.ctx == nil {
		m.ctx = context.Background()
	}
	if m.loc == nil {
		m.loc = time.Local
	}
	if slices.ContainsFunc(areas(), func(a kittui.Area) bool { return a.ID == c.start }) {
		m.area = c.start
	}
	m.list, err = kittui.NewList(kittui.ListSpec{Columns: stackColumns(text, m.tones), Keys: stackListKeys(), Ticks: true,
		Search: kittui.SearchByKey, Empty: textStacksEmpty}, keys, text)
	if err != nil {
		return nil, err
	}
	m.list.SetFocused(true)
	m.runsList, err = kittui.NewList(kittui.ListSpec{Columns: runColumns(text, m.runTones), Keys: listKeys(),
		Search: kittui.SearchByKey, Empty: textRunsEmpty}, keys, text)
	if err != nil {
		return nil, err
	}
	m.runsList.SetFocused(true)
	if m.set, err = newSettings(keys, text); err != nil {
		return nil, err
	}
	if !m.restyle() {
		m.notice = text.Lookup(kittui.TextWarning, text.Lookup(kittui.TextThemeUnknown, c.theme, kittui.ThemeTerminal))
	}
	if len(c.warnings) > 0 {
		m.notice = text.Lookup(kittui.TextWarning, c.warnings[0])
	}
	return m, nil
}

// restyle resolves the styles of the theme under the terminal; false when the theme is unknown.
func (m *Model) restyle() bool {
	var terminal *kittui.TerminalColors
	if m.haveFG && m.haveBG {
		terminal = &m.terminal
	}
	styles, known := m.themes.Resolve(m.theme, m.profile, m.charset, terminal)
	m.styles = styles
	return known
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.reload(), m.tick()}
	if m.themes.AsksTerminalColors(m.theme, m.profile) {
		cmds = append(cmds, tea.RequestForegroundColor, tea.RequestBackgroundColor)
	}
	return tea.Batch(cmds...)
}

func (m *Model) tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// reload reads the service again unless a refresh is under way.
func (m *Model) reload() tea.Cmd {
	if m.loading {
		return nil
	}
	m.loading = true
	ctx, backend := m.ctx, m.backend
	return func() tea.Msg { return fetch(ctx, backend) }
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		return m, tea.Batch(m.reload(), m.tick())
	case refreshMsg:
		m.apply(msg)
		if m.stale {
			m.stale = false
			return m, m.reload()
		}
	case stepMsg:
		return m, m.applyStep(msg)
	case previewMsg:
		return m, m.applyPreview(msg)
	case changelogMsg:
		m.applyChangelog(msg)
	case backupsMsg:
		m.applyBackups(msg)
	case secretsMsg:
		m.applySecrets(msg)
	case settingsLoadMsg:
		m.applySettingsLoad(msg)
	case settingMsg:
		return m, m.applySetting(msg)
	case tea.PasteMsg:
		if m.modal == nil && m.set.form != nil {
			m.set.form.form.Paste(msg.Content)
		}
	case logMsg:
		return m, m.applyLog(msg)
	case logTickMsg:
		return m, m.logTick(msg)
	case tea.ForegroundColorMsg:
		m.terminal.Foreground, m.haveFG = kittui.ColorOf(msg.Color), true
		m.restyle()
	case tea.BackgroundColorMsg:
		m.terminal.Background, m.haveBG = kittui.ColorOf(msg.Color), true
		m.restyle()
	case kittui.InputBatch:
		var cmds []tea.Cmd
		for _, sub := range msg {
			_, cmd := m.Update(sub)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	return m, nil
}

// apply takes a refresh. A failed one keeps the rows read before.
func (m *Model) apply(msg refreshMsg) {
	m.loading = false
	if msg.err != nil {
		m.failure = m.errorText(msg.err)
		return
	}
	m.snap, m.loaded, m.failure = msg.snap, true, ""
	if msg.problem != nil {
		m.failure = m.errorText(msg.problem)
	}
	rows := buildRows(msg.snap)
	ids := itemIDs(rows)
	clear(m.tones)
	clear(m.rows)
	items := make([]kittui.ListItem, len(rows))
	for i, r := range rows {
		items[i] = kittui.ListItem{ID: ids[i], Cells: cells(r, m.text, m.loc, m.tones)}
		m.rows[ids[i]] = r
	}
	if err := m.list.SetItems(items); err != nil {
		m.failure = m.text.Lookup(kittui.TextError, m.text.Lookup(textErrUnshownStacks, err.Error()))
	}
	m.rebuildRuns()
}

// errorText says in the message line why the service could not answer.
func (m *Model) errorText(err error) string {
	return m.text.Lookup(kittui.TextError, m.serviceText(err))
}

// serviceText says why the service could not do what was asked.
func (m *Model) serviceText(err error) string {
	t := m.text
	var remote *control.Error
	var text string
	switch {
	case errors.Is(err, client.ErrNotRunning):
		text = t.Lookup(textErrNotRunning)
	case errors.Is(err, client.ErrNoAccess), errors.As(err, &remote) && remote.Code == control.CodeNotRoot:
		text = t.Lookup(textErrNoAccess)
	case errors.As(err, &remote):
		text = t.Lookup(textErrService, remote.Message)
	default:
		text = t.Lookup(textErrChannel, err.Error())
	}
	return text
}

// input is the input focus of the screen.
func (m *Model) input() kittui.InputFocus {
	if m.helpOpen || m.modal != nil {
		return kittui.InputNavigation
	}
	switch m.area {
	case areaDetail:
		switch {
		case m.set.form != nil:
			return m.set.form.form.Input()
		case m.pick != nil:
			return m.pick.list.Input()
		}
	case areaSettings:
		return m.settingsInput()
	case areaStacks:
		return m.list.Input()
	case areaRuns:
		if m.log.open {
			return m.log.view.Input()
		}
		return m.runsList.Input()
	}
	return kittui.InputNavigation
}

// typed gives a key to the part that has the text focus; handled is false when it let the key pass.
func (m *Model) typed(msg tea.KeyPressMsg) (cmd tea.Cmd, handled bool) {
	switch {
	case m.area == areaStacks:
		open, handled := m.list.Update(msg)
		return m.open(open), handled
	case m.area == areaSettings:
		return m.settingsTyped(msg)
	case m.area == areaDetail && m.pick != nil:
		open, handled := m.pick.list.Update(msg)
		return m.picked(open), handled
	case m.area == areaRuns && m.log.open:
		return nil, m.log.view.Update(msg)
	case m.area == areaRuns:
		open, handled := m.runsList.Update(msg)
		return m.openLog(open), handled
	}
	return nil, false
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	m.notice, m.result = "", ""
	if m.modal != nil {
		return m.modalKey(msg)
	}
	if (m.area == areaSettings || m.area == areaDetail) && m.set.form != nil {
		if m.set.busy {
			if id, ok := m.keys.Match(msg, kittui.InputNavigation); ok && id == actForceQuit {
				return tea.Quit
			}
			return nil
		}
		return m.formKey(msg)
	}
	if m.helpOpen {
		if act, handled := m.help.Update(msg); handled {
			if act != "" {
				m.helpOpen = false
			}
			return nil
		}
		if id, ok := m.keys.Match(msg, kittui.InputNavigation); ok && (id == actQuit || id == actForceQuit) {
			return tea.Quit
		}
		return nil
	}
	focus := m.input()
	if focus == kittui.InputText {
		if cmd, handled := m.typed(msg); handled {
			return cmd
		}
	}
	if id, ok := m.keys.Match(msg, focus); ok {
		switch id {
		case actQuit, actForceQuit:
			return tea.Quit
		case actHelp:
			m.openHelp()
			return nil
		case actReload:
			return m.reloadAll()
		case actArea:
			if i := slices.Index(areaKeys, kittui.KeyName(msg)); i >= 0 {
				return m.enter(areas()[i].ID)
			}
			return nil
		case actStackOnly:
			if m.area == areaRuns && !m.log.open {
				m.toggleStackOnly()
				return nil
			}
		case actBind, actFetch, actGenerate, actAdd, actRemove, actEdit, actTest, actEnter, actRestore, actDataDirs:
			switch m.area {
			case areaSettings:
				if cmd, handled := m.settingsAction(id); handled {
					return cmd
				}
			case areaDetail:
				if cmd, handled := m.detailAction(id); handled {
					return cmd
				}
			}
		case actUpdate, actDeploy, actPause, actUnpause, actPin, actUnpin, actResume, actCheck:
			if m.area == areaStacks {
				return m.beginAction(actionKinds[id])
			}
		case actBack:
			switch m.area {
			case areaSettings:
				cmd, _ := m.settingsAction(actBack)
				return cmd
			case areaDetail:
				if m.pick != nil {
					if _, handled := m.pick.list.Update(msg); !handled {
						m.pick = nil
					}
					return nil
				}
				return m.enter(areaStacks)
			case areaRuns:
				switch {
				case m.log.open:
					if !m.log.view.Update(msg) {
						m.log.open = false
					}
				default:
					if _, handled := m.runsList.Update(msg); !handled {
						return m.enter(areaStacks)
					}
				}
				return nil
			}
		}
		if m.area == areaDetail && m.pick == nil {
			m.scrollDetail(id)
			return nil
		}
	}
	switch {
	case m.area == areaStacks:
		open, _ := m.list.Update(msg)
		return m.open(open)
	case m.area == areaRuns && m.log.open:
		m.log.view.Update(msg)
	case m.area == areaRuns:
		open, _ := m.runsList.Update(msg)
		return m.openLog(open)
	case m.area == areaSettings:
		cmd, _ := m.settingsTyped(msg)
		return cmd
	case m.area == areaDetail && m.pick != nil:
		open, _ := m.pick.list.Update(msg)
		return m.picked(open)
	}
	return nil
}

// reloadAll reads the service again; the detail and an open log are read again with it.
func (m *Model) reloadAll() tea.Cmd {
	cmds := []tea.Cmd{m.reload()}
	switch {
	case m.area == areaDetail:
		cmds = append(cmds, m.loadDetail(true))
	case m.area == areaSettings:
		cmds = append(cmds, m.loadPage(true))
	case m.area == areaRuns && m.log.open && !m.log.busy:
		m.log.busy = true
		cmds = append(cmds, m.readLog())
	}
	return tea.Batch(cmds...)
}

// enter shows an area and starts what that area reads on its own.
func (m *Model) enter(area kittui.AreaID) tea.Cmd {
	if area != areaDetail {
		m.pick = nil
	}
	m.area = area
	switch area {
	case areaDetail:
		return m.loadDetail(false)
	case areaRuns:
		return m.followLog()
	case areaSettings:
		return m.loadPage(false)
	}
	return nil
}

// open shows the stack id in the detail area.
func (m *Model) open(id kittui.ItemID) tea.Cmd {
	if id == "" {
		return nil
	}
	m.chosen = id
	return m.enter(areaDetail)
}

func (m *Model) openHelp() {
	help, err := kittui.NewHelp(kittui.HelpSpec{Topics: helptopics.All(), Screen: m.helpScreen(),
		Global: []kittui.ActionID{actHelp, actArea, actReload, actBack, actQuit}, Keys: helpKeys()}, m.keys, m.text)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return
	}
	m.help, m.helpOpen = help, true
}

// footer is the key line of the screen.
func (m *Model) footer() []kittui.Hint {
	if m.modal != nil {
		if m.modal.sheet() {
			return m.modalFooter()
		}
		// The keys of the screen behind a question do nothing; the question shows its own.
		return nil
	}
	nav := []kittui.Hint{
		{Action: actUp, Label: kittui.TextKeyChoose, Kind: kittui.HintNavigation},
		{Action: actDown, Label: kittui.TextKeyChoose, Kind: kittui.HintNavigation},
	}
	common := []kittui.Hint{
		{Action: actReload, Label: kittui.TextKeyReload, Priority: 2},
		{Action: actArea, Label: textKeyArea, Priority: 3},
		{Action: actQuit, Label: kittui.TextKeyQuit, Priority: 1},
	}
	switch {
	case m.area == areaSettings:
		return m.settingsFooter(common, nav)
	case m.area == areaDetail && m.set.form != nil:
		return append(m.set.form.form.Hints(), kittui.Hint{Action: actBack, Label: textKeyCancelForm})
	case m.area == areaDetail && m.pick != nil:
		return append(append([]kittui.Hint{{Action: actEnter, Label: textKeyPick}, {Action: actBack, Label: kittui.TextKeyBack, Priority: 1}},
			common...), nav...)
	case m.area == areaDetail:
		scroll := []kittui.Hint{
			{Action: actUp, Label: kittui.TextHelpScroll, Kind: kittui.HintNavigation},
			{Action: actDown, Label: kittui.TextHelpScroll, Kind: kittui.HintNavigation},
		}
		if _, ok := m.rows[m.chosen]; !ok {
			return common
		}
		return append(append(m.detailHints(), common...), scroll...)
	case m.area == areaRuns && m.log.open:
		if m.log.view.Searching() {
			return m.log.view.Hints()
		}
		return append(m.log.view.Hints(), common...)
	case m.area == areaRuns:
		return m.listFooter(m.runsList, len(m.runByID), textKeyLog, append(
			[]kittui.Hint{m.stackOnlyHint()}, common...), nav)
	case m.area == areaStacks:
		if len(m.rows) > 0 {
			common = append(m.actionHints(), common...)
		}
		return m.listFooter(m.list, len(m.rows), textKeyDetails, common, nav)
	}
	return common
}

// helpScreen are the keys the help lists for the screen it was opened from.
func (m *Model) helpScreen() []kittui.Hint {
	hints := m.footer()
	if m.area == areaStacks && len(m.rows) > 0 {
		hints = append(hints, m.moreActionHints()...)
	}
	return hints
}

func (m *Model) stackOnlyHint() kittui.Hint {
	label := textKeyStackOnly
	if m.runsOnly {
		label = textKeyAllStacks
	}
	return kittui.Hint{Action: actStackOnly, Label: label, Priority: 1}
}

// listFooter is the key line of a screen that is a list: enter opens the marked row.
func (m *Model) listFooter(list *kittui.List, rows int, enter kittui.TextID, common, nav []kittui.Hint) []kittui.Hint {
	if rows == 0 {
		return common
	}
	if list.Searching() {
		return []kittui.Hint{
			{Action: actEnter, Label: textFilterKeep},
			{Action: actBack, Label: textFilterClear},
		}
	}
	hints := []kittui.Hint{
		{Action: actEnter, Label: enter},
		{Action: actSearch, Label: kittui.TextKeySearch, Priority: 1},
	}
	if list.Query() != "" {
		hints = append(hints, kittui.Hint{Action: actBack, Label: textFilterClear})
	}
	return append(append(hints, common...), nav...)
}

// marks are the status marks of the header.
func (m *Model) marks() []kittui.StatusMark {
	t := m.text
	var marks []kittui.StatusMark
	if m.run != nil {
		marks = append(marks, m.busyMark())
	}
	if m.set.busy {
		marks = append(marks, m.settingsMark())
	}
	if m.loading {
		marks = append(marks, kittui.StatusMark{Kind: kittui.StatusBusy, Text: t.Lookup(textHeaderRefreshing)})
	}
	if !m.loaded {
		return marks
	}
	if m.snap.status.Engine.PollError != "" {
		marks = append(marks, kittui.StatusMark{Kind: kittui.StatusWarn, Text: t.Lookup(textHeaderPollFailed),
			Short: t.Lookup(textHeaderPollFailedS)})
	}
	if w := m.snap.status.Window; w != nil {
		if w.Open {
			marks = append(marks, kittui.StatusMark{Kind: kittui.StatusOK, Text: t.Lookup(textHeaderWindowOpen),
				Short: t.Lookup(textHeaderWindowOpenS)})
		} else {
			marks = append(marks, kittui.StatusMark{Text: t.Lookup(textHeaderWindowShut), Short: t.Lookup(textHeaderWindowShutS)})
		}
	}
	return marks
}

func (m *Model) message() *kittui.Message {
	switch {
	case m.failure != "":
		return &kittui.Message{Kind: kittui.MessageError, Text: m.failure}
	case m.notice != "":
		return &kittui.Message{Kind: kittui.MessageWarning, Text: m.notice}
	case m.result != "":
		return &kittui.Message{Kind: kittui.MessageResult, Text: m.result}
	}
	return nil
}

func (m *Model) frameSpec() kittui.FrameSpec {
	spec := kittui.FrameSpec{
		Width: m.width, Height: m.height, Product: productName, Status: m.marks(),
		HeaderKeys: []kittui.Hint{{Action: actHelp, Label: kittui.TextKeyHelp}},
		Areas:      areas(), ActiveArea: m.area, Focus: kittui.ZoneRows, Input: m.input(),
		Message: m.message(), Footer: m.footer(),
	}
	switch {
	case m.area == areaStacks:
		spec.Position = m.list.Position()
	case m.area == areaRuns && !m.log.open:
		spec.Position = m.runsList.Position()
		spec.Back = &kittui.Hint{Action: actBack, Label: kittui.TextKeyBack}
	case m.area == areaSettings:
		spec.Position = m.settingsPosition()
		spec.Back = &kittui.Hint{Action: actBack, Label: kittui.TextKeyBack}
	default:
		spec.Back = &kittui.Hint{Action: actBack, Label: kittui.TextKeyBack}
	}
	return spec
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// render draws the whole screen in the size of the terminal.
func (m *Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return m.styles.Paint(m.resizeView(), m.width, m.height)
	}
	spec := m.frameSpec()
	room := m.frame.Workspace(spec)
	spec.Workspace = m.workspace(room.Width, room.Height)
	if m.area == areaDetail || m.modal != nil && m.modal.sheet() {
		spec.Position = m.scrollNote
	}
	view, _ := m.frame.Render(m.styles, spec)
	if m.modal != nil && !m.modal.sheet() {
		if over, _, ok := m.modal.dialog.Overlay(m.styles, view, m.width, m.height, room); ok {
			view = over
		} else {
			spec.Footer, spec.Input, spec.Position, spec.Back = m.modal.dialog.Hints(), kittui.InputNavigation, "", nil
			room = m.frame.Workspace(spec)
			spec.Workspace, _ = m.modal.dialog.View(m.styles, room.Width, room.Height)
			view, _ = m.frame.Render(m.styles, spec)
		}
	}
	if m.helpOpen {
		if over, _, ok := m.help.Overlay(m.styles, view, m.width, m.height, room); ok {
			view = over
		} else {
			spec.Footer, spec.Input, spec.Position, spec.Back = m.help.Hints(), kittui.InputNavigation, "", nil
			room = m.frame.Workspace(spec)
			spec.Workspace, _ = m.help.View(m.styles, room.Width, room.Height)
			view, _ = m.frame.Render(m.styles, spec)
		}
	}
	return m.styles.Paint(view, m.width, m.height)
}

func (m *Model) resizeView() string {
	t := m.text
	lines := []string{
		m.styles.Render(kittui.StyleTitle, t.Lookup(kittui.TextResizeTitle)),
		t.Lookup(kittui.TextResizeNeed, minWidth, minHeight),
		t.Lookup(kittui.TextResizeHave, m.width, m.height),
	}
	for i, l := range lines {
		lines[i] = kittui.Clip(l, m.width)
	}
	return strings.Join(lines[:min(len(lines), m.height)], "\n")
}

// workspace draws the content of the active area in width by height cells.
func (m *Model) workspace(width, height int) string {
	if m.modal != nil && m.modal.sheet() {
		return m.modalView(width, height)
	}
	if m.area == areaSettings {
		return m.settingsView(width, height)
	}
	if m.area == areaDetail && m.set.form != nil {
		return m.formView(width, height)
	}
	if !m.loaded {
		return m.note(m.text.Lookup(textStacksLoading), width)
	}
	switch m.area {
	case areaDetail:
		if m.pick != nil {
			m.scrollNote = ""
			return m.pickView(width, height)
		}
		return m.detailView(width, height)
	case areaRuns:
		return m.runsView(width, height)
	}
	view, _ := m.list.View(m.styles, width, height)
	return view
}

// note is a dimmed sentence wrapped into width cells and indented like the rows of a list.
func (m *Model) note(text string, width int) string {
	lines := kittui.Wrap(kittui.Escape(text), max(width-2, 1))
	for i, l := range lines {
		lines[i] = "  " + m.styles.Render(kittui.StyleHint, l)
	}
	return strings.Join(lines, "\n")
}
