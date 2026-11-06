package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// containerInterval is how often the Containers area reads the container engine again while it is shown.
const containerInterval = 5 * time.Second

// containerState is the last container list read. A failed read keeps the groups read before, so a short
// outage does not empty the screen.
type containerState struct {
	groups          []control.ContainerGroup
	loaded, loading bool
	err             error
	// gen separates the refresh timer of the current visit to the area from those of earlier visits.
	gen int
	// actGen separates the answer of the detail read before a question from an older one; readGen separates the
	// answer of the list read that follows an action from a read that began before it.
	actGen, readGen int
}

// containerView is the detail of one container, opened from the list.
type containerView struct {
	open            bool
	name            string
	gen             int
	detail          control.ContainerDetail
	loaded, loading bool
	err             error
	top, total      int
	rows            int
}

type containersMsg struct {
	gen int
	res control.ContainerListResult
	err error
}

type containerDetailMsg struct {
	name string
	gen  int
	res  control.ContainerDetail
	err  error
}

type containerTickMsg struct{ gen int }

func containerColumns(t kittui.Translator, tones map[string]kittui.TextStyle) []kittui.Column {
	tone := func(cell string) kittui.TextStyle {
		if s, ok := tones[cell]; ok {
			return s
		}
		return kittui.StyleText
	}
	return []kittui.Column{
		{Title: textColName},
		{Title: textColState, Tone: tone},
		{Title: textColImage, Flex: true},
		{Title: textColUptime, Optional: true},
		{Title: textColRestarts, Optional: true},
	}
}

var containerStateTexts = map[string]kittui.TextID{
	control.ContainerOK: textCtOK, control.ContainerUnhealthy: textCtUnhealthy, control.ContainerRestarting: textCtRestarting,
	control.ContainerStopped: textCtStopped, control.ContainerStarting: textCtStarting,
}

var containerStateTones = map[string]kittui.TextStyle{
	control.ContainerOK: kittui.StyleOK, control.ContainerUnhealthy: kittui.StyleFail, control.ContainerRestarting: kittui.StyleFail,
	control.ContainerStopped: kittui.StyleWarn, control.ContainerStarting: kittui.StyleActive,
}

// stateWord is the word of a container state; a state this version does not know shows as the service wrote it.
func (m *Model) stateWord(state string) (string, kittui.TextStyle) {
	word, tone := state, kittui.StyleText
	if id, ok := containerStateTexts[state]; ok {
		word = m.text.Lookup(id)
	}
	if s, ok := containerStateTones[state]; ok {
		tone = s
	}
	return word, tone
}

// groupTitle names a group with its kind. The kit orders groups by this text, so the kind word leads it.
func (m *Model) groupTitle(g control.ContainerGroup) string {
	t := m.text
	switch {
	case g.Kind == control.ContainerKindOwn:
		return t.Lookup(textCtGroupOwn, g.Name)
	case g.Kind == control.ContainerKindForeign && g.Conflict:
		return t.Lookup(textCtGroupConflict, g.Name)
	case g.Kind == control.ContainerKindForeign:
		return t.Lookup(textCtGroupForeign, g.Name)
	}
	return t.Lookup(textCtGroupSingle)
}

// uptimeText shortens seconds to the two largest units; a container that does not run has no uptime.
func (m *Model) uptimeText(s int64) string {
	switch {
	case s <= 0:
		return m.text.Lookup(textNoValue)
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh%dm", s/3600, s%3600/60)
	}
	return fmt.Sprintf("%dd%dh", s/86400, s%86400/3600)
}

// containerErrorText says in the message line why the container engine could not be read.
func (m *Model) containerErrorText(err error) string {
	t := m.text
	var remote *control.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return t.Lookup(kittui.TextError, t.Lookup(textErrContainersSlow))
	case errors.As(err, &remote) && remote.Code == control.CodeDockerUnreachable:
		return t.Lookup(kittui.TextError, t.Lookup(textErrDockerUnreachable))
	case errors.As(err, &remote) && remote.Code == control.CodeDockerPermission:
		return t.Lookup(kittui.TextError, t.Lookup(textErrDockerPermission))
	case errors.As(err, &remote) && remote.Code == control.CodeDockerTimeout:
		return t.Lookup(kittui.TextError, t.Lookup(textErrDockerTimeout))
	}
	return m.errorText(err)
}

// startContainers begins a visit to the Containers area: it reads at once and starts the refresh timer. The
// timer of an earlier visit is dropped by its generation.
func (m *Model) startContainers() tea.Cmd {
	m.ct.gen++
	return tea.Batch(m.readContainers(), m.readContainer(), m.containerTimer())
}

func (m *Model) containerTimer() tea.Cmd {
	gen := m.ct.gen
	return m.after(containerInterval, func(time.Time) tea.Msg { return containerTickMsg{gen: gen} })
}

