package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/datadir"
)

// The data directories of a stack that waits for them: the detail shows the finding, one key opens a form for
// owner and mode, a confirmation names what would be created, and only that confirmation creates the
// directories and starts a run of the stack.

const formDataDirs formKind = formSecret + 1

// findingDataDirMissing is the kind of the engine finding for missing data directories; the interface does not
// import the engine, so the name is repeated here.
const findingDataDirMissing = "data-dir-missing"

// dataDirsResult is what the confirmed creation came to. A creation that failed has createErr and nothing else
// to say; a run that could not be started leaves the directories made.
type dataDirsResult struct {
	stack    string
	res      datadir.Result
	createEr error
	run      control.DeployResult
	deployEr error
}

// dataDirFinding is the data-dir-missing finding of a stack, if the service reports one.
func (m *Model) dataDirFinding(stack string) (control.FindingInfo, bool) {
	for _, st := range m.snap.status.Engine.Stacks {
		if st.Name != stack {
			continue
		}
		for _, f := range st.Findings {
			if f.Kind == findingDataDirMissing && len(f.Dirs) > 0 {
				return f, true
			}
		}
	}
	return control.FindingInfo{}, false
}

// commonOwner is the owner every directory proposes, empty when they propose none or different ones.
func commonOwner(dirs []control.DirInfo) string {
	owner := ""
	for i, d := range dirs {
		if d.Owner == "" || (i > 0 && d.Owner != owner) {
			return ""
		}
		owner = d.Owner
	}
	return owner
}

func dirPaths(dirs []control.DirInfo) []string {
	paths := make([]string, len(dirs))
	for i, d := range dirs {
		paths[i] = d.Path
	}
	return paths
}

// dataDirSection is the finding in the detail: the paths with the proposed owners and the way to create them.
func (m *Model) dataDirSection(stack string, width int) []string {
	f, ok := m.dataDirFinding(stack)
	if !ok {
		return nil
	}
	t := m.text
	lines := []string{"", m.heading(textDetailDataDirs, width)}
	for _, d := range f.Dirs {
		text := t.Lookup(textDataDirLineNoOwner, d.Path)
		if d.Owner != "" {
			text = t.Lookup(textDataDirLine, d.Path, d.Owner)
		}
		lines = append(lines, m.line(detailIndent, kittui.StyleWarn, text, width))
	}
	return append(lines, m.wrapped(detailIndent, kittui.StyleHint, t.Lookup(textDataDirsHint), width)...)
}

// formDataDirLines are the paths a data directory form is about, shown above its fields.
func (m *Model) formDataDirLines(f *settingsForm, width int) []string {
	lines := m.wrapped(detailMargin, kittui.StyleHint, m.text.Lookup(textDataDirsIntro, f.stackDir), width)
	for _, d := range f.dirs {
		lines = append(lines, m.line(detailIndent, kittui.StyleText, d.Path, width))
	}
	return append(lines, "")
}

// openDataDirForm opens the owner and mode form for the missing directories of the detail's stack. Without
// the finding, or without root, it only says so.
func (m *Model) openDataDirForm() tea.Cmd {
	stack, ok := m.detailStack()
	if !ok || m.detailBusy() {
		return nil
	}
	finding, ok := m.dataDirFinding(stack)
	if !ok {
		m.warn(m.text.Lookup(textDataDirsNone))
		return nil
	}
	if m.euid() != 0 {
		m.warn(m.text.Lookup(textDataDirsNeedRoot))
		return nil
	}
	dir, ok := m.snap.stacks.StackDir(stack)
	if !ok {
		m.warn(m.text.Lookup(textDataDirsNoFolder))
		return nil
	}
	f := m.newSettingsForm(formDataDirs, textTitleDataDirs, []kittui.FormField{
		textField("owner", textFieldDataOwner, textHintDataOwner, commonOwner(finding.Dirs)),
		textField("mode", textFieldDataMode, textHintDataMode, fmt.Sprintf("%04o", datadir.DefaultMode)),
	})
	if f != nil {
		f.stack, f.dirs, f.stackDir = stack, finding.Dirs, dir
	}
	return m.openForm(f)
}

// submitDataDirs checks owner and mode like the command line does and, when they hold up, asks for the
// confirmation. Nothing is created before its yes.
func (m *Model) submitDataDirs(f *settingsForm, ownerText, modeText string) tea.Cmd {
	t := m.text
	var ps []kittui.FormProblem
	owner, err := datadir.ParseOwner(ownerText)
	if err != nil {
		ps = append(ps, m.bad("owner", textProblemDataOwner))
	}
	mode, err := datadir.ParseMode(modeText)
	if err != nil {
		ps = append(ps, m.bad("mode", textProblemDataMode))
	}
	if len(ps) > 0 {
		f.problems(ps...)
		return nil
	}
	stack, paths, stackDir := f.stack, dirPaths(f.dirs), f.stackDir
	details := []string{t.Lookup(textDataDirsFolder, stackDir), t.Lookup(textDataDirsOwner, owner),
		t.Lookup(textDataDirsMode, fmt.Sprintf("%04o", mode))}
	for _, p := range paths {
		details = append(details, "  "+p)
	}
	req := datadir.Request{StackDir: stackDir, Paths: paths, Owner: owner, Mode: mode}
	m.ask(textTitleDataDirs, t.Lookup(textAskDataDirs, stack), t.Lookup(textWhatDataDirs), details, false, func() tea.Cmd {
		return m.createDataDirs(stack, req)
	})
	return nil
}

// createDataDirs makes the confirmed directories and then starts a run of the stack, off the event loop.
func (m *Model) createDataDirs(stack string, req datadir.Request) tea.Cmd {
	mk := m.makeDirs
	return m.writeCall(opDataDirs, stack, func(b Backend) (any, error) {
		out := dataDirsResult{stack: stack}
		if out.res, out.createEr = mk(req); out.createEr != nil {
			return out, nil
		}
		out.run, out.deployEr = b.Deploy(m.ctx, stack)
		return out, nil
	})
}

// dataDirsDone shows what a confirmed creation came to; a creation that failed stays in the open form.
func (m *Model) dataDirsDone(out dataDirsResult, done func(text string, details ...string)) tea.Cmd {
	t := m.text
	var details []string
	add := func(id kittui.TextID, list []string) {
		if len(list) > 0 {
			details = append(details, t.Lookup(id, strings.Join(list, ", ")))
		}
	}
	add(textDataDirsCreated, out.res.Created)
	add(textDataDirsParents, out.res.Parents)
	add(textDataDirsExisting, out.res.Existing)
	if out.createEr != nil {
		if m.set.form != nil {
			m.set.form.problem(t.Lookup(textErrDataDirs, out.createEr.Error()), t.Lookup(textFormRetry))
		}
		return m.reloadNow()
	}
	switch {
	case out.deployEr != nil:
		details = append(details, t.Lookup(textDataDirsDeployFailed, m.serviceText(out.deployEr)))
	default:
		details = append(details, t.Lookup(textDoneDeploy, out.run.ID, short(out.run.Commit)))
	}
	done(t.Lookup(textDoneDataDirs, out.stack), details...)
	return m.reloadNow()
}
