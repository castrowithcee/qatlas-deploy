package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// ctActionKinds maps the keys that act on a container. They are the keys of the stack actions, which the
// Stacks area reads; the area decides which one is meant.
var ctActionKinds = map[kittui.ActionID]actionKind{
	actStackStart: kindStart, actStackStop: kindStop, actStackRestart: kindRestart,
}

// ctActionTexts are the question and the sentence on what follows of each container action.
var ctActionTexts = map[actionKind]struct{ ask, consequence kittui.TextID }{
	kindStart:   {textAskCtStart, textWhatCtStart},
	kindStop:    {textAskCtStop, textWhatCtStop},
	kindRestart: {textAskCtRestart, textWhatCtRestart},
}

var ctDoneTexts = map[actionKind]kittui.TextID{
	kindStart: textDoneCtStart, kindStop: textDoneCtStop, kindRestart: textDoneCtRestart,
}

var ctNotReadyTexts = map[string]kittui.TextID{
	"exited": textCtNotReadyExited, "restarting": textCtNotReadyRestarting, "not-running": textCtNotReadyNotRunning,
	"unhealthy": textCtNotReadyUnhealthy, "starting": textCtNotReadyStarting,
}

// ctReadMsg carries the detail read before the question, so the question names what is there now.
type ctReadMsg struct {
	gen  int
	kind actionKind
	res  control.ContainerDetail
	err  error
}

// ctDoneMsg carries the answer of the confirmed action.
type ctDoneMsg struct {
	gen    int
	kind   actionKind
	detail control.ContainerDetail
	res    control.ContainerActionResult
	err    error
}

// containerAction takes S, X and Z in the Containers area; ok is false when the key means nothing there.
func (m *Model) containerAction(id kittui.ActionID) (cmd tea.Cmd, ok bool) {
	kind, known := ctActionKinds[id]
	if !known || m.area != areaContainers || m.clogShown() {
		return nil, false
	}
	return m.beginContainerAction(kind), true
}

// containerTarget is the name of the container the open detail shows or the list marks; a group row has none.
func (m *Model) containerTarget() string {
	if m.ctv.open {
		return m.ctv.name
	}
	id, found := m.ctList.Selected()
	if name, known := m.ctNames[id]; found && known {
		return name
	}
	return ""
}

// beginContainerAction reads the container, then asks. Nothing reaches the service before the answer.
func (m *Model) beginContainerAction(kind actionKind) tea.Cmd {
	if m.busy() {
		m.warn(m.text.Lookup(textActionBusy))
		return nil
	}
	name := m.containerTarget()
	if name == "" {
		return nil
	}
	m.ct.actGen++
	ctx, b, gen := m.ctx, m.backend, m.ct.actGen
	return func() tea.Msg {
		res, err := b.Container(ctx, name)
		return ctReadMsg{gen: gen, kind: kind, res: res, err: err}
	}
}

func (m *Model) applyContainerRead(msg ctReadMsg) {
	if msg.gen != m.ct.actGen || m.modal != nil || m.busy() || m.area != areaContainers {
		return
	}
	if msg.err != nil {
		m.warn(m.containerActionError(msg.err))
		return
	}
	dialog, err := m.containerDialog(msg.kind, msg.res)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return
	}
	m.modal = &modal{kind: modalAsk, dialog: dialog, yes: func() tea.Cmd { return m.startContainerAction(msg.kind, msg.res) }}
}

// containerKindText names the kind and the project of a container in the question.
func (m *Model) containerKindText(d control.ContainerDetail) string {
	switch d.Kind {
	case control.ContainerKindOwn:
		return m.text.Lookup(textCtKindOwn, kittui.Escape(d.Project))
	case control.ContainerKindForeign:
		return m.text.Lookup(textCtKindForeign, kittui.Escape(d.Project))
	}
	return m.text.Lookup(textCtKindSingle)
}

// stateText is the state of a container with the word of the engine and its health.
func (m *Model) stateText(s control.ContainerSummary) string {
	word, _ := m.stateWord(s.State)
	if s.EngineState != "" {
		word += " (" + kittui.Escape(s.EngineState) + ")"
	}
	if s.Health != "" {
		word += ", " + kittui.Escape(s.Health)
	}
	return word
}

// containerDialog is the question. Cancel is the default answer, so a stray enter does nothing.
func (m *Model) containerDialog(kind actionKind, d control.ContainerDetail) (*kittui.Dialog, error) {
	t := m.text
	info := ctActionTexts[kind]
	consequence := t.Lookup(info.consequence)
	if kind == kindStop {
		what := textWhatCtStop
		if d.Kind == control.ContainerKindOwn {
			what = textWhatCtStopOwn
		}
		consequence = t.Lookup(what, control.ContainerDefaultGraceSeconds)
	}
	return kittui.NewDialog(kittui.DialogSpec{Title: kindTexts[kind].word,
		Question:    t.Lookup(info.ask, kittui.Escape(d.Name), m.containerKindText(d)),
		Consequence: consequence, Details: dialogLines([]string{t.Lookup(textCtStateBefore, m.stateText(d.ContainerSummary))}),
		Answers: []kittui.Answer{{Action: actYes, Label: textAnswerYes}, {Action: actNo, Label: textAnswerCancel}},
		Default: actNo, Keys: kittui.DialogKeys{Enter: actEnter}}, m.keys, t)
}