// containerTick refreshes the area and sets the next timer, but only while the area is shown.
func (m *Model) containerTick(msg containerTickMsg) tea.Cmd {
	if msg.gen != m.ct.gen || m.area != areaContainers {
		return nil
	}
	return tea.Batch(m.readContainers(), m.readContainer(), m.containerTimer())
}

// readContainers reads the container list unless a read is under way.
func (m *Model) readContainers() tea.Cmd {
	if m.ct.loading {
		return nil
	}
	m.ct.loading = true
	ctx, b, gen := m.ctx, m.backend, m.ct.readGen
	return func() tea.Msg {
		res, err := b.Containers(ctx)
		return containersMsg{gen: gen, res: res, err: err}
	}
}

func (m *Model) applyContainers(msg containersMsg) {
	if msg.gen != m.ct.readGen {
		return
	}
	m.ct.loading = false
	if msg.err != nil {
		m.ct.err = msg.err
		return
	}
	m.ct.err, m.ct.loaded, m.ct.groups = nil, true, msg.res.Groups
	clear(m.ctTones)
	clear(m.ctNames)
	var items []kittui.ListItem
	var names []string
	for _, g := range msg.res.Groups {
		title := m.groupTitle(g)
		for _, c := range g.Containers {
			word, tone := m.stateWord(c.State)
			m.ctTones[word] = tone
			names = append(names, c.Name)
			items = append(items, kittui.ListItem{Group: title, Cells: []string{c.Name, word, c.Image,
				m.uptimeText(c.UptimeSeconds), fmt.Sprint(c.Restarts)}})
		}
	}
	for i, id := range uniqueIDs(names) {
		items[i].ID = id
		m.ctNames[id] = names[i]
	}
	if err := m.ctList.SetItems(items); err != nil {
		m.ct.err = err
	}
}

// openContainer shows the detail of the container id of the list.
func (m *Model) openContainer(id kittui.ItemID) tea.Cmd {
	name, ok := m.ctNames[id]
	if !ok {
		return nil
	}
	m.ctv = containerView{open: true, name: name, gen: m.ctv.gen + 1}
	return m.readContainer()
}

// readContainer reads the container the detail shows, unless a read is under way.
func (m *Model) readContainer() tea.Cmd {
	v := &m.ctv
	if !v.open || v.loading {
		return nil
	}
	v.loading = true
	ctx, b, name, gen := m.ctx, m.backend, v.name, v.gen
	return func() tea.Msg {
		res, err := b.Container(ctx, name)
		return containerDetailMsg{name: name, gen: gen, res: res, err: err}
	}
}

func (m *Model) applyContainer(msg containerDetailMsg) {
	v := &m.ctv
	if !v.open || msg.gen != v.gen || msg.name != v.name {
		return
	}
	v.loading = false
	if v.err = msg.err; msg.err != nil {
		return
	}
	v.loaded, v.detail = true, msg.res
}

// containersView is the Containers area: the grouped list, or the detail of the container chosen in it.
func (m *Model) containersView(width, height int) string {
	m.scrollNote = ""
	switch {
	case m.ctv.open:
		return m.containerDetailView(width, height)
	case !m.ct.loaded && m.ct.err != nil:
		return strings.Join(m.wrapped(2, kittui.StyleFail, m.containerErrorText(m.ct.err), width), "\n")
	case !m.ct.loaded:
		return m.note(m.text.Lookup(textCtLoading), width)
	}
	view, _ := m.ctList.View(m.styles, width, height)
	return view
}

func (m *Model) containerDetailView(width, height int) string {
	lines := m.containerLines(width)
	v := &m.ctv
	v.total, v.rows = len(lines), height
	v.top = max(min(v.top, v.total-v.rows), 0)
	if v.total > v.rows {
		m.scrollNote = fmt.Sprintf("%d/%d", v.top+v.rows, v.total)
	}
	return strings.Join(lines[v.top:min(v.top+height, len(lines))], "\n")
}

// field is a label and a value in its tone.
type field struct {
	label, value string
	tone         kittui.TextStyle
}

// fieldLines draws the fields with the labels aligned.
func (m *Model) fieldLines(fields []field, width int) []string {
	labelWidth := 0
	for _, f := range fields {
		labelWidth = max(labelWidth, lipgloss.Width(kittui.Escape(f.label)))
	}
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = "  " + m.styles.Render(kittui.StyleHint, kittui.Pad(kittui.Escape(f.label), labelWidth)) + "  " +
			m.styles.Render(f.tone, m.styles.Truncate(kittui.Escape(f.value), max(width-labelWidth-4, 1)))
	}
	return out
}

