package compose

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

type fakeLog struct{ lines []string }

func (l *fakeLog) Step(name string) { l.lines = append(l.lines, "step "+name) }
func (l *fakeLog) Printf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}
func (l *fakeLog) text() string { return strings.Join(l.lines, "\n") }

// fakeRunner records commands and answers ps with the next scripted listing.
type fakeRunner struct {
	calls   []Command
	ps      []string // successive answers to ps; the last repeats
	failOn  string   // compose verb that fails with exit 1
	outputs map[string]string
}

func (r *fakeRunner) Run(_ context.Context, c Command) (Result, error) {
	r.calls = append(r.calls, c)
	verb := commandVerb(c.Args)
	if verb == "ps" {
		out := ""
		if len(r.ps) > 0 {
			out = r.ps[0]
			if len(r.ps) > 1 {
				r.ps = r.ps[1:]
			}
		}
		return Result{Output: []byte(out)}, nil
	}
	if verb == r.failOn {
		return Result{Output: []byte("boom")}, &ExitError{Code: 1}
	}
	return Result{Output: []byte(r.outputs[verb])}, nil
}

// commandVerb is the first word after the last -f pair of a compose command line.
func commandVerb(args []string) string {
	verb := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "-f" {
			i++
			verb = ""
			if i+1 < len(args) {
				verb = args[i+1]
			}
		}
	}
	return verb
}

type fakeClock struct {
	t      time.Time
	sleeps int
}

func (c *fakeClock) Now() time.Time { return c.t }
func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.sleeps++
	c.t = c.t.Add(d)
	return nil
}

