package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/docker"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
)

type monEngine struct {
	states map[string]engine.StackState
	active map[string]string
	err    error
	// repairs are the attempts made; stackRestarts counts those that restarted the whole stack.
	repairs       []engine.Repair
	stackRestarts int
	// onRestart runs inside an attempt, as the restart would.
	onRestart func()
}

// Repair mirrors the engine: a busy slot refuses, an unrepairable stack refuses, otherwise the record is stored
// before the restart.
func (e *monEngine) Repair(ctx context.Context, stack string, rec engine.Repair, restart func(context.Context) error) (bool, error) {
	if _, busy := e.active[stack]; busy {
		return false, engine.ErrRunActive
	}
	cur, ok := e.states[stack]
	if !ok || cur.Deployed == "" || cur.Removed || cur.Stopped != nil || cur.Paused != nil || cur.OperatorPause != nil ||
		(cur.Repair != nil && cur.Repair.Halted != nil) {
		return false, engine.ErrNotRepairable
	}
	cur.Repair = &rec
	e.states[stack] = cur
	e.repairs = append(e.repairs, rec)
	if e.onRestart != nil {
		e.onRestart()
	}
	if restart == nil {
		e.stackRestarts++
		return true, nil
	}
	return true, restart(ctx)
}

func (e *monEngine) RecordRepair(stack string, rec *engine.Repair) (bool, error) {
	if e.err != nil {
		return false, e.err
	}
	cur, ok := e.states[stack]
	if !ok || (cur.Repair != nil && cur.Repair.Halted != nil) {
		return false, nil
	}
	if rec != nil {
		c := *rec
		rec = &c
	}
	cur.Repair = rec
	e.states[stack] = cur
	return true, nil
}

func (e *monEngine) Snapshot() (engine.Snapshot, error) {
	st := map[string]engine.StackState{}
	for k, v := range e.states {
		st[k] = v
	}
	act := map[string]string{}
	for k, v := range e.active {
		act[k] = v
	}
	return engine.Snapshot{State: engine.State{Stacks: st}, Active: act}, nil
}

func (e *monEngine) RecordMonitor(stack string, f *engine.Failure, services []string) error {
	if e.err != nil {
		return e.err
	}
	cur, ok := e.states[stack]
	if !ok {
		return nil
	}
	cur.Failure, cur.Services = f, append([]string(nil), services...)
	e.states[stack] = cur
	return nil
}

func (e *monEngine) UpdateStoppedContainers(stack string, add, remove []string) error {
	cur, ok := e.states[stack]
	if !ok {
		return nil
	}
	var ids []string
	for _, id := range cur.StoppedContainers {
		if !slices.Contains(remove, id) && !slices.Contains(add, id) {
			ids = append(ids, id)
		}
	}
	cur.StoppedContainers = append(ids, add...)
	e.states[stack] = cur
	return nil
}

type monDocker struct {
	Docker
	list []docker.Container
	err  error
	// restarted are the container IDs restarted, in order.
	restarted []string
}

func (d *monDocker) List(context.Context) ([]docker.Container, error) { return d.list, d.err }

func (d *monDocker) Restart(_ context.Context, ref string, _ time.Duration) error {
	d.restarted = append(d.restarted, ref)
	return nil
}

func (d *monDocker) WaitHealthy(_ context.Context, refs []string, _ docker.WaitOptions) ([]docker.WaitResult, error) {
	return nil, nil
}

type monClock struct{ now time.Time }

func (c *monClock) Now() time.Time                       { return c.now }
func (c *monClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }

type monRig struct {
	t      *testing.T
	eng    *monEngine
	dock   *monDocker
	clock  *monClock
	mon    *containerMonitor
	events []MonitorEvent
	logs   *bytes.Buffer
}