func (m *Model) containerLines(width int) []string {
	t, v := m.text, &m.ctv
	lines := []string{"  " + m.styles.TitleLine(v.name, "", max(width-2, 1)), ""}
	if v.err != nil {
		return append(lines, m.wrapped(detailMargin, kittui.StyleFail, m.containerErrorText(v.err), width)...)
	}
	if !v.loaded {
		return append(lines, m.line(detailMargin, kittui.StyleHint, t.Lookup(textDetailLoading), width))
	}
	d := v.detail
	word, tone := m.stateWord(d.State)
	if d.EngineState != "" {
		word += " (" + d.EngineState + ")"
	}
	fields := []field{
		{t.Lookup(textCtFieldGroup), m.detailGroup(d), kittui.StyleText},
		{t.Lookup(textCtFieldState), word, tone},
		{t.Lookup(textCtFieldImage), d.Image, kittui.StyleText},
		{t.Lookup(textCtFieldUptime), m.uptimeText(d.UptimeSeconds), kittui.StyleText},
		{t.Lookup(textCtFieldRestarts), fmt.Sprint(d.Restarts), kittui.StyleText},
	}
	if d.Health != "" {
		fields = append(fields, field{t.Lookup(textCtFieldHealth), d.Health, kittui.StyleText})
	}
	lines = append(lines, m.fieldLines(fields, width)...)
	section := func(key kittui.TextID, body []string) {
		if len(body) == 0 {
			body = []string{m.line(detailIndent, kittui.StyleHint, t.Lookup(textCtNone), width)}
		}
		lines = append(lines, "", m.heading(key, width))
		lines = append(lines, body...)
	}
	section(textCtPorts, m.plainLines(portTexts(d.Ports), width))
	section(textCtMounts, m.plainLines(m.mountTexts(d.Mounts), width))
	section(textCtNetworks, m.plainLines(networkTexts(d.Networks), width))
	section(textCtLabels, m.plainLines(labelTexts(d.Labels), width))
	section(textCtEnvNames, m.plainLines(d.EnvNames, width))
	return lines
}

// detailGroup says which stack, project or single container a container belongs to.
func (m *Model) detailGroup(d control.ContainerDetail) string {
	switch d.Kind {
	case control.ContainerKindOwn, control.ContainerKindForeign:
		return m.groupTitle(control.ContainerGroup{Kind: d.Kind, Name: d.Project, Conflict: d.Conflict})
	}
	return m.text.Lookup(textCtKindSingle)
}

func (m *Model) plainLines(texts []string, width int) []string {
	out := make([]string, len(texts))
	for i, s := range texts {
		out[i] = m.line(detailIndent, kittui.StyleText, s, width)
	}
	return out
}

func portTexts(ports []control.ContainerPort) []string {
	out := make([]string, len(ports))
	for i, p := range ports {
		s := fmt.Sprintf("%d/%s", p.PrivatePort, p.Protocol)
		if p.PublicPort != 0 {
			s = fmt.Sprintf("%s:%d->%s", p.HostIP, p.PublicPort, s)
		}
		out[i] = s
	}
	return out
}

func (m *Model) mountTexts(mounts []control.ContainerMount) []string {
	out := make([]string, len(mounts))
	for i, mt := range mounts {
		src := mt.Source
		if mt.Name != "" {
			src = mt.Name
		}
		out[i] = fmt.Sprintf("%s %s:%s", mt.Type, src, mt.Destination)
		if mt.ReadOnly {
			out[i] += " (" + m.text.Lookup(textCtReadOnly) + ")"
		}
	}
	return out
}

func networkTexts(networks []control.ContainerNetwork) []string {
	out := make([]string, len(networks))
	for i, n := range networks {
		out[i] = strings.TrimSpace(n.Name + " " + n.IPAddress)
	}
	return out
}

func labelTexts(labels map[string]string) []string {
	out := make([]string, 0, len(labels))
	for k, v := range labels {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// stackContainerLines are the containers of one stack in the stack detail, from the list the Containers area
// reads. The stack is the group of the own kind with its name.
func (m *Model) stackContainerLines(stack string, width int) []string {
	t := m.text
	var out []string
	if m.ct.err != nil {
		out = m.wrapped(detailIndent, kittui.StyleFail, m.containerErrorText(m.ct.err), width)
		if !m.ct.loaded {
			return out
		}
	} else if !m.ct.loaded {
		return []string{m.line(detailIndent, kittui.StyleHint, t.Lookup(textDetailLoading), width)}
	}
	found := false
	for _, g := range m.ct.groups {
		if g.Kind != control.ContainerKindOwn || g.Name != stack {
			continue
		}
		for _, c := range g.Containers {
			found = true
			word, tone := m.stateWord(c.State)
			out = append(out, m.pair(detailIndent, c.Name, word, tone, width))
			out = append(out, m.line(detailIndent+2, kittui.StyleHint, c.Image+"  "+m.uptimeText(c.UptimeSeconds), width))
		}
	}
	if !found {
		out = append(out, m.line(detailIndent, kittui.StyleHint, t.Lookup(textCtNoneOfStack), width))
	}
	return out
}
