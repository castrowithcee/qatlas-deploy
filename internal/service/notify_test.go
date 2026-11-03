package service_test

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/notify"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/notifier"
	"github.com/castrowithcee/qatlas-deploy/internal/registry/registrytest"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
)

const (
	hookToken    = "hook-token-not-real"
	hookKey      = "signing-key-not-real"
	mailPassword = "mail-password-not-real"
	userinfoHook = "https://user:" + hookToken + "@hooks.example.test/x"
)

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

// received is one request a fake receiver saw.
type received struct {
	Path    string
	Header  http.Header
	Body    []byte
	Event   string
	Stack   string
	Variant string
}

// receiver is a TLS server that stands in for the notification services. /ok answers 200, /denied 401, /down
// 500 with a body that echoes the token, /hang waits until release is closed.
type receiver struct {
	*httptest.Server
	mu      sync.Mutex
	reqs    []received
	release chan struct{}
	once    sync.Once
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{release: make(chan struct{})}
	r.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var parsed struct {
			Event string `json:"event"`
			Stack string `json:"stack"`
		}
		_ = json.Unmarshal(body, &parsed)
		r.mu.Lock()
		r.reqs = append(r.reqs, received{Path: req.URL.Path, Header: req.Header.Clone(), Body: body, Event: parsed.Event, Stack: parsed.Stack})
		r.mu.Unlock()
		switch {
		case strings.HasPrefix(req.URL.Path, "/denied/"):
			w.WriteHeader(http.StatusUnauthorized)
		case strings.HasPrefix(req.URL.Path, "/down/"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "internal error for "+hookToken)
		case strings.HasPrefix(req.URL.Path, "/hang/"):
			select {
			case <-r.release:
			case <-req.Context().Done():
			}
		}
	}))
	t.Cleanup(func() { r.unblock(); r.Close() })
	return r
}

// unblock lets the requests of /hang end.
func (r *receiver) unblock() { r.once.Do(func() { close(r.release) }) }

func (r *receiver) url(path string) string { return r.URL + path + "/" + hookToken }

func (r *receiver) seen(pathPrefix string) []received {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []received
	for _, q := range r.reqs {
		if strings.HasPrefix(q.Path, pathPrefix) {
			out = append(out, q)
		}
	}
	return out
}

func (r *receiver) events(pathPrefix string) []string {
	var out []string
	for _, q := range r.seen(pathPrefix) {
		out = append(out, q.Event)
	}
	return out
}

func (r *receiver) await(t *testing.T, pathPrefix string, n int) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if len(r.seen(pathPrefix)) >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: saw %d of %d requests", pathPrefix, len(r.seen(pathPrefix)), n)
		}
	}
}

func (r *receiver) options() notify.Options {
	pool := x509.NewCertPool()
	pool.AddCert(r.Certificate())
	return notify.Options{TLS: &tls.Config{RootCAs: pool}, Backoff: time.Millisecond, Interval: time.Nanosecond, Timeout: 5 * time.Second}
}

func withNotify(r *receiver) func(*service.Config) {
	return func(c *service.Config) { c.Notify = r.options() }
}

func addWebhook(t *testing.T, h *harness, name, url string, events ...string) {
	t.Helper()
	var res control.NotifyChannelInfo
	if err := h.call(control.NotifyAddCommand, control.NotifyAddArgs{
		Name: name, Type: control.NotifyWebhook, Events: events, Secret: url, Key: hookKey,
	}, &res); err != nil {
		t.Fatal(err)
	}
}

