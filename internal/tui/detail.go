package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// Limits of the detail area. The service already bounds its answers; these keep one section from pushing the
// others out of reach.
const (
	// detailRuns is how many of the last runs of a stack the detail shows.
	detailRuns = 5
	// changelogBodyLines is how many lines of one release note are shown.
	changelogBodyLines = 30
	// detailIndent is the indent of a section's content, and detailMargin that of a heading.
	detailMargin = 2
	detailIndent = 4
)

// part is one section of the detail that is read on its own, so a failure of one leaves the others.
type part struct {
	loading, loaded bool
	err             error
}

// detailState is what the detail area holds for the stack it shows. gen separates the answers of a stack that
// was left from those of the current one.
type detailState struct {
	stack     string
	gen       int
	changelog control.ChangelogResult
	backups   control.BackupListResult
	cl, bk    part
	// top is the first shown line; total and rows are what the last drawing found.
	top, total, rows int
}

type changelogMsg struct {
	stack string
	gen   int
	res   control.ChangelogResult
	err   error
}

type backupsMsg struct {
	stack string
	gen   int
	res   control.BackupListResult
	err   error
}

// loadDetail reads the changelog and the backups of the chosen stack: at once for a stack not loaded yet, and
// again for force. A section whose read is under way is not asked twice.
func (m *Model) loadDetail(force bool) tea.Cmd {
	r, ok := m.rows[m.chosen]
	if !ok {
		return nil
	}
	d := &m.detail
	if d.stack != r.name {
		*d = detailState{stack: r.name, gen: d.gen + 1}
	} else if !force {
		return nil
	}
	ctx, b, stack, gen := m.ctx, m.backend, r.name, d.gen
	var cmds []tea.Cmd
	if !d.cl.loading {
		d.cl.loading = true
		cmds = append(cmds, func() tea.Msg {
			res, err := b.Changelog(ctx, stack)
			return changelogMsg{stack: stack, gen: gen, res: res, err: err}
		})
	}
	if !d.bk.loading {
		d.bk.loading = true
		cmds = append(cmds, func() tea.Msg {
			res, err := b.Backups(ctx, stack)
			return backupsMsg{stack: stack, gen: gen, res: res, err: err}
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) applyChangelog(msg changelogMsg) {
	d := &m.detail
	if msg.gen != d.gen || msg.stack != d.stack {
		return
	}
	d.cl.loading = false
	if d.cl.err = msg.err; msg.err != nil {
		d.cl.loaded, d.changelog = false, control.ChangelogResult{}
		return
	}
	d.cl.loaded, d.changelog = true, msg.res
}

func (m *Model) applyBackups(msg backupsMsg) {
	d := &m.detail
	if msg.gen != d.gen || msg.stack != d.stack {
		return
	}
	d.bk.loading = false
	if d.bk.err = msg.err; msg.err != nil {
		d.bk.loaded, d.backups = false, control.BackupListResult{}
		return
	}
	d.bk.loaded, d.backups = true, msg.res
}

// scrollDetail moves the first shown line of the detail and keeps it inside the text.
func (m *Model) scrollDetail(id kittui.ActionID) {
	d := &m.detail
	page := max(d.rows-1, 1)
	switch id {
	case actUp:
		d.top--
	case actDown:
		d.top++
	case actPageUp:
		d.top -= page
	case actPageDown:
		d.top += page
	case actFirst:
		d.top = 0
	case actLast:
		d.top = d.total
	default:
		return
	}
	d.top = max(min(d.top, d.total-d.rows), 0)
}

// detailView is the stack chosen in Stacks: its versions, changelogs, backups and last runs, scrolled to the
// first shown line.
func (m *Model) detailView(width, height int) string {
	r, ok := m.rows[m.chosen]
	if !ok {
		return m.note(m.text.Lookup(textDetailNone), width)
	}
	lines := m.detailLines(r, width)
	d := &m.detail
	d.total, d.rows = len(lines), height
	d.top = max(min(d.top, d.total-d.rows), 0)
	m.scrollNote = ""
	if d.total > d.rows {
		m.scrollNote = fmt.Sprintf("%d/%d", d.top+d.rows, d.total)
	}
	return strings.Join(lines[d.top:min(d.top+height, len(lines))], "\n")
}

// line is text in a style at an indent, cut to the width.
func (m *Model) line(indent int, style kittui.TextStyle, text string, width int) string {
	return strings.Repeat(" ", indent) + m.styles.Render(style, m.styles.Truncate(kittui.Escape(text), max(width-indent, 1)))
}

// wrapped is escaped text broken into lines of the width at an indent; line breaks of the text stay.
func (m *Model) wrapped(indent int, style kittui.TextStyle, text string, width int) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		for _, piece := range kittui.Wrap(kittui.Escape(l), max(width-indent, 1)) {
			out = append(out, strings.Repeat(" ", indent)+m.styles.Render(style, piece))
		}
	}
	return out
}

func (m *Model) heading(key kittui.TextID, width int) string {
	return m.line(detailMargin, kittui.StyleActive, m.text.Lookup(key), width)
}

// detailLines is the whole detail of a stack in width cells.
func (m *Model) detailLines(r stackRow, width int) []string {
	lines := []string{"  " + m.styles.TitleLine(r.name, "", max(width-2, 1)), ""}
	lines = append(lines, m.summaryLines(r, width)...)
	section := func(key kittui.TextID, body []string) {
		lines = append(lines, "", m.heading(key, width))
		lines = append(lines, body...)
	}
	section(textDetailVersions, m.versionLines(r.name, width))
	section(textDetailChangelog, m.changelogLines(width))
	section(textDetailBackups, m.backupLines(width))
	section(textDetailRuns, m.runLines(r.name, width))
	return lines
}

// summaryLines are the update mode, the state and the findings of the stack as the Stacks area has them.
func (m *Model) summaryLines(r stackRow, width int) []string {
	values := cells(r, m.text, m.loc, map[string]kittui.TextStyle{})
	columns := stackColumns(m.text, nil)
	var labels []string
	labelWidth := 0
	for _, i := range []int{3, 4, 6} {
		label := kittui.Escape(m.text.Lookup(columns[i].Title))
		labels = append(labels, label)
		labelWidth = max(labelWidth, lipgloss.Width(label))
	}
	var out []string
	for n, i := range []int{3, 4, 6} {
		out = append(out, "  "+m.styles.Render(kittui.StyleHint, kittui.Pad(labels[n], labelWidth))+"  "+
			m.styles.Render(kittui.StyleText, m.styles.Truncate(kittui.Escape(values[i]), max(width-labelWidth-4, 1))))
	}
	return out
}

// versionItem is one image version variable of a stack.
type versionItem struct {
	variable, image, current, selected, major, status, err string
}

// versionItems reads the variables from the stored update detection, and from the working copy when the stack
// was never checked.
func (m *Model) versionItems(stack string) (items []versionItem, checkErr string) {
	for _, su := range m.snap.updates.Stacks {
		if su.Name != stack {
			continue
		}
		if su.Error != "" {
			checkErr = su.Error
		}
		if !su.Checked.IsZero() {
			for _, it := range su.Items {
				items = append(items, versionItem{variable: it.Variable, image: it.Image, current: it.Current,
					selected: it.Selected, major: it.MajorAvailable, status: it.Status, err: it.Error})
			}
			return items, checkErr
		}
	}
	for _, si := range m.snap.stacks.Stacks {
		if si.Name != stack {
			continue
		}
		for _, img := range si.Images {
			items = append(items, versionItem{variable: img.Var, image: img.Image, current: img.Version})
		}
	}
	return items, checkErr
}

func (m *Model) versionLines(stack string, width int) []string {
	t := m.text
	items, checkErr := m.versionItems(stack)
	var out []string
	if checkErr != "" {
		out = append(out, m.wrapped(detailIndent, kittui.StyleFail, t.Lookup(textAvailError)+": "+checkErr, width)...)
	}
	if len(items) == 0 && checkErr == "" {
		return []string{m.line(detailIndent, kittui.StyleHint, t.Lookup(textDetailNoImages), width)}
	}
	for _, it := range items {
		head := it.variable
		if it.image != "" {
			head += "  " + it.image
		}
		out = append(out, m.line(detailIndent, kittui.StyleText, head, width))
		move := it.current
		if move == "" {
			move = t.Lookup(textNoValue)
		}
		status, tone := t.Lookup(textAvailNotChecked), kittui.StyleHint
		switch it.status {
		case "current":
			status, tone = t.Lookup(textAvailUpToDate), kittui.StyleOK
		case "update":
			move += " -> " + it.selected
			status, tone = t.Lookup(textItemUpdate), kittui.StyleWarn
		case "not-comparable":
			status, tone = t.Lookup(textAvailNotComparable), kittui.StyleHint
		case "error":
			status, tone = t.Lookup(textAvailError), kittui.StyleFail
		}
		if it.major != "" {
			move += "  " + t.Lookup(textItemMajor, it.major)
		}
		out = append(out, m.pair(detailIndent+2, move, status, tone, width))
		if it.err != "" {
			out = append(out, m.wrapped(detailIndent+2, kittui.StyleFail, it.err, width)...)
		}
	}
	return out
}

// pair is a plain text and a state word in its tone, cut to the width; the word is cut first.
func (m *Model) pair(indent int, text, word string, tone kittui.TextStyle, width int) string {
	room := max(width-indent, 1)
	text, word = kittui.Escape(text), kittui.Escape(word)
	if lipgloss.Width(text)+2+lipgloss.Width(word) > room {
		if left := room - lipgloss.Width(text) - 2; left > 1 {
			word = m.styles.Truncate(word, left)
		} else {
			return m.line(indent, kittui.StyleText, text, width)
		}
	}
	return strings.Repeat(" ", indent) + m.styles.Render(kittui.StyleText, text) + "  " + m.styles.Render(tone, word)
}

// section state lines: loading, error or nothing to say yet.
func (m *Model) partNote(p part, width int) (note []string, ready bool) {
	switch {
	case p.err != nil:
		return m.wrapped(detailIndent, kittui.StyleFail, m.errorText(p.err), width), false
	case !p.loaded:
		return []string{m.line(detailIndent, kittui.StyleHint, m.text.Lookup(textDetailLoading), width)}, false
	}
	return nil, true
}

func (m *Model) changelogLines(width int) []string {
	d := &m.detail
	if note, ready := m.partNote(d.cl, width); !ready {
		return note
	}
	return m.changelogItemLines(d.changelog, width)
}

// changelogItemLines is the release notes of one stack's changelog result.
func (m *Model) changelogItemLines(res control.ChangelogResult, width int) []string {
	t := m.text
	hint := func(text string) []string { return m.wrapped(detailIndent, kittui.StyleHint, text, width) }
	switch {
	case res.Checked.IsZero():
		return hint(t.Lookup(textChangelogNever))
	case len(res.Items) == 0:
		return hint(t.Lookup(textChangelogNone))
	}
	var out []string
	for i, it := range res.Items {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, m.line(detailIndent, kittui.StyleText, it.Variable+"  "+it.Current+" -> "+it.Target, width))
		if it.Source != "" {
			out = append(out, m.wrapped(detailIndent+2, kittui.StyleHint, t.Lookup(textChangelogSource, it.Source), width)...)
		}
		if len(it.Releases) == 0 {
			reason := it.Reason
			if reason == "" {
				reason = t.Lookup(textChangelogNoNotes)
			}
			out = append(out, m.wrapped(detailIndent+2, kittui.StyleHint, reason, width)...)
		}
		for _, rel := range it.Releases {
			head := rel.Version
			if rel.Name != "" && rel.Name != rel.Version {
				head += "  " + rel.Name
			}
			if !rel.Published.IsZero() {
				head += "  " + rel.Published.In(m.loc).Format("2006-01-02")
			}
			out = append(out, m.line(detailIndent+2, kittui.StyleActive, head, width))
			out = append(out, m.bodyLines(rel.Body, detailIndent+4, width)...)
		}
		if it.Omitted > 0 {
			out = append(out, m.wrapped(detailIndent+2, kittui.StyleHint, t.Lookup(textChangelogOmitted, it.Omitted), width)...)
		}
		if it.Link != "" {
			out = append(out, m.wrapped(detailIndent+2, kittui.StyleHint, t.Lookup(textChangelogLink, it.Link), width)...)
		}
	}
	return out
}

