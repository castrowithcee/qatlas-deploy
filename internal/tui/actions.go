package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// actionKind is what a key asks the service to do.
type actionKind int

const (
	kindDeploy actionKind = iota
	kindUpdate
	kindPause
	kindUnpause
	kindPin
	kindUnpin
	kindResume
	kindCheck
)

// actionKinds maps the key of each action to what it does.
var actionKinds = map[kittui.ActionID]actionKind{
	actDeploy: kindDeploy, actUpdate: kindUpdate, actPause: kindPause, actUnpause: kindUnpause,
	actPin: kindPin, actUnpin: kindUnpin, actResume: kindResume, actCheck: kindCheck,
}

// kindTexts are the texts of each kind: the word of its command (English in every language), the question, the
// sentence on what follows, and the key label.
var kindTexts = map[actionKind]struct{ word, ask, consequence kittui.TextID }{
	kindDeploy:  {textKeyDeploy, textAskDeploy, textWhatDeploy},
	kindUpdate:  {textKeyUpdate, textAskUpdate, textWhatUpdate},
	kindPause:   {textKeyPause, textAskPause, textWhatPause},
	kindUnpause: {textKeyUnpause, textAskUnpause, textWhatUnpause},
	kindPin:     {textKeyPin, textAskPin, textWhatPin},
	kindUnpin:   {textKeyUnpin, textAskUnpin, textWhatUnpin},
	kindResume:  {textKeyResume, textAskResume, textWhatResume},
	kindCheck:   {textKeyCheck, textAskCheck, textWhatCheck},
}

// maxListedTargets is how many stack names a confirmation lists before it says how many more there are.
const maxListedTargets = 6

// plan is a confirmed-to-be action on stacks, which run one after another. A registry query has no stacks.
type plan struct {
	kind   actionKind
	stacks []string
}

func (p plan) steps() int { return max(len(p.stacks), 1) }

// runRef names a run a finished action started.
type runRef struct{ id, stack string }

// outcome is what one stack, or the registry query, came to.
type outcome struct {
	stack  string
	failed bool
	lines  []string
	runs   []runRef
}

// running is the sequence of calls under way; only one exists at a time.
type running struct {
	gen      int
	plan     plan
	outcomes []outcome
}

// modalKind tells which question or result the modal shows.
type modalKind int

const (
	// modalAsk is a confirmation over the screen.
	modalAsk modalKind = iota
	// modalPreview is the update confirmation with what changes and the changelog; it fills the workspace.
	modalPreview
	// modalSummary is the result of an action; it fills the workspace.
	modalSummary
)

// changelogEntry is the changelog of one stack read for the update preview.
type changelogEntry struct {
	res  control.ChangelogResult
	err  error
	done bool
}

// modal is the one question or result that takes every key.
type modal struct {
	kind   modalKind
	dialog *kittui.Dialog
	plan   plan
	// gen separates the answers of a preview that was left from the current one.
	gen int
	// yes, when set, replaces the plan of the stacks: it is what a confirmed change of the settings does.
	yes        func() tea.Cmd
	changelogs map[string]*changelogEntry
	// summary
	title    string
	outcomes []outcome
	// top is the first shown line of a modal that fills the workspace; total and rows are what the last
	// drawing found.
	top, total, rows int
}

func (md *modal) sheet() bool { return md.kind != modalAsk }

// stepMsg carries the answer of one call of a sequence.
type stepMsg struct {
	gen, index int
	result     any
	err        error
}

// previewMsg carries the changelog of one stack for the update preview.
type previewMsg struct {
	gen   int
	stack string
	res   control.ChangelogResult
	err   error
}

// busy reports whether a sequence of calls runs.
func (m *Model) busy() bool { return m.run != nil }

// targets are the stacks an action applies to: the ticked ones in list order, or the one under the cursor.
func (m *Model) targets() []string {
	var names []string
	for _, id := range m.list.Ticked() {
		if r, ok := m.rows[id]; ok {
			names = append(names, r.name)
		}
	}
	if len(names) > 0 {
		return names
	}
	if id, ok := m.list.Selected(); ok {
		if r, ok := m.rows[id]; ok {
			return []string{r.name}
		}
	}
	return nil
}