func TestNotifyChannelCommands(t *testing.T) {
	rcv := newReceiver(t)
	h := startWith(t, nil, withNotify(rcv))

	var added control.NotifyChannelInfo
	if err := h.call(control.NotifyAddCommand, control.NotifyAddArgs{
		Name: "ops", Type: control.NotifyWebhook, Events: []string{"failed", "rollback-failed"}, Secret: rcv.url("/ok"), Key: hookKey,
	}, &added); err != nil || added.Name != "ops" || !slices.Equal(added.Events, []string{"failed", "rollback-failed"}) {
		t.Fatalf("add webhook = %+v, %v", added, err)
	}
	if err := h.call(control.NotifyAddCommand, control.NotifyAddArgs{Name: "chat", Type: control.NotifySlack, Secret: rcv.url("/ok")}, &added); err != nil ||
		!slices.Equal(added.Events, notifier.AllEvents) {
		t.Fatalf("add slack = %+v, %v", added, err)
	}
	if err := h.call(control.NotifyAddCommand, control.NotifyAddArgs{
		Name: "tg", Type: control.NotifyTelegram, Secret: "123456:" + hookToken, ChatID: "-100123", BaseURL: rcv.URL + "/tg",
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.call(control.NotifyAddCommand, control.NotifyAddArgs{
		Name: "mail", Type: control.NotifySMTP, Host: "mail.example.test", User: "qd", Secret: mailPassword,
		From: "qdeploy@example.test", To: []string{"ops@example.test"},
	}, nil); err != nil {
		t.Fatal(err)
	}

	var list control.NotifyListResult
	if err := h.call(control.NotifyListCommand, nil, &list); err != nil || len(list.Channels) != 4 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	names := []string{list.Channels[0].Name, list.Channels[1].Name, list.Channels[2].Name, list.Channels[3].Name}
	if !slices.Equal(names, []string{"chat", "mail", "ops", "tg"}) || list.Channels[1].Security != "starttls" || list.Channels[1].User != "qd" {
		t.Fatalf("channels = %+v", list.Channels)
	}
	raw, _ := json.Marshal(list)
	for _, secret := range []string{hookToken, hookKey, mailPassword} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("the list carries %q: %s", secret, raw)
		}
	}

	info, err := os.Stat(filepath.Join(h.state, "notify.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings file = %v, %v", info, err)
	}
	file, _ := os.ReadFile(filepath.Join(h.state, "notify.json"))
	for _, secret := range []string{hookToken, hookKey, mailPassword} {
		if strings.Contains(string(file), secret) {
			t.Fatalf("the settings file carries %q", secret)
		}
	}
	var stored control.CredentialListResult
	if err := h.call(control.CredentialListCommand, nil, &stored); err != nil {
		t.Fatal(err)
	}
	var credNames []string
	for _, c := range stored.Credentials {
		credNames = append(credNames, c.Name)
	}
	slices.Sort(credNames)
	if want := []string{"notify.chat", "notify.mail", "notify.ops", "notify.ops.key", "notify.tg"}; !slices.Equal(credNames, want) {
		t.Fatalf("credentials = %v, want %v", credNames, want)
	}

	// Refusals carry fixed texts that never echo the request.
	secretArgs := control.NotifyAddArgs{Name: "ops", Type: control.NotifyWebhook, Secret: rcv.url("/ok"), Key: hookKey}
	wantCode(t, h.call(control.NotifyAddCommand, secretArgs, nil), control.CodeChannelExists)
	for _, bad := range []control.NotifyAddArgs{
		{Name: "Bad Name", Type: control.NotifyWebhook, Secret: rcv.url("/ok"), Key: hookKey},
		{Name: "x", Type: "pager", Secret: hookToken},
		{Name: "x", Type: control.NotifyWebhook, Secret: rcv.url("/ok")},
		{Name: "x", Type: control.NotifyWebhook, Secret: userinfoHook, Key: hookKey},
		{Name: "x", Type: control.NotifySlack, Secret: rcv.url("/ok"), Events: []string{"nonsense"}},
		{Name: "x", Type: control.NotifySlack, Secret: rcv.url("/ok"), Host: "mail.example.test"},
	} {
		err := h.call(control.NotifyAddCommand, bad, nil)
		wantCode(t, err, control.CodeInvalid)
		if strings.Contains(err.Error(), hookToken) || strings.Contains(err.Error(), hookKey) {
			t.Fatalf("the refusal echoes a secret: %v", err)
		}
	}
	wantCode(t, h.call(control.NotifyAddCommand, map[string]any{"name": "x", "type": "slack", "bogus": hookToken}, nil), control.CodeBadRequest)
	wantCode(t, h.call(control.NotifyRemoveCommand, control.NotifyNameArgs{Name: "missing"}, nil), control.CodeNoSuchChannel)
	wantCode(t, h.call(control.NotifyTestCommand, control.NotifyNameArgs{Name: "missing"}, nil), control.CodeNoSuchChannel)

	if err := h.call(control.NotifyRemoveCommand, control.NotifyNameArgs{Name: "ops"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.call(control.CredentialListCommand, nil, &stored); err != nil {
		t.Fatal(err)
	}
	for _, c := range stored.Credentials {
		if strings.HasPrefix(c.Name, "notify.ops") {
			t.Fatalf("removing the channel left %s", c.Name)
		}
	}
	if err := h.call(control.NotifyListCommand, nil, &list); err != nil || len(list.Channels) != 3 {
		t.Fatalf("list after remove = %+v, %v", list, err)
	}
	if strings.Contains(h.logs.String(), hookToken) || strings.Contains(h.logs.String(), hookKey) {
		t.Fatalf("the service log carries a secret: %s", h.logs.String())
	}
}

func TestNotifyTestDeliversAndReportsFailuresWithKitCodes(t *testing.T) {
	rcv := newReceiver(t)
	h := startWith(t, nil, withNotify(rcv))
	addWebhook(t, h, "good", rcv.url("/ok"))
	addWebhook(t, h, "denied", rcv.url("/denied"))
	addWebhook(t, h, "down", rcv.url("/down"))
	if err := h.call(control.NotifyAddCommand, control.NotifyAddArgs{Name: "chat", Type: control.NotifySlack, Secret: rcv.url("/ok")}, nil); err != nil {
		t.Fatal(err)
	}

	var res control.NotifyTestResult
	if err := h.call(control.NotifyTestCommand, control.NotifyNameArgs{Name: "good"}, &res); err != nil || res.Name != "good" || res.Type != "webhook" {
		t.Fatalf("test = %+v, %v", res, err)
	}
	got := rcv.seen("/ok")
	if len(got) != 1 || got[0].Event != "test" {
		t.Fatalf("receiver saw %+v", got)
	}
	ts, sig := got[0].Header.Get(notifier.HeaderTimestamp), got[0].Header.Get(notifier.HeaderSignature)
	if err := notify.Verify([]byte(hookKey), ts, sig, got[0].Body, time.Now(), time.Minute); err != nil ||
		got[0].Header.Get(notifier.HeaderEvent) != "test" {
		t.Fatalf("the webhook is not signed as documented: %v, %v", err, got[0].Header)
	}
	if err := h.call(control.NotifyTestCommand, control.NotifyNameArgs{Name: "chat"}, nil); err != nil {
		t.Fatal(err)
	}

	for name, code := range map[string]string{"denied": "auth", "down": "runtime"} {
		err := h.call(control.NotifyTestCommand, control.NotifyNameArgs{Name: name}, nil)
		wantCode(t, err, code)
		if strings.Contains(err.Error(), hookToken) || strings.Contains(err.Error(), hookKey) {
			t.Fatalf("%s: the error carries a secret: %v", name, err)
		}
	}
	// A test makes a single try.
	if n := len(rcv.seen("/down")); n != 1 {
		t.Fatalf("the failing channel was tried %d times", n)
	}
	if strings.Contains(h.logs.String(), hookToken) || strings.Contains(h.logs.String(), hookKey) {
		t.Fatalf("the service log carries a secret: %s", h.logs.String())
	}
}

func TestRunEventsReachTheChannelsThatSelectedThem(t *testing.T) {
	rcv := newReceiver(t)
	dep := &fakeDeployer{}
	h := boundWith(t, dep, nil, withNotify(rcv))
	addWebhook(t, h, "everything", rcv.url("/all"))
	addWebhook(t, h, "good", rcv.url("/good"), "succeeded")
	addWebhook(t, h, "bad", rcv.url("/bad"), "failed", "rolled-back", "rollback-failed")

	deploy := func() control.RunInfo {
		var d control.DeployResult
		if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &d); err != nil {
			t.Fatal(err)
		}
		return waitRun(t, h, d.ID)
	}
	ok := deploy()
	if ok.Result != engine.ResultSucceeded {
		t.Fatalf("run = %+v", ok)
	}
	rcv.await(t, "/all", 1)
	rcv.await(t, "/good", 1)

	dep.setFailCheck(true)
	failed := deploy()
	if failed.Result != engine.ResultRollbackFailed {
		t.Fatalf("run = %+v", failed)
	}
	rcv.await(t, "/all", 2)
	rcv.await(t, "/bad", 1)

	if got := rcv.events("/all"); !slices.Equal(got, []string{"succeeded", "rollback-failed"}) {
		t.Errorf("all = %v", got)
	}
	if got := rcv.events("/good"); !slices.Equal(got, []string{"succeeded"}) {
		t.Errorf("good = %v", got)
	}
	if got := rcv.events("/bad"); !slices.Equal(got, []string{"rollback-failed"}) {
		t.Errorf("bad = %v", got)
	}
	var body map[string]any
	last := rcv.seen("/bad")[0]
	if err := json.Unmarshal(last.Body, &body); err != nil || body["stack"] != "plain-stack" || body["run"] != failed.ID ||
		body["commit"] != h.git.Head || body["error"] == "" {
		t.Fatalf("body = %s, %v", last.Body, err)
	}

	for _, id := range []string{ok.ID, failed.ID} {
		var log control.LogResult
		if err := h.call(control.LogCommand, control.LogArgs{ID: id}, &log); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(log.Text, hookToken) || strings.Contains(log.Text, hookKey) {
			t.Fatalf("the run log carries a secret: %s", log.Text)
		}
	}
	if strings.Contains(h.logs.String(), hookToken) || strings.Contains(h.logs.String(), hookKey) {
		t.Fatalf("the service log carries a secret: %s", h.logs.String())
	}
}

func TestAHangingChannelDoesNotHoldARun(t *testing.T) {
	rcv := newReceiver(t)
	h := boundWith(t, &fakeDeployer{}, nil, withNotify(rcv))
	addWebhook(t, h, "stuck", rcv.url("/hang"))
	addWebhook(t, h, "down", rcv.url("/down"))
	for i := 0; i < 3; i++ {
		var d control.DeployResult
		if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &d); err != nil {
			t.Fatal(err)
		}
		if r := waitRun(t, h, d.ID); r.Result != engine.ResultSucceeded {
			t.Fatalf("run %d = %+v", i, r)
		}
	}
	rcv.await(t, "/hang", 1)
	rcv.await(t, "/down", 3)
	// The failed delivery is in the service log, without the credential the receiver echoed.
	for deadline := time.Now().Add(10 * time.Second); !strings.Contains(h.logs.String(), "notification not delivered"); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("no failed delivery in the log: %s", h.logs.String())
		}
	}
	if strings.Contains(h.logs.String(), hookToken) || strings.Contains(h.logs.String(), hookKey) {
		t.Fatalf("the service log carries a secret: %s", h.logs.String())
	}
	rcv.unblock()
}

