package notifier_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	kitnotify "github.com/castrowithcee/qatlas-kit/notify"

	"github.com/castrowithcee/qatlas-deploy/internal/notifier"
)

const (
	demoURL          = "https://hooks.example.test/T000/B000/not-a-real-token"
	demoKey          = "signing-key-not-real"
	demoBotToken     = "123456:not-a-real-token"
	demoMailPassword = "mail-password-not-real"
	demoUserinfoURL  = "https://user:pw@example.test/x"
	demoFTPURL       = "ftp://hooks.example.test/x"
	demoPlainURL     = "http://hooks.example.test/x"
	demoLocalURL     = "http://127.0.0.1:8080/x"
)

type creds map[string]string

func (c creds) Value(name string) (string, error) {
	if v, ok := c[name]; ok {
		return v, nil
	}
	return "", errors.New("missing")
}

func webhook(name string, events ...string) notifier.Channel {
	return notifier.Channel{Name: name, Type: notifier.TypeWebhook, Events: events}
}

func TestValidate(t *testing.T) {
	good := []struct {
		c notifier.Channel
		s notifier.Secrets
	}{
		{webhook("ops"), notifier.Secrets{Secret: demoURL, Key: demoKey}},
		{webhook("local"), notifier.Secrets{Secret: demoLocalURL, Key: demoKey}},
		{notifier.Channel{Name: "chat", Type: notifier.TypeSlack}, notifier.Secrets{Secret: demoURL}},
		{notifier.Channel{Name: "tg", Type: notifier.TypeTelegram, ChatID: "-100123"}, notifier.Secrets{Secret: demoBotToken}},
		{notifier.Channel{Name: "tg2", Type: notifier.TypeTelegram, ChatID: "@demo_chat", BaseURL: "https://bot.example.test"}, notifier.Secrets{Secret: demoBotToken}},
		{notifier.Channel{Name: "mail", Type: notifier.TypeSMTP, Host: "mail.example.test", From: "qdeploy@example.test", To: []string{"ops@example.test"}}, notifier.Secrets{}},
		{notifier.Channel{Name: "mail2", Type: notifier.TypeSMTP, Host: "mail.example.test", Port: 465, Security: "tls", User: "u", From: "qdeploy@example.test", To: []string{"ops@example.test"}}, notifier.Secrets{Secret: demoMailPassword}},
	}
	for _, g := range good {
		got, err := notifier.Validate(g.c, &g.s)
		if err != nil {
			t.Errorf("%s: %v", g.c.Name, err)
			continue
		}
		if !slices.Equal(got.Events, notifier.AllEvents) {
			t.Errorf("%s: events = %v, want all", g.c.Name, got.Events)
		}
	}
	mail := notifier.Channel{Type: notifier.TypeSMTP, Host: "mail.example.test", From: "qdeploy@example.test", To: []string{"ops@example.test"}}
	bad := []struct {
		name string
		c    notifier.Channel
		s    notifier.Secrets
	}{
		{"empty name", notifier.Channel{Type: notifier.TypeSlack}, notifier.Secrets{Secret: demoURL}},
		{"dotted name", notifier.Channel{Name: "a.key", Type: notifier.TypeSlack}, notifier.Secrets{Secret: demoURL}},
		{"unknown type", notifier.Channel{Name: "x", Type: "pager"}, notifier.Secrets{}},
		{"unknown event", webhook("x", "exploded"), notifier.Secrets{Secret: demoURL, Key: demoKey}},
		{"webhook without key", webhook("x"), notifier.Secrets{Secret: demoURL}},
		{"webhook userinfo", webhook("x"), notifier.Secrets{Secret: demoUserinfoURL, Key: demoKey}},
		{"webhook scheme", webhook("x"), notifier.Secrets{Secret: demoFTPURL, Key: demoKey}},
		{"webhook plain http", webhook("x"), notifier.Secrets{Secret: demoPlainURL, Key: demoKey}},
		{"slack with key", notifier.Channel{Name: "x", Type: notifier.TypeSlack}, notifier.Secrets{Secret: demoURL, Key: demoKey}},
		{"slack with host", notifier.Channel{Name: "x", Type: notifier.TypeSlack, Host: "h.example.test"}, notifier.Secrets{Secret: demoURL}},
		{"telegram short token", notifier.Channel{Name: "x", Type: notifier.TypeTelegram, ChatID: "1"}, notifier.Secrets{Secret: "abc"}},
		{"telegram bad chat", notifier.Channel{Name: "x", Type: notifier.TypeTelegram, ChatID: "a b"}, notifier.Secrets{Secret: demoBotToken}},
		{"telegram base query", notifier.Channel{Name: "x", Type: notifier.TypeTelegram, ChatID: "1", BaseURL: "https://b.example.test/?k=v"}, notifier.Secrets{Secret: demoBotToken}},
		{"smtp password without user", withName(mail, "x"), notifier.Secrets{Secret: demoMailPassword}},
		{"smtp user without password", withUser(withName(mail, "x"), "u"), notifier.Secrets{}},
		{"smtp bad security", withSecurity(withName(mail, "x"), "none"), notifier.Secrets{}},
		{"smtp display name", withFrom(withName(mail, "x"), "Q <qdeploy@example.test>"), notifier.Secrets{}},
		{"smtp no recipient", withTo(withName(mail, "x"), nil), notifier.Secrets{}},
		{"smtp host with slash", withHost(withName(mail, "x"), "mail/x"), notifier.Secrets{}},
		{"smtp with chat", withChat(withName(mail, "x"), "1"), notifier.Secrets{}},
	}
	for _, b := range bad {
		_, err := notifier.Validate(b.c, &b.s)
		var ie *notifier.InvalidError
		if !errors.As(err, &ie) {
			t.Errorf("%s: err = %v, want InvalidError", b.name, err)
			continue
		}
		for _, secret := range []string{b.s.Secret, b.s.Key} {
			if secret != "" && strings.Contains(err.Error(), secret) {
				t.Errorf("%s: the error echoes a secret: %q", b.name, err)
			}
		}
	}
}

