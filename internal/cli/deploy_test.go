package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

type okDeployer struct{}

func (okDeployer) Stop(context.Context, stacks.Stack, *engine.Log) error { return nil }
func (okDeployer) Backup(context.Context, stacks.Stack, string, *engine.Log) (string, error) {
	return "", engine.ErrNoBackup
}
func (okDeployer) Pull(_ context.Context, st stacks.Stack, log *engine.Log) error {
	log.Step("deploy " + st.Name)
	return nil
}
func (okDeployer) Up(context.Context, stacks.Stack, *engine.Log) error              { return nil }
func (okDeployer) Start(context.Context, stacks.Stack, *engine.Log) error           { return nil }
func (okDeployer) Restart(context.Context, stacks.Stack, *engine.Log) error         { return nil }
func (okDeployer) Check(context.Context, stacks.Stack, *engine.Log) error           { return nil }
func (okDeployer) Restore(context.Context, stacks.Stack, string, *engine.Log) error { return nil }

// checkDeployer fails every check while fail is set, also the check of a rollback.
type checkDeployer struct {
	okDeployer
	fail atomic.Bool
}

func (d *checkDeployer) Check(context.Context, stacks.Stack, *engine.Log) error {
	if d.fail.Load() {
		return errors.New("the demo check failed")
	}
	return nil
}

func startBound(t *testing.T, dep engine.Deployer) *repoEnv {
	t.Helper()
	old := testDeployer
	testDeployer = dep
	t.Cleanup(func() { testDeployer = old })
	e := startRepoService(t)
	if code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir, "--fingerprint", e.srv.Key().Fingerprint); code != 0 {
		t.Fatalf("repo set: %d %s", code, stderr)
	}
	return e
}

// settled reports whether both readable stacks show a deployed commit and no run is active.
// Status lists never-deployed stacks with an empty value, so presence of the key proves nothing.
func settled(stdout string) bool {
	var f map[string]any
	if json.Unmarshal([]byte(stdout), &f) != nil {
		return false
	}
	for _, name := range []string{"demo-stack", "plain-stack"} {
		if v, ok := f["stack."+name+".deployed"].(string); !ok || v == "" {
			return false
		}
	}
	for k := range f {
		if strings.HasSuffix(k, ".active_run") {
			return false
		}
	}
	return true
}

// settle waits until the poll that follows the binding has deployed both readable stacks.
func settle(t *testing.T) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		_, stdout, _ := run(t, "status", "--output", "json")
		if settled(stdout) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("poll did not settle: %s", stdout)
		}
	}
}

func TestDeployRunsLogAndStatus(t *testing.T) {
	startBound(t, okDeployer{})
	settle(t)
	code, stdout, stderr := run(t, "stack", "deploy", "plain-stack", "--output", "json")
	if code != 0 {
		t.Fatalf("deploy: %d %s", code, stderr)
	}
	var dep map[string]any
	if err := json.Unmarshal([]byte(stdout), &dep); err != nil || dep["stack"] != "plain-stack" || dep["run"] == "" || dep["commit"] == "" {
		t.Fatalf("deploy = %q, %v", stdout, err)
	}
	id := dep["run"].(string)
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		_, out, _ := run(t, "stack", "runs", "plain-stack", "--output", "json")
		var rows []map[string]any
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatalf("runs = %q: %v", out, err)
		}
		if len(rows) > 0 && rows[0]["run"] == id && rows[0]["result"] == engine.ResultSucceeded && rows[0]["trigger"] == "manual" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("runs = %q", out)
		}
	}
	if code, out, _ := run(t, "stack", "runs"); code != 0 || !strings.Contains(out, id) {
		t.Fatalf("runs table: %d %q", code, out)
	}
	if code, out, _ := run(t, "stack", "log", id); code != 0 || !strings.Contains(out, "deploy plain-stack") {
		t.Fatalf("log: %d %q", code, out)
	}
	code, out, _ := run(t, "status")
	if code != 0 || !strings.Contains(out, "STACK.PLAIN-STACK.DEPLOYED") || !strings.Contains(out, "succeeded") || !strings.Contains(out, "LAST_POLL") {
		t.Fatalf("status: %d %q", code, out)
	}
}

func TestLogMalformedAndUnknownID(t *testing.T) {
	startBound(t, okDeployer{})
	for _, id := range []string{"x", "../etc/passwd", "20260101T000000Z-00000000"} {
		if code, _, stderr := run(t, "stack", "log", id); code == 0 || !strings.Contains(stderr, "no such run") || !strings.Contains(stderr, "not-found") {
			t.Fatalf("log %q: code %d, stderr %q", id, code, stderr)
		}
	}
}

