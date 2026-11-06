package tui

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// logInterval is the pause between two reads of the log of an active run, counted from the end of the
// previous read, so reads never overlap.
const logInterval = time.Second

// logBufferLines is the log view's buffer: more than a run log of the service's size limit can hold.
const logBufferLines = 20000

// runIDLayout is the time at the start of a run ID.
const runIDLayout = "20060102T150405Z"

// runRow is one run as the Runs area shows it: a recorded one, or an active one that only the status knows.
type runRow struct {
	id, stack, trigger, result, err string
	started                         time.Time
	duration                        time.Duration
	active                          bool
}

// runRows joins the recorded runs with the active runs of the status, newest first. An active run is not
// recorded before it ends; one that is both keeps its record's values and counts as active.
func runRows(s snapshot) []runRow {
	var rows []runRow
	byID := map[string]int{}
	for _, r := range s.runs.Runs {
		byID[r.ID] = len(rows)
		rows = append(rows, runRow{id: r.ID, stack: r.Stack, trigger: r.Trigger, result: r.Result, err: r.Error,
			started: r.Started, duration: time.Duration(r.DurationMS) * time.Millisecond})
	}
	for _, st := range s.status.Engine.Stacks {
		if st.ActiveRun == "" {
			continue
		}
		i, ok := byID[st.ActiveRun]
		if !ok {
			i = len(rows)
			started, _ := time.Parse(runIDLayout, st.ActiveRun[:min(len(st.ActiveRun), len(runIDLayout))])
			rows = append(rows, runRow{id: st.ActiveRun, stack: st.Name, started: started})
		}
		rows[i].active = true
	}
	slices.SortStableFunc(rows, func(a, b runRow) int {
		if c := b.started.Compare(a.started); c != 0 {
			return c
		}
		return strings.Compare(b.id, a.id)
	})
	return rows
}

func runColumns(t kittui.Translator, tones map[string]kittui.TextStyle) []kittui.Column {
	tone := func(cell string) kittui.TextStyle {
		if s, ok := tones[cell]; ok {
			return s
		}
		return kittui.StyleText
	}
	return []kittui.Column{
		{Title: textColStack},
		{Title: textColStarted},
		{Title: textColResult, Tone: tone},
		{Title: textColTrigger, Optional: true},
		{Title: textColDuration, Optional: true},
		{Title: textColRun, Flex: true, Optional: true},
	}
}

var resultTones = map[string]kittui.TextStyle{
	"succeeded": kittui.StyleOK, "failed": kittui.StyleFail, "rolled-back": kittui.StyleFail,
	"rollback-failed": kittui.StyleFail,
}

// runResult is the result word of a run and its tone; an active run reads as running.
func (m *Model) runResult(r runRow) (string, kittui.TextStyle) {
	t := m.text
	switch {
	case r.active:
		return t.Lookup(textStateRunning), kittui.StyleActive
	case r.result == "":
		return t.Lookup(textNoValue), kittui.StyleHint
	}
	word := r.result
	if id, ok := resultTexts[word]; ok {
		word = t.Lookup(id)
	}
	if tone, ok := resultTones[r.result]; ok {
		return word, tone
	}
	return word, kittui.StyleText
}

// runCells translates a run into the cells of runColumns.
func (m *Model) runCells(r runRow) []string {
	none := m.text.Lookup(textNoValue)
	started, trigger, duration := none, none, none
	if !r.started.IsZero() {
		started = r.started.In(m.loc).Format(lastRunLayout)
	}
	if r.trigger != "" {
		trigger = r.trigger
	}
	if !r.active && r.result != "" {
		duration = formatDuration(r.duration)
	}
	result, tone := m.runResult(r)
	m.runTones[result] = tone
	return []string{r.stack, started, result, trigger, duration, r.id}
}

