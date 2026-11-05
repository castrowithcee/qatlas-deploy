package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The values the tests type into masked fields. They are invented; every flow checks that they reach the
// service and appear on no screen.
const (
	hiddenA = "hidden-AAAA-1111-example"
	hiddenB = "hidden-BBBB-2222-example"
)

const (
	repoURL     = "git@example.test:ops/stacks.git"
	stackDir    = "/srv/stacks-example"
	fingerprint = "SHA256:ExampleFingerprintForTestsOnly0000000000"
)

// settingsBackend is the demo host with its settings.
func settingsBackend() *fakeBackend {
	b := demoBackend()
	b.probe = control.RepoProbeResult{Host: "example.test", Port: "22", KeyType: "ssh-ed25519", Fingerprint: fingerprint}
	b.window = control.WindowResult{Days: []string{"sun"}, Start: "03:00", Duration: "2h", Zone: "UTC", NextOpening: at(31, 3, 0)}
	b.creds = []control.CredentialInfo{{Name: "registry.docker.io", Modified: at(30, 9, 0)}, {Name: "github.com", Modified: at(29, 9, 0)}}
	b.notify = []control.NotifyChannelInfo{
		{Name: "ops-hook", Type: control.NotifyWebhook, Events: []string{"failed", "rolled-back"}},
		{Name: "ops-mail", Type: control.NotifySMTP, Host: "mail.example.test", Port: 587, Security: "starttls",
			From: "qdeploy@example.test", To: []string{"ops@example.test"}},
	}
	return b
}

// typed is the keys of a text typed into a field.
func typed(s string) []string { return strings.Split(s, "") }

func seq(parts ...[]string) []string { return slices.Concat(parts...) }

func keys(k ...string) []string { return k }

// The ways to a page of the Settings area from the Stacks area.
var (
	toMenu   = keys("4")
	toRepo   = keys("4", "enter")
	toKey    = keys("4", "down", "enter")
	toCreds  = keys("4", "down", "down", "enter")
	toWindow = keys("4", "down", "down", "down", "enter")
	toNotify = keys("4", "down", "down", "down", "down", "enter")
)

func newSettingsModel(t *testing.T, b *fakeBackend) *Model {
	t.Helper()
	return newTestModel(t, i18n.English, b, wideWidth, 40)
}

// sendHidden sends keys and checks after each that no screen shows a hidden value.
func sendHidden(t *testing.T, m *Model, hidden []string, keys ...string) {
	t.Helper()
	for _, k := range keys {
		send(m, k)
		noLeak(t, m.render(), hidden...)
	}
}

func noLeak(t *testing.T, screen string, hidden ...string) {
	t.Helper()
	for _, h := range hidden {
		if strings.Contains(screen, h) {
			t.Fatalf("a hidden value is on the screen:\n%s", screen)
		}
	}
}

func contains(t *testing.T, m *Model, want ...string) {
	t.Helper()
	screen := m.render()
	for _, w := range want {
		if !strings.Contains(screen, w) {
			t.Fatalf("the screen lacks %q:\n%s", w, screen)
		}
	}
}

func TestSettingsAreaListsItsPages(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	send(m, toMenu...)
	contains(t, m, "Stacks repository", "Deploy key", "Credentials", "Maintenance window", "Notifications")
	send(m, "down", "down", "enter")
	contains(t, m, "registry.docker.io", "github.com")
	// Esc leaves the page, then the area.
	send(m, "esc")
	if m.area != areaSettings || m.set.page != "" {
		t.Fatalf("area %s page %q", m.area, m.set.page)
	}
	send(m, "esc")
	if m.area != areaStacks {
		t.Fatalf("area %s", m.area)
	}
}

