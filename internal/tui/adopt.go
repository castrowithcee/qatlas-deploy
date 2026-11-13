package tui

import (
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// rootStackFolder is the folder the service reports for a stack whose compose file lies directly in the stack
// folder; the interface may not import the stacks package.
const rootStackFolder = "."

// foreignSource is what the Compose labels of a foreign project say about where it lives.
type foreignSource struct {
	project, dir string
	composeFiles []string
	envFiles     []string
}

// adoptProblem is a text of the catalog with its arguments, so a rule stays a pure function.
type adoptProblem struct {
	id   kittui.TextID
	args []any
}

// adoptRejection says why a foreign project cannot become a stack folder. It checks the labels alone while res
// is nil, and the answer of the inspection once there is one. The folder must become a folder of exactly one
// stack whose compose file lies directly in it, and every file the project uses must lie inside the folder.
func adoptRejection(src foreignSource, res *control.SourceInspectResult) (adoptProblem, bool) {
	switch {
	case src.dir == "":
		return adoptProblem{id: textAdoptNoDir}, true
	case !filepath.IsAbs(src.dir):
		return adoptProblem{textAdoptDirRelative, []any{src.dir}}, true
	}
	for _, f := range slices.Concat(src.composeFiles, src.envFiles) {
		if !within(src.dir, f) {
			return adoptProblem{textAdoptOutside, []any{f, src.dir}}, true
		}
	}
	if res == nil || res.Case == control.SourceCaseReject {
		return adoptProblem{}, false
	}
	switch {
	case len(res.Stacks) != 1:
		return adoptProblem{textAdoptStacks, []any{len(res.Stacks)}}, true
	case res.Stacks[0].Folder != rootStackFolder:
		return adoptProblem{textAdoptNotRoot, []any{res.Stacks[0].Folder}}, true
	}
	return adoptProblem{}, false
}

// within says the file lies below dir.
func within(dir, file string) bool {
	if !filepath.IsAbs(file) {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(file))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// adoptWarnings are the differences between what runs and what a deploy by qdeploy would pass to Compose. Each
// one makes the first deploy create new containers; none of them stops the adoption.
func adoptWarnings(src foreignSource, st control.SourceStack) []adoptProblem {
	var out []adoptProblem
	if src.project != st.Project {
		out = append(out, adoptProblem{textAdoptWarnProject, []any{src.project, st.Project}})
	}
	if !samePaths(src.composeFiles, st.ComposeFiles) {
		out = append(out, adoptProblem{textAdoptWarnCompose, []any{src.composeFiles, st.ComposeFiles}})
	}
	if !sameEnvFiles(src, st) {
		out = append(out, adoptProblem{textAdoptWarnEnv, []any{src.envFiles, st.EnvFiles}})
	}
	return out
}

// sameEnvFiles compares the env files of the labels with those a deploy would pass. Without the label Compose
// read the file .env of the working directory if it exists, and a deploy names it only when it exists.
func sameEnvFiles(src foreignSource, st control.SourceStack) bool {
	if len(src.envFiles) > 0 {
		return samePaths(src.envFiles, st.EnvFiles)
	}
	return len(st.EnvFiles) == 0 || samePaths([]string{filepath.Join(src.dir, ".env")}, st.EnvFiles)
}

// samePaths compares two lists of files in order, as cleaned paths.
func samePaths(a, b []string) bool {
	clean := func(l []string) []string {
		out := make([]string, len(l))
		for i, p := range l {
			out[i] = filepath.Clean(p)
		}
		return out
	}
	return slices.Equal(clean(a), clean(b))
}

// groupOf is the group of the container called name.
func (m *Model) groupOf(name string) (control.ContainerGroup, bool) {
	for _, g := range m.ct.groups {
		if slices.ContainsFunc(g.Containers, func(c control.ContainerSummary) bool { return c.Name == name }) {
			return g, true
		}
	}
	return control.ContainerGroup{}, false
}

// foreignTarget is the foreign project the list or the open detail points at: its group row, one of its
// containers, or the detail of one.
func (m *Model) foreignTarget() (foreignSource, bool) {
	var g control.ContainerGroup
	found := false
	if m.ctv.open {
		if g, found = m.groupOf(m.ctv.name); !found && m.ctv.loaded && m.ctv.detail.Kind == control.ContainerKindForeign {
			d := m.ctv.detail
			return foreignSource{project: d.Project, dir: d.WorkingDir, composeFiles: d.ConfigFiles, envFiles: d.EnvFiles}, true
		}
	} else if id, ok := m.ctList.Selected(); ok {
		g, found = m.groupOf(m.ctNames[id])
	} else if key, ok := m.ctList.SelectedGroup(); ok {
		for _, c := range m.ct.groups {
			if m.groupTitle(c) == key {
				g, found = c, true
			}
		}
	}
	if !found || g.Kind != control.ContainerKindForeign {
		return foreignSource{}, false
	}
	return foreignSource{project: g.Name, dir: g.WorkingDir, composeFiles: g.ConfigFiles, envFiles: g.EnvFiles}, true
}

// adoptAction takes the key to add in the Containers area; ok is false when it points at no foreign project.
func (m *Model) adoptAction() (cmd tea.Cmd, ok bool) {
	if m.area != areaContainers || m.clogShown() {
		return nil, false
	}
	src, found := m.foreignTarget()
	if !found {
		return nil, false
	}
	return m.beginAdopt(src), true
}

// adoptHints is the key to take over the project in the key line, while there is one.
func (m *Model) adoptHints() []kittui.Hint {
	if _, ok := m.foreignTarget(); !ok {
		return nil
	}
	return []kittui.Hint{{Action: actAdd, Label: textKeyAdopt, Priority: 3}}
}

// beginAdopt checks the labels, then only inspects the folder. Nothing changes before the questions of the flow.
func (m *Model) beginAdopt(src foreignSource) tea.Cmd {
	switch {
	case m.busy() || m.settingsBusy():
		m.warn(m.text.Lookup(textActionBusy))
		return nil
	case m.folders == nil:
		m.warn(m.text.Lookup(textFoldersNone))
		return nil
	}
	if p, bad := adoptRejection(src, nil); bad {
		m.askAdoptRejection(src, p, nil)
		return nil
	}
	ctx, host := m.ctx, m.folders
	return m.writeCall(opInspect, src.dir, func(b Backend) (any, error) {
		got, err := inspectFolder(ctx, b, host, src.dir)
		if err != nil {
			return nil, err
		}
		if in, ok := got.(inspected); ok {
			in.flow.foreign = &src
		}
		return got, nil
	})
}

// askAdoptRejection tells why the project cannot be taken over; there is no way on.
func (m *Model) askAdoptRejection(src foreignSource, p adoptProblem, res *control.SourceInspectResult) {
	t := m.text
	body := func(width int) []string {
		dir := src.dir
		if dir == "" {
			dir = t.Lookup(textNoValue)
		}
		lines := []string{""}
		lines = append(lines, m.wrapped(detailIndent, kittui.StyleText, t.Lookup(textDetailDir, dir), width)...)
		lines = append(lines, m.foreignLines(src, width)...)
		lines = append(lines, m.wrapped(detailIndent, kittui.StyleFail, t.Lookup(textDetailReason, t.Lookup(p.id, p.args...)), width)...)
		if res != nil {
			lines = append(lines, m.stackLines(*res, width)...)
		}
		return lines
	}
	m.askSheet(textTitleAdopt, t.Lookup(textAskAdoptRejected, src.project), t.Lookup(textWhatNothing), body, nil, nil)
}

// title is the title of the questions of a flow.
func (f *addFlow) title() kittui.TextID {
	if f.foreign != nil {
		return textTitleAdopt
	}
	return textTitleFolderAdd
}

// foreignLines are the project and the files the labels of the foreign project name; the folder is shown by the
// caller.
func (m *Model) foreignLines(src foreignSource, width int) []string {
	t := m.text
	list := func(files []string) string {
		if len(files) == 0 {
			return t.Lookup(textNoValue)
		}
		return strings.Join(files, ", ")
	}
	envFiles := list(src.envFiles)
	if len(src.envFiles) == 0 {
		envFiles = t.Lookup(textAdoptEnvImplicit)
	}
	text := func(id kittui.TextID, args ...any) []string {
		return m.wrapped(detailIndent, kittui.StyleText, t.Lookup(id, args...), width)
	}
	lines := []string{"", m.heading(textHeadForeign, width)}
	lines = append(lines, text(textAdoptProject, src.project)...)
	lines = append(lines, text(textAdoptComposeFiles, list(src.composeFiles))...)
	return append(lines, text(textAdoptEnvFiles, envFiles)...)
}

// warnLines are the differences that make the first deploy start new containers; they stay visible up to the
// last question.
func (m *Model) warnLines(flow *addFlow, width int) []string {
	if flow.foreign == nil || len(flow.res.Stacks) != 1 {
		return nil
	}
	warnings := adoptWarnings(*flow.foreign, flow.res.Stacks[0])
	if len(warnings) == 0 {
		return nil
	}
	t := m.text
	list := func(v any) string {
		if files, ok := v.([]string); ok {
			if len(files) == 0 {
				return t.Lookup(textNoValue)
			}
			return strings.Join(files, ", ")
		}
		if s, ok := v.(string); ok && s == "" {
			return t.Lookup(textNoValue)
		}
		return v.(string)
	}
	lines := []string{"", m.heading(textHeadWarning, width)}
	for _, w := range warnings {
		args := make([]any, len(w.args))
		for i, a := range w.args {
			args[i] = list(a)
		}
		lines = append(lines, m.wrapped(detailIndent, kittui.StyleWarn, t.Lookup(w.id, args...), width)...)
	}
	return append(lines, m.wrapped(detailIndent, kittui.StyleWarn, t.Lookup(textAdoptWarnEffect), width)...)
}

// rereadForeign reads the Containers area again after a project was taken over, so its group shows the change;
// it does nothing for a folder added elsewhere.
func (m *Model) rereadForeign() tea.Cmd {
	if m.area != areaContainers {
		return nil
	}
	return m.rereadContainers()
}
