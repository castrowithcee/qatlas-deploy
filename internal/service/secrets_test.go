package service_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const (
	fakeSecretValue  = "demo-value-not-real-1"
	otherSecretValue = "demo-other-not-real-2"
)

func secretsPath(h *harness, stack string) string { return filepath.Join(h.dir, stack, "secrets.env") }

func TestSecretSetListDeleteRoundTrip(t *testing.T) {
	h := bound(t, &fakeDeployer{})
	var set control.SecretResult
	args := control.SecretSetArgs{Stack: "plain-stack", Key: "DB_PASSWORD", Value: fakeSecretValue}
	if err := h.call(control.SecretSetCommand, args, &set); err != nil || set.Replaced || set.Key != "DB_PASSWORD" {
		t.Fatalf("set = %+v, %v", set, err)
	}
	info, err := os.Stat(secretsPath(h, "plain-stack"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("info %v, err %v", info, err)
	}
	args.Value = otherSecretValue
	if err := h.call(control.SecretSetCommand, args, &set); err != nil || !set.Replaced {
		t.Fatalf("set = %+v, %v", set, err)
	}
	if err := h.call(control.SecretSetCommand, control.SecretSetArgs{Stack: "plain-stack", Key: "API_KEY", Value: fakeSecretValue}, &set); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	var list control.SecretListResult
	if err := h.call(control.SecretListCommand, control.SecretListArgs{Stack: "plain-stack"}, &list); err != nil ||
		strings.Join(list.Keys, ",") != "API_KEY,DB_PASSWORD" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if err := h.call(control.SecretListCommand, control.SecretListArgs{Stack: "plain-stack"}, &raw); err != nil {
		t.Fatal(err)
	}
	if text := fmt.Sprint(raw); strings.Contains(text, fakeSecretValue) || strings.Contains(text, otherSecretValue) {
		t.Fatalf("list shows a value: %s", text)
	}
	if err := h.call(control.SecretDeleteCommand, control.SecretKeyArgs{Stack: "plain-stack", Key: "DB_PASSWORD"}, nil); err != nil {
		t.Fatal(err)
	}
	wantCode(t, h.call(control.SecretDeleteCommand, control.SecretKeyArgs{Stack: "plain-stack", Key: "DB_PASSWORD"}, nil), control.CodeNotFound)
	if err := h.call(control.SecretListCommand, control.SecretListArgs{Stack: "plain-stack"}, &list); err != nil || len(list.Keys) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	// Nothing was deployed by the change.
	if s := stackStatus(t, h, "plain-stack"); s.ActiveRun != "" {
		t.Fatalf("status = %+v", s)
	}
}

func TestSecretRefusals(t *testing.T) {
	h := bound(t, &fakeDeployer{})
	// DEMO_APP_VERSION stands in the stack's .env.
	wantCode(t, h.call(control.SecretSetCommand, control.SecretSetArgs{Stack: "plain-stack", Key: "DEMO_APP_VERSION", Value: fakeSecretValue}, nil), control.CodeKeyExists)
	wantCode(t, h.call(control.SecretSetCommand, control.SecretSetArgs{Stack: "plain-stack", Key: "bad key", Value: fakeSecretValue}, nil), control.CodeInvalid)
	wantCode(t, h.call(control.SecretSetCommand, control.SecretSetArgs{Stack: "plain-stack", Key: "OK_KEY", Value: "abc"}, nil), control.CodeInvalid)
	wantCode(t, h.call(control.SecretSetCommand, control.SecretSetArgs{Stack: "../x", Key: "OK_KEY", Value: fakeSecretValue}, nil), control.CodeInvalid)
	wantCode(t, h.call(control.SecretSetCommand, control.SecretSetArgs{Stack: "nope", Key: "OK_KEY", Value: fakeSecretValue}, nil), control.CodeNoSuchStack)
	wantCode(t, h.call(control.SecretListCommand, control.SecretListArgs{Stack: "nope"}, nil), control.CodeNoSuchStack)
	wantCode(t, h.call(control.SecretSetCommand, map[string]any{"stack": "plain-stack", "key": "K_EY", "value": fakeSecretValue, "x": 1}, nil), control.CodeBadRequest)
	if _, err := os.Lstat(secretsPath(h, "plain-stack")); !os.IsNotExist(err) {
		t.Fatalf("a refused call created the file: %v", err)
	}

	// A symlinked secrets.env is refused and left alone.
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("X_KEY=keep-me-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, secretsPath(h, "plain-stack")); err != nil {
		t.Fatal(err)
	}
	if err := h.call(control.SecretSetCommand, control.SecretSetArgs{Stack: "plain-stack", Key: "OK_KEY", Value: fakeSecretValue}, nil); err == nil {
		t.Fatal("a symlinked secrets.env was written")
	}
	if b, _ := os.ReadFile(target); string(b) != "X_KEY=keep-me-1\n" {
		t.Fatalf("target changed: %q", b)
	}
}

func TestSecretSetWaitsForNoRunAndRefusesWhileActive(t *testing.T) {
	d := &fakeDeployer{}
	h := bound(t, d)
	entered, release := d.hold()
	var dep control.DeployResult
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &dep); err != nil {
		t.Fatal(err)
	}
	<-entered
	wantCode(t, h.call(control.SecretSetCommand, control.SecretSetArgs{Stack: "plain-stack", Key: "OK_KEY", Value: fakeSecretValue}, nil), control.CodeRunActive)
	release()
	waitRun(t, h, dep.ID)
}