func TestRepoBindAsksForTheFingerprintBeforeStoringIt(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toRepo...)
	send(m, "b")
	send(m, typed(repoURL)...)
	send(m, "tab")
	send(m, typed(stackDir)...)
	// Leaving the question keeps the form, and nothing is stored.
	send(m, "f2")
	contains(t, m, "host: example.test", fingerprint, "ssh-ed25519")
	if got := b.actions(); !slices.Equal(got, []string{"probe:" + repoURL}) {
		t.Fatalf("calls = %v", got)
	}
	send(m, "n")
	if m.set.form == nil || len(b.repoSet) != 0 {
		t.Fatalf("form %v, binds %v", m.set.form, b.repoSet)
	}
	send(m, "f2", "enter") // enter is the default answer, which is cancel
	if len(b.repoSet) != 0 {
		t.Fatalf("enter confirmed the host key: %v", b.repoSet)
	}
	send(m, "f2", "y")
	want := control.RepoSetArgs{URL: repoURL, Dir: stackDir, Fingerprint: fingerprint}
	if len(b.repoSet) != 1 || b.repoSet[0] != want {
		t.Fatalf("binds = %+v, want %+v", b.repoSet, want)
	}
	contains(t, m, "The stacks repository is bound")
	if m.set.form != nil {
		t.Fatal("the form stays open after the bind")
	}
}

func TestRepoBindOfAPinnedHostSaysSo(t *testing.T) {
	b := settingsBackend()
	b.probe.Pinned = true
	m := newSettingsModel(t, b)
	send(m, toRepo...)
	send(m, "b")
	send(m, typed(repoURL)...)
	send(m, "tab")
	send(m, typed(stackDir)...)
	send(m, "f2")
	contains(t, m, "already trusts exactly this host key")
}

func TestRepoBindRefusesAMissingURLAndRelativeDirectoryBeforeAskingTheServer(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toRepo...)
	send(m, "b")
	send(m, "f2")
	contains(t, m, "SSH address is required")
	if len(b.actions()) != 0 || m.modal != nil {
		t.Fatalf("calls %v, question %v", b.actions(), m.modal)
	}
	// A directory that is not absolute is refused the same way.
	send(m, "esc", "b")
	send(m, typed(repoURL)...)
	// The stack folder is required, and it must be an absolute path.
	send(m, "f2")
	contains(t, m, "stack folder is required")
	send(m, "tab")
	send(m, typed("stacks")...)
	send(m, "f2")
	contains(t, m, "absolute")
	if len(b.actions()) != 0 {
		t.Fatalf("calls %v", b.actions())
	}
}

func TestRepoBindShowsWhyTheProbeFailed(t *testing.T) {
	b := settingsBackend()
	b.errs["probe:"+repoURL] = control.NewError(control.CodeRemote, "cannot reach the server")
	m := newSettingsModel(t, b)
	send(m, toRepo...)
	send(m, "b")
	send(m, typed(repoURL)...)
	send(m, "tab")
	send(m, typed(stackDir)...)
	send(m, "f2")
	contains(t, m, "cannot reach the server")
	if m.modal != nil || len(b.repoSet) != 0 {
		t.Fatal("a failed probe went on")
	}
}

func TestRepoFetchAsksFirst(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toRepo...)
	send(m, "F", "n")
	if len(b.actions()) != 0 {
		t.Fatalf("calls %v", b.actions())
	}
	send(m, "F", "y")
	if got := b.actions(); !slices.Equal(got, []string{"fetch:"}) {
		t.Fatalf("calls = %v", got)
	}
	contains(t, m, "newer commits")
}

func TestDeployKeyIsGeneratedOnceAndShown(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toKey...)
	contains(t, m, "No deploy key yet")
	send(m, "G", "n")
	if len(b.actions()) != 0 {
		t.Fatalf("calls %v", b.actions())
	}
	send(m, "G", "y")
	if got := b.actions(); !slices.Equal(got, []string{"keygen:"}) {
		t.Fatalf("calls = %v", got)
	}
	send(m, "enter")
	contains(t, m, "ssh-ed25519 AAAAC3", "SHA256:ExampleKey")
	// An existing key is never replaced from here.
	send(m, "G")
	if m.modal != nil || len(b.actions()) != 1 {
		t.Fatalf("a second generate went on: %v", b.actions())
	}
	contains(t, m, "qdeploy key generate --replace")
}

