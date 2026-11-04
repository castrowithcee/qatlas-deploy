package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

func TestSecretLikeEnvValuesAreRedacted(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo-stack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:${DEMO_VERSION}\n",
		".env":         "DEMO_VERSION=1.2.3\nDEMO_PASSWORD=not-a-real-secret\napi_token=not-a-real-token\nDEMO_HOST=host-is-not-secret\n",
		"secrets.env":  "OTHER=not-a-real-extra\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	list, err := stacks.Load(root)
	if err != nil || len(list) != 1 || list[0].Err != nil {
		t.Fatalf("Load: %v %+v", err, list)
	}
	var r redact.Redactor
	registerStackSecrets(&r, list[0])
	got := r.Apply("a=not-a-real-secret b=not-a-real-token c=not-a-real-extra d=host-is-not-secret e=1.2.3")
	want := "a=[redacted] b=[redacted] c=[redacted] d=host-is-not-secret e=1.2.3"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