// bodyLines is the text of a release note, at most changelogBodyLines lines of it.
func (m *Model) bodyLines(body string, indent, width int) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	lines := m.wrapped(indent, kittui.StyleText, body, width)
	if len(lines) > changelogBodyLines {
		more := len(lines) - changelogBodyLines
		lines = append(lines[:changelogBodyLines], m.line(indent, kittui.StyleHint, m.text.Lookup(textChangelogMore, more), width))
	}
	return lines
}

func (m *Model) backupLines(width int) []string {
	t, d := m.text, &m.detail
	if note, ready := m.partNote(d.bk, width); !ready {
		return note
	}
	if len(d.backups.Backups) == 0 {
		return []string{m.line(detailIndent, kittui.StyleHint, t.Lookup(textBackupsNone), width)}
	}
	var out []string
	for _, b := range d.backups.Backups {
		out = append(out, m.line(detailIndent, kittui.StyleText, b.ID, width))
		var facts []string
		if !b.Created.IsZero() {
			facts = append(facts, b.Created.In(m.loc).Format(lastRunLayout))
		}
		facts = append(facts, humanSize(b.Size))
		out = append(out, m.line(detailIndent+2, kittui.StyleHint, strings.Join(facts, "  "), width))
		if b.Incomplete {
			out[len(out)-1] += "  " + m.styles.Render(kittui.StyleWarn, kittui.Escape(t.Lookup(textBackupIncomplete)))
		}
	}
	return out
}

