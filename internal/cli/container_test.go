package cli

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/docker"
	"github.com/castrowithcee/qatlas-kit/output"
)

type fakeEngine struct {
	list    []docker.Container
	details map[string]docker.Details
	err     error
}

func (f *fakeEngine) List(context.Context) ([]docker.Container, error) { return f.list, f.err }

func (f *fakeEngine) Inspect(_ context.Context, ref string) (docker.Details, error) {
	if d, ok := f.details[ref]; ok {
		return d, nil
	}
	return docker.Details{}, &docker.Error{Code: output.CodeNotFound, Message: "no such container"}
}

func (f *fakeEngine) Stats(context.Context, string) (docker.Stats, error) {
	return docker.Stats{CPUPercent: 12.5, MemoryUsed: 3 << 20, MemoryLimit: 1 << 30}, nil
}

func startContainers(t *testing.T, f *fakeEngine) {
	t.Helper()
	old := testDocker
	testDocker = f
	t.Cleanup(func() { testDocker = old })
	startService(t, func(net.Conn) error { return nil })
}

func sampleEngine() *fakeEngine {
	started := time.Now().Add(-2 * time.Hour)
	web := docker.Container{ID: "c1", Name: "other-web-1", Image: "registry.example.test/demo/app:1", State: "running",
		StartedAt: started, Labels: map[string]string{
			"com.docker.compose.project": "other", "com.docker.compose.project.working_dir": "/srv/other",
			"com.docker.compose.project.config_files": "/srv/other/compose.yaml"}}
	lone := docker.Container{ID: "c2", Name: "lonely", Image: "registry.example.test/demo/app:2", State: "running", StartedAt: started}
	return &fakeEngine{list: []docker.Container{web, lone}, details: map[string]docker.Details{
		"lonely": {Container: lone, EnvNames: []string{"DEMO_TOKEN"},
			Ports: []docker.Port{{PrivatePort: 80, PublicPort: 8080, Protocol: "tcp", HostIP: "0.0.0.0"}}},
	}}
}

func TestContainerListTableAndJSON(t *testing.T) {
	startContainers(t, sampleEngine())
	code, out, stderr := run(t, "container", "list", "--metrics")
	if code != 0 {
		t.Fatalf("list: %d %s", code, stderr)
	}
	for _, want := range []string{"foreign", "other", "dir /srv/other", "files /srv/other/compose.yaml", "single", "lonely", "12.5%", "3.0M", "2h0m"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	code, out, stderr = run(t, "container", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("json: %d %s", code, stderr)
	}
	var res struct {
		Groups []struct {
			Kind, Name, WorkingDir string
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || len(res.Groups) != 2 || res.Groups[0].Kind != "foreign" || res.Groups[1].Kind != "single" {
		t.Fatalf("json = %q: %v", out, err)
	}
}

func TestContainerShowNeverShowsValues(t *testing.T) {
	startContainers(t, sampleEngine())
	for _, args := range [][]string{{"container", "show", "lonely"}, {"container", "show", "lonely", "--output", "json"}} {
		code, out, stderr := run(t, args...)
		if code != 0 || !strings.Contains(out, "DEMO_TOKEN") || !strings.Contains(out, "8080") {
			t.Fatalf("%v: %d %q %s", args, code, out, stderr)
		}
	}
	if code, _, stderr := run(t, "container", "show", "nope"); code == 0 || !strings.Contains(stderr, "no such container") || !strings.Contains(stderr, "not-found") {
		t.Fatalf("unknown: %d %q", code, stderr)
	}
}

func TestContainerEngineFailureIsNamed(t *testing.T) {
	f := sampleEngine()
	f.err = &docker.Error{Code: output.CodeUnreachable, Message: "no socket"}
	startContainers(t, f)
	if code, _, stderr := run(t, "container", "list"); code == 0 || !strings.Contains(stderr, "container engine is not reachable") {
		t.Fatalf("unreachable: %d %q", code, stderr)
	}
}

func TestContainersHelpTopic(t *testing.T) {
	code, out, _ := run(t, "help", "containers")
	if code != 0 || !strings.Contains(out, "--docker-socket") || !strings.Contains(out, "never their values") {
		t.Fatalf("help: %d %q", code, out)
	}
}
