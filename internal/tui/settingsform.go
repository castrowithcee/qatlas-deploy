package tui

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

type formKind int

const (
	formRepo formKind = iota
	formCred
	formWindow
	formNotifyBasics
	formNotifyDetail
)

// settingsForm is an open form of the Settings area. Its masked fields are read once, when the person saves;
// nothing of them is kept in the model.
type settingsForm struct {
	kind  formKind
	title kittui.TextID
	form  *kittui.Form
	// notify is what the first step of a channel collected.
	notify control.NotifyAddArgs
	// stack is the stack a secret form belongs to.
	stack string
	// dirs and stackDir are the missing directories and the stack folder a data directory form is about.
	dirs     []control.DirInfo
	stackDir string
}

// problem shows a problem without a field above the form.
func (f *settingsForm) problem(cause, next string) {
	f.form.SetProblems([]kittui.FormProblem{{Cause: cause, NextStep: next}})
}

func (f *settingsForm) problems(ps ...kittui.FormProblem) { f.form.SetProblems(ps) }

// openForm shows a form; nil, from a form the kit refused, shows nothing.
func (m *Model) openForm(f *settingsForm) tea.Cmd {
	if f != nil {
		m.set.form = f
	}
	return nil
}

func (m *Model) newSettingsForm(kind formKind, title kittui.TextID, fields []kittui.FormField) *settingsForm {
	form, err := kittui.NewForm(kittui.FormSpec{Fields: fields, Keys: formKeys()}, m.keys, m.text)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return nil
	}
	return &settingsForm{kind: kind, title: title, form: form}
}

func choice(id string, label kittui.TextID) kittui.Choice {
	return kittui.Choice{ID: kittui.ChoiceID(id), Label: label}
}

func textField(id string, label, hint kittui.TextID, value string) kittui.FormField {
	return kittui.FormField{ID: kittui.FieldID(id), Kind: kittui.FieldText, Label: label, Hint: hint, Value: kittui.FormValue{Text: value}}
}

func maskedField(id string, label, hint kittui.TextID) kittui.FormField {
	return kittui.FormField{ID: kittui.FieldID(id), Kind: kittui.FieldMasked, Label: label, Hint: hint}
}

func (m *Model) repoForm() *settingsForm {
	return m.newSettingsForm(formRepo, textTitleBind, []kittui.FormField{
		textField("url", textFieldURL, textHintURL, ""),
		textField("dir", textFieldDir, textHintDir, ""),
	})
}

func (m *Model) credentialForm() *settingsForm {
	return m.newSettingsForm(formCred, textTitleCred, []kittui.FormField{
		{ID: "kind", Kind: kittui.FieldChoice, Label: textFieldCredKind, Hint: textHintCredKind,
			Choices: []kittui.Choice{choice(credRegistry, textKindRegistry), choice(credGitHub, textKindGitHub)}},
		textField("host", textFieldRegistryHost, textHintRegistryHost, ""),
		textField("user", textFieldRegistryUser, textHintRegistryUser, ""),
		maskedField("token", textFieldToken, textHintToken),
	})
}

func (m *Model) windowForm() *settingsForm {
	w := m.set.windowRes
	return m.newSettingsForm(formWindow, textTitleWindow, []kittui.FormField{
		textField("days", textFieldDays, textHintDays, strings.Join(w.Days, ",")),
		textField("start", textFieldStart, textHintStart, w.Start),
		textField("duration", textFieldDuration, textHintDuration, w.Duration),
		textField("zone", textFieldZone, textHintZone, w.Zone),
	})
}

var notifyEvents = []struct {
	id   string
	text kittui.TextID
}{
	{control.NotifyEventSucceeded, textEventSucceeded}, {control.NotifyEventFailed, textEventFailed},
	{control.NotifyEventRolledBack, textEventRolledBack}, {control.NotifyEventRollbackFailed, textEventRollbackFailed},
	{control.NotifyEventUpdateAvailable, textEventUpdateAvailable},
}