func TestDeployKeyIsShownWhenOneExists(t *testing.T) {
	b := settingsBackend()
	b.key = &control.KeyResult{PublicKey: "ssh-ed25519 AAAAexamplekey qdeploy", Fingerprint: fingerprint}
	m := newSettingsModel(t, b)
	send(m, toKey...)
	contains(t, m, "ssh-ed25519 AAAAexamplekey qdeploy", fingerprint)
}

func TestRegistryCredentialNames(t *testing.T) {
	for host, want := range map[string]string{
		"docker.io":                  "registry.docker.io",
		"Docker.IO":                  "registry.docker.io",
		"index.docker.io":            "registry.docker.io",
		"registry-1.docker.io":       "registry.docker.io",
		"ghcr.io":                    "registry.ghcr.io",
		"Registry.Example.test:5000": "registry.registry.example.test-5000",
		" reg.example.test ":         "registry.reg.example.test",
		"":                           "",
		"two words":                  "",
		"reg.example.test/path":      "",
		"https://reg.example.test":   "",
		"reg.example.test:":          "",
		"reg.example.test:port":      "",
		"-bad.example.test":          "",
	} {
		got, ok := registryCredentialName(host)
		if got != want || ok != (want != "") {
			t.Errorf("registryCredentialName(%q) = %q, %v; want %q", host, got, ok, want)
		}
	}
}

func TestCredentialIsSetWithADerivedNameAndNeverShown(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	hidden := []string{hiddenA}
	send(m, toCreds...)
	send(m, "a")
	sendHidden(t, m, hidden, "down")
	sendHidden(t, m, hidden, typed("Registry.Example.test:5000")...)
	sendHidden(t, m, hidden, "down")
	sendHidden(t, m, hidden, typed("alice")...)
	sendHidden(t, m, hidden, "down")
	sendHidden(t, m, hidden, typed(hiddenA)...)
	contains(t, m, "************************")
	sendHidden(t, m, hidden, "f2")
	contains(t, m, "registry.registry.example.test-5000")
	if len(b.credSets) != 0 {
		t.Fatal("stored before the confirmation")
	}
	sendHidden(t, m, hidden, "y")
	if len(b.credSets) != 1 || b.credSets[0].Name != "registry.registry.example.test-5000" || b.credSets[0].Value != "alice:"+hiddenA {
		t.Fatalf("stored %+v", b.credSets)
	}
	contains(t, m, "registry.registry.example.test-5000")
}

func TestGitHubTokenCredentialTakesOnlyTheToken(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	hidden := []string{hiddenA}
	send(m, toCreds...)
	send(m, "a", "right")
	// The registry fields stay empty and are not asked for.
	sendHidden(t, m, hidden, "down", "down", "down")
	sendHidden(t, m, hidden, typed(hiddenA)...)
	sendHidden(t, m, hidden, "f2")
	contains(t, m, "github.com")
	sendHidden(t, m, hidden, "y")
	if len(b.credSets) != 1 || b.credSets[0] != (control.CredentialSetArgs{Name: "github.com", Value: hiddenA}) {
		t.Fatalf("stored %+v", b.credSets)
	}
}

func TestCredentialFormRefusesWhatCannotBeStored(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toCreds...)
	send(m, "a", "f2")
	contains(t, m, "no registry host", "user is required", "4 to 8192")
	send(m, "esc", "a", "down")
	send(m, typed("reg.example.test")...)
	send(m, "down")
	send(m, typed("al:ice")...)
	send(m, "down")
	send(m, typed(hiddenA)...)
	send(m, "f2")
	contains(t, m, "holds no colon")
	if m.modal != nil || len(b.credSets) != 0 {
		t.Fatal("a refused form went on")
	}
	noLeak(t, m.render(), hiddenA)
}

func TestCredentialIsDeletedAfterConfirmation(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toCreds...)
	send(m, "x", "n")
	if len(b.actions()) != 0 {
		t.Fatalf("calls %v", b.actions())
	}
	send(m, "x", "y")
	if got := b.actions(); !slices.Equal(got, []string{"creddel:registry.docker.io"}) {
		t.Fatalf("calls = %v", got)
	}
	send(m, "esc")
	if strings.Contains(m.render(), "registry.docker.io") {
		t.Errorf("the deleted credential is still listed:\n%s", m.render())
	}
}