func reopenWindow(t *testing.T, h *harness, clock *svcClock, prev control.WindowResult, start string, at time.Time) control.WindowResult {
	t.Helper()
	if err := h.call(control.WindowSetCommand, control.WindowSetArgs{Start: start, Duration: "1h"}, nil); err != nil {
		t.Fatal(err)
	}
	clock.awaitTimer(t, at)
	clock.Advance(at.Sub(clock.Now()))
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		var res control.WindowResult
		if err := h.call(control.WindowShowCommand, nil, &res); err != nil {
			t.Fatal(err)
		}
		if res.LastRun.After(prev.LastRun) && !res.Running {
			return res
		}
		if time.Now().After(deadline) {
			t.Fatalf("the window did not run again: %+v", res)
		}
	}
}

func TestUpdateAvailableIsSentOnlyForManualStacksAndOncePerVersion(t *testing.T) {
	rcv := newReceiver(t)
	dep := &sequentialDeployer{}
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": {"0.9.1", "0.9.2", "1.2.3", "1.3.0", "2.0.0", "2.1.0-rc.1"}}})
	clock := &svcClock{now: noon}
	files := map[string]string{"demo-stack/qdeploy.yaml": manualRules, "plain-stack/qdeploy.yaml": automaticRules}
	h := boundWith(t, dep, files, withRegistry(srv.URL), withClock(clock, nil), withNotify(rcv))
	addWebhook(t, h, "updates", rcv.url("/upd"), "update-available")

	first := openWindow(t, h, clock)
	rcv.await(t, "/upd", 1)
	reopenWindow(t, h, clock, first, "13:20", noon.Add(80*time.Minute))
	time.Sleep(200 * time.Millisecond)

	got := rcv.seen("/upd")
	if len(got) != 1 || got[0].Event != "update-available" || got[0].Stack != "demo-stack" {
		t.Fatalf("receiver saw %d requests: %+v", len(got), got)
	}
	var body struct {
		Items []notifier.Item `json:"items"`
	}
	if err := json.Unmarshal(got[0].Body, &body); err != nil || len(body.Items) != 1 || body.Items[0].Available != "1.3.0" || body.Items[0].Current != "1.2.3" {
		t.Fatalf("body = %s, %v", got[0].Body, err)
	}
}