func newMonRig(t *testing.T) *monRig {
	t.Helper()
	r := &monRig{t: t, eng: &monEngine{states: map[string]engine.StackState{
		"web": {Deployed: strings.Repeat("a", 40)},
	}, active: map[string]string{}}, dock: &monDocker{},
		clock: &monClock{now: time.Date(2026, 5, 4, 3, 0, 0, 0, time.UTC)}, logs: &bytes.Buffer{}}
	r.mon = newContainerMonitor(monitorConfig{
		Docker: r.dock, Engine: r.eng, Clock: r.clock, Log: slog.New(slog.NewTextHandler(r.logs, nil)),
		StackDirs: func() map[string]string { return map[string]string{"web": "/srv/stacks/web", "db": "/srv/stacks/db"} },
		Hook:      func(ev MonitorEvent) { r.events = append(r.events, ev) },
	})
	return r
}

func ctr(id, service string) docker.Container {
	return docker.Container{ID: id, Name: "web-" + service + "-1", State: "running", Labels: map[string]string{
		"com.docker.compose.project": "web", "com.docker.compose.project.working_dir": "/srv/stacks/web",
		serviceLabel: service,
	}}
}

// poll advances the clock and runs one reading.
func (r *monRig) poll(d time.Duration) {
	r.t.Helper()
	r.clock.now = r.clock.now.Add(d)
	r.mon.step(context.Background())
}

func (r *monRig) stopMark(id string) {
	_ = r.eng.UpdateStoppedContainers("web", []string{id}, nil)
}

// restart replaces the monitor by a new instance over the same engine state, as a service restart does.
func (r *monRig) restart() {
	r.mon = newContainerMonitor(r.mon.monitorConfig)
}

func (r *monRig) failedEvents() (failed, recovered int) {
	for _, ev := range r.events {
		switch {
		case ev.Recovered:
			recovered++
		case !ev.Halted:
			failed++
		}
	}
	return
}

func (r *monRig) wantEvents(failed, recovered int) {
	r.t.Helper()
	if f, rc := r.failedEvents(); f != failed || rc != recovered {
		r.t.Fatalf("events failed=%d recovered=%d, want %d/%d: %+v", f, rc, failed, recovered, r.events)
	}
}

func TestMonitorExitedOneFailedAndOneRecovered(t *testing.T) {
	r := newMonRig(t)
	app, db := ctr("1", "app"), ctr("2", "db")
	r.dock.list = []docker.Container{app, db}
	r.poll(30 * time.Second)
	r.wantEvents(0, 0)
	if got := r.eng.states["web"].Services; len(got) != 2 {
		t.Fatalf("baseline = %v", got)
	}
	app.State = "exited"
	r.dock.list = []docker.Container{app, db}
	for range 10 {
		r.poll(30 * time.Second)
	}
	r.wantEvents(1, 0)
	ev := r.events[0]
	f := r.eng.states["web"].Failure
	if ev.Stack != "web" || ev.Kind != engine.FailureExited || ev.Container != "web-app-1" || f == nil ||
		f.Kind != engine.FailureExited || !f.Since.Equal(ev.Since) {
		t.Fatalf("event %+v, stored %+v", ev, f)
	}
	// the stack runs again: one clean reading is not enough, two end the failure
	app.State = "running"
	r.dock.list = []docker.Container{app, db}
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
	r.poll(30 * time.Second)
	r.poll(30 * time.Second)
	r.wantEvents(1, 1)
	if rec := r.events[1]; !rec.Recovered || rec.Kind != engine.FailureExited || !rec.Since.Equal(ev.Since) || r.eng.states["web"].Failure != nil {
		t.Fatalf("recovery %+v", rec)
	}
	// a second failure is a new one
	app.State = "dead"
	r.dock.list = []docker.Container{app, db}
	r.poll(30 * time.Second)
	r.wantEvents(2, 1)
}

