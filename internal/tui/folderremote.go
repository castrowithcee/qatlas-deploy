package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// comparing says the flow ends in the comparison of the folder with the repository at its address: a local
// folder that gets a remote, and a folder of files that is added together with one.
func (f *addFlow) comparing() bool {
	return f.remote || (f.res.Case == control.SourceCaseFiles && f.url != "")
}

// askFolderRemote starts adding a remote to the marked local folder with the form for its address. Nothing
// changes before the last question.
func (m *Model) askFolderRemote() tea.Cmd {
	f, ok := m.selectedFolder()
	switch {
	case !ok:
		m.warn(m.text.Lookup(textChooseOne))
		return nil
	case f.Kind == control.SourceKindRemote:
		m.warn(m.text.Lookup(textFolderHasRemote))
		return nil
	case m.settingsBusy():
		m.warn(m.text.Lookup(textActionBusy))
		return nil
	}
	m.set.add = &addFlow{res: control.SourceInspectResult{Path: f.Path, Case: control.SourceCaseLocal}, remote: true}
	return m.openForm(m.folderURLForm())
}

// compareFolder compares the folder with the repository at the address, with the host key and the deploy key
// confirmed before. The service changes nothing but the pinned host key.
func (m *Model) compareFolder(flow *addFlow) tea.Cmd {
	args := control.SourceCompareArgs{Path: flow.res.Path, URL: flow.url, Fingerprint: flow.probe.Fingerprint}
	return m.writeCall(opCompare, args.Path, func(b Backend) (any, error) { return b.SourceCompare(m.ctx, args) })
}

// showComparison shows how the repository relates to the folder and asks for the step of that state. A secret
// finding only tells: there is no answer that pushes it.
func (m *Model) showComparison(cmp control.SourceCompareResult) {
	flow, t := m.set.add, m.text
	if flow == nil {
		return
	}
	flow.cmp = &cmp
	dir, url, title := flow.res.Path, flow.url, flow.title()
	body := func(width int) []string { return m.comparisonLines(flow, width) }
	yes := []kittui.Answer{{Action: actYes, Label: textAnswerYes}, {Action: actNo, Label: textAnswerCancel}}
	if cmp.Secrets != nil && cmp.Secrets.Total > 0 {
		m.set.add = nil
		m.askSheet(title, t.Lookup(textAskRemoteSecrets, dir, url, cmp.Secrets.Total), t.Lookup(textWhatRemoteSecrets), body, nil, nil)
		return
	}
	final := func() tea.Cmd { return m.connectRemote(flow) }
	if flow.res.Case == control.SourceCaseFiles {
		final = func() tea.Cmd { return m.addFolder(flow) }
	}
	switch cmp.State {
	case control.SourceRemoteEmpty:
		if flow.res.Case == control.SourceCaseFiles {
			// A folder of files has no history to push; the service refuses it, so it is not offered.
			m.set.add = nil
			m.askSheet(title, t.Lookup(textAskRemoteNoCommit, url, dir), t.Lookup(textWhatRemoteNoCommit), body, nil, nil)
			return
		}
		m.askSheet(title, t.Lookup(textAskRemoteEmpty, dir, url, cmp.Branch), t.Lookup(textWhatRemoteEmpty), body, yes, final)
	case control.SourceRemoteEqual:
		m.askSheet(title, t.Lookup(textAskRemoteEqual, dir, url), t.Lookup(textWhatRemoteEqual), body, yes, final)
	case control.SourceRemoteDifferent:
		answers := []kittui.Answer{{Action: actYes, Label: textAnswerPush}, {Action: actNo, Label: textAnswerCancel}}
		m.askSheet(title, t.Lookup(textAskRemoteDifferent, dir, cmp.Branch, url), t.Lookup(textWhatRemoteDifferent), body, answers, final)
	default:
		m.set.add = nil
		m.failure = t.Lookup(kittui.TextError, t.Lookup(textErrService, "unknown state "+cmp.State))
	}
}

// comparisonLines are what the comparison found: folder, address, the paths that differ, the places of
// secret-like files and the deploy key, which comes last because the lists are what the question rests on. Paths come from the remote and the folder and are shown as text.
func (m *Model) comparisonLines(flow *addFlow, width int) []string {
	t, cmp := m.text, flow.cmp
	text := func(style kittui.TextStyle, s string) []string { return m.wrapped(detailIndent, style, s, width) }
	lines := []string{""}
	lines = append(lines, text(kittui.StyleText, t.Lookup(textDetailDir, flow.res.Path))...)
	lines = append(lines, text(kittui.StyleText, t.Lookup(textDetailURL, flow.url))...)
	lines = append(lines, text(kittui.StyleText, t.Lookup(textDetailBranch, cmp.Branch))...)
	if cmp.Commit != "" {
		lines = append(lines, text(kittui.StyleText, t.Lookup(textDetailHead, short(cmp.Commit)))...)
	}
	if cmp.PathCount > 0 {
		lines = append(lines, "", m.line(detailMargin, kittui.StyleActive, t.Lookup(textHeadPaths, cmp.PathCount), width))
		shown := min(len(cmp.Paths), shownEntries)
		for _, p := range cmp.Paths[:shown] {
			lines = append(lines, m.wrapped(detailIndent, kittui.StyleText, p, width)...)
		}
		if cmp.PathCount > shown {
			lines = append(lines, m.line(detailIndent, kittui.StyleHint, t.Lookup(textMorePaths, cmp.PathCount-shown), width))
		}
	}
	if s := cmp.Secrets; s != nil && s.Total > 0 {
		lines = append(lines, "", m.line(detailMargin, kittui.StyleFail, t.Lookup(textHeadSecrets, s.Total), width))
		shown := min(len(s.Findings), shownEntries)
		for _, f := range s.Findings[:shown] {
			lines = append(lines, m.wrapped(detailIndent, kittui.StyleText, t.Lookup(textSecretEntry, short(f.Commit), f.Path, f.Kind), width)...)
		}
		if s.Total > shown {
			lines = append(lines, m.line(detailIndent, kittui.StyleHint, t.Lookup(textMorePaths, s.Total-shown), width))
		}
	}
	if flow.key.PublicKey != "" {
		lines = append(lines, "")
		lines = append(lines, text(kittui.StyleText, t.Lookup(textDetailFingerprint, flow.key.Fingerprint))...)
		lines = append(lines, text(kittui.StyleHint, t.Lookup(textDetailKeyWrite))...)
		for _, l := range kittui.Wrap(kittui.Escape(strings.TrimSpace(flow.key.PublicKey)), max(width-detailIndent, 1)) {
			lines = append(lines, strings.Repeat(" ", detailIndent)+m.styles.Render(kittui.StyleActive, l))
		}
	}
	return lines
}

// connectRemote gives the folder its remote. State and commit are those of the comparison the person was shown,
// so the service refuses and changes nothing when the remote or the folder changed since.
func (m *Model) connectRemote(flow *addFlow) tea.Cmd {
	cmp := flow.cmp
	if cmp == nil {
		return nil
	}
	args := control.SourceRemoteArgs{Path: flow.res.Path, URL: flow.url, Fingerprint: flow.probe.Fingerprint,
		ExpectState: cmp.State, ExpectCommit: cmp.Commit}
	return m.writeCall(opRemote, args.Path, func(b Backend) (any, error) { return b.SourceRemote(m.ctx, args) })
}