func (m *Model) notifyBasicsForm() *settingsForm {
	var events []kittui.Choice
	for _, e := range notifyEvents {
		events = append(events, choice(e.id, e.text))
	}
	return m.newSettingsForm(formNotifyBasics, textTitleNotify, []kittui.FormField{
		textField("name", textFieldName, textHintName, ""),
		{ID: "type", Kind: kittui.FieldChoice, Label: textFieldType, Hint: textHintType, Choices: []kittui.Choice{
			choice(control.NotifyWebhook, textTypeWebhook), choice(control.NotifySlack, textTypeSlack),
			choice(control.NotifyTelegram, textTypeTelegram), choice(control.NotifySMTP, textTypeSMTP)}},
		{ID: "events", Kind: kittui.FieldMultiple, Label: textFieldEvents, Hint: textHintEvents, Choices: events},
	})
}

// notifyDetailForm asks for the settings and secret parts of the type the first step chose.
func (m *Model) notifyDetailForm(basics control.NotifyAddArgs) *settingsForm {
	var fields []kittui.FormField
	switch basics.Type {
	case control.NotifyWebhook:
		fields = []kittui.FormField{maskedField("secret", textFieldWebhookURL, textHintWebhookURL),
			maskedField("key", textFieldSigningKey, textHintSigningKey)}
	case control.NotifySlack:
		fields = []kittui.FormField{maskedField("secret", textFieldSlackURL, textHintSlackURL)}
	case control.NotifyTelegram:
		fields = []kittui.FormField{maskedField("secret", textFieldBotToken, textHintBotToken),
			textField("chat_id", textFieldChatID, textHintChatID, ""), textField("base_url", textFieldBaseURL, textHintBaseURL, "")}
	case control.NotifySMTP:
		fields = []kittui.FormField{textField("host", textFieldHost, textHintHost, ""), textField("port", textFieldPort, textHintPort, ""),
			{ID: "security", Kind: kittui.FieldChoice, Label: textFieldSecurity, Hint: textHintSecurity,
				Choices: []kittui.Choice{choice("starttls", textSecStartTLS), choice("tls", textSecTLS)}},
			textField("user", textFieldUser, textHintUser, ""), maskedField("secret", textFieldPassword, textHintPassword),
			textField("from", textFieldFrom, textHintFrom, ""), textField("to", textFieldTo, textHintTo, "")}
	}
	f := m.newSettingsForm(formNotifyDetail, textTitleNotifyDetail, fields)
	if f != nil {
		f.notify = basics
	}
	return f
}

// submitForm checks a saved form and, when it holds up, opens the confirmation of what it would change. A form
// that does not hold up stays open with its problems under the fields.
func (m *Model) submitForm() tea.Cmd {
	f := m.set.form
	if f == nil || m.settingsBusy() {
		return nil
	}
	v := f.form.Values()
	text := func(id string) string { return strings.TrimSpace(v[kittui.FieldID(id)].Text) }
	switch f.kind {
	case formRepo:
		return m.submitRepo(f, text("url"), text("dir"))
	case formCred:
		return m.submitCredential(f, string(v["kind"].Choice), text("host"), text("user"), v["token"].Text)
	case formWindow:
		return m.submitWindow(f, text("days"), text("start"), text("duration"), text("zone"))
	case formNotifyBasics:
		return m.submitNotifyBasics(f, text("name"), string(v["type"].Choice), v["events"].Selected)
	case formNotifyDetail:
		return m.submitNotifyDetail(f, v)
	case formSecret:
		return m.submitSecret(f, text("key"), v["value"].Text)
	case formDataDirs:
		return m.submitDataDirs(f, text("owner"), text("mode"))
	}
	return nil
}

func (m *Model) bad(field string, cause kittui.TextID, args ...any) kittui.FormProblem {
	return kittui.FormProblem{Field: kittui.FieldID(field), Cause: m.text.Lookup(cause, args...), NextStep: m.text.Lookup(textFormFix)}
}

func (m *Model) submitRepo(f *settingsForm, url, dir string) tea.Cmd {
	var ps []kittui.FormProblem
	// Only the service knows which addresses it accepts; it answers the probe with the reason.
	if url == "" || strings.ContainsFunc(url, unicode.IsSpace) {
		ps = append(ps, m.bad("url", textProblemURL))
	}
	switch {
	case dir == "":
		ps = append(ps, m.bad("dir", textProblemDirRequired))
	case !filepath.IsAbs(dir) || filepath.Clean(dir) != dir:
		ps = append(ps, m.bad("dir", textProblemDir))
	}
	if len(ps) > 0 {
		f.problems(ps...)
		return nil
	}
	m.set.pending = bind{url: url, dir: dir}
	if m.folders == nil {
		return m.probeRepo()
	}
	absent, err := m.folders.Missing(dir)
	if err != nil {
		f.problem(m.serviceText(err), m.text.Lookup(textFormFix))
		return nil
	}
	if absent {
		// Nothing is created before the person has seen the exact path and said yes.
		m.ask(textPageRepo, m.text.Lookup(textAskFolderCreate, dir), m.text.Lookup(textWhatFolderCreate), nil, false,
			func() tea.Cmd { return m.grantFolder(true) })
		return nil
	}
	return m.grantFolder(false)
}

