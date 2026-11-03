package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
)

const (
	demoHookKey   = "demo-signing-key-not-real"
	demoSMTPLogin = "demo-smtp-login-not-real"
)

// receiver answers every request with status and keeps the last one.
type receiver struct {
	*httptest.Server
	mu     sync.Mutex
	path   string
	header http.Header
	body   string
}

func newReceiver(t *testing.T, status int) *receiver {
	t.Helper()
	r := &receiver{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.path, r.header, r.body = req.URL.Path, req.Header.Clone(), string(b)
		r.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *receiver) last() (string, http.Header, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.path, r.header, r.body
}

func TestNotifyAddFromStdinNeverShowsSecrets(t *testing.T) {
	var log syncBuffer
	dir := startServiceLogged(t, allow, &log)
	hook := newReceiver(t, http.StatusNoContent)
	url := hook.URL + "/hook/demo-path-secret"

	code, stdout, stderr := runWithStdin(t, url+"\n"+demoHookKey+"\n", "notify", "add", "ops-hook", "--type", "webhook", "--events", "succeeded,rolled-back")
	if code != 0 || !strings.Contains(stdout, "ops-hook") {
		t.Fatalf("add: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	all := []string{stdout, stderr}
	code, stdout, stderr = run(t, "notify", "list")
	if code != 0 || !strings.Contains(stdout, "ops-hook") || !strings.Contains(stdout, "succeeded,rolled-back") {
		t.Fatalf("list: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	all = append(all, stdout, stderr)
	code, stdout, stderr = runWithStdin(t, "", "notify", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("list json: code %d, stderr %q", code, stderr)
	}
	all = append(all, stdout, stderr)
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil || len(rows) != 1 || rows[0]["type"] != "webhook" {
		t.Fatalf("list json rows %v, %v", rows, err)
	}
	code, stdout, stderr = run(t, "notify", "test", "ops-hook", "--output", "json")
	if code != 0 {
		t.Fatalf("test: code %d, stderr %q", code, stderr)
	}
	all = append(all, stdout, stderr, log.String())
	for _, text := range all {
		for _, secret := range []string{url, "demo-path-secret", demoHookKey} {
			if strings.Contains(text, secret) {
				t.Fatalf("a secret appears in output or log: %q", text)
			}
		}
	}
	path, header, body := hook.last()
	if path != "/hook/demo-path-secret" || header.Get("X-Qdeploy-Event") != "test" || header.Get("X-Qdeploy-Signature") == "" ||
		!strings.Contains(body, `"event":"test"`) {
		t.Fatalf("the receiver got path %q, header %v, body %q", path, header, body)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "credentials"))
	if len(entries) != 2 {
		t.Fatalf("want the URL and the key stored, got %v", entries)
	}
}

func TestNotifyAddTypesAndSettings(t *testing.T) {
	startService(t, allow)
	hook := newReceiver(t, http.StatusNoContent)
	cases := []struct {
		stdin string
		args  []string
		want  []string
	}{
		{hook.URL + "/slack/x\n", []string{"notify", "add", "chat", "--type", "slack"}, nil},
		{"123456:demo-bot-token\n", []string{"notify", "add", "bot", "--type", "telegram", "--chat-id", "-1001", "--base-url", hook.URL + "/telegram"},
			[]string{"chat_id=-1001", "base_url=" + hook.URL + "/telegram"}},
		{demoSMTPLogin + "\n", []string{"notify", "add", "mail", "--type", "smtp", "--host", "mail.example.test", "--from", "qdeploy@example.test",
			"--to", "ops@example.test,dev@example.test", "--user", "qdeploy"},
			[]string{"host=mail.example.test", "security=starttls", "user=qdeploy", "to=ops@example.test,dev@example.test"}},
	}
	for _, c := range cases {
		code, stdout, stderr := runWithStdin(t, c.stdin, c.args...)
		if code != 0 {
			t.Fatalf("%v: code %d, stderr %q", c.args, code, stderr)
		}
		for _, w := range c.want {
			if !strings.Contains(stdout, w) {
				t.Fatalf("%v: output %q lacks %q", c.args, stdout, w)
			}
		}
		if strings.Contains(stdout+stderr, strings.TrimSpace(c.stdin)) {
			t.Fatalf("%v: the secret is echoed: %q %q", c.args, stdout, stderr)
		}
	}
	// Telegram and Slack deliver to the local receiver; the SMTP channel needs a server, so it is not tested here.
	for _, name := range []string{"chat", "bot"} {
		if code, stdout, stderr := run(t, "notify", "test", name); code != 0 || !strings.Contains(stdout, "SENT") {
			t.Fatalf("test %s: code %d, stdout %q, stderr %q", name, code, stdout, stderr)
		}
	}
	// A SMTP channel without a user reads no secret from standard input.
	code, _, stderr := runWithStdin(t, "", "notify", "add", "relay", "--type", "smtp", "--host", "relay.example.test",
		"--from", "qdeploy@example.test", "--to", "ops@example.test")
	if code != 0 {
		t.Fatalf("smtp without user: code %d, stderr %q", code, stderr)
	}
}

func TestNotifyErrors(t *testing.T) {
	startService(t, allow)
	hook := newReceiver(t, http.StatusNoContent)
	if code, _, stderr := runWithStdin(t, hook.URL+"/a\n"+demoHookKey+"\n", "notify", "add", "one", "--type", "webhook"); code != 0 {
		t.Fatalf("add: code %d, stderr %q", code, stderr)
	}

	// channel-exists
	code, _, stderr := runWithStdin(t, hook.URL+"/a\n"+demoHookKey+"\n", "notify", "add", "one", "--type", "webhook")
	if code != 2 || !strings.Contains(stderr, "exists") {
		t.Fatalf("duplicate: code %d, stderr %q", code, stderr)
	}
	// no-such-channel
	for _, sub := range []string{"remove", "test"} {
		code, _, stderr = run(t, "notify", sub, "missing")
		if code == 0 || !strings.Contains(stderr, "no such channel") || !strings.Contains(stderr, "notify list") {
			t.Fatalf("%s missing: code %d, stderr %q", sub, code, stderr)
		}
	}
	notFound := code
	// invalid-argument: the service refuses the name, the events and a foreign setting
	for _, args := range [][]string{
		{"notify", "add", "Bad_Name", "--type", "slack"},
		{"notify", "add", "x", "--type", "slack", "--events", "exploded"},
		{"notify", "add", "x", "--type", "slack", "--host", "mail.example.test"},
		{"notify", "add", "x", "--type", "carrier-pigeon"},
	} {
		code, _, stderr := runWithStdin(t, hook.URL+"/b\n", args...)
		if code != 2 || stderr == "" {
			t.Fatalf("%v: code %d, stderr %q", args, code, stderr)
		}
	}
	// The shape of the input.
	for _, in := range []string{"", hook.URL + "/only-one-line\n", "\n" + demoHookKey + "\n", hook.URL + "/a\n" + demoHookKey + "\nextra\n"} {
		code, _, stderr := runWithStdin(t, in, "notify", "add", "shape", "--type", "webhook")
		if code != 2 || !strings.Contains(stderr, "standard input") && !strings.Contains(stderr, "empty") {
			t.Fatalf("input %q: code %d, stderr %q", in, code, stderr)
		}
	}
	code, _, stderr = runWithStdin(t, "", "notify", "add", "a", "b")
	if code != 2 || !strings.Contains(stderr, "exactly one argument") {
		t.Fatalf("two names: code %d, stderr %q", code, stderr)
	}

	// remove, then the name is free again
	if code, stdout, stderr := run(t, "notify", "remove", "one"); code != 0 || !strings.Contains(stdout, "REMOVED") {
		t.Fatalf("remove: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if code, stdout, _ := run(t, "notify", "list", "--output", "json"); code != 0 || strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("list after remove: code %d, stdout %q", code, stdout)
	}
	if notFound == 0 {
		t.Fatal("no-such-channel must fail")
	}
}

func TestNotifyTestFailureCarriesKitCode(t *testing.T) {
	startService(t, allow)
	denied := newReceiver(t, http.StatusUnauthorized)
	if code, _, stderr := runWithStdin(t, denied.URL+"/x\n", "notify", "add", "denied", "--type", "slack"); code != 0 {
		t.Fatalf("add: code %d, stderr %q", code, stderr)
	}
	code, _, stderr := run(t, "notify", "test", "denied")
	if code == 0 || !strings.Contains(stderr, "refused") {
		t.Fatalf("auth: code %d, stderr %q", code, stderr)
	}

	gone := newReceiver(t, http.StatusNoContent)
	url := gone.URL + "/x"
	gone.Close()
	if code, _, stderr := runWithStdin(t, url+"\n", "notify", "add", "gone", "--type", "slack"); code != 0 {
		t.Fatalf("add: code %d, stderr %q", code, stderr)
	}
	code, stdout, stderr := run(t, "notify", "test", "gone", "--output", "json")
	if code == 0 || strings.Contains(stdout+stderr, url) {
		t.Fatalf("unreachable: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "unreachable") {
		t.Fatalf("the error class is not named: %q", stderr)
	}
	if _, out, errOut := run(t, "notify", "test", "denied", "--output", "json"); !strings.Contains(out+errOut, "auth") {
		t.Fatalf("the auth class is not named: %q %q", out, errOut)
	}
}

func TestNotifyNeedsRunningService(t *testing.T) {
	useEndpoint(t, channeltest.Endpoint(t))
	code, _, stderr := run(t, "notify", "list")
	if code != ExitServiceNotRunning || !strings.Contains(stderr, "not running") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
}
