package tui

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The check of a stack: a form in the detail with the container check and the HTTP probe. It sets the check
// section only; the rest of the settings stays as the service holds it.

const formCheck formKind = formCompose + 1

// The limits of the probe and its defaults, as the service enforces and applies them; the form checks them
// early so a refusal comes at the field. The interface cannot import the rules, so they are repeated here.
const (
	minStatus      = 100
	maxStatus      = 599
	defaultStatus  = 200
	minTimeout     = time.Second
	maxTimeout     = 10 * time.Minute
	defaultTimeout = "10s"
	choiceOn       = "on"
	choiceOff      = "off"
)

// checkPathRe finds the field path the service names in a refusal, such as "check.http.url:".
var checkPathRe = regexp.MustCompile(`check(?:\.(containers|http(?:\.(?:url|status|timeout))?))?:`)

// validProbeURL reports whether s is an absolute http or https URL.
func validProbeURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// parseTimeout reads a Go duration between the limits.
func parseTimeout(s string) (time.Duration, bool) {
	d, err := time.ParseDuration(s)
	return d, err == nil && d >= minTimeout && d <= maxTimeout
}

func (m *Model) openCheckForm() tea.Cmd { return m.openSettingsRead(opStackSettingsCheck) }

func onOff(on bool) string {
	if on {
		return choiceOn
	}
	return choiceOff
}

// checkFormFor builds the form from the stored check, or from the defaults for a stack without one.
func (m *Model) checkFormFor(res control.StackSettingsResult) tea.Cmd {
	stack, ok := m.detailStack()
	if !ok || stack != res.Stack || m.set.form != nil || m.modal != nil {
		return nil
	}
	containers, http := true, false
	probeURL, status, timeout := "", strconv.Itoa(defaultStatus), defaultTimeout
	if res.Settings != nil && res.Settings.Check != nil {
		c := res.Settings.Check
		if c.Containers != nil {
			containers = *c.Containers
		}
		if h := c.HTTP; h != nil {
			http, probeURL = true, h.URL
			if h.Status != 0 {
				status = strconv.Itoa(h.Status)
			}
			if h.Timeout != "" {
				timeout = h.Timeout
			}
		}
	}
	pick := func(id string, label, hint kittui.TextID, cur bool) kittui.FormField {
		return kittui.FormField{ID: kittui.FieldID(id), Kind: kittui.FieldChoice, Label: label, Hint: hint,
			Choices: []kittui.Choice{choice(choiceOn, textCheckOn), choice(choiceOff, textCheckOff)},
			Value:   kittui.FormValue{Choice: kittui.ChoiceID(onOff(cur))}}
	}
	f := m.newSettingsForm(formCheck, textTitleCheckForm, []kittui.FormField{
		pick("containers", textFieldCheckContainers, textHintCheckContainers, containers),
		pick("http", textFieldCheckHTTP, textHintCheckHTTP, http),
		textField("url", textFieldCheckURL, textHintCheckURL, probeURL),
		textField("status", textFieldCheckStatus, textHintCheckStatus, status),
		textField("timeout", textFieldCheckTimeout, textHintCheckTimeout, timeout),
	})
	if f == nil {
		return nil
	}
	f.stack = stack
	return m.openForm(f)
}

// checkValues are what a saved check form came to. With http false the probe fields are not used.
type checkValues struct {
	containers, http bool
	url              string
	status           int
	timeout          string
}

// submitCheck checks the form like the service checks the section and, when it holds up, asks for the
// confirmation. Nothing is sent before its yes. The probe fields count only while the probe is on.
func (m *Model) submitCheck(f *settingsForm, v map[kittui.FieldID]kittui.FormValue) tea.Cmd {
	r := checkValues{containers: v["containers"].Choice == choiceOn, http: v["http"].Choice == choiceOn,
		url: strings.TrimSpace(v["url"].Text), timeout: strings.TrimSpace(v["timeout"].Text)}
	var ps []kittui.FormProblem
	if r.http {
		if r.url == "" {
			ps = append(ps, m.bad("url", textProblemCheckURLEmpty))
		} else if !validProbeURL(r.url) {
			ps = append(ps, m.bad("url", textProblemCheckURL))
		}
		n, err := strconv.Atoi(strings.TrimSpace(v["status"].Text))
		if err != nil || n < minStatus || n > maxStatus {
			ps = append(ps, m.bad("status", textProblemCheckStatus, minStatus, maxStatus))
		}
		r.status = n
		if _, ok := parseTimeout(r.timeout); !ok {
			ps = append(ps, m.bad("timeout", textProblemCheckTimeout))
		}
	}
	if len(ps) > 0 {
		f.problems(ps...)
		return nil
	}
	t := m.text
	details := []string{t.Lookup(textDetailCheckContainers, t.Lookup(onOffText(r.containers)))}
	if r.http {
		details = append(details, t.Lookup(textDetailCheckHTTP, r.url, r.status, r.timeout))
	} else {
		details = append(details, t.Lookup(textDetailCheckNoHTTP))
	}
	stack := f.stack
	m.ask(textTitleCheckForm, t.Lookup(textAskCheckForm, stack), m.whatSettings(stack, textWhatCheckAfter), details, false, func() tea.Cmd {
		return m.writeCall(opStackSettingsSet, stack, func(b Backend) (any, error) { return saveCheck(m, b, stack, r) })
	})
	return nil
}

