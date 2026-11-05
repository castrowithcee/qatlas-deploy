package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/backup"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
	"github.com/castrowithcee/qatlas-deploy/internal/service/servicetest"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const backupID = "20260504T030201Z-0123abcd"

type fakeBackups struct {
	mu      sync.Mutex
	calls   []string
	err     error
	block   chan struct{}
	entered chan struct{}
}

func (f *fakeBackups) rec(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

func (f *fakeBackups) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeBackups) wait(ctx context.Context) {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
		}
	}
}

func (f *fakeBackups) Create(ctx context.Context, st stacks.Stack, commit string, _ backup.Logger) (backup.Result, error) {
	f.rec("create " + st.Name + " " + commit)
	f.wait(ctx)
	if f.err != nil {
		return backup.Result{}, f.err
	}
	return backup.Result{
		Meta:    backup.Meta{ID: backupID, Stack: st.Name, Created: time.Date(2026, 5, 4, 3, 2, 1, 0, time.UTC), Commit: commit, Items: []backup.Item{{Kind: backup.KindPath, Name: "data/db", Size: 42}}},
		Removed: []string{"20260101T000000Z-00000000"},
	}, nil
}

func (f *fakeBackups) List(stack string) ([]backup.Meta, error) {
	f.rec("list " + stack)
	return []backup.Meta{{ID: backupID, Stack: stack, Items: []backup.Item{{Kind: backup.KindPath, Name: "data/db", Size: 42}}}, {ID: "20250101T000000Z-aaaaaaaa", Stack: stack, Incomplete: true}}, f.err
}

func (f *fakeBackups) Restore(ctx context.Context, st stacks.Stack, id string, _ backup.Logger) (backup.Meta, error) {
	f.rec("restore " + st.Name + " " + id)
	f.wait(ctx)
	return backup.Meta{ID: id, Stack: st.Name}, f.err
}

const backupRules = "version: 1\nbackup:\n  paths: [data/db]\n"

func withBackups(f *fakeBackups) func(*service.Config) {
	return func(c *service.Config) { c.Backups = f }
}

func backupHarness(t *testing.T, f *fakeBackups) *harness {
	t.Helper()
	return boundWith(t, &fakeDeployer{}, map[string]string{servicetest.Settings("plain-stack"): backupRules}, withBackups(f))
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ce *control.Error
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a channel error", err)
	}
	return ce.Code
}

