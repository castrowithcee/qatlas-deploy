package service_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/registry/registrytest"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// svcClock is a manual clock for the service's poll loop and maintenance window.
type svcClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []svcTimer
}

type svcTimer struct {
	at time.Time
	ch chan time.Time
}

func (c *svcClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *svcClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, svcTimer{c.now.Add(d), ch})
	return ch
}

func (c *svcClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	keep := c.timers[:0]
	for _, tm := range c.timers {
		if !tm.at.After(c.now) {
			tm.ch <- c.now
		} else {
			keep = append(keep, tm)
		}
	}
	c.timers = keep
}

// noon is a Monday at noon on the host's own clock, far from the default window at 03:00 in any zone, and far
// from any DST change.
var noon = time.Date(2026, 6, 15, 12, 0, 0, 0, time.Local)

type noticeLog struct {
	mu   sync.Mutex
	list []service.UpdateNotice
}

func (n *noticeLog) add(u service.UpdateNotice) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.list = append(n.list, u)
}

func (n *noticeLog) all() []service.UpdateNotice {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.list)
}

func withClock(c *svcClock, n *noticeLog) func(*service.Config) {
	return func(cfg *service.Config) {
		cfg.Clock = c
		if n != nil {
			cfg.OnUpdateAvailable = n.add
		}
	}
}

const automaticRules = "version: 1\nupdate:\n  mode: automatic\n"

// windowHarness binds the stacks with the given extra files and deploys the readable ones, on a clock at noon.
func windowHarness(t *testing.T, dep engine.Deployer, files map[string]string) (*harness, *svcClock, *noticeLog) {
	t.Helper()
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": {"0.9.1", "0.9.2", "1.2.3", "1.3.0", "2.0.0", "2.1.0-rc.1"}}})
	clock, notices := &svcClock{now: noon}, &noticeLog{}
	h := boundWith(t, dep, files, withRegistry(srv.URL), withClock(clock, notices))
	return h, clock, notices
}

// openWindow sets the window to open half an hour from now for an hour, lets the clock reach it and waits
// until the window's work is done.
func openWindow(t *testing.T, h *harness, clock *svcClock) control.WindowResult {
	t.Helper()
	if err := h.call(control.WindowSetCommand, control.WindowSetArgs{Start: "12:30", Duration: "1h"}, nil); err != nil {
		t.Fatal(err)
	}
	clock.Advance(30 * time.Minute)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		var res control.WindowResult
		if err := h.call(control.WindowShowCommand, nil, &res); err != nil {
			t.Fatal(err)
		}
		if !res.LastRun.IsZero() && !res.Running {
			return res
		}
		if time.Now().After(deadline) {
			t.Fatalf("the window did not run: %+v", res)
		}
	}
}

func runCount(t *testing.T, h *harness) int {
	t.Helper()
	var res control.RunsResult
	if err := h.call(control.RunsCommand, control.RunsArgs{}, &res); err != nil {
		t.Fatal(err)
	}
	return len(res.Runs)
}

func storedUpdates(t *testing.T, h *harness, name string) control.StackUpdates {
	t.Helper()
	var res control.UpdatesResult
	if err := h.call(control.UpdatesListCommand, control.UpdatesArgs{Stack: name}, &res); err != nil {
		t.Fatal(err)
	}
	return res.Stacks[0]
}

// sequentialDeployer records the stacks it pulls and whether two pulls ever overlapped.
type sequentialDeployer struct {
	fakeDeployer
	mu      sync.Mutex
	active  int
	overlap bool
	pulls   []string
	onPull  func(stack string)
}

func (d *sequentialDeployer) Pull(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	d.mu.Lock()
	d.active++
	d.overlap = d.overlap || d.active > 1
	d.pulls = append(d.pulls, st.Name)
	hook := d.onPull
	d.mu.Unlock()
	if hook != nil {
		hook(st.Name)
	}
	err := d.fakeDeployer.Pull(ctx, st, log)
	d.mu.Lock()
	d.active--
	d.mu.Unlock()
	return err
}

func (d *sequentialDeployer) reset(onPull func(string)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pulls, d.onPull, d.overlap = nil, onPull, false
}

func (d *sequentialDeployer) pulled() ([]string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.pulls), d.overlap
}