// rebuildRuns fills the runs list from the runs read last, limited to the chosen stack when the limit is on.
func (m *Model) rebuildRuns() {
	m.runData = runRows(m.snap)
	clear(m.runTones)
	clear(m.runByID)
	shown := m.runData
	if m.runsOnly {
		shown = nil
		for _, r := range m.runData {
			if r.stack == m.chosenName() {
				shown = append(shown, r)
			}
		}
	}
	names := make([]string, len(shown))
	for i, r := range shown {
		names[i] = r.id
	}
	ids := uniqueIDs(names)
	items := make([]kittui.ListItem, len(shown))
	for i, r := range shown {
		items[i] = kittui.ListItem{ID: ids[i], Cells: m.runCells(r)}
		m.runByID[ids[i]] = r
	}
	if err := m.runsList.SetItems(items); err != nil {
		m.failure = m.text.Lookup(kittui.TextError, m.text.Lookup(textErrUnshownStacks, err.Error()))
	}
	if m.log.open {
		// The record of a run that has ended replaces what the status alone knew; the log keeps its own view of
		// whether the run is active.
		if i := slices.IndexFunc(m.runData, func(r runRow) bool { return r.id == m.log.row.id }); i >= 0 {
			m.log.row = m.runData[i]
			m.log.row.active = m.log.active
		}
	}
}

// chosenName is the name of the stack chosen in Stacks, empty when none is.
func (m *Model) chosenName() string { return m.rows[m.chosen].name }

// toggleStackOnly limits the runs to the chosen stack or lifts the limit.
func (m *Model) toggleStackOnly() {
	if !m.runsOnly && m.chosenName() == "" {
		m.notice = m.text.Lookup(kittui.TextWarning, m.text.Lookup(textRunsNoStack))
		return
	}
	m.runsOnly = !m.runsOnly
	m.rebuildRuns()
}

// runLog is the log of the run the Runs area shows. It follows the run while the status lists it as active.
type runLog struct {
	open bool
	row  runRow
	view *kittui.LogView
	// consumed is the part of the log text the view holds: complete lines only while the run is active.
	consumed string
	loaded   bool
	err      error
	active   bool
	// busy is set while a read is under way, pending while the timer of the next read runs.
	busy, pending bool
	// gen tells the answers of a run that was left from those of the current one.
	gen int
}

type logMsg struct {
	gen    int
	text   string
	active bool
	err    error
}

type logTickMsg struct{ gen int }

// openLog shows the log of a run, read at once; a running one is then followed.
func (m *Model) openLog(id kittui.ItemID) tea.Cmd {
	row, ok := m.runByID[id]
	if !ok {
		return nil
	}
	return m.showLog(row)
}

// showLog shows the log of row, read at once.
func (m *Model) showLog(row runRow) tea.Cmd {
	view, err := m.newLogView(kittui.LogFilter{})
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return nil
	}
	m.log = runLog{open: true, row: row, view: view, active: row.active, busy: true, gen: m.log.gen + 1}
	return m.readLog()
}

// newLogView builds a log view that starts with the period filter.
func (m *Model) newLogView(filter kittui.LogFilter) (*kittui.LogView, error) {
	return kittui.NewLogView(kittui.LogViewSpec{Lines: logBufferLines, Keys: logKeys(), Filter: filter, Now: m.now,
		Units: []kittui.LogFilterUnit{kittui.LogUnitMinutes, kittui.LogUnitHours, kittui.LogUnitDays}}, m.keys, m.text)
}

// refeedFilteredLog fills the view again after the person chose another period. The log is the one already
// read; the view only holds the lines inside the period.
func (m *Model) refeedFilteredLog() {
	l := &m.log
	if !l.open || l.view == nil {
		return
	}
	if _, changed := l.view.TakeFilterChange(); !changed {
		return
	}
	l.view.Clear()
	m.appendLog(l.consumed)
}

// readLog reads the status and then the log of the open run. The status comes first: a run that is not active
// in it has written its last line before the log is read.
func (m *Model) readLog() tea.Cmd {
	ctx, b, gen, id := m.ctx, m.backend, m.log.gen, m.log.row.id
	return func() tea.Msg {
		st, err := b.Status(ctx)
		if err != nil {
			return logMsg{gen: gen, err: err}
		}
		active := slices.ContainsFunc(st.Engine.Stacks, func(s control.StackStatus) bool { return s.ActiveRun == id })
		res, err := b.Log(ctx, id)
		return logMsg{gen: gen, text: res.Text, active: active, err: err}
	}
}

