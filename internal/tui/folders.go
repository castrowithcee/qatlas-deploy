package tui

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

const (
	// formFolder asks for the path of a folder to add; formFolderURL for the address a missing or empty one is
	// cloned from.
	formFolder    formKind = formGeneral + 1
	formFolderURL formKind = formFolder + 1
)

// shownEntries is how many files and left-out paths the inspection of a folder lists before it counts the rest.
const shownEntries = 10

// polledLayout is the time of the last poll in the folder list.
const polledLayout = "2006-01-02 15:04"

// addFlow is a stack folder being added: the answer of its inspection and what the later steps found. Neither
// the host nor the service changes before the person said yes to the last question.
type addFlow struct {
	res control.SourceInspectResult
	// readOnly says the host found the folder on a read-only file system; res is then a rejection.
	readOnly bool
	// missing says a clone goes into a folder that does not exist yet.
	missing bool
	plan    PermPlan
	url     string
	probe   control.RepoProbeResult
	// foreign is set when the folder is that of a foreign Compose project the person chose in the Containers
	// area; the flow is the same, with the labels shown and compared.
	foreign *foreignSource
}

// inspected is the answer of the inspection of a folder to add.
type inspected struct{ flow *addFlow }

// keyChecked is the answer of the look for the deploy key of a folder to add.
type keyChecked struct{ found bool }

// listChanged is the answer for a folder of files whose list changed after it was shown; nothing was changed.
type listChanged struct{ path string }

// hostError is a failure of the host side, which is no failure of the service.
type hostError struct{ err error }

func (e hostError) Error() string { return e.err.Error() }
func (e hostError) Unwrap() error { return e.err }

// keptChanges is a failure after the permissions of the folder were changed; they stay changed.
type keptChanges struct {
	err     error
	changed int
}

func (e keptChanges) Error() string { return e.err.Error() }
func (e keptChanges) Unwrap() error { return e.err }

func newFolderList(keys *kittui.Keymap, text kittui.Translator) (*kittui.List, error) {
	failed := text.Lookup(textFolderFailed)
	l, err := kittui.NewList(kittui.ListSpec{Columns: []kittui.Column{{Title: textColPath}, {Title: textColKind},
		{Title: textColCommit}, {Title: textColState, Tone: func(cell string) kittui.TextStyle {
			if cell == failed {
				return kittui.StyleFail
			}
			return kittui.StyleText
		}}, {Title: textColPolled, Optional: true, Dimmed: true}},
		Keys: listKeys(), Search: kittui.SearchByKey, Empty: textFoldersEmpty}, keys, text)
	if err != nil {
		return nil, err
	}
	l.SetFocused(true)
	return l, nil
}

// fillFolders sets the rows of the folder list from the status read last.
func (m *Model) fillFolders() {
	s, t := m.set, m.text
	src := m.snap.status.Engine.Sources
	s.folderData = map[kittui.ItemID]control.SourceStatus{}
	items := make([]kittui.ListItem, len(src))
	for i, f := range src {
		id := kittui.ItemID(f.Path)
		s.folderData[id] = f
		kind, state, polled, head := t.Lookup(textFolderKindLocal), t.Lookup(textFolderOK), t.Lookup(textNoValue), t.Lookup(textNoValue)
		if f.Kind == control.SourceKindRemote {
			kind = t.Lookup(textFolderKindRemote)
		}
		if f.PollError != "" {
			state = t.Lookup(textFolderFailed)
		}
		if !f.LastPoll.IsZero() {
			polled = f.LastPoll.In(m.loc).Format(polledLayout)
		}
		if f.Head != "" {
			head = f.Head[:min(len(f.Head), shortCommit)]
		}
		items[i] = kittui.ListItem{ID: id, Cells: []string{f.Path, kind, head, state, polled}}
	}
	if err := s.folderList.SetItems(items); err != nil {
		m.failure = t.Lookup(kittui.TextError, err.Error())
	}
}

