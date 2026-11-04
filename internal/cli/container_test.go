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
	acts    []string
}

func (f *fakeEngine) List(context.Context) ([]docker.Container, error) { return f.list, f.err }

func (f *fakeEngine) Inspect(_ context.Context, ref string) (docker.Details, error) {
	if d, ok := f.details[ref]; ok {
		return d, nil
	}
	return docker.Details{}, &docker.Error{Code: output.CodeNotFound, Message: "no such container"}
}

func (f *fakeEngine) Start(_ context.Context, ref string) error { return f.act("start", ref) }
func (f *fakeEngine) Stop(_ context.Context, ref string, _ time.Duration) error {
	return f.act("stop", ref)
}
func (f *fakeEngine) Restart(_ context.Context, ref string, _ time.Duration) error {
	return f.act("restart", ref)
}

func (f *fakeEngine) act(verb, ref string) error {
	f.acts = append(f.acts, verb+" "+ref)
	return nil
}

func (f *fakeEngine) WaitHealthy(_ context.Context, refs []string, _ docker.WaitOptions) ([]docker.WaitResult, error) {
	return []docker.WaitResult{{Ref: refs[0], OK: true}}, nil
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
	detail := docker.Details{Container: lone, EnvNames: []string{"DEMO_TOKEN"},
		Ports: []docker.Port{{PrivatePort: 80, PublicPort: 8080, Protocol: "tcp", HostIP: "0.0.0.0"}}}
	return &fakeEngine{list: []docker.Container{web, lone}, details: map[string]docker.Details{"lonely": detail, "c2": detail}}
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

func TestContainerActionNeedsYesWithoutTerminal(t *testing.T) {
	f := sampleEngine()
	startContainers(t, f)
	code, stdout, stderr := run(t, "container", "stop", "lonely", "--agent")
	if code == 0 || stdout != "" || !strings.Contains(stderr, "--yes") || !strings.Contains(stderr, "confirmation-required") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if len(f.acts) != 0 {
		t.Fatalf("a refused action reached the engine: %v", f.acts)
	}
	for _, verb := range []string{"stop", "start", "restart"} {
		code, stdout, stderr = run(t, "container", verb, "lonely", "--yes", "--output", "json")
		var got map[string]any
		if err := json.Unmarshal([]byte(stdout), &got); code != 0 || err != nil || got["name"] != "lonely" || got["kind"] != "single" {
			t.Fatalf("%s: %d %q %q %v", verb, code, stdout, stderr, err)
		}
	}
	if strings.Join(f.acts, ",") != "stop c2,start c2,restart c2" {
		t.Fatalf("acts = %v", f.acts)
	}
	if code, _, stderr := run(t, "container", "stop", "nope", "--yes"); code == 0 || !strings.Contains(stderr, "no such container") {
		t.Fatalf("unknown: %d %q", code, stderr)
	}
}