func TestSecretValueNeverReachesTheServiceLog(t *testing.T) {
	h := bound(t, &fakeDeployer{})
	_ = h.call(control.SecretSetCommand, control.SecretSetArgs{Stack: "plain-stack", Key: "OK_KEY", Value: fakeSecretValue}, nil)
	_ = h.call(control.SecretSetCommand, control.SecretSetArgs{Stack: "nope", Key: "OK_KEY", Value: otherSecretValue}, nil)
	if logs := h.logs.String(); strings.Contains(logs, fakeSecretValue) || strings.Contains(logs, otherSecretValue) {
		t.Fatalf("a value is in the service log:\n%s", logs)
	}
}

// printingDeployer prints the secrets of the stack in a step, as Compose output could.
type printingDeployer struct{ fakeDeployer }

func (d *printingDeployer) Pull(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	values, err := stacks.SecretValues(st.Dir)
	if err != nil {
		return err
	}
	for k, v := range values {
		log.Printf("compose printed %s=%s", k, v)
	}
	return d.fakeDeployer.Pull(ctx, st, log)
}

func TestRunLogRedactsTheSecretsOfTheStack(t *testing.T) {
	h := bound(t, &printingDeployer{})
	if err := h.call(control.SecretSetCommand, control.SecretSetArgs{Stack: "plain-stack", Key: "OK_KEY", Value: fakeSecretValue}, nil); err != nil {
		t.Fatal(err)
	}
	// A value written by hand is registered at the next run as well.
	if err := os.WriteFile(secretsPath(h, "plain-stack"),
		[]byte("OK_KEY='"+fakeSecretValue+"'\nHAND_KEY="+otherSecretValue+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var dep control.DeployResult
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &dep); err != nil {
		t.Fatal(err)
	}
	if r := waitRun(t, h, dep.ID); r.Result != engine.ResultSucceeded {
		t.Fatalf("run = %+v", r)
	}
	var log control.LogResult
	if err := h.call(control.LogCommand, control.LogArgs{ID: dep.ID}, &log); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log.Text, fakeSecretValue) || strings.Contains(log.Text, otherSecretValue) ||
		strings.Count(log.Text, "[redacted]") < 2 || !strings.Contains(log.Text, "compose printed OK_KEY=[redacted]") {
		t.Fatalf("run log:\n%s", log.Text)
	}
	if logs := h.logs.String(); strings.Contains(logs, fakeSecretValue) || strings.Contains(logs, otherSecretValue) {
		t.Fatalf("a value is in the service log:\n%s", logs)
	}
}
