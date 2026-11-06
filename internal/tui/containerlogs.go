package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// clogTail is how many of the last lines the first read of a container log asks for.
const clogTail = 500

// containerLog is the output of one container, or of all containers of a stack, shown in the area it was
// opened from.
type containerLog struct {
	open bool
	// area is where the log was opened: the Containers area or the stack detail.
	area kittui.AreaID
	// name is the container, stack the stack whose containers are merged; exactly one is set.
	name, stack string
	view        *kittui.LogView
	loaded      bool
	err         error
	// cursor is where the next read continues; zero until a line has arrived.
	cursor time.Time
	// busy is set while a read is under way, pending while the timer of the next read runs.
	busy, pending bool
	// gen tells the answers of a log that was left or reloaded from those of the current one.
	gen int
}

type containerLogMsg struct {
	gen int
	res control.ContainerLogsResult
	err error
}

type containerLogTickMsg struct{ gen int }

// clogShown reports whether a container log is what the active area shows.
func (m *Model) clogShown() bool { return m.clog.open && m.area == m.clog.area }

// openContainerLogs opens the log of the container the Containers area marks or shows, or of the stack the
// stack detail shows; ok is false when the key means nothing on this screen.
func (m *Model) openContainerLogs() (cmd tea.Cmd, ok bool) {
	switch {
	case m.area == areaContainers && m.ctv.open:
		return m.showContainerLog(m.ctv.name, ""), true
	case m.area == areaContainers:
		id, found := m.ctList.Selected()
		if name, known := m.ctNames[id]; found && known {
			return m.showContainerLog(name, ""), true
		}
	case m.area == areaDetail && m.pick == nil:
		if stack, found := m.detailStack(); found {
			return m.showContainerLog("", stack), true
		}
	}
	return nil, false
}

// showContainerLog opens the log view and asks for the last lines.
func (m *Model) showContainerLog(name, stack string) tea.Cmd {
	view, err := m.newLogView(kittui.LogFilter{})
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return nil
	}
	m.clog = containerLog{open: true, area: m.area, name: name, stack: stack, view: view, gen: m.clog.gen + 1}
	return m.readContainerLog()
}

// closeContainerLog leaves the log; answers and timers still under way are dropped by the generation.
func (m *Model) closeContainerLog() { m.clog = containerLog{gen: m.clog.gen + 1} }

// containerLogArgs asks for the last lines of the period, or, once lines have arrived, for those after them.
func (m *Model) containerLogArgs() control.ContainerLogsArgs {
	l := &m.clog
	args := control.ContainerLogsArgs{Name: l.name, Stack: l.stack}
	if !l.cursor.IsZero() {
		args.Cursor = l.cursor
		return args
	}
	args.Since, args.Until = l.view.Filter().Bounds(m.now())
	args.Tail = clogTail
	return args
}

// readContainerLog starts a read; the caller has checked that none is under way.
func (m *Model) readContainerLog() tea.Cmd {
	l := &m.clog
	l.busy = true
	ctx, b, gen, args := m.ctx, m.backend, l.gen, m.containerLogArgs()
	return func() tea.Msg {
		res, err := b.ContainerLogs(ctx, args)
		return containerLogMsg{gen: gen, res: res, err: err}
	}
}

// applyContainerLog takes a page: it appends its lines, reads the next page at once when the service holds
// more back, and otherwise leaves the next read to the timer.
func (m *Model) applyContainerLog(msg containerLogMsg) tea.Cmd {
	l := &m.clog
	if !l.open || msg.gen != l.gen {
		return nil
	}
	l.busy, l.loaded = false, true
	if l.err = msg.err; msg.err != nil {
		return nil
	}
	lines := make([]kittui.LogLine, len(msg.res.Lines))
	for i, ln := range msg.res.Lines {
		lines[i] = kittui.LogLine{Text: ln.Text, Time: ln.Time.In(m.loc)}
		if l.stack != "" {
			lines[i].Source = ln.Source
		}
	}
	l.view.Append(lines...)
	if !msg.res.Cursor.IsZero() {
		l.cursor = msg.res.Cursor
	}
	if msg.res.More {
		return m.readContainerLog()
	}
	return m.followContainerLog()
}