func (m *Model) warn(text string) {
	m.notice = m.text.Lookup(kittui.TextWarning, text)
}

// beginAction asks for the confirmation of an action. Nothing reaches the service before the answer.
func (m *Model) beginAction(kind actionKind) tea.Cmd {
	if m.busy() {
		m.warn(m.text.Lookup(textActionBusy))
		return nil
	}
	if !m.loaded {
		return nil
	}
	p := plan{kind: kind}
	if kind != kindCheck {
		if p.stacks = m.targets(); len(p.stacks) == 0 {
			m.warn(m.text.Lookup(textActionNoStack))
			return nil
		}
	}
	if kind == kindResume {
		// Only a stack paused after a failed rollback is resumed; the narrower reading refuses the whole
		// action rather than leaving a ticked stack out unseen.
		for _, name := range p.stacks {
			if r := m.rowNamed(name); r.state != stateRollbackPause {
				m.warn(m.text.Lookup(textActionNotResumable, name))
				return nil
			}
		}
	}
	if kind == kindUpdate {
		return m.openPreview(p)
	}
	dialog, err := m.confirmDialog(p, nil)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return nil
	}
	m.modal = &modal{kind: modalAsk, dialog: dialog, plan: p}
	return nil
}

func (m *Model) rowNamed(name string) stackRow {
	for _, r := range m.rows {
		if r.name == name {
			return r
		}
	}
	return stackRow{}
}

// subject is what a question names: the stack, or the number of stacks.
func (m *Model) subject(p plan) string {
	switch {
	case p.kind == kindCheck:
		return m.text.Lookup(textAllStacks)
	case len(p.stacks) == 1:
		return p.stacks[0]
	}
	return m.text.Lookup(textStackCount, len(p.stacks))
}

// confirmDialog is the question of a plan. Cancel is the default answer, so a stray enter does nothing.
func (m *Model) confirmDialog(p plan, answers []kittui.Answer) (*kittui.Dialog, error) {
	t := m.text
	info := kindTexts[p.kind]
	var details []string
	if len(p.stacks) > 1 {
		for i, name := range p.stacks {
			if i == maxListedTargets {
				details = append(details, t.Lookup(textTargetsMore, len(p.stacks)-i))
				break
			}
			details = append(details, name)
		}
	}
	if answers == nil {
		answers = []kittui.Answer{{Action: actYes, Label: textAnswerYes}, {Action: actNo, Label: textAnswerCancel}}
	}
	return kittui.NewDialog(kittui.DialogSpec{Title: info.word, Question: t.Lookup(info.ask, m.subject(p)),
		Consequence: t.Lookup(info.consequence), Details: details, Answers: answers, Default: actNo,
		Keys: kittui.DialogKeys{Enter: actEnter}}, m.keys, t)
}

// openPreview shows what an update would change and reads the changelog of the stacks that have an update.
// The yes answer exists once every changelog is read or has failed.
func (m *Model) openPreview(p plan) tea.Cmd {
	md := &modal{kind: modalPreview, plan: p, changelogs: map[string]*changelogEntry{}}
	m.modalGen++
	md.gen = m.modalGen
	var wanted []string
	for _, name := range p.stacks {
		if m.hasUpdate(name) {
			md.changelogs[name] = &changelogEntry{}
			wanted = append(wanted, name)
		}
	}
	if !m.setPreviewDialog(md) {
		return nil
	}
	m.modal = md
	return m.readChangelog(md.gen, wanted)
}

// hasUpdate reports whether the stored detection knows an update for the stack.
func (m *Model) hasUpdate(stack string) bool {
	items, _ := m.versionItems(stack)
	return slices.ContainsFunc(items, func(it versionItem) bool { return it.status == "update" })
}

// setPreviewDialog builds the dialog of a preview: with the yes answer once the changelogs are in.
func (m *Model) setPreviewDialog(md *modal) bool {
	answers := []kittui.Answer{{Action: actNo, Label: textAnswerCancel}}
	if md.changelogsRead() {
		answers = []kittui.Answer{{Action: actYes, Label: textAnswerYes}, {Action: actNo, Label: textAnswerCancel}}
	}
	dialog, err := m.confirmDialog(md.plan, answers)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return false
	}
	md.dialog = dialog
	return true
}

