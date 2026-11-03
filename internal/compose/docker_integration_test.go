//go:build dockerintegration

package compose

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// These tests run Docker Compose for real. They pull an image and create containers, so they refuse to run
// anywhere but the simulated e2e host, which marks itself with QDEPLOY_E2E_HOST=1; QDEPLOY_E2E_IMAGE names the
// demo image, whose healthcheck fails while DEMO_HEALTH=fail.

type testLog struct{ t *testing.T }

func (l testLog) Step(name string)                  { l.t.Logf("step: %s", name) }
func (l testLog) Printf(format string, args ...any) { l.t.Logf(format, args...) }

// integrationStack writes a one-service stack into a temp dir and removes its containers when the test ends.
func integrationStack(t *testing.T, health string) (stacks.Stack, *Executor) {
	t.Helper()
	if os.Getenv("QDEPLOY_E2E_HOST") != "1" {
		t.Skip("runs only on the simulated e2e host (QDEPLOY_E2E_HOST=1)")
	}
	image := os.Getenv("QDEPLOY_E2E_IMAGE")
	if image == "" {
		t.Fatal("QDEPLOY_E2E_IMAGE is not set")
	}
	var rnd [4]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		t.Fatal(err)
	}
	name := "qdeploye2e-it-" + hex.EncodeToString(rnd[:])
	dir := t.TempDir()
	write := func(file, content string) {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("compose.yaml", "services:\n  app:\n    image: "+image+"\n    environment:\n      DEMO_HEALTH: ${DEMO_HEALTH}\n")
	write(".env", "DEMO_HEALTH="+health+"\n")
	st := stacks.Stack{Name: name, Dir: dir, ComposeFiles: []string{"compose.yaml"}}
	x := &Executor{Runner: &ExecRunner{}, Env: ProductionEnv(os.Getenv)}
	t.Cleanup(func() {
		down := exec.Command("docker", "compose", "--project-name", name, "down", "--volumes", "--remove-orphans")
		down.Dir = dir
		_ = down.Run()
	})
	return st, x
}

func TestDockerHealthyStackIsDeployedAndChecked(t *testing.T) {
	st, x := integrationStack(t, "ok")
	if err := os.WriteFile(filepath.Join(st.Dir, stacks.SecretsFile), []byte("DEMO_SECRET=not-a-real-secret-it\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := &Deployer{Exec: x, Checker: &Checker{Exec: x, Deadline: 90 * time.Second}}
	if err := d.deploy(context.Background(), st, testLog{t}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	cs, err := x.Ps(context.Background(), st, testLog{t})
	if err != nil || len(cs) != 1 || cs[0].State != "running" || cs[0].Health != "healthy" {
		t.Fatalf("containers = %v, %v", cs, err)
	}
}

func TestDockerUnhealthyStackFailsTheCheck(t *testing.T) {
	st, x := integrationStack(t, "fail")
	if err := os.WriteFile(filepath.Join(st.Dir, stacks.SecretsFile), []byte("DEMO_SECRET=not-a-real-secret-it\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := &Deployer{Exec: x, Checker: &Checker{Exec: x, Deadline: 90 * time.Second}}
	err := d.deploy(context.Background(), st, testLog{t})
	if err == nil || !strings.Contains(err.Error(), "unhealthy") {
		t.Fatalf("deploy error = %v, want unhealthy", err)
	}
}

func TestDockerMissingSecretsFileStopsBeforeCompose(t *testing.T) {
	st, x := integrationStack(t, "ok")
	d := &Deployer{Exec: x, Checker: &Checker{Exec: x}}
	err := d.deploy(context.Background(), st, testLog{t})
	if err == nil || !strings.Contains(err.Error(), stacks.SecretsFile) {
		t.Fatalf("deploy error = %v, want a missing %s", err, stacks.SecretsFile)
	}
	out, cerr := exec.Command("docker", "ps", "--all", "--quiet", "--filter", fmt.Sprintf("label=com.docker.compose.project=%s", st.Name)).Output()
	if cerr != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("containers exist for the project: %q, %v", out, cerr)
	}
}