func newStack(t *testing.T, withSecrets bool) stacks.Stack {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_TAG=1.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if withSecrets {
		if err := os.WriteFile(filepath.Join(dir, "secrets.env"), []byte("DEMO_PASSWORD=not-a-real-secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return stacks.Stack{Name: "demo-stack", Dir: dir, ComposeFiles: []string{"compose.yaml", "compose.override.yaml"}}
}

func newDeployer(r *fakeRunner) (*Deployer, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	x := &Executor{Runner: r, Env: []string{"PATH=/bin"}}
	return &Deployer{Exec: x, Checker: &Checker{Exec: x, Now: clk.Now, Sleep: clk.Sleep,
		Deadline: time.Minute, Interval: 5 * time.Second}}, clk
}

const runningJSON = `{"Name":"demo-stack-web-1","Service":"web","State":"running","Health":"healthy"}
{"Name":"demo-stack-db-1","Service":"db","State":"running","Health":""}`

func globalArgs(st stacks.Stack) []string {
	return []string{"compose", "--project-name", "demo-stack", "--project-directory", st.Dir,
		"--env-file", ".env", "--env-file", "secrets.env", "-f", "compose.yaml", "-f", "compose.override.yaml"}
}

func TestDeployCommandSequence(t *testing.T) {
	st := newStack(t, true)
	r := &fakeRunner{ps: []string{runningJSON}}
	d, _ := newDeployer(r)
	log := &fakeLog{}
	if err := d.deploy(context.Background(), st, log); err != nil {
		t.Fatalf("deploy: %v\n%s", err, log.text())
	}
	want := [][]string{
		append(globalArgs(st), "pull"),
		append(globalArgs(st), "up", "-d", "--remove-orphans"),
		append(globalArgs(st), "ps", "--all", "--format", "json"),
	}
	if len(r.calls) != len(want) {
		t.Fatalf("calls = %d, want %d", len(r.calls), len(want))
	}
	for i, c := range r.calls {
		if c.Name != "docker" || c.Dir != st.Dir || strings.Join(c.Args, "\x00") != strings.Join(want[i], "\x00") {
			t.Errorf("call %d = %s %q (dir %s)", i, c.Name, c.Args, c.Dir)
		}
		if strings.Join(c.Env, ",") != "PATH=/bin" {
			t.Errorf("call %d env = %q", i, c.Env)
		}
	}
	if strings.Contains(log.text(), "not-a-real-secret") {
		t.Error("secret value in log")
	}
}

func TestStopCommand(t *testing.T) {
	st := newStack(t, true)
	r := &fakeRunner{}
	d, _ := newDeployer(r)
	if err := d.Exec.Stop(context.Background(), st, &fakeLog{}); err != nil {
		t.Fatal(err)
	}
	if got := r.calls[0].Args; got[len(got)-1] != "stop" || len(got) != len(globalArgs(st))+1 {
		t.Errorf("args = %q", got)
	}
}

func TestMissingSecretsAbortsBeforeCompose(t *testing.T) {
	st := newStack(t, false)
	r := &fakeRunner{}
	d, _ := newDeployer(r)
	log := &fakeLog{}
	err := d.deploy(context.Background(), st, log)
	if err == nil || !strings.Contains(err.Error(), "secrets.env does not exist") || !strings.Contains(err.Error(), "missing-secrets-env") {
		t.Fatalf("err = %v", err)
	}
	if len(r.calls) != 0 {
		t.Errorf("compose was called: %v", r.calls)
	}
}

func TestMissingEnvFileAborts(t *testing.T) {
	st := newStack(t, true)
	_ = os.Remove(filepath.Join(st.Dir, ".env"))
	r := &fakeRunner{}
	d, _ := newDeployer(r)
	if err := d.deploy(context.Background(), st, &fakeLog{}); err == nil || len(r.calls) != 0 {
		t.Fatalf("err = %v calls = %d", err, len(r.calls))
	}
}

func TestSymlinkedSecretsRejected(t *testing.T) {
	st := newStack(t, false)
	if err := os.Symlink("/etc/hostname", filepath.Join(st.Dir, "secrets.env")); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{}
	d, _ := newDeployer(r)
	if err := d.deploy(context.Background(), st, &fakeLog{}); err == nil || len(r.calls) != 0 {
		t.Fatalf("err = %v calls = %d", err, len(r.calls))
	}
}

func TestUnsafeNamesRefused(t *testing.T) {
	for _, name := range []string{"Demo", "demo stack", "-demo", "demo;rm", "", "../demo", "demo.stack"} {
		st := newStack(t, true)
		st.Name = name
		r := &fakeRunner{}
		d, _ := newDeployer(r)
		if err := d.deploy(context.Background(), st, &fakeLog{}); err == nil || len(r.calls) != 0 {
			t.Errorf("name %q: err = %v calls = %d", name, err, len(r.calls))
		}
	}
	for _, f := range []string{"/etc/passwd", "../x.yaml", "-f", "a/../b.yaml", "", "a\\b"} {
		st := newStack(t, true)
		st.ComposeFiles = []string{f}
		r := &fakeRunner{}
		d, _ := newDeployer(r)
		if err := d.deploy(context.Background(), st, &fakeLog{}); err == nil || len(r.calls) != 0 {
			t.Errorf("file %q: err = %v calls = %d", f, err, len(r.calls))
		}
	}
}

func TestBrokenStackNotDeployed(t *testing.T) {
	st := newStack(t, true)
	st.Err = errors.New("bad rules")
	r := &fakeRunner{}
	d, _ := newDeployer(r)
	if err := d.deploy(context.Background(), st, &fakeLog{}); err == nil || len(r.calls) != 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestStepFailureStopsRun(t *testing.T) {
	for _, tc := range []struct {
		failOn string
		calls  int
	}{{"pull", 1}, {"up", 2}} {
		st := newStack(t, true)
		r := &fakeRunner{failOn: tc.failOn, ps: []string{runningJSON}}
		d, _ := newDeployer(r)
		log := &fakeLog{}
		err := d.deploy(context.Background(), st, log)
		var ee *ExitError
		if !errors.As(err, &ee) || len(r.calls) != tc.calls {
			t.Errorf("%s: err = %v calls = %d", tc.failOn, err, len(r.calls))
		}
		if !strings.Contains(log.text(), "boom") {
			t.Errorf("%s: output not logged", tc.failOn)
		}
	}
}

func TestContainerStates(t *testing.T) {
	web := func(state, health string) string {
		return fmt.Sprintf(`{"Service":"web","State":%q,"Health":%q,"ExitCode":3}`, state, health)
	}
	tests := []struct {
		name    string
		ps      []string
		wantErr string // empty: success
		sleeps  int
	}{
		{"running", []string{web("running", "")}, "", 0},
		{"healthy", []string{web("running", "healthy")}, "", 0},
		{"array form", []string{`[` + web("running", "healthy") + `]`}, "", 0},
		{"exited", []string{web("exited", "")}, "exited with code 3", 0},
		{"unhealthy", []string{web("running", "unhealthy")}, "unhealthy", 0},
		{"starting then healthy", []string{web("running", "starting"), web("running", "starting"), web("running", "healthy")}, "", 2},
		{"created then running", []string{web("created", ""), web("running", "")}, "", 1},
		{"stays starting", []string{web("running", "starting")}, "not ready in time: web=running/starting", 12},
		{"none", []string{""}, "no containers", 12},
		{"bad output", []string{"not json"}, "unexpected output", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newStack(t, true)
			r := &fakeRunner{ps: tc.ps}
			d, clk := newDeployer(r)
			err := d.Checker.Check(context.Background(), st, &fakeLog{})
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("err = %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			if clk.sleeps != tc.sleeps {
				t.Errorf("sleeps = %d, want %d", clk.sleeps, tc.sleeps)
			}
		})
	}
}

func TestContainerCheckCanBeDisabled(t *testing.T) {
	st := newStack(t, true)
	st.Rules = &stacks.Rules{Check: stacks.Check{Containers: false}}
	r := &fakeRunner{}
	d, _ := newDeployer(r)
	if err := d.Checker.Check(context.Background(), st, &fakeLog{}); err != nil || len(r.calls) != 0 {
		t.Fatalf("err = %v calls = %d", err, len(r.calls))
	}
}

func TestContextCancelStopsWaiting(t *testing.T) {
	st := newStack(t, true)
	r := &fakeRunner{ps: []string{`{"Service":"web","State":"running","Health":"starting"}`}}
	d, _ := newDeployer(r)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Checker.Check(ctx, st, &fakeLog{}); err == nil {
		t.Fatal("want error")
	}
}

func TestHTTPCheck(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(200)
		case "/flaky":
			if hits.Add(1) < 3 {
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(204)
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		default:
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()

	tests := []struct {
		name    string
		path    string
		status  int
		wantErr string
		sleeps  int
	}{
		{"ok", "/ok", 200, "", 0},
		{"retries until ok", "/flaky", 204, "", 2},
		{"wrong status", "/bad", 200, "status 500, want 200", 3},
		{"redirect is judged, not followed", "/redirect", 200, "status 302, want 200", 3},
		{"redirect expected", "/redirect", 302, "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hits.Store(0)
			st := newStack(t, true)
			st.Rules = &stacks.Rules{Check: stacks.Check{HTTP: &stacks.HTTPCheck{URL: srv.URL + tc.path, Status: tc.status, Timeout: 15 * time.Second}}}
			r := &fakeRunner{}
			d, clk := newDeployer(r)
			d.Checker.Interval = 5 * time.Second
			err := d.Checker.Check(context.Background(), st, &fakeLog{})
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("err = %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			if clk.sleeps != tc.sleeps {
				t.Errorf("sleeps = %d, want %d", clk.sleeps, tc.sleeps)
			}
		})
	}
}

func TestHTTPCheckRunsAfterContainers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	st := newStack(t, true)
	st.Rules = &stacks.Rules{Check: stacks.Check{Containers: true, HTTP: &stacks.HTTPCheck{URL: srv.URL, Status: 200, Timeout: time.Second}}}
	r := &fakeRunner{ps: []string{`{"Service":"web","State":"exited"}`}}
	d, _ := newDeployer(r)
	if err := d.Checker.Check(context.Background(), st, &fakeLog{}); err == nil {
		t.Fatal("exited container must fail before the probe")
	}
}

func TestParseProductionEnv(t *testing.T) {
	env := ProductionEnv(func(k string) string {
		return map[string]string{"HOME": "/home/demo", "DOCKER_HOST": "tcp://example.invalid:1", "DOCKER_CONFIG": ""}[k]
	})
	got := strings.Join(env, " ")
	if !strings.Contains(got, "HOME=/home/demo") || strings.Contains(got, "DOCKER_HOST") || strings.Contains(got, "DOCKER_CONFIG") {
		t.Errorf("env = %q", env)
	}
}