// shortCommit is the length of a commit in the folder list, which has little room.
const shortCommit = 7

func (m *Model) selectedFolder() (control.SourceStatus, bool) {
	id, ok := m.set.folderList.Selected()
	f, found := m.set.folderData[id]
	return f, ok && found
}

// foldersView draws the folder page: the list, and below it what the list leaves out of the chosen folder.
func (m *Model) foldersView(width, height int) string {
	s, t := m.set, m.text
	lines := []string{m.heading(textPageFolders, width), ""}
	if !m.loaded {
		lines = append(lines, m.line(detailIndent, kittui.StyleHint, t.Lookup(textDetailLoading), width))
		return strings.Join(lines, "\n")
	}
	var detail []string
	if f, ok := m.selectedFolder(); ok {
		detail = append([]string{""}, m.folderDetail(f, width)...)
	}
	room := max(height-len(lines)-len(detail), 1)
	rows := room
	if l := s.folderList; len(s.folderData) > 0 {
		// The header and the rows, and the search line while there is one; the rest of the room is the detail's.
		rows = len(s.folderData) + 1
		if l.Searching() || l.Query() != "" {
			rows++
		}
		rows = min(rows, room)
	}
	view := m.listView(s.folderList, width, rows, len(lines))
	out := append(lines, strings.Split(view, "\n")...)
	out = append(out, detail...)
	return strings.Join(out[:min(len(out), height)], "\n")
}

// folderDetail is the address, the last poll and why it failed for one folder.
func (m *Model) folderDetail(f control.SourceStatus, width int) []string {
	t := m.text
	lines := []string{m.line(detailMargin, kittui.StyleActive, f.Path, width)}
	if f.URL != "" {
		lines = append(lines, m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailURL, f.URL), width))
	}
	if !f.LastPoll.IsZero() {
		lines = append(lines, m.line(detailIndent, kittui.StyleText, t.Lookup(textDetailLastPoll, m.stamp(f.LastPoll.In(m.loc))), width))
	}
	if f.PollError != "" {
		lines = append(lines, m.wrapped(detailIndent, kittui.StyleFail, t.Lookup(textDetailPollError, f.PollError), width)...)
	}
	if f.Note != "" {
		lines = append(lines, m.wrapped(detailIndent, kittui.StyleHint, t.Lookup(textDetailNote, f.Note), width)...)
	}
	return lines
}

func (m *Model) askFolderFetch() tea.Cmd {
	f, ok := m.selectedFolder()
	switch {
	case !ok:
		m.warn(m.text.Lookup(textChooseOne))
		return nil
	case f.Kind != control.SourceKindRemote:
		m.warn(m.text.Lookup(textFolderLocalFetch))
		return nil
	}
	m.ask(textPageFolders, m.text.Lookup(textAskFetch, f.Path), m.text.Lookup(textWhatFetch), nil, false, func() tea.Cmd {
		return m.writeCall(opFetch, f.Path, func(b Backend) (any, error) { return b.RepoFetch(m.ctx, f.Path) })
	})
	return nil
}

// fetchDone reports the fetch of one folder: its own entry of the answer, else the answer itself.
func (m *Model) fetchDone(res control.RepoFetchResult, dir string, done func(string, ...string)) {
	t := m.text
	head, remote, newer := res.Head, res.RemoteHead, res.UpdatesAvailable
	if i := slices.IndexFunc(res.Sources, func(s control.RepoFetchSource) bool { return s.Dir == dir }); i >= 0 {
		e := res.Sources[i]
		if e.Error != "" {
			m.failure = t.Lookup(kittui.TextError, t.Lookup(textFetchFailed, dir, e.Error))
			return
		}
		head, remote, newer = e.Head, e.RemoteHead, e.UpdatesAvailable
	}
	text := t.Lookup(textDoneFetchedCurrent, dir)
	if newer {
		text = t.Lookup(textDoneFetchedNew, dir)
	}
	done(text, t.Lookup(textDetailHead, short(head)), t.Lookup(textDetailRemote, short(remote)))
}