func TestDeployRefusals(t *testing.T) {
	startBound(t, okDeployer{})
	code, _, stderr := run(t, "stack", "deploy", "no-such-secret")
	if code == 0 || !strings.Contains(stderr, "no such stack") || strings.Count(stderr, "no-such-secret") != 0 || !strings.Contains(stderr, "qdeploy stack list") {
		t.Fatalf("unknown: %d %q", code, stderr)
	}
	if code, _, stderr := run(t, "stack", "deploy", "broken-stack"); code == 0 || !strings.Contains(stderr, "qdeploy stack check") {
		t.Fatalf("unreadable: %d %q", code, stderr)
	}
}

func TestDeployWithoutDeployerOrRepo(t *testing.T) {
	startBound(t, nil)
	if code, _, stderr := run(t, "stack", "deploy", "plain-stack"); code == 0 || !strings.Contains(stderr, "not available") {
		t.Fatalf("no deployer: %d %q", code, stderr)
	}
	testDeployer = okDeployer{}
	startRepoService(t)
	if code, _, stderr := run(t, "stack", "deploy", "plain-stack"); code == 0 || !strings.Contains(stderr, "qdeploy repo set") {
		t.Fatalf("no repo: %d %q", code, stderr)
	}
	code, out, _ := run(t, "status")
	if code != 0 || strings.Contains(out, "STACK.") {
		t.Fatalf("status without repo: %d %q", code, out)
	}
}

func TestPausedStackAndResume(t *testing.T) {
	d := &checkDeployer{}
	startBound(t, d)
	settle(t)
	if code, _, stderr := run(t, "stack", "resume", "plain-stack"); code != 1 || !strings.Contains(stderr, "not paused") {
		t.Fatalf("resume of a stack that is not paused: %d %q", code, stderr)
	}
	d.fail.Store(true)
	code, stdout, stderr := run(t, "stack", "deploy", "plain-stack", "--output", "json")
	if code != 0 {
		t.Fatalf("deploy: %d %s", code, stderr)
	}
	var dep map[string]any
	if err := json.Unmarshal([]byte(stdout), &dep); err != nil {
		t.Fatal(err)
	}
	id := dep["run"].(string)
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		_, out, _ := run(t, "status", "--output", "json")
		if strings.Contains(out, "stack.plain-stack.paused") && strings.Contains(out, engine.ResultRollbackFailed) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status = %s", out)
		}
	}
	if code, _, stderr := run(t, "stack", "deploy", "plain-stack"); code != 1 || !strings.Contains(stderr, "paused") || !strings.Contains(stderr, "qdeploy stack resume") {
		t.Fatalf("deploy of a paused stack: %d %q", code, stderr)
	}
	if code, out, _ := run(t, "stack", "runs", "plain-stack"); code != 0 || !strings.Contains(out, engine.ResultRollbackFailed) {
		t.Fatalf("runs: %d %q", code, out)
	}
	code, out, stderr := run(t, "stack", "resume", "plain-stack", "--output", "json")
	var res map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res["stack"] != "plain-stack" || res["run"] != id ||
		!strings.Contains(res["reason"].(string), "demo check failed") {
		t.Fatalf("resume: %d %q %q", code, out, stderr)
	}
	if _, out, _ := run(t, "status", "--output", "json"); strings.Contains(out, "stack.plain-stack.paused") {
		t.Fatalf("status after resume = %s", out)
	}
	if code, _, _ := run(t, "stack", "resume", "plain-stack"); code != 1 {
		t.Fatalf("second resume: %d", code)
	}
}