func TestFailedDeliveryIsRecordedInTheRunLog(t *testing.T) {
	rcv := newReceiver(t)
	h := boundWith(t, &fakeDeployer{}, nil, withNotify(rcv))
	addWebhook(t, h, "fine", rcv.url("/ok"), "succeeded")
	addWebhook(t, h, "down", rcv.url("/down"), "succeeded")
	addWebhook(t, h, "denied", rcv.url("/denied"), "failed")

	var d control.DeployResult
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &d); err != nil {
		t.Fatal(err)
	}
	if r := waitRun(t, h, d.ID); r.Result != engine.ResultSucceeded {
		t.Fatalf("run = %+v", r)
	}
	rcv.await(t, "/ok", 1)
	rcv.await(t, "/down", 1)

	var text string
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		var log control.LogResult
		if err := h.call(control.LogCommand, control.LogArgs{ID: d.ID}, &log); err != nil {
			t.Fatal(err)
		}
		text = log.Text
		if strings.Contains(text, "not delivered") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no failed delivery in the run log:\n%s", text)
		}
	}
	if strings.Count(text, "not delivered") != 1 || !strings.Contains(text, "channel down not delivered") {
		t.Fatalf("run log:\n%s", text)
	}
	if strings.Contains(text, "channel fine") || strings.Contains(text, "channel denied") {
		t.Fatalf("run log names a channel that delivered or was not selected:\n%s", text)
	}
	if strings.Contains(text, hookToken) || strings.Contains(text, hookKey) {
		t.Fatalf("the run log carries a secret:\n%s", text)
	}
	if !strings.Contains(h.logs.String(), "notification not delivered") {
		t.Fatalf("the service log lost the failure: %s", h.logs.String())
	}
}

func TestSuccessfulDeliveryAddsNothingToTheRunLog(t *testing.T) {
	rcv := newReceiver(t)
	h := boundWith(t, &fakeDeployer{}, nil, withNotify(rcv))
	var after control.LogResult
	var d control.DeployResult
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &d); err != nil {
		t.Fatal(err)
	}
	waitRun(t, h, d.ID)
	addWebhook(t, h, "fine", rcv.url("/ok"), "succeeded")
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &d); err != nil {
		t.Fatal(err)
	}
	waitRun(t, h, d.ID)
	rcv.await(t, "/ok", 1)
	// The delivery is logged by the service after the request; give it time to land before reading.
	for deadline := time.Now().Add(10 * time.Second); !strings.Contains(h.logs.String(), "notification delivered"); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("delivery not logged")
		}
	}
	if err := h.call(control.LogCommand, control.LogArgs{ID: d.ID}, &after); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(after.Text, "notification") {
		t.Fatalf("run log:\n%s", after.Text)
	}
}
