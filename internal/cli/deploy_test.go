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

// settle waits until the poll that follows the binding has deployed both readable stacks.
func settle(t *testing.T) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		_, stdout, _ := run(t, "status", "--output", "json")
		if strings.Count(stdout, ".deployed\":\"") >= 2 && !strings.Contains(stdout, "active_run") {
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
	code, stdout, stderr := run(t, "deploy", "plain-stack", "--output", "json")
	if code != 0 {
		t.Fatalf("deploy: %d %s", code, stderr)
	}
	var dep map[string]any
	if err := json.Unmarshal([]byte(stdout), &dep); err != nil || dep["stack"] != "plain-stack" || dep["run"] == "" || dep["commit"] == "" {
		t.Fatalf("deploy = %q, %v", stdout, err)
	}
	id := dep["run"].(string)
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		_, out, _ := run(t, "runs", "plain-stack", "--output", "json")
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
	if code, out, _ := run(t, "runs"); code != 0 || !strings.Contains(out, id) {
		t.Fatalf("runs table: %d %q", code, out)
	}
	if code, out, _ := run(t, "log", id); code != 0 || !strings.Contains(out, "deploy plain-stack") {
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
		if code, _, stderr := run(t, "log", id); code == 0 || !strings.Contains(stderr, "no such run") || !strings.Contains(stderr, "not-found") {
			t.Fatalf("log %q: code %d, stderr %q", id, code, stderr)
		}
	}
}

func TestDeployRefusals(t *testing.T) {
	startBound(t, okDeployer{})
	code, _, stderr := run(t, "deploy", "no-such-secret")
	if code == 0 || !strings.Contains(stderr, "no such stack") || strings.Count(stderr, "no-such-secret") != 0 || !strings.Contains(stderr, "qdeploy stacks") {
		t.Fatalf("unknown: %d %q", code, stderr)
	}
	if code, _, stderr := run(t, "deploy", "broken-stack"); code == 0 || !strings.Contains(stderr, "qdeploy check") {
		t.Fatalf("unreadable: %d %q", code, stderr)
	}
}

func TestDeployWithoutDeployerOrRepo(t *testing.T) {
	startBound(t, nil)
	if code, _, stderr := run(t, "deploy", "plain-stack"); code == 0 || !strings.Contains(stderr, "not available") {
		t.Fatalf("no deployer: %d %q", code, stderr)
	}
	testDeployer = okDeployer{}
	startRepoService(t)
	if code, _, stderr := run(t, "deploy", "plain-stack"); code == 0 || !strings.Contains(stderr, "qdeploy repo set") {
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
	if code, _, stderr := run(t, "resume", "plain-stack"); code != 1 || !strings.Contains(stderr, "not paused") {
		t.Fatalf("resume of a stack that is not paused: %d %q", code, stderr)
	}
	d.fail.Store(true)
	code, stdout, stderr := run(t, "deploy", "plain-stack", "--output", "json")
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
	if code, _, stderr := run(t, "deploy", "plain-stack"); code != 1 || !strings.Contains(stderr, "paused") || !strings.Contains(stderr, "qdeploy resume") {
		t.Fatalf("deploy of a paused stack: %d %q", code, stderr)
	}
	if code, out, _ := run(t, "runs", "plain-stack"); code != 0 || !strings.Contains(out, engine.ResultRollbackFailed) {
		t.Fatalf("runs: %d %q", code, out)
	}
	code, out, stderr := run(t, "resume", "plain-stack", "--output", "json")
	var res map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res["stack"] != "plain-stack" || res["run"] != id ||
		!strings.Contains(res["reason"].(string), "demo check failed") {
		t.Fatalf("resume: %d %q %q", code, out, stderr)
	}
	if _, out, _ := run(t, "status", "--output", "json"); strings.Contains(out, "stack.plain-stack.paused") {
		t.Fatalf("status after resume = %s", out)
	}
	if code, _, _ := run(t, "resume", "plain-stack"); code != 1 {
		t.Fatalf("second resume: %d", code)
	}
}