func TestMonitorQdeployStopIsNoFailure(t *testing.T) {
	r := newMonRig(t)
	app, db := ctr("1", "app"), ctr("2", "db")
	r.dock.list = []docker.Container{app, db}
	r.poll(30 * time.Second)
	app.State = "exited"
	r.stopMark("1")
	r.dock.list = []docker.Container{app, db}
	for range 5 {
		r.poll(30 * time.Second)
	}
	r.wantEvents(0, 0)
	if got := r.eng.states["web"].Failure; got != nil {
		t.Fatalf("failure %+v", got)
	}
	// running again clears the memory, a later exit is a failure
	app.State = "running"
	r.dock.list = []docker.Container{app, db}
	r.poll(30 * time.Second)
	if got := r.eng.states["web"].StoppedContainers; len(got) != 0 {
		t.Fatalf("stop still remembered: %v", got)
	}
	app.State = "exited"
	r.dock.list = []docker.Container{app, db}
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
}

func TestMonitorUnhealthyNeedsMoreThanAMinuteObservedContinuously(t *testing.T) {
	r := newMonRig(t)
	app := ctr("1", "app")
	app.Health = "unhealthy"
	r.dock.list = []docker.Container{app}
	r.poll(0)
	r.poll(30 * time.Second)
	r.poll(30 * time.Second) // exactly 60 s
	r.wantEvents(0, 0)
	// healthy in between resets the timer
	app.Health = "healthy"
	r.dock.list = []docker.Container{app}
	r.poll(30 * time.Second)
	app.Health = "unhealthy"
	r.dock.list = []docker.Container{app}
	r.poll(30 * time.Second)
	r.poll(30 * time.Second)
	r.poll(30 * time.Second)
	r.wantEvents(0, 0)
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
	if k := r.events[0].Kind; k != engine.FailureUnhealthy {
		t.Fatalf("kind %s", k)
	}
}

func TestMonitorRestartLoopRule(t *testing.T) {
	r := newMonRig(t)
	app := ctr("1", "app")
	for i := range 3 {
		app.RestartCount = i
		r.dock.list = []docker.Container{app}
		r.poll(30 * time.Second)
	}
	r.wantEvents(0, 0) // rose by 2 within 5 minutes
	app.RestartCount = 3
	r.dock.list = []docker.Container{app}
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
	if k := r.events[0].Kind; k != engine.FailureRestartLoop {
		t.Fatalf("kind %s", k)
	}

	// a slow rise (one per three minutes) is no loop
	slow := newMonRig(t)
	for i := range 8 {
		app.RestartCount = i
		slow.dock.list = []docker.Container{app}
		slow.poll(3 * time.Minute)
	}
	slow.wantEvents(0, 0)
}

func TestMonitorMissingContainer(t *testing.T) {
	r := newMonRig(t)
	app, db := ctr("1", "app"), ctr("2", "db")
	r.dock.list = []docker.Container{app, db}
	r.poll(30 * time.Second)
	r.dock.list = []docker.Container{app}
	r.poll(30 * time.Second)
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
	if ev := r.events[0]; ev.Kind != engine.FailureMissing || ev.Container != "db" {
		t.Fatalf("event %+v", ev)
	}
	r.dock.list = []docker.Container{app, db}
	r.poll(30 * time.Second)
	r.poll(30 * time.Second)
	r.wantEvents(1, 1)

	// no container at all for a deployed stack, without a baseline
	z := newMonRig(t)
	z.poll(30 * time.Second)
	z.wantEvents(1, 0)
	if z.events[0].Kind != engine.FailureMissing {
		t.Fatalf("event %+v", z.events[0])
	}
}

func TestMonitorBaselineIsResetByARun(t *testing.T) {
	r := newMonRig(t)
	app, db := ctr("1", "app"), ctr("2", "db")
	r.dock.list = []docker.Container{app, db}
	r.poll(30 * time.Second)
	// a run is active while the db service is removed by the new version: nothing is evaluated
	r.eng.active["web"] = "20260504T030000Z-deadbeef"
	r.dock.list = []docker.Container{app}
	r.poll(30 * time.Second)
	r.wantEvents(0, 0)
	if len(r.eng.states["web"].Services) != 0 {
		t.Fatal("baseline kept across a run")
	}
	delete(r.eng.active, "web")
	r.poll(30 * time.Second)
	r.wantEvents(0, 0)
	if got := r.eng.states["web"].Services; len(got) != 1 || got[0] != "app" {
		t.Fatalf("new baseline = %v", got)
	}
}