func (m *Model) askFolderRemove() tea.Cmd {
	f, ok := m.selectedFolder()
	if !ok {
		m.warn(m.text.Lookup(textChooseOne))
		return nil
	}
	m.ask(textPageFolders, m.text.Lookup(textAskRelease, f.Path), m.text.Lookup(textWhatRelease), nil, false, func() tea.Cmd {
		return m.writeCall(opSourceRemove, f.Path, func(b Backend) (any, error) { return b.SourceRemove(m.ctx, f.Path) })
	})
	return nil
}

// openFolderKey opens the key page for the chosen folder; a local folder has no key to offer.
func (m *Model) openFolderKey() tea.Cmd {
	f, ok := m.selectedFolder()
	switch {
	case !ok:
		m.warn(m.text.Lookup(textChooseOne))
		return nil
	case f.Kind != control.SourceKindRemote:
		m.warn(m.text.Lookup(textFolderLocalKey))
		return nil
	}
	return m.openKeyPage(f.Path)
}

// openKeyPage shows the key page for exactly the folder dir.
func (m *Model) openKeyPage(dir string) tea.Cmd {
	s := m.set
	s.page, s.keyFor, s.key, s.noKey = pageKey, dir, part{}, false
	return m.loadPage(true)
}

func (m *Model) folderForm() *settingsForm {
	return m.newSettingsForm(formFolder, textTitleFolderAdd, []kittui.FormField{
		textField("path", textFieldFolderPath, textHintFolderPath, ""),
	})
}

// submitFolder inspects the folder of the form; what it is decides the next step. Nothing changes here.
func (m *Model) submitFolder(f *settingsForm, path string) tea.Cmd {
	switch {
	case path == "":
		f.problems(m.bad("path", textProblemDirRequired))
		return nil
	case !filepath.IsAbs(path) || filepath.Clean(path) != path:
		f.problems(m.bad("path", textProblemDir))
		return nil
	case m.folders == nil:
		f.problem(m.text.Lookup(textFoldersNone), m.text.Lookup(textFormRetry))
		return nil
	}
	ctx, host := m.ctx, m.folders
	return m.writeCall(opInspect, path, func(b Backend) (any, error) { return inspectFolder(ctx, b, host, path) })
}

// inspectFolder asks the service what the folder is and adds what only the host can tell: a read-only file
// system, a missing folder and the permissions the service would need. It only reads.
func inspectFolder(ctx context.Context, b Backend, host Folders, path string) (any, error) {
	res, err := b.SourceInspect(ctx, path, "")
	if err != nil {
		return nil, err
	}
	flow := &addFlow{res: res}
	if res.Case == control.SourceCaseReject {
		return inspected{flow}, nil
	}
	// The service is sandboxed and sees every folder it may not write as read-only, so only the host can tell.
	readOnly, err := host.ReadOnly(path)
	if err != nil {
		return nil, hostError{err}
	}
	if readOnly {
		flow.readOnly = true
		flow.res = control.SourceInspectResult{Path: res.Path, Case: control.SourceCaseReject, Stacks: []control.SourceStack{}}
		return inspected{flow}, nil
	}
	if res.Case == control.SourceCaseClone {
		if flow.missing, err = host.Missing(path); err != nil {
			return nil, hostError{err}
		}
	}
	if flow.plan, err = host.PlanPermissions(ctx, res); err != nil {
		return nil, hostError{err}
	}
	return inspected{flow}, nil
}

