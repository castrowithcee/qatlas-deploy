package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/backup"
	"github.com/castrowithcee/qatlas-deploy/internal/service/servicetest"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const cliBackupID = "20260504T030201Z-0123abcd"

type recordingBackups struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (r *recordingBackups) rec(s string) {
	r.mu.Lock()
	r.calls = append(r.calls, s)
	r.mu.Unlock()
}

func (r *recordingBackups) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func (r *recordingBackups) Create(_ context.Context, st stacks.Stack, commit string, _ backup.Logger) (backup.Result, error) {
	r.rec("create " + st.Name)
	if r.err != nil {
		return backup.Result{}, r.err
	}
	return backup.Result{Meta: backup.Meta{ID: cliBackupID, Stack: st.Name, Commit: commit, Created: time.Date(2026, 5, 4, 3, 2, 1, 0, time.UTC),
		Items: []backup.Item{{Kind: backup.KindPath, Name: "data/db", Size: 7}}}, Removed: []string{"20250101T000000Z-aaaaaaaa"}}, nil
}

func (r *recordingBackups) List(stack string) ([]backup.Meta, error) {
	r.rec("list " + stack)
	return []backup.Meta{{ID: cliBackupID, Stack: stack, Created: time.Date(2026, 5, 4, 3, 2, 1, 0, time.UTC),
		Items: []backup.Item{{Kind: backup.KindPath, Name: "data/db", Size: 7}}}}, nil
}

func (r *recordingBackups) Restore(_ context.Context, st stacks.Stack, id string, _ backup.Logger) (backup.Meta, error) {
	r.rec("restore " + st.Name + " " + id)
	return backup.Meta{ID: id, Stack: st.Name}, r.err
}

func startBackupService(t *testing.T) *recordingBackups {
	t.Helper()
	rb := &recordingBackups{}
	old := testBackups
	testBackups = rb
	t.Cleanup(func() { testBackups = old })
	e := startRepoService(t)
	e.git.Files[servicetest.Settings("plain-stack")] = "version: 1\nbackup:\n  paths: [data/db]\n"
	if code, _, stderr := run(t, "repo", "set", e.srv.URL(), "--dir", e.dir, "--fingerprint", e.srv.Key().Fingerprint); code != 0 {
		t.Fatalf("repo set: %d %s", code, stderr)
	}
	return rb
}

func TestBackupCreateAndList(t *testing.T) {
	rb := startBackupService(t)
	code, stdout, stderr := run(t, "stack", "backup", "create", "plain-stack", "--output", "json")
	if code != 0 {
		t.Fatalf("create: %d %s", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil || got["backup"] != cliBackupID || got["stack"] != "plain-stack" ||
		got["size"] != float64(7) || got["items"] != float64(1) || got["removed"] != "20250101T000000Z-aaaaaaaa" {
		t.Fatalf("create = %q, %v", stdout, err)
	}
	code, stdout, stderr = run(t, "stack", "backup", "list", "plain-stack", "--output", "json")
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); code != 0 || err != nil || len(rows) != 1 || rows[0]["backup"] != cliBackupID || rows[0]["status"] != "complete" {
		t.Fatalf("list: %d %q %q %v", code, stdout, stderr, err)
	}
	if code, out, _ := run(t, "stack", "backup", "list", "plain-stack"); code != 0 || !strings.Contains(out, cliBackupID) {
		t.Fatalf("list table: %d %q", code, out)
	}
	if len(rb.got()) != 3 {
		t.Fatalf("calls = %v", rb.got())
	}
}

// --agent never asks a question, exactly like a run without a terminal, but does not depend on the streams of
// the test process.
func TestBackupRestoreWithoutTerminalNeedsYes(t *testing.T) {
	rb := startBackupService(t)
	code, stdout, stderr := run(t, "stack", "backup", "restore", "plain-stack", cliBackupID, "--agent")
	if code == 0 || stdout != "" || !strings.Contains(stderr, "--yes") || !strings.Contains(stderr, "confirmation-required") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if got := rb.got(); len(got) != 0 {
		t.Fatalf("a refused restore reached the service: %v", got)
	}
	code, stdout, stderr = run(t, "stack", "backup", "restore", "plain-stack", cliBackupID, "--yes", "--output", "json")
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); code != 0 || err != nil || got["restored"] != true || got["backup"] != cliBackupID {
		t.Fatalf("restore: %d %q %q %v", code, stdout, stderr, err)
	}
	if got := rb.got(); len(got) != 1 || got[0] != "restore plain-stack "+cliBackupID {
		t.Fatalf("calls = %v", got)
	}
}

func TestBackupRestoreRefusals(t *testing.T) {
	startBackupService(t)
	for _, id := range []string{"x", "../etc", "latest"} {
		if code, _, stderr := run(t, "stack", "backup", "restore", "plain-stack", id, "--yes"); code == 0 || !strings.Contains(stderr, "no such backup") {
			t.Fatalf("id %q: %d %q", id, code, stderr)
		}
	}
	if code, _, stderr := run(t, "stack", "backup", "restore", "plain-stack", "--yes"); code == 0 || !strings.Contains(stderr, "exactly two arguments") {
		t.Fatalf("missing id: %d %q", code, stderr)
	}
	if code, _, stderr := run(t, "stack", "backup", "restore", "plain-stack", cliBackupID, "--yes"); code != 0 {
		t.Fatalf("restore: %d %q", code, stderr)
	}
}

func TestBackupFailuresAreMapped(t *testing.T) {
	rb := startBackupService(t)
	if code, _, stderr := run(t, "stack", "backup", "create", "no-such-stack"); code == 0 || !strings.Contains(stderr, "no such stack") {
		t.Fatalf("unknown stack: %d %q", code, stderr)
	}
	if code, _, stderr := run(t, "stack", "backup", "create", "../x"); code == 0 || !strings.Contains(stderr, "not valid") {
		t.Fatalf("bad name: %d %q", code, stderr)
	}
	rb.err = backup.ErrNoRules
	if code, _, stderr := run(t, "stack", "backup", "create", "demo-stack"); code == 0 || !strings.Contains(stderr, "no backup section") || !strings.Contains(stderr, "stack settings set") {
		t.Fatalf("no rules: %d %q", code, stderr)
	}
	rb.err = errors.New("docker exploded")
	if code, _, stderr := run(t, "stack", "backup", "create", "plain-stack"); code == 0 || !strings.Contains(stderr, "docker exploded") {
		t.Fatalf("failed: %d %q", code, stderr)
	}
}

func TestBackupWithoutManagerOrService(t *testing.T) {
	e := startRepoService(t)
	if code, _, stderr := run(t, "stack", "backup", "list", "plain-stack"); code == 0 || !strings.Contains(stderr, "not available") {
		t.Fatalf("no manager: %d %q", code, stderr)
	}
	_ = e
}