func onOffText(on bool) kittui.TextID {
	if on {
		return textCheckOn
	}
	return textCheckOff
}

// saveCheck sets the check on the settings the service holds now, so that what the form does not show stays
// as it is. A value the form shows as its default stays out of the file unless the file already had it, so
// saving what was shown commits nothing.
func saveCheck(m *Model, b Backend, stack string, r checkValues) (control.StackSettingsSetResult, error) {
	s, err := currentSettings(m, b, stack)
	if err != nil {
		return control.StackSettingsSetResult{}, err
	}
	old := control.StackCheckSettings{}
	if s.Check != nil {
		old = *s.Check
	}
	c := &control.StackCheckSettings{}
	if !r.containers || old.Containers != nil {
		c.Containers = &r.containers
	}
	if r.http {
		h := &control.StackHTTPSettings{URL: r.url}
		oh := control.StackHTTPSettings{}
		if old.HTTP != nil {
			oh = *old.HTTP
		}
		if r.status != defaultStatus || oh.Status != 0 {
			h.Status = r.status
		}
		if r.timeout != defaultTimeout || oh.Timeout != "" {
			h.Timeout = r.timeout
			if oh.Timeout != "" && mustDuration(oh.Timeout) == mustDuration(r.timeout) {
				h.Timeout = oh.Timeout
			}
		}
		c.HTTP = h
	}
	s.Check = c
	if c.Containers == nil && c.HTTP == nil {
		s.Check = nil
	}
	return b.StackSettingsSet(m.ctx, stack, s)
}

func mustDuration(s string) time.Duration {
	d, _ := time.ParseDuration(s)
	return d
}

// checkProblems places a refusal of the service at the field its path names; ok is false for any other
// refusal, which stands above the form.
func (m *Model) checkProblems(msg string, err error) (kittui.FormProblem, bool) {
	sub := checkPathRe.FindStringSubmatch(msg)
	if sub == nil {
		return kittui.FormProblem{}, false
	}
	field := map[string]kittui.FieldID{"": "containers", "containers": "containers", "http": "url", "http.url": "url",
		"http.status": "status", "http.timeout": "timeout"}[sub[1]]
	return kittui.FormProblem{Field: field, Cause: m.serviceText(err), NextStep: m.text.Lookup(textFormFix)}, true
}

// formCheckLines is the line above the form.
func (m *Model) formCheckLines(f *settingsForm, width int) []string {
	lines := m.wrapped(detailMargin, kittui.StyleHint, m.text.Lookup(textCheckIntro, f.stack), width)
	return append(lines, "")
}

// openRemoveSettings asks for the removal of all settings of the detail's stack. The service refuses a stack
// without settings, and that refusal is told plainly.
func (m *Model) openRemoveSettings() tea.Cmd {
	stack, ok := m.detailStack()
	if !ok || m.detailBusy() {
		return nil
	}
	what := textWhatRemoveUnknown
	switch m.sourceKind(stack) {
	case control.SourceKindRemote:
		what = textWhatRemoveRemote
	case control.SourceKindLocal:
		what = textWhatRemoveLocal
	}
	m.ask(textTitleSettingsRemove, m.text.Lookup(textAskSettingsRemove, stack), m.text.Lookup(what), nil, true, func() tea.Cmd {
		return m.writeCall(opStackSettingsRemove, stack, func(b Backend) (any, error) { return b.StackSettingsRemove(m.ctx, stack) })
	})
	return nil
}