// showInspection shows what the inspection found and asks for the next step of the case.
func (m *Model) showInspection(flow *addFlow) {
	t, res, title := m.text, flow.res, flow.title()
	body := func(width int) []string { return m.inspectionLines(flow, width) }
	cont := []kittui.Answer{{Action: actYes, Label: textAnswerContinue}, {Action: actNo, Label: textAnswerCancel}}
	yes := []kittui.Answer{{Action: actYes, Label: textAnswerYes}, {Action: actNo, Label: textAnswerCancel}}
	if flow.foreign != nil {
		if p, bad := adoptRejection(*flow.foreign, &res); bad {
			m.askAdoptRejection(*flow.foreign, p, &res)
			return
		}
	}
	switch res.Case {
	case control.SourceCaseClone:
		m.askSheet(title, t.Lookup(textAskFolderClone, res.Path), t.Lookup(textWhatFolderClone), body, cont,
			func() tea.Cmd { return m.openForm(m.folderURLForm()) })
	case control.SourceCaseAdopt:
		flow.url = res.URL
		m.askSheet(title, t.Lookup(textAskFolderAdopt, res.Path, res.URL), t.Lookup(textWhatFolderAdopt), body, cont,
			m.probeFolder)
	case control.SourceCaseLocal:
		m.askSheet(title, t.Lookup(textAskFolderLocal, res.Path), t.Lookup(textWhatFolderApply), body, yes,
			func() tea.Cmd { return m.addFolder(flow) })
	case control.SourceCaseFiles:
		if len(res.PlannedFiles) == 0 {
			m.askSheet(title, t.Lookup(textAskFolderNoFiles, res.Path), t.Lookup(textWhatFolderNoFiles), body, nil, nil)
			return
		}
		m.askSheet(title, t.Lookup(textAskFolderFiles, res.Path), t.Lookup(textWhatFolderFiles), body, yes,
			func() tea.Cmd { return m.addFolder(flow) })
	default:
		m.askSheet(title, t.Lookup(textAskFolderRejected, res.Path), t.Lookup(textWhatNothing), body, nil, nil)
	}
}

func (m *Model) folderURLForm() *settingsForm {
	return m.newSettingsForm(formFolderURL, textTitleFolderURL, []kittui.FormField{textField("url", textFieldURL, textHintURL, "")})
}

func (m *Model) submitFolderURL(f *settingsForm, url string) tea.Cmd {
	// Only the service knows which addresses it accepts; it answers the probe with the reason.
	if url == "" || strings.ContainsFunc(url, unicode.IsSpace) {
		f.problems(m.bad("url", textProblemURL))
		return nil
	}
	if m.set.add == nil {
		m.dropForm()
		return nil
	}
	m.set.add.url = url
	return m.probeFolder()
}

// probeFolder reads the host key the server of the address presents; it stores nothing.
func (m *Model) probeFolder() tea.Cmd {
	flow := m.set.add
	if flow == nil {
		return nil
	}
	url := flow.url
	return m.writeCall(opProbe, "", func(b Backend) (any, error) { return b.RepoProbe(m.ctx, url) })
}

// askHostKey asks whether the host key the server presented is the one to trust. Yes passes nothing on: the
// key reaches the service only with the last question.
func (m *Model) askHostKey() {
	flow, t := m.set.add, m.text
	if flow == nil {
		return
	}
	consequence := t.Lookup(textWhatHostKey)
	if flow.probe.Pinned {
		consequence = t.Lookup(textWhatHostKeyPinned)
	}
	m.ask(flow.title(), t.Lookup(textAskHostKey, flow.url), consequence, m.hostKeyDetails(flow), false, func() tea.Cmd {
		return m.checkFolderKey(flow)
	})
}

func (m *Model) hostKeyDetails(flow *addFlow) []string {
	t, p := m.text, flow.probe
	target := p.Host
	if p.Port != "22" && p.Port != "" {
		target += ":" + p.Port
	}
	return []string{t.Lookup(textDetailHost, target), t.Lookup(textDetailKeyType, p.KeyType),
		t.Lookup(textDetailFingerprint, p.Fingerprint), t.Lookup(textDetailDir, flow.res.Path)}
}