func (md *modal) changelogsRead() bool {
	for _, e := range md.changelogs {
		if !e.done {
			return false
		}
	}
	return true
}

// readChangelog reads the changelog of the first of stacks; the next is read when it answers, so the service
// is never asked for several at once.
func (m *Model) readChangelog(gen int, stacks []string) tea.Cmd {
	if len(stacks) == 0 {
		return nil
	}
	ctx, b, stack := m.ctx, m.backend, stacks[0]
	return func() tea.Msg {
		res, err := b.Changelog(ctx, stack)
		return previewMsg{gen: gen, stack: stack, res: res, err: err}
	}
}

func (m *Model) applyPreview(msg previewMsg) tea.Cmd {
	md := m.modal
	if md == nil || md.kind != modalPreview || md.gen != msg.gen {
		return nil
	}
	e, ok := md.changelogs[msg.stack]
	if !ok || e.done {
		return nil
	}
	e.res, e.err, e.done = msg.res, msg.err, true
	m.setPreviewDialog(md)
	var rest []string
	for _, name := range md.plan.stacks {
		if c, ok := md.changelogs[name]; ok && !c.done {
			rest = append(rest, name)
		}
	}
	return m.readChangelog(md.gen, rest)
}

// modalKey takes every key while a question or result is open. Only its answers act; ctrl+c still quits.
func (m *Model) modalKey(msg tea.KeyPressMsg) tea.Cmd {
	md := m.modal
	if answer, handled := md.dialog.Update(msg); handled {
		return m.answer(answer)
	}
	if id, ok := m.keys.Match(msg, kittui.InputNavigation); ok {
		if id == actForceQuit {
			return tea.Quit
		}
		if md.sheet() {
			md.scroll(id)
		}
	}
	return nil
}

func (md *modal) scroll(id kittui.ActionID) {
	page := max(md.rows-1, 1)
	switch id {
	case actUp:
		md.top--
	case actDown:
		md.top++
	case actPageUp:
		md.top -= page
	case actPageDown:
		md.top += page
	case actFirst:
		md.top = 0
	case actLast:
		md.top = md.total
	default:
		return
	}
	md.top = max(min(md.top, md.total-md.rows), 0)
}

func (m *Model) answer(answer kittui.ActionID) tea.Cmd {
	md := m.modal
	m.modal = nil
	switch answer {
	case actYes:
		if md.yes != nil {
			return md.yes()
		}
		return m.start(md.plan)
	case actShowLog:
		return m.showRuns(md.runs())
	}
	return nil
}

// runs are the runs the outcomes of a summary started.
func (md *modal) runs() []runRef {
	var refs []runRef
	for _, o := range md.outcomes {
		refs = append(refs, o.runs...)
	}
	return refs
}

// start runs the confirmed plan: one call per stack, the next when the previous answered.
func (m *Model) start(p plan) tea.Cmd {
	m.runGen++
	m.run = &running{gen: m.runGen, plan: p}
	return m.step()
}

// step makes the next call of the sequence off the event loop.
func (m *Model) step() tea.Cmd {
	r := m.run
	index := len(r.outcomes)
	stack := ""
	if r.plan.kind != kindCheck {
		stack = r.plan.stacks[index]
	}
	ctx, b, kind, gen := m.ctx, m.backend, r.plan.kind, r.gen
	return func() tea.Msg {
		res, err := callService(ctx, b, kind, stack)
		return stepMsg{gen: gen, index: index, result: res, err: err}
	}
}

func callService(ctx context.Context, b Backend, kind actionKind, stack string) (any, error) {
	switch kind {
	case kindDeploy:
		return b.Deploy(ctx, stack)
	case kindUpdate:
		return b.Update(ctx, []string{stack})
	case kindPause:
		return b.Pause(ctx, stack)
	case kindUnpause:
		return b.Unpause(ctx, stack)
	case kindPin:
		return b.Pin(ctx, stack)
	case kindUnpin:
		return b.Unpin(ctx, stack)
	case kindResume:
		return b.Resume(ctx, stack)
	}
	return b.Updates(ctx, control.UpdatesArgs{Refresh: true})
}