func TestMonitorSkipsWhatItMustNotEvaluate(t *testing.T) {
	failing := func() []docker.Container {
		a := ctr("1", "app")
		a.State = "exited"
		return []docker.Container{a}
	}
	mark := &engine.Hold{Since: time.Now()}
	cases := map[string]func(r *monRig){
		"run active": func(r *monRig) { r.eng.active["web"] = "container stop" },
		"stopped":    func(r *monRig) { s := r.eng.states["web"]; s.Stopped = mark; r.eng.states["web"] = s },
		"rollback pause": func(r *monRig) {
			s := r.eng.states["web"]
			s.Paused = &engine.Pause{Time: time.Now()}
			r.eng.states["web"] = s
		},
		"operator pause": func(r *monRig) { s := r.eng.states["web"]; s.OperatorPause = mark; r.eng.states["web"] = s },
		"never deployed": func(r *monRig) { r.eng.states["web"] = engine.StackState{} },
		"removed":        func(r *monRig) { s := r.eng.states["web"]; s.Removed = true; r.eng.states["web"] = s },
		"foreign dir": func(r *monRig) {
			c := failing()
			c[0].ID = "9"
			c[0].Labels["com.docker.compose.project.working_dir"] = "/elsewhere/web"
			r.dock.list = append(c, ctr("2", "db"))
		},
		"single container": func(r *monRig) {
			c := failing()
			c[0].ID, c[0].Labels = "9", nil
			r.dock.list = append(c, ctr("2", "db"))
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newMonRig(t)
			r.dock.list = failing()
			setup(r)
			for range 4 {
				r.poll(30 * time.Second)
			}
			r.wantEvents(0, 0)
			if r.eng.states["web"].Failure != nil {
				t.Fatalf("failure stored: %+v", r.eng.states["web"].Failure)
			}
			if len(r.eng.repairs) != 0 || len(r.dock.restarted) != 0 {
				t.Fatalf("repaired: %+v %v", r.eng.repairs, r.dock.restarted)
			}
		})
	}
}

func TestMonitorBecomingStoppedDropsTheFailureQuietly(t *testing.T) {
	r := newMonRig(t)
	a := ctr("1", "app")
	a.State = "exited"
	r.dock.list = []docker.Container{a}
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
	s := r.eng.states["web"]
	s.Stopped = &engine.Hold{Since: r.clock.now}
	r.eng.states["web"] = s
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
	if r.eng.states["web"].Failure != nil {
		t.Fatal("failure of a stopped stack kept")
	}
}

func TestMonitorDockerErrorMarksNothingAndLogsOncePerStreak(t *testing.T) {
	r := newMonRig(t)
	a := ctr("1", "app")
	a.State = "exited"
	r.dock.list = []docker.Container{a}
	r.dock.err = errors.New("engine down")
	for range 5 {
		r.poll(30 * time.Second)
	}
	r.wantEvents(0, 0)
	if r.eng.states["web"].Failure != nil {
		t.Fatal("failure from a failed reading")
	}
	if n := strings.Count(r.logs.String(), "cannot read the containers"); n != 1 {
		t.Fatalf("logged %d times:\n%s", n, r.logs.String())
	}
	r.dock.err = nil
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
	r.dock.err = errors.New("engine down")
	r.poll(30 * time.Second)
	if n := strings.Count(r.logs.String(), "cannot read the containers"); n != 2 {
		t.Fatalf("a new streak was not logged: %d", n)
	}
}