func TestPauseAndPinCommands(t *testing.T) {
	startBound(t, okDeployer{})
	settle(t)
	for _, tc := range []struct{ set, clear, key string }{
		{"pause", "unpause", "stack.plain-stack.operator_pause"}, {"pin", "unpin", "stack.plain-stack.pinned"},
	} {
		code, out, stderr := run(t, "stack", tc.set, "plain-stack", "--output", "json")
		var res map[string]any
		if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res["stack"] != "plain-stack" || res["active"] != true || res["changed"] != true {
			t.Fatalf("%s: %d %q %q", tc.set, code, out, stderr)
		}
		if code, out, _ := run(t, "stack", tc.set, "plain-stack", "--output", "json"); code != 0 || !strings.Contains(out, `"changed": false`) && !strings.Contains(out, `"changed":false`) {
			t.Fatalf("%s again: %d %q", tc.set, code, out)
		}
		if _, out, _ := run(t, "status", "--output", "json"); !strings.Contains(out, tc.key) || strings.Contains(out, "stack.plain-stack.paused") {
			t.Fatalf("status after %s = %s", tc.set, out)
		}
		if code, _, stderr := run(t, "stack", tc.set, "nope"); code == 0 || !strings.Contains(stderr, "qdeploy stack list") {
			t.Fatalf("%s unknown: %d %q", tc.set, code, stderr)
		}
		if code, out, _ := run(t, "stack", tc.clear, "plain-stack", "--output", "json"); code != 0 || !strings.Contains(out, `"active"`) {
			t.Fatalf("%s: %d %q", tc.clear, code, out)
		}
		if _, out, _ := run(t, "status", "--output", "json"); strings.Contains(out, tc.key) {
			t.Fatalf("status after %s = %s", tc.clear, out)
		}
		if code, _, _ := run(t, "stack", tc.set); code == 0 {
			t.Fatalf("%s without a stack succeeded", tc.set)
		}
	}
}

func TestStackActionCommands(t *testing.T) {
	startBound(t, okDeployer{})
	settle(t)
	if code, _, stderr := run(t, "stack", "stop", "plain-stack"); code == 0 || !strings.Contains(stderr, "--yes") {
		t.Fatalf("stop without a question answer: %d %q", code, stderr)
	}
	code, out, stderr := run(t, "stack", "stop", "plain-stack", "--yes", "--output", "json")
	if code != 0 || !strings.Contains(out, "plain-stack") || !strings.Contains(out, "stop") {
		t.Fatalf("stop: %d %q %q", code, out, stderr)
	}
	if _, out, _ := run(t, "status", "--output", "json"); !strings.Contains(out, "stack.plain-stack.stopped") {
		t.Fatalf("status after stop = %s", out)
	}
	if _, out, _ := run(t, "stack", "list"); !strings.Contains(out, "stopped since") {
		t.Fatalf("stack list after stop = %s", out)
	}
	if code, _, stderr := run(t, "stack", "restart", "plain-stack", "--yes"); code == 0 || !strings.Contains(stderr, "qdeploy stack start") {
		t.Fatalf("restart of a stopped stack: %d %q", code, stderr)
	}
	if code, _, stderr := run(t, "stack", "start", "plain-stack", "--yes"); code != 0 {
		t.Fatalf("start: %d %q", code, stderr)
	}
	if _, out, _ := run(t, "status", "--output", "json"); strings.Contains(out, "stack.plain-stack.stopped") {
		t.Fatalf("status after start = %s", out)
	}
	if code, _, stderr := run(t, "stack", "restart", "plain-stack", "--yes"); code != 0 {
		t.Fatalf("restart: %d %q", code, stderr)
	}
	if code, _, stderr := run(t, "stack", "stop", "nope", "--yes"); code == 0 || !strings.Contains(stderr, "qdeploy stack list") {
		t.Fatalf("unknown: %d %q", code, stderr)
	}
}

func TestUpdateOfAPinnedStackSaysToUnpin(t *testing.T) {
	startUpdate(t, okDeployer{})
	if code, _, stderr := run(t, "stack", "pin", "demo-stack"); code != 0 {
		t.Fatalf("pin: %d %q", code, stderr)
	}
	if code, _, stderr := run(t, "stack", "update", "demo-stack"); code != 1 || !strings.Contains(stderr, "pinned") || !strings.Contains(stderr, "qdeploy stack unpin") {
		t.Fatalf("update of a pinned stack: %d %q", code, stderr)
	}
}

func TestSettled(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		want     bool
	}{
		{"deployed", `{"stack.demo-stack.deployed":"abc123","stack.plain-stack.deployed":"abc123"}`, true},
		{"empty value", `{"stack.demo-stack.deployed":"","stack.plain-stack.deployed":"abc123"}`, false},
		{"missing stack", `{"stack.demo-stack.deployed":"abc123"}`, false},
		{"active run", `{"stack.demo-stack.deployed":"abc123","stack.plain-stack.deployed":"abc123","stack.plain-stack.active_run":"r1"}`, false},
		{"invalid json", `not json`, false},
	} {
		if got := settled(tc.in); got != tc.want {
			t.Errorf("%s: settled = %v, want %v", tc.name, got, tc.want)
		}
	}
}