// applyStep takes the answer of a call, goes on with the next stack whatever it was, and ends the sequence
// with the summary.
func (m *Model) applyStep(msg stepMsg) tea.Cmd {
	r := m.run
	if r == nil || msg.gen != r.gen || msg.index != len(r.outcomes) {
		return nil
	}
	stack := ""
	if r.plan.kind != kindCheck {
		stack = r.plan.stacks[msg.index]
	}
	r.outcomes = append(r.outcomes, m.outcomeOf(r.plan.kind, stack, msg))
	if len(r.outcomes) < r.plan.steps() {
		return tea.Batch(m.step(), m.reloadNow())
	}
	return m.finish()
}

// finish shows the summary, leaves the stacks that failed ticked, and reads the service again.
func (m *Model) finish() tea.Cmd {
	r := m.run
	m.run = nil
	failed := 0
	var again []kittui.ItemID
	for _, o := range r.outcomes {
		if !o.failed {
			continue
		}
		failed++
		for id, row := range m.rows {
			if row.name == o.stack {
				again = append(again, id)
			}
		}
	}
	if r.plan.kind != kindCheck {
		_ = m.list.SetTicked(again)
	}
	m.detail = detailState{gen: m.detail.gen + 1}
	md := &modal{kind: modalSummary, plan: r.plan, outcomes: r.outcomes}
	md.title = m.text.Lookup(textSummaryTitle, m.text.Lookup(kindTexts[r.plan.kind].word), len(r.outcomes)-failed, failed)
	answers := []kittui.Answer{{Action: actNo, Label: textAnswerClose}}
	if len(md.runs()) > 0 {
		answers = append(answers, kittui.Answer{Action: actShowLog, Label: textAnswerLog})
	}
	dialog, err := kittui.NewDialog(kittui.DialogSpec{Title: kindTexts[r.plan.kind].word, Question: md.title,
		Consequence: m.text.Lookup(textSummaryNote), Answers: answers, Default: actNo,
		Keys: kittui.DialogKeys{Enter: actEnter}}, m.keys, m.text)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return m.reloadNow()
	}
	md.dialog = dialog
	m.modal = md
	return m.reloadNow()
}

// reloadNow reads the service again; a refresh already under way is followed by another, since it may have
// read before the change.
func (m *Model) reloadNow() tea.Cmd {
	if m.loading {
		m.stale = true
		return nil
	}
	return m.reload()
}

// outcomeOf turns the answer of one call into what the summary shows.
func (m *Model) outcomeOf(kind actionKind, stack string, msg stepMsg) outcome {
	t := m.text
	o := outcome{stack: stack}
	if kind == kindCheck {
		o.stack = t.Lookup(textAllStacks)
	}
	if msg.err != nil {
		o.failed = true
		o.lines = []string{m.serviceText(msg.err)}
		return o
	}
	switch res := msg.result.(type) {
	case control.DeployResult:
		o.lines = []string{t.Lookup(textDoneDeploy, res.ID, res.Commit[:min(len(res.Commit), maxShownCommit)])}
		o.runs = []runRef{{id: res.ID, stack: stack}}
	case control.UpdateRunResult:
		m.updateOutcome(&o, res)
	case control.HoldResult:
		done := map[actionKind]kittui.TextID{kindPause: textDonePause, kindUnpause: textDoneUnpause,
			kindPin: textDonePin, kindUnpin: textDoneUnpin}[kind]
		o.lines = []string{t.Lookup(done)}
		if !res.Changed {
			o.lines = []string{t.Lookup(textDoneUnchanged, o.lines[0])}
		}
	case control.ResumeResult:
		o.lines = []string{t.Lookup(textDoneResume)}
	case control.UpdatesResult:
		var updates, broken int
		for _, su := range res.Stacks {
			switch {
			case su.Error != "" || slices.ContainsFunc(su.Items, func(it control.UpdateInfo) bool { return it.Status == "error" }):
				broken++
			case slices.ContainsFunc(su.Items, func(it control.UpdateInfo) bool { return it.Status == "update" }):
				updates++
			}
		}
		o.lines = []string{t.Lookup(textDoneCheck, len(res.Stacks), updates, broken)}
	}
	return o
}