// checkFolderKey looks for the deploy key of the folder; without one the clone could not work.
func (m *Model) checkFolderKey(flow *addFlow) tea.Cmd {
	path := flow.res.Path
	return m.writeCall(opKeyCheck, path, func(b Backend) (any, error) {
		_, err := b.KeyShow(m.ctx, path)
		var remote *control.Error
		switch {
		case err == nil:
			return keyChecked{found: true}, nil
		case errors.As(err, &remote) && remote.Code == control.CodeNotFound:
			return keyChecked{}, nil
		}
		return nil, err
	})
}

// keyDone takes the look for the deploy key: without one the key page for the folder opens and the adding ends
// there; with one comes the last question.
func (m *Model) keyDone(found bool) tea.Cmd {
	s, t := m.set, m.text
	flow := s.add
	if flow == nil {
		return nil
	}
	if !found {
		s.add = nil
		cmd := m.openKeyPage(flow.res.Path)
		m.tell(textTitleKey, t.Lookup(textFolderNoKey, flow.res.Path), []string{t.Lookup(textFolderNoKeyNext)})
		return cmd
	}
	question := t.Lookup(textAskFolderAdopt, flow.res.Path, flow.url)
	if flow.res.Case == control.SourceCaseClone {
		question = t.Lookup(textAskFolderCloneFinal, flow.url, flow.res.Path)
	}
	body := func(width int) []string {
		var lines []string
		for _, d := range m.hostKeyDetails(flow) {
			lines = append(lines, m.wrapped(detailIndent, kittui.StyleText, d, width)...)
		}
		lines = append(lines, m.warnLines(flow, width)...)
		return append(lines, m.changeLines(flow, width)...)
	}
	m.askSheet(flow.title(), question, t.Lookup(textWhatFolderFinal), body,
		[]kittui.Answer{{Action: actYes, Label: textAnswerYes}, {Action: actNo, Label: textAnswerCancel}},
		func() tea.Cmd { return m.addFolder(flow) })
	return nil
}

// addFolder makes the changes the person confirmed: the permissions, the drop-in, then the call of the service.
// A folder of files is looked at once more first, and nothing changes when its list differs from the one shown.
func (m *Model) addFolder(flow *addFlow) tea.Cmd {
	ctx, host, path := m.ctx, m.folders, flow.res.Path
	return m.writeCall(opFolderAdd, path, func(b Backend) (any, error) {
		res := flow.res
		if res.Case == control.SourceCaseFiles {
			now, err := b.SourceInspect(ctx, path, "")
			if err != nil {
				return nil, err
			}
			if listDiffers(res, now) {
				return listChanged{path: path}, nil
			}
		}
		if !flow.plan.Empty() {
			if err := host.ApplyPermissions(ctx, flow.plan); err != nil {
				return nil, hostError{err}
			}
		}
		kept := func(err error) error {
			if err != nil && !flow.plan.Empty() {
				return keptChanges{err: err, changed: flow.plan.Changed}
			}
			return err
		}
		if err := host.Grant(ctx, path, flow.missing); err != nil {
			return nil, kept(hostError{err})
		}
		switch res.Case {
		case control.SourceCaseClone, control.SourceCaseAdopt:
			set, err := b.RepoSet(ctx, control.RepoSetArgs{URL: flow.url, Dir: path, Fingerprint: flow.probe.Fingerprint})
			return set, kept(err)
		case control.SourceCaseFiles:
			added, err := b.SourceAdd(ctx, control.SourceAddArgs{Path: path, Files: res.PlannedFiles})
			return added, kept(err)
		}
		added, err := b.SourceAdd(ctx, control.SourceAddArgs{Path: path})
		return added, kept(err)
	})
}

// listDiffers says the answer now differs from the one shown in the case, the planned files or what is left out.
func listDiffers(shown, now control.SourceInspectResult) bool {
	excluded := func(r control.SourceInspectResult) []string {
		out := make([]string, 0, len(r.Excluded))
		for _, e := range r.Excluded {
			out = append(out, e.Path+"\x00"+e.Reason)
		}
		return out
	}
	same := func(a, b []string) bool {
		a, b = slices.Clone(a), slices.Clone(b)
		slices.Sort(a)
		slices.Sort(b)
		return slices.Equal(a, b)
	}
	return now.Case != shown.Case || !same(shown.PlannedFiles, now.PlannedFiles) || !same(excluded(shown), excluded(now))
}