func TestWindowIsChangedAfterConfirmation(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toWindow...)
	contains(t, m, "days: sun", "start: 03:00", "duration: 2h", "time zone: UTC")
	// Nothing differs: no question.
	send(m, "e", "f2")
	contains(t, m, "Nothing differs")
	if m.modal != nil {
		t.Fatal("an unchanged window was asked")
	}
	send(m, "tab", "backspace", "backspace", "backspace", "backspace", "backspace")
	send(m, typed("04:30")...)
	send(m, "f2")
	contains(t, m, "start: 04:30")
	send(m, "n")
	if len(b.windows) != 0 {
		t.Fatal("changed before the confirmation")
	}
	send(m, "f2", "y")
	if len(b.windows) != 1 || !windowArgsEqual(b.windows[0], control.WindowSetArgs{Start: "04:30"}) {
		t.Fatalf("windows = %+v", b.windows)
	}
	send(m, "enter")
	contains(t, m, "start: 04:30")
}

func windowArgsEqual(a, b control.WindowSetArgs) bool {
	return slices.Equal(a.Days, b.Days) && a.Start == b.Start && a.Duration == b.Duration && a.Zone == b.Zone
}

func TestWindowDaysAreSplit(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toWindow...)
	send(m, "e")
	for range 3 {
		send(m, "backspace")
	}
	send(m, typed("mon, thu")...)
	send(m, "f2", "y")
	if len(b.windows) != 1 || !slices.Equal(b.windows[0].Days, []string{"mon", "thu"}) {
		t.Fatalf("windows = %+v", b.windows)
	}
}

func TestNotificationsAreListedWithoutSecrets(t *testing.T) {
	m := newSettingsModel(t, settingsBackend())
	send(m, toNotify...)
	contains(t, m, "ops-hook", "webhook", "failed,rolled-back", "ops-mail", "mail.example.test:587")
}

func TestWebhookChannelIsAddedWithMaskedSecrets(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	hidden := []string{hiddenA, hiddenB}
	send(m, toNotify...)
	send(m, "a")
	sendHidden(t, m, hidden, typed("deploy-hook")...)
	// The type is a webhook, and the first event is ticked.
	sendHidden(t, m, hidden, "down", "down", "space", "f2")
	contains(t, m, "Webhook URL", "Signing key")
	sendHidden(t, m, hidden, typed(hiddenA)...)
	sendHidden(t, m, hidden, "down")
	sendHidden(t, m, hidden, typed(hiddenB)...)
	sendHidden(t, m, hidden, "f2")
	contains(t, m, "deploy-hook")
	if len(b.adds) != 0 {
		t.Fatal("added before the confirmation")
	}
	sendHidden(t, m, hidden, "y")
	if len(b.adds) != 1 {
		t.Fatalf("adds = %+v", b.adds)
	}
	got := b.adds[0]
	if got.Name != "deploy-hook" || got.Type != "webhook" || got.Secret != hiddenA || got.Key != hiddenB ||
		!slices.Equal(got.Events, []string{"succeeded"}) {
		t.Fatalf("add = %+v", got)
	}
	send(m, "enter")
	contains(t, m, "deploy-hook")
}

func TestSMTPChannelTakesItsSettings(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	hidden := []string{hiddenA}
	send(m, toNotify...)
	send(m, "a")
	send(m, typed("alerts")...)
	send(m, "down", "right", "right", "right", "f2")
	contains(t, m, "SMTP host", "Password")
	send(m, typed("mail.example.test")...)
	send(m, "down")
	send(m, typed("2525")...)
	send(m, "down", "right", "down") // tls
	send(m, typed("qd")...)
	send(m, "down")
	send(m, typed(hiddenA)...)
	send(m, "down")
	send(m, typed("qdeploy@example.test")...)
	send(m, "down")
	send(m, typed("ops@example.test, dev@example.test")...)
	sendHidden(t, m, hidden, "f2", "y")
	if len(b.adds) != 1 {
		t.Fatalf("adds = %+v", b.adds)
	}
	want := control.NotifyAddArgs{Name: "alerts", Type: "smtp", Host: "mail.example.test", Port: 2525, Security: "tls", User: "qd",
		Secret: hiddenA, From: "qdeploy@example.test", To: []string{"ops@example.test", "dev@example.test"}}
	got := b.adds[0]
	if got.Name != want.Name || got.Type != want.Type || got.Host != want.Host || got.Port != want.Port || got.Security != want.Security ||
		got.User != want.User || got.Secret != want.Secret || got.From != want.From || !slices.Equal(got.To, want.To) {
		t.Fatalf("add = %+v, want %+v", got, want)
	}
}

