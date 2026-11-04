package service_test

import (
	"context"
	"errors"
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
	mu        sync.Mutex
	block     chan struct{}
	entered   chan struct{}
	failCheck bool
	calls     []string
}

// hold makes the next deployments wait until the returned function is called.
func (d *fakeDeployer) hold() (entered <-chan struct{}, release func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.block, d.entered = make(chan struct{}), make(chan struct{}, 4)
	return d.entered, func() { close(d.block) }
}

// setFailCheck makes every check fail, also the one of a rollback.
func (d *fakeDeployer) setFailCheck(fail bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.failCheck = fail
}

func (d *fakeDeployer) Stop(_ context.Context, st stacks.Stack, _ *engine.Log) error {
	d.record("stop " + st.Name)
	return nil
}

func (d *fakeDeployer) Backup(context.Context, stacks.Stack, string, *engine.Log) (string, error) {
	return "", engine.ErrNoBackup
}

func (d *fakeDeployer) Pull(ctx context.Context, st stacks.Stack, log *engine.Log) error {
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

func (d *fakeDeployer) Up(context.Context, stacks.Stack, *engine.Log) error { return nil }

func (d *fakeDeployer) Start(_ context.Context, st stacks.Stack, _ *engine.Log) error {
	d.record("start " + st.Name)
	return nil
}

func (d *fakeDeployer) Restart(_ context.Context, st stacks.Stack, _ *engine.Log) error {
	d.record("restart " + st.Name)
	return nil
}

func (d *fakeDeployer) callsOf(stack string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for _, c := range d.calls {
		if strings.HasSuffix(c, " "+stack) {
			out = append(out, c)
		}
	}
	return out
}

func (d *fakeDeployer) record(call string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, call)
}

func (d *fakeDeployer) Check(context.Context, stacks.Stack, *engine.Log) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failCheck {
		return errors.New("the demo check failed")
	}
	return nil
}

func (d *fakeDeployer) Restore(context.Context, stacks.Stack, string, *engine.Log) error { return nil }

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

func stackStatus(t *testing.T, h *harness, name string) control.StackStatus {
	t.Helper()
	var st control.StatusResult
	if err := h.call(control.StatusCommand, nil, &st); err != nil {
		t.Fatal(err)
	}
	for _, s := range st.Engine.Stacks {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no stack %s in %+v", name, st.Engine)
	return control.StackStatus{}
}

func TestFailedRollbackPausesUntilResumed(t *testing.T) {
	d := &fakeDeployer{}
	h := bound(t, d)
	wantCode(t, h.call(control.ResumeCommand, control.ResumeArgs{Stack: "plain-stack"}, nil), control.CodeNotPaused)

	// the check of the new and of the previous state fail: the rollback fails and pauses the stack
	d.setFailCheck(true)
	var dep control.DeployResult
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &dep); err != nil {
		t.Fatal(err)
	}
	if r := waitRun(t, h, dep.ID); r.Result != engine.ResultRollbackFailed || !strings.Contains(r.Error, "demo check failed") {
		t.Fatalf("run = %+v", r)
	}
	s := stackStatus(t, h, "plain-stack")
	if s.Paused == nil || s.Paused.Run != dep.ID || !strings.Contains(s.Paused.Reason, "demo check failed") ||
		s.LastRun != engine.ResultRollbackFailed || len(s.Findings) == 0 || s.Findings[0].Kind != engine.FindingPaused {
		t.Fatalf("status = %+v", s)
	}
	wantCode(t, h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, nil), control.CodePaused)

	var res control.ResumeResult
	if err := h.call(control.ResumeCommand, control.ResumeArgs{Stack: "plain-stack"}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Stack != "plain-stack" || res.Pause.Run != dep.ID || res.Pause.Since.IsZero() {
		t.Fatalf("resume = %+v", res)
	}
	if s := stackStatus(t, h, "plain-stack"); s.Paused != nil || s.ActiveRun != "" {
		t.Fatalf("status after resume = %+v", s)
	}
	wantCode(t, h.call(control.ResumeCommand, control.ResumeArgs{Stack: "plain-stack"}, nil), control.CodeNotPaused)

	// a manual run works again and the check passes now
	d.setFailCheck(false)
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &dep); err != nil {
		t.Fatal(err)
	}
	if r := waitRun(t, h, dep.ID); r.Result != engine.ResultSucceeded {
		t.Fatalf("run = %+v", r)
	}
}