// applyLog takes a read of the log: it appends the lines that are new and decides whether to read on.
func (m *Model) applyLog(msg logMsg) tea.Cmd {
	l := &m.log
	if !l.open || msg.gen != l.gen {
		return nil
	}
	l.busy, l.loaded = false, true
	if l.err = msg.err; msg.err != nil {
		l.active, l.row.active = false, false
		return nil
	}
	was := l.active
	l.active, l.row.active = msg.active, msg.active
	m.feedLog(msg.text)
	if was && !l.active {
		// The run ended: the stack row and the run list change with it.
		return m.reload()
	}
	return m.followLog()
}

// feedLog hands the lines that arrived since the last read to the view. A line still being written is held
// back until its end arrives or the run has ended, so no line is drawn twice or in halves.
func (m *Model) feedLog(text string) {
	l := &m.log
	if l.active {
		text = text[:strings.LastIndexByte(text, '\n')+1]
	}
	if !strings.HasPrefix(text, l.consumed) {
		// The log was rewritten; start over rather than guess what is new.
		if view, err := m.newLogView(l.view.Filter()); err == nil {
			l.view, l.consumed = view, ""
		}
	}
	delta := text[len(l.consumed):]
	if delta == "" {
		return
	}
	l.consumed = text
	m.appendLog(delta)
}

// appendLog hands complete log lines to the view, which drops those outside its period.
func (m *Model) appendLog(delta string) {
	if delta == "" {
		return
	}
	var lines []kittui.LogLine
	for _, raw := range strings.Split(strings.TrimSuffix(delta, "\n"), "\n") {
		lines = append(lines, m.logLine(raw))
	}
	m.log.view.Append(lines...)
}

// logLine splits a line of the service's run log into its time, its kind and its text.
func (m *Model) logLine(raw string) kittui.LogLine {
	raw = strings.TrimSuffix(raw, "\r")
	stamp, rest, ok := strings.Cut(raw, " ")
	at, err := time.Parse(time.RFC3339, stamp)
	if !ok || err != nil {
		return kittui.LogLine{Text: raw}
	}
	kind, text, ok := strings.Cut(rest, " ")
	if !ok || kind == "" || len(kind) > 12 {
		return kittui.LogLine{Text: rest, Time: at.In(m.loc)}
	}
	return kittui.LogLine{Text: text, Time: at.In(m.loc), Source: kind}
}

// followLog arms the timer of the next read while the Runs area shows the log of an active run. At most one
// timer and one read exist at a time.
func (m *Model) followLog() tea.Cmd {
	l := &m.log
	if !l.open || !l.active || l.busy || l.pending || m.area != areaRuns {
		return nil
	}
	l.pending = true
	gen := l.gen
	return m.after(logInterval, func(time.Time) tea.Msg { return logTickMsg{gen: gen} })
}

// logTick runs the read the timer stands for, if it is still wanted.
func (m *Model) logTick(msg logTickMsg) tea.Cmd {
	l := &m.log
	if msg.gen != l.gen {
		return nil
	}
	l.pending = false
	if !l.open || !l.active || l.busy || m.area != areaRuns {
		return nil
	}
	l.busy = true
	return m.readLog()
}

// runsView draws the Runs area: the list of runs, or the log of the run chosen in it.
func (m *Model) runsView(width, height int) string {
	if m.log.open {
		return m.logPane(width, height)
	}
	if !m.runsOnly {
		return m.listView(m.runsList, width, height, 0)
	}
	note := m.note(m.text.Lookup(textRunsOnly, m.chosenName()), width)
	rows := strings.Count(note, "\n") + 1
	view := m.listView(m.runsList, width, max(height-rows, 1), rows)
	return note + "\n" + view
}

func (m *Model) logPane(width, height int) string {
	l := &m.log
	state, _ := m.runResult(l.row)
	lines := []string{"  " + m.styles.TitleLine(l.row.stack, state+"  "+l.row.id, max(width-2, 1))}
	switch {
	case l.err != nil:
		lines = append(lines, m.wrapped(2, kittui.StyleFail, m.errorText(l.err), width)...)
	case !l.loaded:
		lines = append(lines, m.line(2, kittui.StyleHint, m.text.Lookup(textLogLoading), width))
	}
	room := max(height-len(lines), 1)
	view, _ := l.view.View(m.styles, width, room)
	return strings.Join(lines, "\n") + "\n" + view
}