func TestChannelFormsRefuseWhatTheServiceWouldRefuse(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toNotify...)
	send(m, "a", "f2")
	contains(t, m, "1 to 32 characters")
	send(m, typed("Bad Name")...)
	send(m, "f2")
	contains(t, m, "1 to 32 characters")
	send(m, "esc", "a")
	send(m, typed("hook")...)
	send(m, "down", "right", "f2", "f2") // slack without a URL
	contains(t, m, "4 to 8192")
	if m.modal != nil || len(b.adds) != 0 {
		t.Fatal("a refused channel went on")
	}
	// A password without a user is refused for smtp.
	send(m, "esc", "a")
	send(m, typed("mail")...)
	send(m, "down", "right", "right", "right", "f2")
	send(m, typed("mail.example.test")...)
	send(m, "down", "down", "down", "down")
	send(m, typed(hiddenA)...)
	send(m, "f2")
	contains(t, m, "password needs a user")
	noLeak(t, m.render(), hiddenA)
}

func TestChannelTestAndRemovalAskFirst(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toNotify...)
	send(m, "s", "n", "x", "n")
	if len(b.actions()) != 0 {
		t.Fatalf("calls %v", b.actions())
	}
	send(m, "s", "y")
	contains(t, m, "ops-hook was delivered")
	send(m, "enter", "down", "x", "y")
	if got := b.actions(); !slices.Equal(got, []string{"notifytest:ops-hook", "notifyremove:ops-mail"}) {
		t.Fatalf("calls = %v", got)
	}
	send(m, "enter")
	if strings.Contains(m.render(), "ops-mail") {
		t.Errorf("the removed channel is still listed:\n%s", m.render())
	}
}

func TestServiceFailuresOfSettingsAreShownWithoutAValue(t *testing.T) {
	b := settingsBackend()
	b.errs["credset:registry.reg.example.test"] = control.NewError(control.CodeInvalid, "the credential store refused the write")
	m := newSettingsModel(t, b)
	send(m, toCreds...)
	send(m, "a", "down")
	send(m, typed("reg.example.test")...)
	send(m, "down")
	send(m, typed("alice")...)
	send(m, "down")
	send(m, typed(hiddenA)...)
	send(m, "f2", "y")
	contains(t, m, "the credential store refused the write")
	if m.set.form == nil {
		t.Fatal("the form was dropped by a failed write")
	}
	noLeak(t, m.render(), hiddenA)
}

func TestSettingsKeysDoNothingOutsideTheirPage(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, "a", "x", "e", "b", "F", "G", "s")
	if m.modal != nil || m.set.form != nil || len(b.actions()) != 0 {
		t.Fatal("a settings key acted in the Stacks area")
	}
	send(m, toMenu...)
	send(m, "a", "x", "e", "b", "F", "G", "s")
	if m.modal != nil || m.set.form != nil || len(b.actions()) != 0 {
		t.Fatal("a settings key acted on the menu")
	}
}

func TestWritesRefuseAKeyWhileOneIsUnderWay(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	send(m, toRepo...)
	send(m, "b")
	send(m, typed(repoURL)...)
	send(m, "tab")
	send(m, typed(stackDir)...)
	cmd := press(m, "f2")
	if !m.set.busy {
		t.Fatal("no write under way")
	}
	// A key while the probe runs changes nothing.
	press(m, "x", "esc")
	if m.set.form == nil {
		t.Fatal("the form was closed under a write")
	}
	drain(m, cmd)
	if m.set.busy {
		t.Fatal("still busy")
	}
}