func TestBackupCreateAndListThroughTheChannel(t *testing.T) {
	f := &fakeBackups{}
	h := backupHarness(t, f)
	var res control.BackupCreateResult
	if err := h.call(control.BackupCreateCommand, control.BackupArgs{Stack: "plain-stack"}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Backup.ID != backupID || res.Backup.Commit != h.git.Head || res.Backup.Size != 42 || len(res.Removed) != 1 {
		t.Fatalf("res = %+v", res)
	}
	if got := f.got(); len(got) != 1 || got[0] != "create plain-stack "+h.git.Head {
		t.Fatalf("calls = %v", got)
	}
	var list control.BackupListResult
	if err := h.call(control.BackupListCommand, control.BackupArgs{Stack: "plain-stack"}, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Backups) != 2 || list.Backups[0].ID != backupID || !list.Backups[1].Incomplete {
		t.Fatalf("list = %+v", list)
	}
}

func TestBackupRestoreNeedsConfirmation(t *testing.T) {
	f := &fakeBackups{}
	h := backupHarness(t, f)
	for _, a := range []control.BackupRestoreArgs{
		{Stack: "plain-stack", ID: backupID},
		{Stack: "plain-stack", ID: backupID, Confirm: false},
		{Stack: "nope", ID: "x"},
	} {
		if err := h.call(control.BackupRestoreCommand, a, nil); codeOf(t, err) != control.CodeConfirmation {
			t.Fatalf("args %+v: err = %v", a, err)
		}
	}
	// A request that does not name the field at all is a bare call as well.
	var raw map[string]any
	if err := h.call(control.BackupRestoreCommand, map[string]any{"stack": "plain-stack", "id": backupID}, &raw); codeOf(t, err) != control.CodeConfirmation {
		t.Fatalf("err = %v", err)
	}
	if got := f.got(); len(got) != 0 {
		t.Fatalf("a refused restore reached the manager: %v", got)
	}
	var res control.BackupRestoreResult
	args := control.BackupRestoreArgs{Stack: "plain-stack", ID: backupID, Confirm: true}
	if err := h.call(control.BackupRestoreCommand, args, &res); err != nil || res.Backup.ID != backupID {
		t.Fatalf("res %+v, err %v", res, err)
	}
	if got := f.got(); len(got) != 1 || got[0] != "restore plain-stack "+backupID {
		t.Fatalf("calls = %v", got)
	}
}

func TestBackupRefusals(t *testing.T) {
	f := &fakeBackups{}
	h := backupHarness(t, f)
	check := func(command string, args any, want string) {
		t.Helper()
		if err := h.call(command, args, nil); codeOf(t, err) != want {
			t.Fatalf("%s %+v: err = %v, want %s", command, args, err, want)
		}
	}
	check(control.BackupCreateCommand, control.BackupArgs{Stack: "../etc"}, control.CodeInvalid)
	check(control.BackupCreateCommand, control.BackupArgs{Stack: "Upper"}, control.CodeInvalid)
	check(control.BackupListCommand, control.BackupArgs{Stack: "a/b"}, control.CodeInvalid)
	check(control.BackupCreateCommand, control.BackupArgs{Stack: "nope"}, control.CodeNoSuchStack)
	check(control.BackupCreateCommand, control.BackupArgs{Stack: "broken-stack"}, control.CodeUnreadable)
	check(control.BackupRestoreCommand, control.BackupRestoreArgs{Stack: "plain-stack", ID: "../x", Confirm: true}, control.CodeNoSuchBackup)
	check(control.BackupCreateCommand, map[string]any{"stack": "plain-stack", "extra": 1}, control.CodeBadRequest)
	if got := f.got(); len(got) != 0 {
		t.Fatalf("calls = %v", got)
	}

	f.err = backup.ErrNoRules
	check(control.BackupCreateCommand, control.BackupArgs{Stack: "plain-stack"}, control.CodeNoBackupRules)
	f.err = backup.ErrNoSuchBackup
	check(control.BackupRestoreCommand, control.BackupRestoreArgs{Stack: "plain-stack", ID: backupID, Confirm: true}, control.CodeNoSuchBackup)
	f.err = errors.New("docker: boom\x00 at /var/lib/x")
	err := h.call(control.BackupCreateCommand, control.BackupArgs{Stack: "plain-stack"}, nil)
	if codeOf(t, err) != control.CodeBackupFailed || strings.Contains(err.Error(), "\x00") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestBackupWithoutManager(t *testing.T) {
	h := bound(t, &fakeDeployer{})
	for command, args := range map[string]any{
		control.BackupCreateCommand:  control.BackupArgs{Stack: "plain-stack"},
		control.BackupListCommand:    control.BackupArgs{Stack: "plain-stack"},
		control.BackupRestoreCommand: control.BackupRestoreArgs{Stack: "plain-stack", ID: backupID, Confirm: true},
	} {
		if err := h.call(command, args, nil); codeOf(t, err) != control.CodeNoBackups {
			t.Fatalf("%s: err = %v", command, err)
		}
	}
}

func TestBackupExcludesRunsOfTheSameStack(t *testing.T) {
	f := &fakeBackups{block: make(chan struct{}), entered: make(chan struct{}, 2)}
	h := backupHarness(t, f)
	done := make(chan error, 1)
	go func() { done <- h.call(control.BackupCreateCommand, control.BackupArgs{Stack: "plain-stack"}, nil) }()
	<-f.entered
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, nil); codeOf(t, err) != control.CodeRunActive {
		t.Fatalf("deploy during backup: %v", err)
	}
	if err := h.call(control.BackupCreateCommand, control.BackupArgs{Stack: "plain-stack"}, nil); codeOf(t, err) != control.CodeRunActive {
		t.Fatalf("second backup: %v", err)
	}
	args := control.BackupRestoreArgs{Stack: "plain-stack", ID: backupID, Confirm: true}
	if err := h.call(control.BackupRestoreCommand, args, nil); codeOf(t, err) != control.CodeRunActive {
		t.Fatalf("restore during backup: %v", err)
	}
	// Another stack is not blocked.
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "demo-stack"}, nil); err != nil {
		t.Fatalf("deploy of another stack: %v", err)
	}
	close(f.block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := f.got(); len(got) != 1 {
		t.Fatalf("calls = %v", got)
	}
}

func TestDeployRefusesWhileTheBackupRuns(t *testing.T) {
	f := &fakeBackups{block: make(chan struct{}), entered: make(chan struct{}, 1)}
	h := backupHarness(t, f)
	done := make(chan error, 1)
	go func() {
		done <- h.call(control.BackupRestoreCommand, control.BackupRestoreArgs{Stack: "plain-stack", ID: backupID, Confirm: true}, nil)
	}()
	<-f.entered
	var st control.StatusResult
	if err := h.call(control.StatusCommand, nil, &st); err != nil {
		t.Fatal(err)
	}
	var active string
	for _, s := range st.Engine.Stacks {
		if s.Name == "plain-stack" {
			active = s.ActiveRun
		}
	}
	if active != "restore" {
		t.Fatalf("active run = %q", active)
	}
	close(f.block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
