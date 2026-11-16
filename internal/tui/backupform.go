package tui

import (
	"errors"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The backup and the compose files of a stack: two forms in the detail. Each sets its own section of the
// stack settings only; the rest stays as the service holds it.

const (
	formBackup  formKind = formUpdate + 1
	formCompose formKind = formBackup + 1
)

// The limits of the two sections, as the service enforces them; the forms check them early so a refusal comes
// at the field. The interface cannot import the rules, so they are repeated here.
const (
	maxComposeFiles = 16
	defaultKeep     = 7
)

var (
	volumeNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	// backupPathRe and composePathRe find the field path the service names in a refusal.
	backupPathRe  = regexp.MustCompile(`backup(?:\.(paths|volumes|command|retention\.keep|retention\.max_age_days))?(?:\[\d+\])?:`)
	composePathRe = regexp.MustCompile(`compose\.files(?:\[\d+\])?:`)
)

// relativeProblem says whether p is no clean relative path below the stack folder.
func relativeProblem(p string) bool {
	switch {
	case p == "", strings.HasPrefix(p, "/"), strings.HasPrefix(p, `\`), strings.ContainsAny(p, "\x00\\"):
		return true
	}
	if slices.Contains(strings.Split(p, "/"), "..") {
		return true
	}
	return path.Clean(p) != p || p == "."
}

func checkBackupPath(m *Model, e listEntry) []kittui.FormProblem {
	if relativeProblem(e.key) {
		return []kittui.FormProblem{m.bad("key", textProblemPath)}
	}
	return nil
}

func checkComposeFile(m *Model, e listEntry) []kittui.FormProblem {
	if relativeProblem(e.key) {
		return []kittui.FormProblem{m.bad("key", textProblemPath)}
	}
	if strings.HasPrefix(e.key, "-") {
		return []kittui.FormProblem{m.bad("key", textProblemComposeDash)}
	}
	return nil
}

func plainEntries(values []string) []listEntry {
	out := make([]listEntry, len(values))
	for i, v := range values {
		out[i] = listEntry{key: v}
	}
	return out
}

func plainValues(entries []listEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.key
	}
	return out
}

func (m *Model) stackVolumes(stack string) []string {
	info, _ := m.stackInfo(stack)
	return info.Volumes
}

// volumeChoices are the saved volumes, in their order, then the others of the stack. A saved volume the
// stack no longer has stays in and is marked.
func volumeChoices(saved, known []string) []kittui.Choice {
	var out []kittui.Choice
	for _, v := range saved {
		label := kittui.TextID(v)
		if !slices.Contains(known, v) {
			label += " (!)"
		}
		out = append(out, kittui.Choice{ID: kittui.ChoiceID(v), Label: label})
	}
	for _, v := range known {
		if !slices.Contains(saved, v) {
			out = append(out, kittui.Choice{ID: kittui.ChoiceID(v), Label: kittui.TextID(v)})
		}
	}
	return out
}

// openBackupForm reads the settings of the detail's stack; the form opens when they arrive.
func (m *Model) openBackupForm() tea.Cmd { return m.openSettingsRead(opStackSettingsBackup) }

// openComposeForm is openBackupForm for the compose files.
func (m *Model) openComposeForm() tea.Cmd { return m.openSettingsRead(opStackSettingsCompose) }

func (m *Model) openSettingsRead(op settingOp) tea.Cmd {
	stack, ok := m.detailStack()
	if !ok || m.detailBusy() {
		return nil
	}
	return m.writeCall(op, stack, func(b Backend) (any, error) { return b.StackSettings(m.ctx, stack) })
}

// listField is the field of a list editor.
func listField(ed *listEditor, label, hint kittui.TextID) kittui.FormField {
	return kittui.FormField{ID: ed.spec.field, Kind: kittui.FieldList, Label: label, Hint: hint, Value: kittui.FormValue{Text: ed.summary()}}
}

// backupFormFor builds the form from the stored backup section, or an empty one for a stack without it.
func (m *Model) backupFormFor(res control.StackSettingsResult) tea.Cmd {
	stack, ok := m.detailStack()
	if !ok || stack != res.Stack || m.set.form != nil || m.modal != nil {
		return nil
	}
	var cur control.StackBackupSettings
	if res.Settings != nil && res.Settings.Backup != nil {
		cur = *res.Settings.Backup
	}
	pathEd := m.newListEditor(listEditorSpec{field: "paths", title: textTitleBackupPaths, valueLabel: textColPath, valueHint: textHintPath,
		intro: textPathsIntro, empty: textPathsEmpty, check: checkBackupPath}, plainEntries(cur.Paths))
	cmdEd := m.newListEditor(listEditorSpec{field: "command", title: textTitleBackupCommand, valueLabel: textColArgument, valueHint: textHintArgument,
		intro: textCommandIntro, empty: textCommandEmpty, ordered: true, repeat: true}, plainEntries(cur.Command))
	if pathEd == nil || cmdEd == nil {
		return nil
	}
	keep, maxAge := "", ""
	if rt := cur.Retention; rt != nil {
		if rt.Keep > 0 {
			keep = strconv.Itoa(rt.Keep)
		}
		if rt.MaxAgeDays > 0 {
			maxAge = strconv.Itoa(rt.MaxAgeDays)
		}
	}
	if keep == "" && maxAge == "" {
		keep = strconv.Itoa(defaultKeep)
	}
	known := m.stackVolumes(stack)
	volumes := kittui.FormField{ID: "volumes", Label: textFieldBackupVolumes, Hint: textHintBackupVolumes}
	if cs := volumeChoices(cur.Volumes, known); len(cs) > 0 {
		volumes.Kind, volumes.Choices = kittui.FieldMultiple, cs
		for _, v := range cur.Volumes {
			volumes.Value.Selected = append(volumes.Value.Selected, kittui.ChoiceID(v))
		}
		if len(cur.Volumes) > 0 && slices.ContainsFunc(cur.Volumes, func(v string) bool { return !slices.Contains(known, v) }) {
			volumes.Hint = textHintBackupVolumesMissing
		}
	} else {
		volumes.Kind, volumes.Value.Text = kittui.FieldReadOnly, m.text.Lookup(textVolumesNone)
	}
	f := m.newSettingsForm(formBackup, textTitleBackup, []kittui.FormField{
		listField(pathEd, textFieldBackupPaths, textHintBackupPaths),
		volumes,
		listField(cmdEd, textFieldBackupCommand, textHintBackupCommand),
		textField("keep", textFieldKeep, textHintKeep, keep),
		textField("maxage", textFieldMaxAge, textHintMaxAge, maxAge),
	})
	if f == nil {
		return nil
	}
	f.stack = stack
	f.lists = map[kittui.FieldID]*listEditor{"paths": pathEd, "command": cmdEd}
	return m.openForm(f)
}

// composeFormFor builds the form from the stored compose files.
func (m *Model) composeFormFor(res control.StackSettingsResult) tea.Cmd {
	stack, ok := m.detailStack()
	if !ok || stack != res.Stack || m.set.form != nil || m.modal != nil {
		return nil
	}
	var files []string
	if res.Settings != nil && res.Settings.Compose != nil {
		files = res.Settings.Compose.Files
	}
	ed := m.newListEditor(listEditorSpec{field: "files", title: textTitleComposeFiles, valueLabel: textColFile, valueHint: textHintFile,
		intro: textComposeFilesIntro, empty: textComposeFilesEmpty, ordered: true, check: checkComposeFile}, plainEntries(files))
	if ed == nil {
		return nil
	}
	f := m.newSettingsForm(formCompose, textTitleCompose, []kittui.FormField{listField(ed, textFieldComposeFiles, textHintComposeFiles)})
	if f == nil {
		return nil
	}
	f.stack = stack
	f.lists = map[kittui.FieldID]*listEditor{"files": ed}
	return m.openForm(f)
}

// backupValues are what a saved backup form came to.
type backupValues struct {
	paths, volumes, command []string
	keep, maxAge            int
}

// positive reads an optional whole number of at least 1; 0 stands for empty.
func positive(s string) (n int, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 1
}

// submitBackup checks the form like the service checks the section and, when it holds up, asks for the
// confirmation. Nothing is sent before its yes.
func (m *Model) submitBackup(f *settingsForm, v map[kittui.FieldID]kittui.FormValue) tea.Cmd {
	r := backupValues{paths: plainValues(f.lists["paths"].entries), command: plainValues(f.lists["command"].entries)}
	for _, id := range v["volumes"].Selected {
		r.volumes = append(r.volumes, string(id))
	}
	var ps []kittui.FormProblem
	if len(r.paths) == 0 && len(r.volumes) == 0 && len(r.command) == 0 {
		ps = append(ps, m.bad("paths", textProblemBackupNeeded))
	}
	for _, p := range r.paths {
		if relativeProblem(p) {
			ps = append(ps, kittui.FormProblem{Field: "paths", Cause: p + ": " + m.text.Lookup(textProblemPath), NextStep: m.text.Lookup(textFormFix)})
		}
	}
	for _, name := range r.volumes {
		if !volumeNameRe.MatchString(name) {
			ps = append(ps, kittui.FormProblem{Field: "volumes", Cause: name + ": " + m.text.Lookup(textProblemVolume), NextStep: m.text.Lookup(textFormFix)})
		}
	}
	if len(r.command) > 0 && r.command[0] == "" {
		ps = append(ps, m.bad("command", textProblemCommand))
	}
	var ok bool
	if r.keep, ok = positive(v["keep"].Text); !ok {
		ps = append(ps, m.bad("keep", textProblemKeep))
	}
	if r.maxAge, ok = positive(v["maxage"].Text); !ok {
		ps = append(ps, m.bad("maxage", textProblemMaxAge))
	}
	if len(ps) > 0 {
		f.problems(ps...)
		return nil
	}
	t := m.text
	details := []string{
		t.Lookup(textDetailBackupPaths, listText(t, r.paths, ", ")),
		t.Lookup(textDetailBackupVolumes, listText(t, r.volumes, ", ")),
		t.Lookup(textDetailBackupCommand, listText(t, r.command, " ")),
	}
	switch {
	case r.keep == 0 && r.maxAge == 0:
		details = append(details, t.Lookup(textDetailKeepDefault, defaultKeep))
	default:
		if r.keep > 0 {
			details = append(details, t.Lookup(textDetailKeep, r.keep))
		}
		if r.maxAge > 0 {
			details = append(details, t.Lookup(textDetailMaxAge, r.maxAge))
		}
	}
	stack := f.stack
	m.ask(textTitleBackup, t.Lookup(textAskBackup, stack), m.whatSettings(stack, textWhatBackupAfter), details, false, func() tea.Cmd {
		return m.writeCall(opStackSettingsSet, stack, func(b Backend) (any, error) { return saveBackup(m, b, stack, r) })
	})
	return nil
}

func listText(t kittui.Translator, values []string, sep string) string {
	if len(values) == 0 {
		return t.Lookup(textModeNone)
	}
	return strings.Join(values, sep)
}

// whatSettings says where a commit of the stack's settings goes, by the kind of its stack folder, and what
// the change acts on.
func (m *Model) whatSettings(stack string, after kittui.TextID) string {
	what := textWhatSettingsUnknown
	switch m.sourceKind(stack) {
	case control.SourceKindRemote:
		what = textWhatSettingsRemote
	case control.SourceKindLocal:
		what = textWhatSettingsLocal
	}
	return m.text.Lookup(what) + " " + m.text.Lookup(after)
}

// submitCompose checks the file list and, when it holds up, asks for the confirmation. An empty list takes the
// section out, which leaves the files to the stack's own detection.
func (m *Model) submitCompose(f *settingsForm) tea.Cmd {
	files := plainValues(f.lists["files"].entries)
	var ps []kittui.FormProblem
	if len(files) > maxComposeFiles {
		ps = append(ps, m.bad("files", textProblemComposeMax, maxComposeFiles))
	}
	seen := map[string]bool{}
	for _, p := range files {
		cause := ""
		switch {
		case relativeProblem(p):
			cause = m.text.Lookup(textProblemPath)
		case strings.HasPrefix(p, "-"):
			cause = m.text.Lookup(textProblemComposeDash)
		case seen[p]:
			cause = m.text.Lookup(textProblemListTwice, p)
		}
		seen[p] = true
		if cause != "" {
			ps = append(ps, kittui.FormProblem{Field: "files", Cause: p + ": " + cause, NextStep: m.text.Lookup(textFormFix)})
		}
	}
	if len(ps) > 0 {
		f.problems(ps...)
		return nil
	}
	t := m.text
	var details []string
	for i, p := range files {
		details = append(details, t.Lookup(textDetailComposeFile, i+1, p))
	}
	if len(files) == 0 {
		details = append(details, t.Lookup(textDetailComposeDefault))
	}
	stack := f.stack
	m.ask(textTitleCompose, t.Lookup(textAskCompose, stack), m.whatSettings(stack, textWhatComposeAfter), details, false, func() tea.Cmd {
		return m.writeCall(opStackSettingsSet, stack, func(b Backend) (any, error) { return saveCompose(m, b, stack, files) })
	})
	return nil
}

// currentSettings are the settings the service holds now.
func currentSettings(m *Model, b Backend, stack string) (control.StackSettings, error) {
	cur, err := b.StackSettings(m.ctx, stack)
	if err != nil {
		return control.StackSettings{}, err
	}
	if cur.Settings == nil {
		return control.StackSettings{Version: 1}, nil
	}
	return *cur.Settings, nil
}

// saveBackup sets the backup section on the settings the service holds now, so that what the form does not
// show stays as it is.
func saveBackup(m *Model, b Backend, stack string, r backupValues) (control.StackSettingsSetResult, error) {
	s, err := currentSettings(m, b, stack)
	if err != nil {
		return control.StackSettingsSetResult{}, err
	}
	bk := &control.StackBackupSettings{Paths: r.paths, Volumes: r.volumes, Command: r.command}
	if r.keep > 0 || r.maxAge > 0 {
		bk.Retention = &control.StackRetentionSettings{Keep: r.keep, MaxAgeDays: r.maxAge}
	}
	s.Backup = bk
	return b.StackSettingsSet(m.ctx, stack, s)
}

// saveCompose sets the compose files on the settings the service holds now.
func saveCompose(m *Model, b Backend, stack string, files []string) (control.StackSettingsSetResult, error) {
	s, err := currentSettings(m, b, stack)
	if err != nil {
		return control.StackSettingsSetResult{}, err
	}
	s.Compose = nil
	if len(files) > 0 {
		s.Compose = &control.StackComposeSettings{Files: files}
	}
	return b.StackSettingsSet(m.ctx, stack, s)
}

// settingsProblems places a refusal of the service at the field its path names; ok is false for any other
// refusal, which stands above the form.
func (m *Model) settingsProblems(kind formKind, err error) (kittui.FormProblem, bool) {
	var remote *control.Error
	if !errors.As(err, &remote) || remote.Code != control.CodeInvalid {
		return kittui.FormProblem{}, false
	}
	var field kittui.FieldID
	switch kind {
	case formUpdate:
		return m.updateProblems(err)
	case formBackup:
		sub := backupPathRe.FindStringSubmatch(remote.Message)
		if sub == nil {
			return kittui.FormProblem{}, false
		}
		field = map[string]kittui.FieldID{"": "paths", "paths": "paths", "volumes": "volumes", "command": "command",
			"retention.keep": "keep", "retention.max_age_days": "maxage"}[sub[1]]
	case formCompose:
		if !composePathRe.MatchString(remote.Message) {
			return kittui.FormProblem{}, false
		}
		field = "files"
	default:
		return kittui.FormProblem{}, false
	}
	return kittui.FormProblem{Field: field, Cause: m.serviceText(err), NextStep: m.text.Lookup(textFormFix)}, true
}

// settingsSaved says what a confirmed save came to, by the form that asked.
func (m *Model) settingsSaved(res control.StackSettingsSetResult, done func(text string, details ...string)) tea.Cmd {
	same, saved := textDoneUpdateSame, textDoneUpdate
	if f := m.set.form; f != nil {
		switch f.kind {
		case formBackup:
			same, saved = textDoneBackupSame, textDoneBackup
		case formCompose:
			same, saved = textDoneComposeSame, textDoneCompose
		}
	}
	t := m.text
	if !res.Changed {
		done(t.Lookup(same, res.Stack))
		return nil
	}
	done(t.Lookup(saved, res.Stack), t.Lookup(textDetailHead, short(res.Commit)))
	return m.reloadNow()
}

// formSettingsLines is the line above the backup and the compose files form.
func (m *Model) formSettingsLines(f *settingsForm, width int) []string {
	intro := textBackupIntro
	if f.kind == formCompose {
		intro = textComposeIntro
	}
	lines := m.wrapped(detailMargin, kittui.StyleHint, m.text.Lookup(intro, f.stack), width)
	return append(lines, "")
}