func withName(c notifier.Channel, v string) notifier.Channel     { c.Name = v; return c }
func withUser(c notifier.Channel, v string) notifier.Channel     { c.User = v; return c }
func withSecurity(c notifier.Channel, v string) notifier.Channel { c.Security = v; return c }
func withFrom(c notifier.Channel, v string) notifier.Channel     { c.From = v; return c }
func withTo(c notifier.Channel, v []string) notifier.Channel     { c.To = v; return c }
func withHost(c notifier.Channel, v string) notifier.Channel     { c.Host = v; return c }
func withChat(c notifier.Channel, v string) notifier.Channel     { c.ChatID = v; return c }

func TestEventsAreCanonical(t *testing.T) {
	got, err := notifier.NormalizeEvents([]string{"update-available", "failed", "failed"})
	if err != nil || !slices.Equal(got, []string{"failed", "update-available"}) {
		t.Fatalf("events = %v, %v", got, err)
	}
}

func TestSettingsAreStoredPrivatelyWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	s, err := notifier.OpenSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if list, err := s.List(); err != nil || len(list) != 0 {
		t.Fatalf("empty = %v, %v", list, err)
	}
	b, _ := notifier.Validate(webhook("b", "failed"), &notifier.Secrets{Secret: demoURL, Key: demoKey})
	a, _ := notifier.Validate(webhook("a"), &notifier.Secrets{Secret: demoURL, Key: demoKey})
	for _, c := range []notifier.Channel{b, a} {
		if err := s.Add(c); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Add(a); !errors.Is(err, notifier.ErrExists) {
		t.Fatalf("duplicate = %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "notify.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings file = %v, %v", info, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "notify.json"))
	if strings.Contains(string(raw), demoURL) || strings.Contains(string(raw), demoKey) {
		t.Fatalf("the settings hold a secret: %s", raw)
	}
	again, err := notifier.OpenSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	list, err := again.List()
	if err != nil || len(list) != 2 || list[0].Name != "a" || list[1].Name != "b" || !slices.Equal(list[1].Events, []string{"failed"}) {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if err := again.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if err := again.Remove("a"); !errors.Is(err, notifier.ErrNotFound) {
		t.Fatalf("second remove = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notify.json"), []byte(`{"schema":1,"channels":[{"name":"x","type":"pager","events":[]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := again.List(); err == nil {
		t.Fatal("a malformed settings file was accepted")
	}
}

func TestSeenSurvivesReopening(t *testing.T) {
	dir := t.TempDir()
	open := func() *notifier.Seen {
		s, err := notifier.OpenSeen(dir)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	items := []notifier.Item{{Variable: "APP_VERSION", Current: "1.0.0", Available: "1.1.0"}}
	s := open()
	if fresh, err := s.Fresh("web", items); err != nil || len(fresh) != 1 {
		t.Fatalf("fresh = %v, %v", fresh, err)
	}
	if err := s.Mark("web", items); err != nil {
		t.Fatal(err)
	}
	s = open()
	if fresh, _ := s.Fresh("web", items); len(fresh) != 0 {
		t.Fatalf("after reopening = %v", fresh)
	}
	if fresh, _ := s.Fresh("other", items); len(fresh) != 1 {
		t.Fatalf("other stack = %v", fresh)
	}
	items[0].Available = "1.2.0"
	if fresh, _ := s.Fresh("web", items); len(fresh) != 1 {
		t.Fatalf("newer version = %v", fresh)
	}
	info, err := os.Stat(filepath.Join(dir, "notify-updates.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("record file = %v, %v", info, err)
	}
}

func TestMessagesAreShortAndStable(t *testing.T) {
	commit := "0123456789abcdef0123456789abcdef01234567"
	cases := []struct {
		ev       notifier.Event
		subject  string
		contains []string
	}{
		{notifier.Event{Type: notifier.EventSucceeded, Stack: "web", Run: "r1", Commit: commit}, "Stack web deployed", []string{"r1", "0123456789ab"}},
		{notifier.Event{Type: notifier.EventFailed, Stack: "web", Run: "r1", Commit: commit, Error: "check failed"}, "Stack web failed", []string{"Error: check failed"}},
		{notifier.Event{Type: notifier.EventRolledBack, Stack: "web", Run: "r1", Commit: commit, Error: "check failed"}, "Stack web rolled back", []string{"rolled back"}},
		{notifier.Event{Type: notifier.EventRollbackFailed, Stack: "web", Run: "r1", Commit: commit, Error: "boom"}, "Stack web: rollback failed", []string{"no further runs"}},
		{notifier.Event{Type: notifier.EventUpdateAvailable, Stack: "web", Items: []notifier.Item{{Variable: "APP_VERSION", Current: "1.0.0", Available: "1.1.0"}}},
			"Update available for stack web", []string{"APP_VERSION: 1.0.0 -> 1.1.0"}},
	}
	for _, c := range cases {
		m := notifier.Message(c.ev)
		if m.Subject != c.subject || m.Event != c.ev.Type {
			t.Errorf("%s: subject = %q, event %q", c.ev.Type, m.Subject, m.Event)
		}
		for _, want := range c.contains {
			if !strings.Contains(m.Text, want) {
				t.Errorf("%s: text %q lacks %q", c.ev.Type, m.Text, want)
			}
		}
		if strings.Contains(m.Text, commit) {
			t.Errorf("%s: text carries the full commit", c.ev.Type)
		}
		var body map[string]any
		if err := json.Unmarshal(m.JSON, &body); err != nil || body["event"] != c.ev.Type || body["stack"] != "web" {
			t.Errorf("%s: json = %s, %v", c.ev.Type, m.JSON, err)
		}
		if c.ev.Type == notifier.EventUpdateAvailable {
			items, _ := body["items"].([]any)
			first, _ := items[0].(map[string]any)
			if len(items) != 1 || first["variable"] != "APP_VERSION" || first["available"] != "1.1.0" || first["current"] != "1.0.0" {
				t.Errorf("update json = %s", m.JSON)
			}
		} else if body["run"] != "r1" || body["commit"] != commit {
			t.Errorf("%s: json = %s", c.ev.Type, m.JSON)
		}
	}
}

// fakeSender records what reaches which webhook address.
type fakeSender struct {
	mu    sync.Mutex
	got   map[string][]string
	block chan struct{}
	fail  map[string]bool
}

func (f *fakeSender) Send(ctx context.Context, ch kitnotify.Channel, m kitnotify.Message) error {
	w := ch.(kitnotify.Webhook)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail[w.URL] {
		return &kitnotify.Error{Code: "unreachable", Message: "the webhook receiver is not reachable"}
	}
	if f.got == nil {
		f.got = map[string][]string{}
	}
	f.got[w.URL] = append(f.got[w.URL], m.Event)
	return nil
}

func (f *fakeSender) events(url string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.got[url])
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func dispatcher(t *testing.T, s notifier.Sender, list []notifier.Channel, tune func(*notifier.DispatchConfig)) (*notifier.Dispatcher, *lockedBuffer) {
	t.Helper()
	store := creds{}
	for _, c := range list {
		store[notifier.SecretName(c.Name)] = "https://hooks.example.test/" + c.Name
		store[notifier.KeyName(c.Name)] = demoKey
	}
	logs := &lockedBuffer{}
	cfg := notifier.DispatchConfig{
		Sender: s, Channels: func() ([]notifier.Channel, error) { return list, nil }, Creds: store,
		Log: slog.New(slog.NewTextHandler(logs, nil)), Grace: 50 * time.Millisecond,
	}
	if tune != nil {
		tune(&cfg)
	}
	d := notifier.NewDispatcher(cfg)
	t.Cleanup(d.Close)
	return d, logs
}

func TestEveryEventReachesExactlyTheChannelsThatSelectedIt(t *testing.T) {
	list := []notifier.Channel{
		{Name: "all", Type: notifier.TypeWebhook, Events: notifier.AllEvents},
		{Name: "bad", Type: notifier.TypeWebhook, Events: []string{notifier.EventFailed, notifier.EventRollbackFailed}},
		{Name: "updates", Type: notifier.TypeWebhook, Events: []string{notifier.EventUpdateAvailable}},
	}
	fs := &fakeSender{}
	d, _ := dispatcher(t, fs, list, nil)
	for _, e := range notifier.AllEvents {
		d.Submit(notifier.Event{Type: e, Stack: "web", Run: "r1"})
	}
	d.Close() // drains
	url := func(n string) string { return "https://hooks.example.test/" + n }
	if got := fs.events(url("all")); !slices.Equal(got, notifier.AllEvents) {
		t.Errorf("all = %v", got)
	}
	if got := fs.events(url("bad")); !slices.Equal(got, []string{"failed", "rollback-failed"}) {
		t.Errorf("bad = %v", got)
	}
	if got := fs.events(url("updates")); !slices.Equal(got, []string{"update-available"}) {
		t.Errorf("updates = %v", got)
	}
}

func TestSubmitReportsWhetherAnythingWasQueued(t *testing.T) {
	list := []notifier.Channel{{Name: "bad", Type: notifier.TypeWebhook, Events: []string{notifier.EventFailed}}}
	d, _ := dispatcher(t, &fakeSender{}, list, nil)
	if d.Submit(notifier.Event{Type: notifier.EventSucceeded, Stack: "web"}) {
		t.Error("an event no channel selected was queued")
	}
	if !d.Submit(notifier.Event{Type: notifier.EventFailed, Stack: "web"}) {
		t.Error("a selected event was not queued")
	}
	d.Close()
	if d.Submit(notifier.Event{Type: notifier.EventFailed, Stack: "web"}) {
		t.Error("an event was queued after Close")
	}
}

func TestAHangingChannelNeverBlocksTheCaller(t *testing.T) {
	list := []notifier.Channel{{Name: "stuck", Type: notifier.TypeWebhook, Events: notifier.AllEvents}}
	fs := &fakeSender{block: make(chan struct{})}
	d, logs := dispatcher(t, fs, list, func(c *notifier.DispatchConfig) { c.Queue = 4 })
	start := time.Now()
	queued := 0
	for i := 0; i < 50; i++ {
		if d.Submit(notifier.Event{Type: notifier.EventFailed, Stack: "web", Run: "r"}) {
			queued++
		}
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("Submit took %s", took)
	}
	if queued == 50 || queued < 4 {
		t.Fatalf("queued %d of 50 with a queue of 4", queued)
	}
	if !strings.Contains(logs.String(), "queue is full") {
		t.Fatalf("a dropped event is not logged: %s", logs.String())
	}
	closed := make(chan struct{})
	go func() { d.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return while a channel hangs")
	}
}

func TestAFailingChannelIsLoggedAndTheOthersStillGetTheEvent(t *testing.T) {
	list := []notifier.Channel{
		{Name: "down", Type: notifier.TypeWebhook, Events: notifier.AllEvents},
		{Name: "up", Type: notifier.TypeWebhook, Events: notifier.AllEvents},
	}
	fs := &fakeSender{fail: map[string]bool{"https://hooks.example.test/down": true}}
	d, logs := dispatcher(t, fs, list, nil)
	d.Submit(notifier.Event{Type: notifier.EventFailed, Stack: "web", Run: "r7"})
	d.Close()
	if got := fs.events("https://hooks.example.test/up"); len(got) != 1 {
		t.Fatalf("up = %v", got)
	}
	out := logs.String()
	if !strings.Contains(out, "notification not delivered") || !strings.Contains(out, "channel=down") ||
		!strings.Contains(out, "code=unreachable") || !strings.Contains(out, "run=r7") {
		t.Fatalf("log = %s", out)
	}
	if strings.Contains(out, "hooks.example.test") || strings.Contains(out, demoKey) {
		t.Fatalf("the log carries a credential: %s", out)
	}
}

func TestContainerMessagesNameStackKindContainerAndTimes(t *testing.T) {
	since := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	at := since.Add(95 * time.Second)
	failed := notifier.Message(notifier.Event{Type: notifier.EventContainerFailed, Stack: "web", Kind: "unhealthy", Container: "web-app-1", Since: since, Time: since})
	if failed.Subject != "Stack web: Container web-app-1 failed (unhealthy)" || failed.Event != "container-failed" {
		t.Errorf("failed = %q, %q", failed.Event, failed.Subject)
	}
	for _, want := range []string{"web-app-1 of stack web is unhealthy", "Since: 2026-03-01T10:00:00Z"} {
		if !strings.Contains(failed.Text, want) {
			t.Errorf("failed text %q lacks %q", failed.Text, want)
		}
	}
	var body map[string]any
	if err := json.Unmarshal(failed.JSON, &body); err != nil || body["event"] != "container-failed" || body["stack"] != "web" ||
		body["kind"] != "unhealthy" || body["container"] != "web-app-1" || body["since"] != "2026-03-01T10:00:00Z" || body["at"] != nil {
		t.Errorf("failed json = %s, %v", failed.JSON, err)
	}

	rec := notifier.Message(notifier.Event{Type: notifier.EventRecovered, Stack: "web", Kind: "missing", Container: "app", Since: since, Time: at})
	if rec.Subject != "Stack web: Service app recovered" {
		t.Errorf("recovered subject = %q", rec.Subject)
	}
	for _, want := range []string{"Service app of stack web is fine again", "(missing)", "Failed since: 2026-03-01T10:00:00Z", "down for 1m35s"} {
		if !strings.Contains(rec.Text, want) {
			t.Errorf("recovered text %q lacks %q", rec.Text, want)
		}
	}
	body = nil
	if err := json.Unmarshal(rec.JSON, &body); err != nil || body["event"] != "recovered" || body["kind"] != "missing" || body["at"] != "2026-03-01T10:01:35Z" {
		t.Errorf("recovered json = %s, %v", rec.JSON, err)
	}
}

func TestChannelsWithoutTheMonitorEventsNeverGetThem(t *testing.T) {
	oldAll := []string{"succeeded", "failed", "rolled-back", "rollback-failed", "update-available"}
	list := []notifier.Channel{
		{Name: "old", Type: notifier.TypeWebhook, Events: oldAll},
		{Name: "mon", Type: notifier.TypeWebhook, Events: []string{notifier.EventContainerFailed, notifier.EventRecovered}},
	}
	// A stored list is kept as it is: it does not grow the events added later.
	if got, err := notifier.NormalizeEvents(oldAll); err != nil || !slices.Equal(got, oldAll) {
		t.Fatalf("stored list = %v, %v", got, err)
	}
	if got, err := notifier.NormalizeEvents([]string{"recovered", "container-failed"}); err != nil || !slices.Equal(got, []string{"container-failed", "recovered"}) {
		t.Fatalf("new events = %v, %v", got, err)
	}
	fs := &fakeSender{}
	d, _ := dispatcher(t, fs, list, nil)
	d.Submit(notifier.Event{Type: notifier.EventContainerFailed, Stack: "web", Kind: "exited", Container: "web-app-1"})
	d.Submit(notifier.Event{Type: notifier.EventRecovered, Stack: "web", Kind: "exited", Container: "web-app-1"})
	d.Close()
	if got := fs.events("https://hooks.example.test/old"); len(got) != 0 {
		t.Errorf("old = %v", got)
	}
	if got := fs.events("https://hooks.example.test/mon"); !slices.Equal(got, []string{"container-failed", "recovered"}) {
		t.Errorf("mon = %v", got)
	}
}

func TestRepairMessagesTellOfTheRestart(t *testing.T) {
	since := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	at := since.Add(2 * time.Minute)
	rec := notifier.Message(notifier.Event{Type: notifier.EventRecovered, Stack: "web", Kind: "exited", Container: "web-app-1",
		Since: since, Time: at, Restarted: true})
	if !strings.Contains(rec.Text, "A restart by qdeploy fixed it.") {
		t.Errorf("recovered text %q", rec.Text)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.JSON, &body); err != nil || body["restarted"] != true {
		t.Errorf("recovered json = %s, %v", rec.JSON, err)
	}
	plain := notifier.Message(notifier.Event{Type: notifier.EventRecovered, Stack: "web", Kind: "exited", Since: since, Time: at})
	body = nil
	if err := json.Unmarshal(plain.JSON, &body); err != nil || body["restarted"] != nil || strings.Contains(plain.Text, "restart") {
		t.Errorf("recovery without restart = %q, %s", plain.Text, plain.JSON)
	}

	failed := notifier.Message(notifier.Event{Type: notifier.EventRecoveryFailed, Stack: "web", Kind: "unhealthy",
		Container: "web-app-1", Since: since, Time: at, Attempts: 3})
	if failed.Event != "recovery-failed" || failed.Subject != "Stack web: Container web-app-1 not repaired" {
		t.Errorf("recovery-failed = %q, %q", failed.Event, failed.Subject)
	}
	for _, want := range []string{"web-app-1 of stack web is unhealthy and 3 restarts did not fix it", "until an operator resumes",
		"Failed since: 2026-03-01T10:00:00Z"} {
		if !strings.Contains(failed.Text, want) {
			t.Errorf("recovery-failed text %q lacks %q", failed.Text, want)
		}
	}
	body = nil
	if err := json.Unmarshal(failed.JSON, &body); err != nil || body["event"] != "recovery-failed" || body["kind"] != "unhealthy" ||
		body["container"] != "web-app-1" || body["attempts"] != float64(3) || body["at"] != "2026-03-01T10:02:00Z" {
		t.Errorf("recovery-failed json = %s, %v", failed.JSON, err)
	}
	if got, err := notifier.NormalizeEvents([]string{"recovery-failed"}); err != nil || !slices.Equal(got, []string{"recovery-failed"}) {
		t.Errorf("select recovery-failed = %v, %v", got, err)
	}
}