func TestWindowUpdatesAutomaticStacksOneAfterAnother(t *testing.T) {
	dep := &sequentialDeployer{}
	h, clock, notices := windowHarness(t, dep, map[string]string{"plain-stack/qdeploy.yaml": automaticRules})
	dep.reset(nil)
	before := runCount(t, h)

	res := openWindow(t, h, clock)
	if !res.Open || !res.LastRun.Equal(noon.Add(30*time.Minute)) {
		t.Fatalf("window = %+v", res)
	}
	want := []string{"Update demo-stack DEMO_APP_VERSION from 1.2.3 to 1.3.0", "Update plain-stack DEMO_APP_VERSION from 0.9.1 to 0.9.2"}
	if got := h.git.Pushed(); !slices.Equal(got, want) {
		t.Fatalf("pushed = %q", got)
	}
	if pulls, overlap := dep.pulled(); !slices.Equal(pulls, []string{"demo-stack", "plain-stack"}) || overlap {
		t.Fatalf("pulls = %v, overlap %v", pulls, overlap)
	}
	var runs control.RunsResult
	if err := h.call(control.RunsCommand, control.RunsArgs{}, &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs.Runs) != before+2 || runs.Runs[0].Trigger != engine.TriggerUpdate || runs.Runs[0].Result != engine.ResultSucceeded {
		t.Fatalf("runs = %+v", runs.Runs)
	}
	// the stored detection follows the update
	if s := storedUpdates(t, h, "demo-stack"); s.Checked.IsZero() || s.Items[0].Current != "1.3.0" || s.Items[0].Status != "current" {
		t.Fatalf("demo-stack updates = %+v", s)
	}
	if len(notices.all()) != 0 {
		t.Fatalf("notices = %+v", notices.all())
	}
}

func TestWindowEndBeginsNothingNewAndAbortsNothing(t *testing.T) {
	dep := &sequentialDeployer{}
	majorRules := "version: 1\nupdate:\n  mode: automatic\n  ceiling: major\n"
	h, clock, _ := windowHarness(t, dep, map[string]string{"demo-stack/qdeploy.yaml": majorRules, "plain-stack/qdeploy.yaml": automaticRules})
	// the first step of demo-stack runs past the end of the window
	plainBefore := stackStatus(t, h, "plain-stack").Deployed
	var once sync.Once
	dep.reset(func(stack string) {
		if stack == "demo-stack" {
			once.Do(func() { clock.Advance(2 * time.Hour) })
		}
	})

	res := openWindow(t, h, clock)
	if res.Open {
		t.Fatalf("window = %+v", res)
	}
	if got := h.git.Pushed(); !slices.Equal(got, []string{"Update demo-stack DEMO_APP_VERSION from 1.2.3 to 1.3.0"}) {
		t.Fatalf("pushed = %q", got)
	}
	var runs control.RunsResult
	if err := h.call(control.RunsCommand, control.RunsArgs{}, &runs); err != nil {
		t.Fatal(err)
	}
	if r := runs.Runs[0]; r.Stack != "demo-stack" || r.Trigger != engine.TriggerUpdate || r.Result != engine.ResultSucceeded {
		t.Fatalf("last run = %+v", r)
	}
	if pulls, _ := dep.pulled(); !slices.Equal(pulls, []string{"demo-stack"}) {
		t.Fatalf("pulls = %v", pulls)
	}
	if s := stackStatus(t, h, "plain-stack"); s.Deployed != plainBefore || s.ActiveRun != "" {
		t.Fatalf("plain-stack = %+v, deployed before %s", s, plainBefore)
	}
}