// startContainerAction makes the confirmed call by ID, so a name given to another container meanwhile cannot hit
// that one. The plan is borrowed for the busy mark of the header and the guard against a second action.
func (m *Model) startContainerAction(kind actionKind, d control.ContainerDetail) tea.Cmd {
	m.runGen++
	m.run = &running{gen: m.runGen, plan: plan{kind: kind, stacks: []string{d.Name}}}
	target := d.ID
	if target == "" {
		target = d.Name
	}
	ctx, b, gen := m.ctx, m.backend, m.runGen
	return func() tea.Msg {
		var res control.ContainerActionResult
		var err error
		switch kind {
		case kindStart:
			res, err = b.ContainerStart(ctx, target)
		case kindStop:
			res, err = b.ContainerStop(ctx, target)
		default:
			res, err = b.ContainerRestart(ctx, target)
		}
		return ctDoneMsg{gen: gen, kind: kind, detail: d, res: res, err: err}
	}
}

// applyContainerDone shows the result and reads the list and the open detail again at once.
func (m *Model) applyContainerDone(msg ctDoneMsg) tea.Cmd {
	if m.run == nil || msg.gen != m.run.gen {
		return nil
	}
	m.run = nil
	o := m.containerOutcome(msg)
	t := m.text
	failed := 0
	if o.failed {
		failed = 1
	}
	md := &modal{kind: modalSummary, outcomes: []outcome{o}}
	md.title = t.Lookup(textSummaryTitle, t.Lookup(kindTexts[msg.kind].word), 1-failed, failed)
	dialog, err := kittui.NewDialog(kittui.DialogSpec{Title: kindTexts[msg.kind].word, Question: md.title,
		Consequence: t.Lookup(textSummaryNoteCt), Answers: []kittui.Answer{{Action: actNo, Label: textAnswerClose}},
		Default: actNo, Keys: kittui.DialogKeys{Enter: actEnter}}, m.keys, t)
	if err != nil {
		m.failure = t.Lookup(kittui.TextError, err.Error())
	} else {
		md.dialog, m.modal = dialog, md
	}
	return tea.Batch(m.reloadNow(), m.rereadContainers())
}

// containerOutcome turns the answer into what the result shows. A container that is not ready counts as a
// failure, like the command does.
func (m *Model) containerOutcome(msg ctDoneMsg) outcome {
	t := m.text
	o := outcome{stack: msg.detail.Name}
	if msg.err != nil {
		o.failed = true
		o.lines = []string{m.containerActionError(msg.err)}
		return o
	}
	done := t.Lookup(ctDoneTexts[msg.kind])
	if reason := msg.res.NotReady; reason != "" {
		o.failed = true
		if id, ok := ctNotReadyTexts[reason]; ok {
			reason = t.Lookup(id)
		}
		done = t.Lookup(textDoneCtNotReady, done, kittui.Escape(reason))
	}
	o.lines = []string{done, t.Lookup(textCtStateAfter, m.stateText(msg.res.ContainerSummary))}
	return o
}

// containerActionError says in plain words why a container action could not be made.
func (m *Model) containerActionError(err error) string {
	t := m.text
	var remote *control.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return t.Lookup(textErrCtSlow)
	case errors.As(err, &remote):
		switch remote.Code {
		case control.CodeRunActive:
			return t.Lookup(textErrRunActive)
		case control.CodeNotFound:
			return t.Lookup(textErrCtGone)
		case control.CodeDockerUnreachable:
			return t.Lookup(textErrCtUnreachable)
		case control.CodeDockerPermission:
			return t.Lookup(textErrCtPermission)
		case control.CodeDockerTimeout:
			return t.Lookup(textErrCtEngineSlow)
		}
	}
	return m.serviceText(err)
}

// rereadContainers reads the list and the open detail now, even when a read is under way: that one may have
// read before the change, so its answer is dropped by the generation.
func (m *Model) rereadContainers() tea.Cmd {
	m.ct.readGen++
	m.ct.loading = false
	if m.ctv.open {
		m.ctv.gen++
		m.ctv.loading = false
	}
	return tea.Batch(m.readContainers(), m.readContainer())
}

// containerActionHints are the keys of the container actions in the key line.
func containerActionHints() []kittui.Hint {
	return []kittui.Hint{
		{Action: actStackStart, Label: textKeyStart, Priority: 3}, {Action: actStackStop, Label: textKeyStop, Priority: 3},
		{Action: actStackRestart, Label: textKeyRestart, Priority: 3},
	}
}