func (m *Model) runLines(stack string, width int) []string {
	var mine []runRow
	for _, r := range m.runData {
		if r.stack == stack {
			mine = append(mine, r)
		}
	}
	if len(mine) == 0 {
		return []string{m.line(detailIndent, kittui.StyleHint, m.text.Lookup(textRunsEmpty), width)}
	}
	var out []string
	for _, r := range mine[:min(len(mine), detailRuns)] {
		result, tone := m.runResult(r)
		facts := []string{result}
		if !r.started.IsZero() {
			facts = append(facts, r.started.In(m.loc).Format(lastRunLayout))
		}
		if r.trigger != "" {
			facts = append(facts, r.trigger)
		}
		if !r.active && r.result != "" {
			facts = append(facts, formatDuration(r.duration))
		}
		out = append(out, m.line(detailIndent, tone, strings.Join(facts, "  "), width))
		out = append(out, m.line(detailIndent+2, kittui.StyleHint, r.id, width))
		if r.err != "" {
			out = append(out, m.wrapped(detailIndent+2, kittui.StyleFail, r.err, width)...)
		}
	}
	return out
}

// humanSize writes a size in bytes with a binary unit.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	f, suffix := float64(n), ""
	for _, s := range []string{"KiB", "MiB", "GiB", "TiB"} {
		f /= unit
		suffix = s
		if f < unit {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", f, suffix)
}

// formatDuration writes a run's duration: milliseconds below a second, whole seconds above.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return d.Round(time.Second).String()
}