func TestWindowLeavesPausedAndPinnedStacksAlone(t *testing.T) {
	dep := &sequentialDeployer{}
	h, clock, notices := windowHarness(t, dep, map[string]string{"plain-stack/qdeploy.yaml": automaticRules})
	if err := h.call(control.PinCommand, control.HoldArgs{Stack: "demo-stack"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.call(control.PauseCommand, control.HoldArgs{Stack: "plain-stack"}, nil); err != nil {
		t.Fatal(err)
	}
	dep.reset(nil)
	before := runCount(t, h)

	openWindow(t, h, clock)
	if got := h.git.Pushed(); len(got) != 0 {
		t.Fatalf("pushed = %q", got)
	}
	if pulls, _ := dep.pulled(); len(pulls) != 0 || runCount(t, h) != before {
		t.Fatalf("pulls = %v, runs %d, before %d", pulls, runCount(t, h), before)
	}
	// the registry result is still stored, so the update stays visible
	for name, want := range map[string]string{"demo-stack": "1.3.0", "plain-stack": "0.9.2"} {
		s := storedUpdates(t, h, name)
		if s.Checked.IsZero() || s.Items[0].Status != "update" || s.Items[0].Selected != want {
			t.Fatalf("%s updates = %+v", name, s)
		}
	}
	if len(notices.all()) != 0 {
		t.Fatalf("notices = %+v", notices.all())
	}
}

func TestWindowOnlyReportsAManualStack(t *testing.T) {
	dep := &sequentialDeployer{}
	h, clock, notices := windowHarness(t, dep, map[string]string{"demo-stack/qdeploy.yaml": manualRules, "plain-stack/qdeploy.yaml": manualRules})
	dep.reset(nil)
	before := runCount(t, h)

	openWindow(t, h, clock)
	if got := h.git.Pushed(); len(got) != 0 {
		t.Fatalf("pushed = %q", got)
	}
	if pulls, _ := dep.pulled(); len(pulls) != 0 || runCount(t, h) != before {
		t.Fatalf("pulls = %v, runs %d, before %d", pulls, runCount(t, h), before)
	}
	got := notices.all()
	if len(got) != 2 || got[0].Stack != "demo-stack" || got[1].Stack != "plain-stack" ||
		len(got[0].Items) != 1 || got[0].Items[0].Selected != "1.3.0" || got[1].Items[0].Selected != "0.9.2" {
		t.Fatalf("notices = %+v", got)
	}
	if s := storedUpdates(t, h, "plain-stack"); s.Checked.IsZero() || s.Items[0].Status != "update" {
		t.Fatalf("plain-stack updates = %+v", s)
	}
}

func TestWindowShowAndSetValidateEveryPart(t *testing.T) {
	clock := &svcClock{now: noon}
	h := startWith(t, nil, withClock(clock, nil))
	var res control.WindowResult
	if err := h.call(control.WindowShowCommand, nil, &res); err != nil {
		t.Fatal(err)
	}
	tomorrow := time.Date(2026, 6, 16, 3, 0, 0, 0, time.Local)
	if len(res.Days) != 7 || res.Days[0] != "mon" || res.Start != "03:00" || res.Duration != "2h" || res.Zone != "local" ||
		res.Open || !res.NextOpening.Equal(tomorrow) || !res.LastRun.IsZero() || res.Running {
		t.Fatalf("default = %+v", res)
	}

	for _, args := range []any{
		control.WindowSetArgs{},
		control.WindowSetArgs{Days: []string{}},
		control.WindowSetArgs{Days: []string{"funday"}},
		control.WindowSetArgs{Days: []string{"mon", "Monday"}},
		control.WindowSetArgs{Days: []string{"daily", "mon"}},
		control.WindowSetArgs{Start: "24:00"},
		control.WindowSetArgs{Start: "3:00"},
		control.WindowSetArgs{Duration: "0m"},
		control.WindowSetArgs{Duration: "13h"},
		control.WindowSetArgs{Duration: "30s"},
		control.WindowSetArgs{Zone: "Local"},
		control.WindowSetArgs{Zone: "Nowhere/Atlantis"},
		control.WindowSetArgs{Zone: "../../etc/passwd"},
		control.WindowSetArgs{Start: "04:00", Zone: "Nowhere/Atlantis"},
	} {
		wantCode(t, h.call(control.WindowSetCommand, args, nil), control.CodeInvalid)
	}
	wantCode(t, h.call(control.WindowSetCommand, map[string]any{"start": "04:00", "extra": 1}, nil), control.CodeBadRequest)
	wantCode(t, h.call(control.WindowSetCommand, map[string]any{"days": "mon"}, nil), control.CodeBadRequest)
	wantCode(t, h.call(control.WindowShowCommand, map[string]any{"x": 1}, nil), control.CodeBadRequest)
	var same control.WindowResult
	if err := h.call(control.WindowShowCommand, nil, &same); err != nil || same.Start != "03:00" || same.Zone != "local" {
		t.Fatalf("after refused changes = %+v, %v", same, err)
	}

	var set control.WindowResult
	if err := h.call(control.WindowSetCommand, control.WindowSetArgs{Days: []string{"Sat", "sunday"}, Start: "04:15", Zone: "UTC"}, &set); err != nil {
		t.Fatal(err)
	}
	saturday := time.Date(2026, 6, 20, 4, 15, 0, 0, time.UTC)
	if !slices.Equal(set.Days, []string{"sat", "sun"}) || set.Start != "04:15" || set.Duration != "2h" || set.Zone != "UTC" ||
		!set.NextOpening.Equal(saturday) || set.NextOpening.Location().String() != "UTC" {
		t.Fatalf("set = %+v", set)
	}
	var st control.StatusResult
	if err := h.call(control.StatusCommand, nil, &st); err != nil {
		t.Fatal(err)
	}
	if st.Window == nil || st.Window.Start != "04:15" || !st.Window.NextOpening.Equal(saturday) {
		t.Fatalf("status window = %+v", st.Window)
	}
}