// inspectionLines are what the inspection found, and for a folder that can be taken over what yes changes.
func (m *Model) inspectionLines(flow *addFlow, width int) []string {
	t, res := m.text, flow.res
	text := func(style kittui.TextStyle, s string) []string { return m.wrapped(detailIndent, style, s, width) }
	lines := []string{""}
	lines = append(lines, text(kittui.StyleText, t.Lookup(textDetailDir, res.Path))...)
	lines = append(lines, text(kittui.StyleText, t.Lookup(textDetailCase, t.Lookup(caseText(res.Case))))...)
	if flow.foreign != nil {
		lines = append(lines, m.foreignLines(*flow.foreign, width)...)
		lines = append(lines, m.warnLines(flow, width)...)
		lines = append(lines, "")
	}
	if res.Case == control.SourceCaseReject {
		reason, fix := res.Reason, res.Fix
		if flow.readOnly {
			reason, fix = t.Lookup(textReasonReadOnly), t.Lookup(textFixReadOnly)
		}
		lines = append(lines, text(kittui.StyleFail, t.Lookup(textDetailReason, reason))...)
		if fix != "" {
			lines = append(lines, text(kittui.StyleText, t.Lookup(textDetailFix, fix))...)
		}
		return lines
	}
	if a := res.Access; a != nil {
		if a.ModePath != res.Path {
			lines = append(lines, text(kittui.StyleText, t.Lookup(textDetailMissing, a.ModePath))...)
		}
		lines = append(lines, text(kittui.StyleText, t.Lookup(textDetailOwner, nameOrID(a.Owner, a.OwnerID), nameOrID(a.Group, a.GroupID), a.Mode))...)
		lines = append(lines, text(kittui.StyleText, t.Lookup(textDetailServiceWrites, yesNo(t, a.ServiceCanWrite)))...)
		lines = append(lines, text(kittui.StyleText, t.Lookup(textDetailSetsPerms, yesNo(t, !flow.plan.Empty())))...)
		if a.GitWritable != nil {
			lines = append(lines, text(kittui.StyleText, t.Lookup(textDetailGitWritable, yesNo(t, *a.GitWritable)))...)
		}
	}
	if res.Case == control.SourceCaseFiles {
		lines = append(lines, "", m.line(detailMargin, kittui.StyleActive, t.Lookup(textHeadFiles, len(res.PlannedFiles)), width))
		lines = append(lines, m.entryLines(res.PlannedFiles, width)...)
		lines = append(lines, "", m.line(detailMargin, kittui.StyleActive, t.Lookup(textHeadExcluded, len(res.Excluded)), width))
		if len(res.Excluded) == 0 {
			lines = append(lines, text(kittui.StyleHint, t.Lookup(textExcludedNone))...)
		}
		var left []string
		for _, e := range res.Excluded {
			entry := t.Lookup(textExcludedEntry, e.Path, e.Reason)
			if e.Stack != "" {
				entry = t.Lookup(textExcludedStack, e.Path, e.Reason, e.Stack)
			}
			left = append(left, entry)
		}
		lines = append(lines, m.entryLines(left, width)...)
	}
	lines = append(lines, m.stackLines(res, width)...)
	return append(lines, m.changeLines(flow, width)...)
}

// entryLines lists the first entries and counts the rest.
func (m *Model) entryLines(entries []string, width int) []string {
	var lines []string
	for i, e := range entries {
		if i == shownEntries {
			lines = append(lines, m.line(detailIndent, kittui.StyleHint, m.text.Lookup(textMoreEntries, len(entries)-i), width))
			break
		}
		lines = append(lines, m.line(detailIndent, kittui.StyleText, e, width))
	}
	return lines
}