// followContainerLog arms the timer of the next read while the log is shown and following. At most one timer
// and one read exist at a time.
func (m *Model) followContainerLog() tea.Cmd {
	l := &m.clog
	if !m.clogShown() || l.err != nil || !l.view.Following() || l.busy || l.pending {
		return nil
	}
	l.pending = true
	gen := l.gen
	return m.after(logInterval, func(time.Time) tea.Msg { return containerLogTickMsg{gen: gen} })
}

// containerLogTick runs the read the timer stands for, if it is still wanted.
func (m *Model) containerLogTick(msg containerLogTickMsg) tea.Cmd {
	l := &m.clog
	if msg.gen != l.gen {
		return nil
	}
	l.pending = false
	if !m.clogShown() || l.err != nil || !l.view.Following() || l.busy {
		return nil
	}
	return m.readContainerLog()
}

// reloadContainerLog reads again after the person chose another period, or to retry after an error. A period
// starts over from the last lines; a retry continues where the log stopped.
func (m *Model) reloadContainerLog(fresh bool) tea.Cmd {
	l := &m.clog
	if fresh {
		l.view.Clear()
		l.cursor, l.err, l.loaded, l.busy, l.pending = time.Time{}, nil, false, false, false
		l.gen++
	} else if l.busy {
		return nil
	}
	return m.readContainerLog()
}

// containerLogKey takes a key while the log is shown; the log view keeps the rest of its keys to itself.
func (m *Model) containerLogKey(msg tea.KeyPressMsg) tea.Cmd {
	l := &m.clog
	if l.view.FilterOpen() {
		// The period form takes every key; only the forced quit passes.
		if id, ok := m.keys.Match(msg, kittui.InputNavigation); ok && id == actForceQuit {
			return tea.Quit
		}
		l.view.Update(msg)
		return m.afterContainerLogKey()
	}
	focus := l.view.Input()
	if focus == kittui.InputText && l.view.Update(msg) {
		return nil
	}
	if id, ok := m.keys.Match(msg, focus); ok {
		if cmd, handled := m.navKey(id, msg, focus); handled {
			return cmd
		}
		switch id {
		case actQuit, actForceQuit:
			return tea.Quit
		case actHelp:
			m.openHelp()
			return nil
		case actReload:
			return tea.Batch(m.reload(), m.reloadContainerLog(false))
		case actBack:
			if !l.view.Update(msg) {
				m.closeContainerLog()
			}
			return nil
		}
	}
	l.view.Update(msg)
	return m.afterContainerLogKey()
}

// afterContainerLogKey reads again for a period the person just chose, and starts the timer when a key
// switched following on.
func (m *Model) afterContainerLogKey() tea.Cmd {
	if _, changed := m.clog.view.TakeFilterChange(); changed {
		return m.reloadContainerLog(true)
	}
	return m.followContainerLog()
}

// containerLogErrorText says in the log why it could not be read.
func (m *Model) containerLogErrorText(err error) string {
	t := m.text
	var remote *control.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return t.Lookup(kittui.TextError, t.Lookup(textErrCtLogSlow))
	case errors.As(err, &remote) && remote.Code == control.CodeNotFound:
		return t.Lookup(kittui.TextError, t.Lookup(textErrCtLogGone))
	case errors.As(err, &remote) && remote.Code == control.CodeNoSuchStack:
		return t.Lookup(kittui.TextError, t.Lookup(textErrCtLogStack))
	}
	return m.containerErrorText(err)
}

// containerLogPane draws the log: its title, a line for a failed read or a read still under way, and the view.
func (m *Model) containerLogPane(width, height int) string {
	m.scrollNote = ""
	l := &m.clog
	title, note := l.name, ""
	if l.stack != "" {
		title, note = l.stack, m.text.Lookup(textCtLogStack)
	}
	lines := []string{"  " + m.styles.TitleLine(title, note, max(width-2, 1))}
	switch {
	case l.err != nil:
		lines = append(lines, m.wrapped(2, kittui.StyleFail, m.containerLogErrorText(l.err), width)...)
	case !l.loaded:
		lines = append(lines, m.line(2, kittui.StyleHint, m.text.Lookup(textLogLoading), width))
	}
	view, _ := l.view.View(m.styles, width, max(height-len(lines), 1))
	return strings.Join(lines, "\n") + "\n" + view
}