// grantFolder prepares the stack folder; the probe of the repository follows when it is done.
func (m *Model) grantFolder(create bool) tea.Cmd {
	dir := m.set.pending.dir
	return m.writeCall(opFolder, dir, func(Backend) (any, error) { return folderReady{}, m.folders.Grant(m.ctx, dir, create) })
}

// folderReady is the answer of a prepared stack folder.
type folderReady struct{}

func (m *Model) probeRepo() tea.Cmd {
	url := m.set.pending.url
	return m.writeCall(opProbe, "", func(b Backend) (any, error) { return b.RepoProbe(m.ctx, url) })
}

// The kinds of credential the form offers: the two names qdeploy reads.
const (
	credRegistry = "registry"
	credGitHub   = "github"
	// githubCredential is the name of the token for the changelogs.
	githubCredential = "github.com"
)

// dockerHubHosts are the names under which Docker Hub is entered; its credential is registry.docker.io.
var dockerHubHosts = map[string]bool{"docker.io": true, "index.docker.io": true, "registry-1.docker.io": true}

var registryHostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?$`)

// registryCredentialName is the name of the credential that holds the login for a registry host: "registry."
// and the host in lower case, a port colon written as '-'; Docker Hub is registry.docker.io. ok is false for
// text that is no host.
func registryCredentialName(host string) (name string, ok bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if !registryHostRe.MatchString(host) {
		return "", false
	}
	if dockerHubHosts[host] {
		host = "docker.io"
	}
	name = "registry." + strings.ReplaceAll(host, ":", "-")
	return name, validCredentialName(name)
}

// credentialValue is the name and value to store for the form. The registry login is "<user>:<token>", which
// is split at the first colon, so a user name holds none.
func credentialFor(kind, host, user, token string) (name, value string, ok bool) {
	switch kind {
	case credGitHub:
		return githubCredential, token, true
	default:
		name, ok = registryCredentialName(host)
		return name, user + ":" + token, ok
	}
}

func (m *Model) submitCredential(f *settingsForm, kind, host, user, token string) tea.Cmd {
	var ps []kittui.FormProblem
	name, value, ok := credentialFor(kind, host, user, token)
	if kind == credRegistry {
		if !ok {
			ps = append(ps, m.bad("host", textProblemHost))
		}
		if user == "" || strings.Contains(user, ":") {
			ps = append(ps, m.bad("user", textProblemUser))
		}
	}
	if token == "" || !validCredentialValue(value) {
		ps = append(ps, m.bad("token", textProblemSecret))
	}
	if len(ps) > 0 {
		f.problems(ps...)
		return nil
	}
	m.ask(textTitleCred, m.text.Lookup(textAskCredSet, name), m.text.Lookup(textWhatCredSet), nil, false, func() tea.Cmd {
		return m.writeCall(opCredSet, name, func(b Backend) (any, error) { return nil, b.CredentialSet(m.ctx, name, value) })
	})
	return nil
}

func (m *Model) submitWindow(f *settingsForm, days, start, duration, zone string) tea.Cmd {
	cur := m.set.windowRes
	var args control.WindowSetArgs
	var details []string
	if want := splitList(days); len(want) > 0 && strings.Join(want, ",") != strings.Join(cur.Days, ",") {
		args.Days = want
		details = append(details, m.text.Lookup(textDetailDays, m.dayList(want)))
	}
	for _, part := range []struct {
		value, current string
		set            *string
		detail         kittui.TextID
	}{{start, cur.Start, &args.Start, textDetailStart}, {duration, cur.Duration, &args.Duration, textDetailDuration},
		{zone, cur.Zone, &args.Zone, textDetailZone}} {
		if part.value != "" && part.value != part.current {
			*part.set = part.value
			details = append(details, m.text.Lookup(part.detail, part.value))
		}
	}
	if len(details) == 0 {
		f.problem(m.text.Lookup(textProblemNoChange), m.text.Lookup(textFormFix))
		return nil
	}
	m.ask(textTitleWindow, m.text.Lookup(textAskWindow), m.text.Lookup(textWhatWindow), details, false, func() tea.Cmd {
		return m.writeCall(opWindowSet, "", func(b Backend) (any, error) { return b.WindowSet(m.ctx, args) })
	})
	return nil
}

// splitList splits a comma list and drops blanks.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var channelNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func (m *Model) submitNotifyBasics(f *settingsForm, name, typ string, events []kittui.ChoiceID) tea.Cmd {
	if !channelNameRe.MatchString(name) {
		f.problems(m.bad("name", textProblemChannelName))
		return nil
	}
	args := control.NotifyAddArgs{Name: name, Type: typ}
	for _, e := range events {
		args.Events = append(args.Events, string(e))
	}
	return m.openForm(m.notifyDetailForm(args))
}

func (m *Model) submitNotifyDetail(f *settingsForm, v map[kittui.FieldID]kittui.FormValue) tea.Cmd {
	text := func(id string) string { return strings.TrimSpace(v[kittui.FieldID(id)].Text) }
	secret := func(id string) string { return v[kittui.FieldID(id)].Text }
	args := f.notify
	var ps []kittui.FormProblem
	needSecret := func(id string) string {
		s := secret(id)
		if !validCredentialValue(s) {
			ps = append(ps, m.bad(id, textProblemSecret))
		}
		return s
	}
	require := func(id string) string {
		s := text(id)
		if s == "" {
			ps = append(ps, m.bad(id, textProblemRequired))
		}
		return s
	}
	switch args.Type {
	case control.NotifyWebhook:
		args.Secret, args.Key = needSecret("secret"), needSecret("key")
	case control.NotifySlack:
		args.Secret = needSecret("secret")
	case control.NotifyTelegram:
		args.Secret, args.ChatID, args.BaseURL = needSecret("secret"), require("chat_id"), text("base_url")
	case control.NotifySMTP:
		args.Host, args.From, args.User = require("host"), require("from"), text("user")
		args.Security, args.To = string(v["security"].Choice), splitList(text("to"))
		if len(args.To) == 0 {
			ps = append(ps, m.bad("to", textProblemRequired))
		}
		if p := text("port"); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil || n < 1 || n > 65535 {
				ps = append(ps, m.bad("port", textProblemPort))
			}
			args.Port = n
		}
		switch {
		case args.User != "":
			args.Secret = needSecret("secret")
		case secret("secret") != "":
			ps = append(ps, m.bad("secret", textProblemPasswordNoUser))
		}
	}
	if len(ps) > 0 {
		f.problems(ps...)
		return nil
	}
	m.ask(textTitleNotify, m.text.Lookup(textAskNotifyAdd, args.Name, args.Type), m.text.Lookup(textWhatNotifyAdd), nil, false, func() tea.Cmd {
		return m.writeCall(opNotifyAdd, args.Name, func(b Backend) (any, error) { return b.NotifyAdd(m.ctx, args) })
	})
	return nil
}

// formKey hands a key to the open form. Back ends the form; nothing is kept of it.
func (m *Model) formKey(msg tea.KeyPressMsg) tea.Cmd {
	f := m.set.form
	if act, handled := f.form.Update(msg); handled {
		if act == actSave {
			return m.submitForm()
		}
		return nil
	}
	if id, ok := m.keys.Match(msg, f.form.Input()); ok {
		switch id {
		case actForceQuit:
			return tea.Quit
		case actBack:
			m.set.form = nil
		}
	}
	return nil
}

// The limits of a stored credential, as the service enforces them; the form checks them early so a refusal
// never has to quote a value.
const (
	minCredentialValue = 4
	maxCredentialValue = 8 << 10
)

var credentialNameRe = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

func validCredentialName(name string) bool { return credentialNameRe.MatchString(name) }

func validCredentialValue(v string) bool {
	return len(v) >= minCredentialValue && len(v) <= maxCredentialValue && utf8.ValidString(v) &&
		strings.IndexFunc(v, unicode.IsControl) < 0
}
