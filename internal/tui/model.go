package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/helptopics"
)

// The areas of the tab line, in its order.
const (
	areaStacks kittui.AreaID = "stacks"
	areaDetail kittui.AreaID = "detail"
	areaRuns   kittui.AreaID = "runs"
)

func areas() []kittui.Area {
	return []kittui.Area{
		{ID: areaStacks, Title: textAreaStacks},
		{ID: areaDetail, Title: textAreaDetail},
		{ID: areaRuns, Title: textAreaRuns},
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

// Model is the state of the interface. It reads only; no key triggers an effect in the service.
type Model struct {
	ctx     context.Context
	backend Backend
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
	// chosen is the stack the detail area shows.
	chosen   kittui.ItemID
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
	m := &Model{ctx: c.ctx, backend: c.backend, keys: keys, text: text, frame: kittui.NewFrame(keys, text),
		loc: c.loc, themes: themes, theme: c.theme, profile: c.profile, charset: c.charset, area: areaStacks,
		tones: map[string]kittui.TextStyle{}, rows: map[kittui.ItemID]stackRow{}}
	if m.ctx == nil {
		m.ctx = context.Background()
	}
	if m.loc == nil {
		m.loc = time.Local
	}
	if slices.ContainsFunc(areas(), func(a kittui.Area) bool { return a.ID == c.start }) {
		m.area = c.start
	}
	m.list, err = kittui.NewList(kittui.ListSpec{Columns: stackColumns(text, m.tones), Keys: listKeys(),
		Search: kittui.SearchByKey, Empty: textStacksEmpty}, keys, text)
	if err != nil {
		return nil, err
	}
	m.list.SetFocused(true)
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
}

// errorText says in the message line why the service could not answer.
func (m *Model) errorText(err error) string {
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
	return t.Lookup(kittui.TextError, text)
}

// input is the input focus of the screen.
func (m *Model) input() kittui.InputFocus {
	if m.area == areaStacks && !m.helpOpen {
		return m.list.Input()
	}
	return kittui.InputNavigation
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	m.notice = ""
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
	if m.area == areaStacks && focus == kittui.InputText {
		if open, handled := m.list.Update(msg); handled {
			m.open(open)
			return nil
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
			return m.reload()
		case actArea:
			if i := slices.Index(areaKeys, kittui.KeyName(msg)); i >= 0 {
				m.area = areas()[i].ID
			}
			return nil
		case actBack:
			if m.area != areaStacks {
				m.area = areaStacks
				return nil
			}
		}
	}
	if m.area == areaStacks {
		open, _ := m.list.Update(msg)
		m.open(open)
	}
	return nil
}

// open shows the stack id in the detail area.
func (m *Model) open(id kittui.ItemID) {
	if id == "" {
		return
	}
	m.chosen, m.area = id, areaDetail
}

func (m *Model) openHelp() {
	help, err := kittui.NewHelp(kittui.HelpSpec{Topics: helptopics.All(), Screen: m.footer(),
		Global: []kittui.ActionID{actHelp, actArea, actReload, actBack, actQuit}, Keys: helpKeys()}, m.keys, m.text)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return
	}
	m.help, m.helpOpen = help, true
}

// footer is the key line of the screen.
func (m *Model) footer() []kittui.Hint {
	nav := []kittui.Hint{
		{Action: actUp, Label: kittui.TextKeyChoose, Kind: kittui.HintNavigation},
		{Action: actDown, Label: kittui.TextKeyChoose, Kind: kittui.HintNavigation},
	}
	common := []kittui.Hint{
		{Action: actReload, Label: kittui.TextKeyReload, Priority: 2},
		{Action: actArea, Label: textKeyArea, Priority: 3},
		{Action: actQuit, Label: kittui.TextKeyQuit, Priority: 1},
	}
	if m.area != areaStacks || len(m.rows) == 0 {
		return common
	}
	if m.list.Searching() {
		return []kittui.Hint{
			{Action: actEnter, Label: textFilterKeep},
			{Action: actBack, Label: textFilterClear},
		}
	}
	hints := []kittui.Hint{
		{Action: actEnter, Label: textKeyDetails},
		{Action: actSearch, Label: kittui.TextKeySearch, Priority: 1},
	}
	if m.list.Query() != "" {
		hints = append(hints, kittui.Hint{Action: actBack, Label: textFilterClear})
	}
	return append(append(hints, common...), nav...)
}

// marks are the status marks of the header.
func (m *Model) marks() []kittui.StatusMark {
	t := m.text
	var marks []kittui.StatusMark
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
	if m.area == areaStacks {
		spec.Position = m.list.Position()
	} else {
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
	view, _ := m.frame.Render(m.styles, spec)
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
	if !m.loaded {
		return m.note(m.text.Lookup(textStacksLoading), width)
	}
	switch m.area {
	case areaDetail:
		return m.detailView(width)
	case areaRuns:
		return m.note(m.text.Lookup(textRunsRecorded, len(m.snap.runs.Runs)), width)
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

// detailView is the stack chosen in the Stacks area with the values of its row.
func (m *Model) detailView(width int) string {
	r, ok := m.rows[m.chosen]
	if !ok {
		return m.note(m.text.Lookup(textDetailNone), width)
	}
	values := cells(r, m.text, m.loc, map[string]kittui.TextStyle{})
	columns := stackColumns(m.text, nil)
	labels := make([]string, len(columns))
	labelWidth := 0
	for i, c := range columns {
		labels[i] = kittui.Escape(m.text.Lookup(c.Title))
		labelWidth = max(labelWidth, lipgloss.Width(labels[i]))
	}
	lines := []string{"  " + m.styles.TitleLine(r.name, "", max(width-2, 1)), ""}
	for i := 1; i < len(columns); i++ {
		lines = append(lines, "  "+m.styles.Render(kittui.StyleHint, kittui.Pad(labels[i], labelWidth))+"  "+
			m.styles.Render(kittui.StyleText, kittui.Escape(values[i])))
	}
	return strings.Join(lines, "\n")
}