func TestMonitorFailedWriteSendsNothingAndRetries(t *testing.T) {
	r := newMonRig(t)
	a := ctr("1", "app")
	a.State = "exited"
	r.dock.list = []docker.Container{a}
	r.eng.err = errors.New("disk full")
	r.poll(30 * time.Second)
	r.poll(30 * time.Second)
	r.wantEvents(0, 0)
	r.eng.err = nil
	r.poll(30 * time.Second)
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
}

func TestMonitorRunStopsWithContext(t *testing.T) {
	r := newMonRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.mon.run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("monitor still running after the context ended")
	}
}

func TestMonitorFailureShownInStatus(t *testing.T) {
	since := time.Date(2026, 5, 4, 3, 2, 1, 0, time.UTC)
	snap := engine.Snapshot{State: engine.State{Stacks: map[string]engine.StackState{"web": {
		Deployed: strings.Repeat("a", 40), Failure: &engine.Failure{Kind: engine.FailureExited, Container: "web-app-1", Since: since},
	}}}}
	got := stackStatuses(snap)[0].Findings
	if len(got) != 1 || got[0].Kind != "container-failed" || got[0].Detail != "since 2026-05-04T03:02:01Z: exited web-app-1" {
		t.Fatalf("findings = %+v", got)
	}
	want := &control.FailureInfo{Kind: "exited", Container: "web-app-1", Since: since}
	if f := stackStatuses(snap)[0].Failure; f == nil || *f != *want {
		t.Fatalf("failure = %+v", f)
	}
	snap.State.Stacks["web"] = engine.StackState{Deployed: strings.Repeat("a", 40)}
	if f := stackStatuses(snap)[0].Failure; f != nil {
		t.Fatalf("failure without a monitored failure = %+v", f)
	}
}

func TestMonitorStopMarkSurvivesRestartAndEndsWithTheContainer(t *testing.T) {
	r := newMonRig(t)
	app, db := ctr("1", "app"), ctr("2", "db")
	r.dock.list = []docker.Container{app, db}
	r.poll(30 * time.Second)
	app.State, app.Status = "exited", "Exited (137) 2 seconds ago"
	r.dock.list = []docker.Container{app, db}
	r.stopMark("1")
	r.poll(30 * time.Second)
	r.restart()
	r.poll(30 * time.Second)
	r.wantEvents(0, 0)
	// the container is gone: the mark is dropped
	r.dock.list = []docker.Container{db}
	r.poll(30 * time.Second)
	if got := r.eng.states["web"].StoppedContainers; len(got) != 0 {
		t.Fatalf("mark of a gone container kept: %v", got)
	}
}

func TestMonitorCompletedOneShotIsNoFailure(t *testing.T) {
	r := newMonRig(t)
	app, job := ctr("1", "app"), ctr("2", "migrate")
	job.State, job.Status = "exited", "Exited (0) 5 minutes ago"
	r.dock.list = []docker.Container{app, job}
	for range 4 {
		r.poll(30 * time.Second)
	}
	r.wantEvents(0, 0)
	if got := r.eng.states["web"].Services; len(got) != 2 {
		t.Fatalf("a one-shot blocks or leaves the baseline: %v", got)
	}
	// the one-shot is present for the missing rule, and gone is missing
	r.dock.list = []docker.Container{app}
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
	if ev := r.events[0]; ev.Kind != engine.FailureMissing || ev.Container != "migrate" {
		t.Fatalf("event %+v", ev)
	}
}

func TestMonitorExitedWithErrorOrUnparseableStatusIsFailure(t *testing.T) {
	for _, status := range []string{"Exited (137) 5 seconds ago", "Exited (1) 1 minute ago", "", "Exited abnormally", " Exited (0) x"} {
		r := newMonRig(t)
		app := ctr("1", "app")
		app.State, app.Status = "exited", status
		r.dock.list = []docker.Container{app}
		r.poll(30 * time.Second)
		r.wantEvents(1, 0)
	}
}