// stackLines are the stacks the folder brings: what works and what does not, the findings and the containers
// that would collide, each briefly.
func (m *Model) stackLines(res control.SourceInspectResult, width int) []string {
	t := m.text
	text := func(indent int, style kittui.TextStyle, s string) []string { return m.wrapped(indent, style, s, width) }
	var lines []string
	if len(res.Stacks) > 0 {
		lines = append(lines, "", m.line(detailMargin, kittui.StyleActive, t.Lookup(textHeadStacks, len(res.Stacks)), width))
	}
	for _, st := range res.Stacks {
		lines = append(lines, text(detailIndent, kittui.StyleTitle, t.Lookup(textStackFolder, st.Name, st.Folder))...)
		var works, fails []string
		for _, f := range st.Functions {
			if f.Works {
				works = append(works, f.Function)
			} else {
				fails = append(fails, f.Function)
			}
		}
		if len(works) > 0 {
			lines = append(lines, text(detailIndent+2, kittui.StyleText, t.Lookup(textStackWorks, strings.Join(works, ", ")))...)
		}
		if len(fails) > 0 {
			lines = append(lines, text(detailIndent+2, kittui.StyleWarn, t.Lookup(textStackFails, strings.Join(fails, ", ")))...)
		}
		for _, f := range st.Findings {
			style := kittui.StyleWarn
			if f.Severity == "error" {
				style = kittui.StyleFail
			}
			lines = append(lines, text(detailIndent+2, style, t.Lookup(textStackFinding, f.Severity, f.Rule, f.Cause))...)
		}
		for _, c := range st.Conflicts {
			lines = append(lines, text(detailIndent+2, kittui.StyleWarn, t.Lookup(textStackConflict, c.Container, c.Project))...)
		}
	}
	if res.StacksError != "" {
		lines = append(lines, text(detailIndent, kittui.StyleFail, t.Lookup(textStacksError, res.StacksError))...)
	}
	if res.ContainersError != "" {
		lines = append(lines, text(detailIndent, kittui.StyleWarn, t.Lookup(textContainersError, res.ContainersError))...)
	}
	return lines
}

// changeLines are the changes yes makes.
func (m *Model) changeLines(flow *addFlow, width int) []string {
	t := m.text
	text := func(s string) []string { return m.wrapped(detailIndent, kittui.StyleText, s, width) }
	lines := []string{"", m.line(detailMargin, kittui.StyleActive, t.Lookup(textHeadChanges), width)}
	switch flow.res.Case {
	case control.SourceCaseClone:
		if flow.missing {
			lines = append(lines, text(t.Lookup(textChangeCreate))...)
		}
		lines = append(lines, text(t.Lookup(textChangeClone))...)
	case control.SourceCaseAdopt:
		lines = append(lines, text(t.Lookup(textChangeAdopt))...)
	case control.SourceCaseFiles:
		lines = append(lines, text(t.Lookup(textChangeFiles))...)
	default:
		lines = append(lines, text(t.Lookup(textChangeLocal))...)
	}
	if flow.plan.Empty() {
		lines = append(lines, text(t.Lookup(textChangeNoPerms))...)
	}
	// The plan is the host side's own words; they stay as it wrote them.
	for _, l := range flow.plan.Summary {
		lines = append(lines, m.wrapped(detailIndent, kittui.StyleText, l, width)...)
	}
	return append(lines, text(t.Lookup(textChangeDropIn))...)
}

func caseText(c string) kittui.TextID {
	switch c {
	case control.SourceCaseClone:
		return textCaseClone
	case control.SourceCaseAdopt:
		return textCaseAdopt
	case control.SourceCaseLocal:
		return textCaseLocal
	case control.SourceCaseFiles:
		return textCaseFiles
	}
	return textCaseReject
}

func nameOrID(name string, id uint32) string {
	if name != "" {
		return name
	}
	return strconv.FormatUint(uint64(id), 10)
}