// maxShownCommit is how many characters of a commit a result shows.
const maxShownCommit = 12

// updateOutcome reads the answer to a one-stack update. The stack failed when it was refused or a step did not
// succeed.
func (m *Model) updateOutcome(o *outcome, res control.UpdateRunResult) {
	t := m.text
	i := slices.IndexFunc(res.Stacks, func(s control.StackUpdateRun) bool { return s.Name == o.stack })
	if i < 0 {
		o.failed = true
		o.lines = []string{t.Lookup(textDoneNoAnswer)}
		return
	}
	run := res.Stacks[i]
	switch {
	case run.Error != "":
		o.failed = true
		o.lines = []string{t.Lookup(textDoneRefused, run.Error)}
	case len(run.Steps) == 0:
		o.lines = []string{t.Lookup(textDoneCurrent)}
	}
	for _, st := range run.Steps {
		result := t.Lookup(textStepSkipped)
		if st.Result != "" {
			result = st.Result
			if id, ok := resultTexts[st.Result]; ok {
				result = t.Lookup(id)
			}
		}
		o.lines = append(o.lines, t.Lookup(textDoneStep, st.Variable, st.From, st.To, result))
		if st.Error != "" {
			o.lines = append(o.lines, st.Error)
		}
		if st.Result != "succeeded" {
			o.failed = true
		}
		if st.Run != "" {
			o.runs = append(o.runs, runRef{id: st.Run, stack: o.stack})
		}
	}
}

// showRuns opens the Runs area: on the log when the action started exactly one run, on the list otherwise.
func (m *Model) showRuns(refs []runRef) tea.Cmd {
	if len(refs) != 1 {
		m.log.open = false
		return m.enter(areaRuns)
	}
	row := runRow{id: refs[0].id, stack: refs[0].stack}
	if i := slices.IndexFunc(m.runData, func(r runRow) bool { return r.id == row.id }); i >= 0 {
		row = m.runData[i]
	} else {
		row.started, _ = time.Parse(runIDLayout, row.id[:min(len(row.id), len(runIDLayout))])
	}
	m.area = areaRuns
	return m.showLog(row)
}

// actionHints are the keys of the actions in the key line of the Stacks area: the ones used most. The others
// are in the help (see moreActionHints), so the key line keeps the position of the list.
func (m *Model) actionHints() []kittui.Hint {
	hints := []kittui.Hint{
		{Action: actToggle, Label: textKeyTick, Priority: 1},
		{Action: actUpdate, Label: textKeyUpdate, Priority: 2},
		{Action: actDeploy, Label: textKeyDeploy, Priority: 2},
	}
	if id, ok := m.list.Selected(); ok && m.rows[id].state == stateRollbackPause {
		hints = append(hints, kittui.Hint{Action: actResume, Label: textKeyResume, Priority: 2})
	}
	return hints
}

// moreActionHints are the actions the key line leaves out.
func (m *Model) moreActionHints() []kittui.Hint {
	hints := []kittui.Hint{
		{Action: actPause, Label: textKeyPause}, {Action: actUnpause, Label: textKeyUnpause},
		{Action: actPin, Label: textKeyPin}, {Action: actUnpin, Label: textKeyUnpin},
		{Action: actCheck, Label: textKeyCheck},
	}
	if id, ok := m.list.Selected(); !ok || m.rows[id].state != stateRollbackPause {
		hints = append(hints, kittui.Hint{Action: actResume, Label: textKeyResume})
	}
	return hints
}

// busyMark is the status mark of the header while a sequence runs.
func (m *Model) busyMark() kittui.StatusMark {
	r, t := m.run, m.text
	word := t.Lookup(kindTexts[r.plan.kind].word)
	n := min(len(r.outcomes)+1, r.plan.steps())
	text := t.Lookup(textBusyShort, word, n, r.plan.steps())
	if r.plan.kind != kindCheck {
		text = t.Lookup(textBusyRun, word, n, r.plan.steps(), r.plan.stacks[n-1])
	}
	return kittui.StatusMark{Kind: kittui.StatusBusy, Text: text, Short: t.Lookup(textBusyShort, word, n, r.plan.steps())}
}

