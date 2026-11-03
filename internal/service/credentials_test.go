package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
)

const demoToken = "demo-token-not-real"

func TestRedactHandlerCoversMessageAndAttributes(t *testing.T) {
	var secrets redact.Redactor
	secrets.Add(demoToken)
	var buf bytes.Buffer
	log := slog.New(redactHandler{next: slog.NewTextHandler(&buf, nil), r: &secrets}).With("pre", demoToken)
	log.WithGroup("g").Error("failed "+demoToken, "text", demoToken, "err", errors.New("boom "+demoToken),
		slog.Group("inner", "v", demoToken), "bytes", []byte(demoToken), "n", 3)
	out := buf.String()
	if strings.Contains(out, demoToken) || !strings.Contains(out, "[redacted]") || !strings.Contains(out, "n=3") {
		t.Fatalf("log = %q", out)
	}
}

func TestStoredSecretsAreRedactedAfterRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	first, err := credentials.Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Set("registry", demoToken); err != nil {
		t.Fatal(err)
	}
	var secrets redact.Redactor
	if _, err := credentials.Open(dir, secrets.Add); err != nil {
		t.Fatal(err)
	}
	if got := secrets.Apply("login with " + demoToken); strings.Contains(got, demoToken) {
		t.Fatalf("not redacted: %q", got)
	}
}

func TestRunRefusesAnUnsafeStateDirectory(t *testing.T) {
	cfg := Config{Endpoint: channeltest.Endpoint(t), StateDir: "relative"}
	if err := Run(context.Background(), quiet, cfg); err == nil {
		t.Fatal("Run accepted a relative state directory")
	}
	if err := Run(context.Background(), quiet, Config{Endpoint: cfg.Endpoint}); err == nil {
		t.Fatal("Run accepted an empty state directory")
	}
}

func TestCredentialAnswersCarryFixedTexts(t *testing.T) {
	ep := runService(t, Config{PeerCheck: allowAll})
	cl, err := client.Dial(context.Background(), ep)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	call := func(command string, args any) *control.Error {
		t.Helper()
		var ce *control.Error
		if err := cl.Call(context.Background(), command, args, nil); !errors.As(err, &ce) {
			t.Fatalf("%s: err = %v", command, err)
		}
		return ce
	}
	if ce := call(control.CredentialSetCommand, map[string]any{"name": "ok", "value": "x", "extra": demoToken}); ce.Code != control.CodeBadRequest ||
		strings.Contains(ce.Message, demoToken) {
		t.Fatalf("unknown field: %v", ce)
	}
	if ce := call(control.CredentialSetCommand, control.CredentialSetArgs{Name: "../x", Value: demoToken}); ce.Code != control.CodeInvalid ||
		strings.Contains(ce.Message, demoToken) || strings.Contains(ce.Message, "..") {
		t.Fatalf("bad name: %v", ce)
	}
	if ce := call(control.KeyShowCommand, nil); ce.Code != control.CodeNotFound {
		t.Fatalf("show: %v", ce)
	}
	if err := cl.Call(context.Background(), control.KeyGenerateCommand, control.KeyGenerateArgs{}, nil); err != nil {
		t.Fatal(err)
	}
	if ce := call(control.KeyGenerateCommand, control.KeyGenerateArgs{}); ce.Code != control.CodeKeyExists {
		t.Fatalf("second generate: %v", ce)
	}
	if ce := call(control.CredentialDeleteCommand, control.CredentialNameArgs{Name: "missing"}); ce.Code != control.CodeNotFound {
		t.Fatalf("delete: %v", ce)
	}
}
