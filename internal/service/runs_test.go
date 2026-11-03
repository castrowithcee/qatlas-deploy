package service_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

type fakeDeployer struct {
	mu      sync.Mutex
	block   chan struct{}
	entered chan struct{}
}

// hold makes the next deployments wait until the returned function is called.
func (d *fakeDeployer) hold() (entered <-chan struct{}, release func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.block, d.entered = make(chan struct{}), make(chan struct{}, 4)
	return d.entered, func() { close(d.block) }
}

func (d *fakeDeployer) Deploy(ctx context.Context, st stacks.Stack, commit string, log *engine.Log) error {
	log.Step("deploy " + st.Name)
	d.mu.Lock()
	block, entered := d.block, d.entered
	d.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
		}
	}
	return nil
}

func bound(t *testing.T, dep engine.Deployer) *harness {
	t.Helper()
	return boundWith(t, dep, nil)
}

// boundWith is bound with extra files in the working copy and options for the service.
func boundWith(t *testing.T, dep engine.Deployer, files map[string]string, opts ...func(*service.Config)) *harness {
	t.Helper()
	h := startWith(t, dep, opts...)
	for k, v := range files {
		h.git.Files[k] = v
	}
	h.withKey()
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	if dep != nil {
		// Binding wakes the poll, which deploys the two readable stacks; the tests start after that.
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
			var st control.StatusResult
			if err := h.call(control.StatusCommand, nil, &st); err != nil {
				t.Fatal(err)
			}
			done := 0
			for _, s := range st.Engine.Stacks {
				if s.Deployed != "" && s.ActiveRun == "" {
					done++
				}
			}
			if done == 2 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("poll did not settle: %+v", st.Engine)
			}
		}
	}
	return h
}

// waitRun returns the recorded run with the id once it has a result.
func waitRun(t *testing.T, h *harness, id string) control.RunInfo {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		var res control.RunsResult
		if err := h.call(control.RunsCommand, control.RunsArgs{}, &res); err != nil {
			t.Fatal(err)
		}
		for _, r := range res.Runs {
			if r.ID == id && r.Result != "" {
				return r
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("runs = %+v", res)
		}
	}
}

func TestDeployRunsListsAndLogs(t *testing.T) {
	h := bound(t, &fakeDeployer{})
	var dep control.DeployResult
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &dep); err != nil {
		t.Fatal(err)
	}
	if dep.Stack != "plain-stack" || dep.Commit != h.git.Head || dep.ID == "" {
		t.Fatalf("deploy = %+v", dep)
	}
	if r := waitRun(t, h, dep.ID); r.Trigger != engine.TriggerManual || r.Result != engine.ResultSucceeded || r.Stack != "plain-stack" {
		t.Fatalf("run = %+v", r)
	}
	var all, one control.RunsResult
	if err := h.call(control.RunsCommand, control.RunsArgs{}, &all); err != nil || len(all.Runs) < 3 || all.Runs[0].ID != dep.ID {
		t.Fatalf("runs = %+v, %v", all, err)
	}
	if err := h.call(control.RunsCommand, control.RunsArgs{Stack: "demo-stack"}, &one); err != nil || len(one.Runs) != 1 {
		t.Fatalf("filtered runs = %+v, %v", one, err)
	}
	var log control.LogResult
	if err := h.call(control.LogCommand, control.LogArgs{ID: dep.ID}, &log); err != nil || !strings.Contains(log.Text, "deploy plain-stack") {
		t.Fatalf("log = %q, %v", log.Text, err)
	}
	var st control.StatusResult
	if err := h.call(control.StatusCommand, nil, &st); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range st.Engine.Stacks {
		if s.Name == "plain-stack" {
			found = s.Deployed == h.git.Head && s.LastRun == engine.ResultSucceeded
		}
	}
	if !found {
		t.Fatalf("status = %+v", st.Engine)
	}
}

func TestDeployRefusals(t *testing.T) {
	h := bound(t, &fakeDeployer{})
	err := h.call(control.DeployCommand, control.DeployArgs{Stack: "secret-name"}, nil)
	wantCode(t, err, control.CodeNoSuchStack)
	if strings.Contains(err.Error(), "secret-name") {
		t.Fatalf("the answer echoes the name: %v", err)
	}
	wantCode(t, h.call(control.DeployCommand, control.DeployArgs{Stack: "broken-stack"}, nil), control.CodeUnreadable)
	wantCode(t, h.call(control.DeployCommand, map[string]string{"x": "y"}, nil), control.CodeBadRequest)
}

func TestDeployWithoutDeployerOrRepo(t *testing.T) {
	h := bound(t, nil)
	wantCode(t, h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, nil), control.CodeNoDeployer)
	h = startWith(t, &fakeDeployer{})
	wantCode(t, h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, nil), control.CodeNoRepo)
	var st control.StatusResult
	if err := h.call(control.StatusCommand, nil, &st); err != nil || len(st.Engine.Stacks) != 0 {
		t.Fatalf("status = %+v, %v", st, err)
	}
}

func TestDeployWhileRunActive(t *testing.T) {
	d := &fakeDeployer{}
	h := bound(t, d)
	entered, release := d.hold()
	var dep control.DeployResult
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &dep); err != nil {
		t.Fatal(err)
	}
	<-entered
	wantCode(t, h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, nil), control.CodeRunActive)
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
	if active == "" {
		t.Fatalf("status = %+v", st.Engine)
	}
	release()
	waitRun(t, h, dep.ID)
}

func TestLogRefusesUnknownIDs(t *testing.T) {
	h := startWith(t, nil)
	for _, id := range []string{"", "../../etc/passwd", "20260101T000000Z-00000000"} {
		wantCode(t, h.call(control.LogCommand, control.LogArgs{ID: id}, nil), control.CodeNoSuchRun)
	}
}