// modalFooter is the key line of a result or a question that fills the workspace.
func (m *Model) modalFooter() []kittui.Hint {
	return append(m.modal.dialog.Hints(),
		kittui.Hint{Action: actUp, Label: kittui.TextHelpScroll, Kind: kittui.HintNavigation},
		kittui.Hint{Action: actDown, Label: kittui.TextHelpScroll, Kind: kittui.HintNavigation})
}

// modalView draws a preview or summary in width by height cells, scrolled to its first shown line.
func (m *Model) modalView(width, height int) string {
	md := m.modal
	var lines []string
	if md.kind == modalPreview {
		lines = m.previewLines(width)
	} else {
		lines = m.summaryLinesOf(width)
	}
	md.total, md.rows = len(lines), height
	md.top = max(min(md.top, md.total-md.rows), 0)
	m.scrollNote = ""
	if md.total > md.rows {
		m.scrollNote = fmt.Sprintf("%d/%d", md.top+md.rows, md.total)
	}
	return strings.Join(lines[md.top:min(md.top+height, len(lines))], "\n")
}

// questionLines are the warning glyph with the question, and the sentence on what follows.
func (m *Model) questionLines(question, consequence string, width int, warning bool) []string {
	lead := "  "
	if warning {
		lead += m.styles.Render(kittui.StyleWarn, m.styles.Glyph(kittui.GlyphWarning)) + " "
	}
	var out []string
	for i, l := range kittui.Wrap(kittui.Escape(question), max(width-lipgloss.Width(lead), 1)) {
		prefix := strings.Repeat(" ", lipgloss.Width(lead))
		if i == 0 {
			prefix = lead
		}
		out = append(out, prefix+m.styles.Render(kittui.StyleTitle, l))
	}
	if consequence != "" {
		out = append(out, "")
		out = append(out, m.wrapped(detailMargin, kittui.StyleHint, consequence, width)...)
	}
	return out
}

// previewLines is the update question, then per stack the versions that change and the changelog.
func (m *Model) previewLines(width int) []string {
	md, t := m.modal, m.text
	info := kindTexts[kindUpdate]
	lines := m.questionLines(t.Lookup(info.ask, m.subject(md.plan)), t.Lookup(info.consequence), width, true)
	for _, name := range md.plan.stacks {
		lines = append(lines, "", m.line(detailMargin, kittui.StyleActive, name, width))
		lines = append(lines, m.versionLines(name, width)...)
		items, _ := m.versionItems(name)
		if !m.hasUpdate(name) {
			lines = append(lines, m.wrapped(detailIndent, kittui.StyleHint, t.Lookup(textPreviewNoUpdate), width)...)
		}
		if slices.ContainsFunc(items, func(it versionItem) bool { return it.major != "" }) {
			lines = append(lines, m.wrapped(detailIndent, kittui.StyleHint, t.Lookup(textPreviewMajor), width)...)
		}
		e, ok := md.changelogs[name]
		if !ok {
			continue
		}
		lines = append(lines, "", m.heading(textDetailChangelog, width))
		switch {
		case !e.done:
			lines = append(lines, m.line(detailIndent, kittui.StyleHint, t.Lookup(textDetailLoading), width))
		case e.err != nil:
			lines = append(lines, m.wrapped(detailIndent, kittui.StyleFail, m.errorText(e.err), width)...)
		default:
			lines = append(lines, m.changelogItemLines(e.res, width)...)
		}
	}
	return lines
}

// summaryLinesOf is the result of an action: one entry per stack.
func (m *Model) summaryLinesOf(width int) []string {
	md, t := m.modal, m.text
	lines := m.questionLines(md.title, "", width, false)
	for _, o := range md.outcomes {
		word, tone := t.Lookup(textOutcomeOK), kittui.StyleOK
		if o.failed {
			word, tone = t.Lookup(textOutcomeFailed), kittui.StyleFail
		}
		lines = append(lines, "", m.pair(detailMargin, o.stack, word, tone, width))
		for _, l := range o.lines {
			lines = append(lines, m.wrapped(detailIndent, kittui.StyleText, l, width)...)
		}
	}
	return lines
}
