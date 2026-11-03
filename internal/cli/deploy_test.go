package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

type okDeployer struct{}

func (okDeployer) Deploy(_ context.Context, st stacks.Stack, _ string, log *engine.Log) error {
	log.Step("deploy " + st.Name)
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
