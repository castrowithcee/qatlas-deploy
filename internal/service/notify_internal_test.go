package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/notify"
	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
)

const noticeToken = "notice-token-not-real"

type noticeEnv struct {
	dir    string
	srv    *httptest.Server
	opts   notify.Options
	logs   *bytes.Buffer
	mu     sync.Mutex
	bodies []string
	status int
}

func newNoticeEnv(t *testing.T) *noticeEnv {
	t.Helper()
	e := &noticeEnv{dir: filepath.Join(t.TempDir(), "state"), logs: &bytes.Buffer{}, status: http.StatusOK}
	e.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		e.mu.Lock()
		e.bodies = append(e.bodies, string(b))
		status := e.status
		e.mu.Unlock()
		w.WriteHeader(status)
		if status != http.StatusOK {
			_, _ = io.WriteString(w, "rejected "+noticeToken)
		}
	}))
	t.Cleanup(e.srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(e.srv.Certificate())
	e.opts = notify.Options{TLS: &tls.Config{RootCAs: pool}, Backoff: time.Millisecond, Interval: time.Nanosecond}
	return e
}

// start opens a notification service over the state directory, as a started service does.
func (e *noticeEnv) start(t *testing.T) *notifyService {
	t.Helper()
	var secrets redact.Redactor
	store, err := credentials.Open(e.dir, secrets.Add)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(redactHandler{next: slog.NewTextHandler(&lockedWriter{mu: &e.mu, w: e.logs}, nil), r: &secrets})
	n, err := newNotifyService(e.dir, store, e.opts, &secrets, "test", log)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (e *noticeEnv) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.bodies)
}

func (e *noticeEnv) await(t *testing.T, n int) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if e.count() >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("saw %d of %d requests", e.count(), n)
		}
	}
}

func (e *noticeEnv) add(t *testing.T, n *notifyService, name string, events ...string) {
	t.Helper()
	raw, _ := json.Marshal(control.NotifyAddArgs{
		Name: name, Type: control.NotifyWebhook, Events: events, Secret: e.srv.URL + "/hook/" + noticeToken, Key: "signing-key-not-real",
	})
	if _, err := n.handleAdd(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
}

func notice(version string) UpdateNotice {
	return UpdateNotice{Stack: "web", Items: []engine.UpdateItem{
		{Variable: "WEB_VERSION", Current: "1.0.0", Selected: version, Status: engine.UpdateAvailable},
		{Variable: "DB_VERSION", Current: "2.0.0", Selected: "2.0.0", Status: engine.UpdateCurrent},
	}}
}

func TestUpdateNoticeIsSentOncePerVersionAcrossARestart(t *testing.T) {
	e := newNoticeEnv(t)
	first := e.start(t)
	e.add(t, first, "ops", "update-available")

	first.updateAvailable(notice("1.1.0"))
	e.await(t, 1)
	first.updateAvailable(notice("1.1.0"))
	first.close()

	second := e.start(t)
	defer second.close()
	second.updateAvailable(notice("1.1.0"))
	second.updateAvailable(notice("1.2.0"))
	second.close()
	e.await(t, 2)
	if got := e.count(); got != 2 {
		t.Fatalf("receiver saw %d requests, want 2", got)
	}
	if !strings.Contains(e.bodies[0], `"available":"1.1.0"`) || !strings.Contains(e.bodies[1], `"available":"1.2.0"`) ||
		strings.Contains(e.bodies[0], "DB_VERSION") {
		t.Fatalf("bodies = %q", e.bodies)
	}
}

func TestUpdateNoticeWaitsForAChannelThatSelectsIt(t *testing.T) {
	e := newNoticeEnv(t)
	n := e.start(t)
	defer n.close()
	e.add(t, n, "ops", "failed")
	n.updateAvailable(notice("1.1.0"))
	e.add(t, n, "updates", "update-available")
	n.updateAvailable(notice("1.1.0"))
	e.await(t, 1)
	n.close()
	if got := e.count(); got != 1 {
		t.Fatalf("receiver saw %d requests, want 1", got)
	}
}

func TestFailedDeliveryNeverLogsTheCredential(t *testing.T) {
	e := newNoticeEnv(t)
	e.status = http.StatusInternalServerError
	n := e.start(t)
	e.add(t, n, "ops")
	n.runEvent(engine.Event{Kind: engine.ResultFailed, Stack: "web", Run: "r1", Commit: "0123456789abcdef", Error: "check failed"})
	e.await(t, 3)
	n.close()
	e.mu.Lock()
	out := e.logs.String()
	e.mu.Unlock()
	if !strings.Contains(out, "notification not delivered") || strings.Contains(out, noticeToken) || strings.Contains(out, "signing-key-not-real") {
		t.Fatalf("log = %s", out)
	}
}