// fakeFolders records the host side of binding a stack folder.
type fakeFolders struct {
	absent   bool
	grantErr error
	calls    []string
}

func (f *fakeFolders) Missing(dir string) (bool, error) {
	f.calls = append(f.calls, "missing:"+dir)
	return f.absent, nil
}

func (f *fakeFolders) Grant(_ context.Context, dir string, create bool) error {
	f.calls = append(f.calls, fmt.Sprintf("grant:%s:%t", dir, create))
	return f.grantErr
}

func TestRepoBindCreatesAMissingFolderOnlyAfterConfirmation(t *testing.T) {
	for _, lang := range []struct {
		lang i18n.Lang
		want string
	}{{i18n.English, "Create the stack folder " + stackDir}, {i18n.German, "Stack-Ordner " + stackDir + " anlegen"}} {
		b := settingsBackend()
		m := newTestModel(t, lang.lang, b, wideWidth, settingsRows)
		f := &fakeFolders{absent: true}
		m.folders = f
		send(m, toRepo...)
		send(m, "b")
		send(m, typed(repoURL)...)
		send(m, "tab")
		send(m, typed(stackDir)...)
		send(m, "f2")
		contains(t, m, lang.want)
		if got := f.calls; !slices.Equal(got, []string{"missing:" + stackDir}) || len(b.actions()) != 0 {
			t.Fatalf("before the answer: folder calls %v, backend calls %v", got, b.actions())
		}
		send(m, "n")
		send(m, "f2", "enter") // enter is the default answer, which is cancel
		if len(f.calls) != 2 || len(b.actions()) != 0 {
			t.Fatalf("a declined folder had an effect: %v, %v", f.calls, b.actions())
		}
		send(m, "f2", "y")
		want := []string{"missing:" + stackDir, "missing:" + stackDir, "missing:" + stackDir, "grant:" + stackDir + ":true"}
		if !slices.Equal(f.calls, want) {
			t.Fatalf("folder calls = %v, want %v", f.calls, want)
		}
		if got := b.actions(); !slices.Equal(got, []string{"probe:" + repoURL}) {
			t.Fatalf("the probe does not follow the folder: %v", got)
		}
	}
}

func TestRepoBindOfAnExistingFolderOnlyGrantsIt(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	f := &fakeFolders{}
	m.folders = f
	send(m, toRepo...)
	send(m, "b")
	send(m, typed(repoURL)...)
	send(m, "tab")
	send(m, typed(stackDir)...)
	send(m, "f2")
	if want := []string{"missing:" + stackDir, "grant:" + stackDir + ":false"}; !slices.Equal(f.calls, want) {
		t.Fatalf("folder calls = %v, want %v", f.calls, want)
	}
	contains(t, m, "host: example.test")
}

func TestRepoBindStopsWhenTheFolderCannotBeGranted(t *testing.T) {
	b := settingsBackend()
	m := newSettingsModel(t, b)
	m.folders = &fakeFolders{grantErr: errors.New("the service manager refused")}
	send(m, toRepo...)
	send(m, "b")
	send(m, typed(repoURL)...)
	send(m, "tab")
	send(m, typed(stackDir)...)
	send(m, "f2")
	contains(t, m, "the service manager refused")
	if len(b.actions()) != 0 || m.modal != nil {
		t.Fatalf("the bind went on: %v", b.actions())
	}
}

func TestWindowDaysInUILanguage(t *testing.T) {
	week := []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	for _, c := range []struct {
		days []string
		want string
	}{{week, "Tage: mo,di,mi,do,fr,sa,so"}, {[]string{"mon", "thu", "x"}, "Tage: mo,do,x"}} {
		b := settingsBackend()
		b.window.Days = c.days
		m := newTestModel(t, i18n.German, b, wideWidth, settingsRows)
		send(m, toWindow...)
		contains(t, m, c.want)
		contains(t, m, "Sa 2026-10-31 03:00")
	}
}
