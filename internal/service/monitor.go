package service

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/castrowithcee/qatlas-kit/docker"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
)

const (
	defaultMonitorInterval = 30 * time.Second
	monitorListTimeout     = 20 * time.Second
	// unhealthyAfter is how long a container must be observed unhealthy without a break before it counts; it
	// must be exceeded, not just reached.
	unhealthyAfter = time.Minute
	// A restart loop is a restart count that rose by restartRise or more within restartWindow.
	restartWindow = 5 * time.Minute
	restartRise   = 3
	// recoverPolls is how many consecutive clean readings end a failure, so a container that a restart policy
	// brings back for a moment does not report a recovery per cycle.
	recoverPolls = 2
	serviceLabel = "com.docker.compose.service"
	// repairStable is how long a stack runs without a failure before its restarts no longer count.
	repairStable = time.Hour
)

// repairDelays are the waits before each repair attempt: the first from the start of the failure, each further
// one from the attempt before. The first restarts the failed containers, the others the stack.
var repairDelays = [engine.MaxRepairAttempts]time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

var exitedStatusRe = regexp.MustCompile(`^Exited \((\d+)\)`)

// completedOneShot is a container that exited with code 0, such as an init or migration job: no failure. An
// unparseable status counts as a failure.
func completedOneShot(c docker.Container) bool {
	if c.State != "exited" {
		return false
	}
	m := exitedStatusRe.FindStringSubmatch(c.Status)
	if m == nil {
		return false
	}
	code, err := strconv.Atoi(m[1])
	return err == nil && code == 0
}

// MonitorEvent is one transition of the container monitor: a stack's containers failed, run again, or its
// repair by restarts was halted. Exactly one failed event is sent per failure and one recovered event when it
// ends; a halted event is sent once per halt.
type MonitorEvent struct {
	Stack string
	// Kind is the engine.Failure* kind of the failure; for a recovery, the kind it had.
	Kind string
	// Container is the name of the container (the Compose service for a missing one); may be empty.
	Container string
	Recovered bool
	// Restarted is set on a recovery that followed a repair attempt.
	Restarted bool
	// Halted is set when the repair attempts did not end the failure; Attempts is how many were made.
	Halted   bool
	Attempts int
	// Since is when the failure was first observed; Time is when this transition was found.
	Since, Time time.Time
}

func logMonitorEvent(log *slog.Logger, ev MonitorEvent) {
	if ev.Recovered {
		log.Info("container failure recovered", "stack", ev.Stack, "kind", ev.Kind, "container", ev.Container,
			"since", ev.Since.UTC().Format(time.RFC3339), "restarted", ev.Restarted)
		return
	}
	if ev.Halted {
		log.Warn("container failure not repaired; repair halted until resume", "stack", ev.Stack, "kind", ev.Kind,
			"container", ev.Container, "attempts", ev.Attempts)
		return
	}
	log.Warn("container failure", "stack", ev.Stack, "kind", ev.Kind, "container", ev.Container)
}

// monitorEngine is the part of the engine the monitor uses.
type monitorEngine interface {
	Snapshot() (engine.Snapshot, error)
	RecordMonitor(stack string, failure *engine.Failure, services []string) error
	UpdateStoppedContainers(stack string, add, remove []string) error
	Repair(ctx context.Context, stack string, rec engine.Repair, restart func(ctx context.Context) error) (bool, error)
	RecordRepair(stack string, rec *engine.Repair) (bool, error)
}

type monitorConfig struct {
	Docker    Docker
	Engine    monitorEngine
	Clock     engine.Clock // nil means the system clock
	Interval  time.Duration
	StackDirs func() map[string]string
	Hook      func(MonitorEvent)
	Log       *slog.Logger
}

type restartSample struct {
	at    time.Time
	count int
}

// containerMonitor reads the containers every interval and tells when an own stack fails or runs again. It
// repairs a failure by at most engine.MaxRepairAttempts restarts, each under the stack's run slot, and halts the
// repair when they do not help. It holds no engine lock while it talks to Docker. A stack is evaluated only if
// it was deployed, is not stopped, not paused (a failed rollback or 'qdeploy stack pause' alike) and has no run
// or action active; a failed reading marks nothing.
type containerMonitor struct {
	monitorConfig
	// Per container ID, only for containers evaluated in the last reading.
	unhealthySince map[string]time.Time
	restarts       map[string][]restartSample
	// clean counts the consecutive clean readings of a stack with an open failure.
	clean map[string]int
	// failing is set while the readings fail, so an error streak is logged once.
	failing bool
	// repairErr is the last refusal of a repair attempt per stack, so a lasting one is logged once.
	repairErr map[string]string
}

// repairAttempt is a repair attempt that is due. containers are the IDs to restart, none for the whole stack;
// all are the IDs of the stack's containers, whose timers start afresh after the attempt.
type repairAttempt struct {
	stack           string
	rec             engine.Repair
	containers, all []string
}

type realMonitorClock struct{}

func (realMonitorClock) Now() time.Time                         { return time.Now() }
func (realMonitorClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func newContainerMonitor(cfg monitorConfig) *containerMonitor {
	if cfg.Clock == nil {
		cfg.Clock = realMonitorClock{}
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultMonitorInterval
	}
	if cfg.Hook == nil {
		cfg.Hook = func(MonitorEvent) {}
	}
	return &containerMonitor{monitorConfig: cfg, unhealthySince: map[string]time.Time{},
		restarts: map[string][]restartSample{}, clean: map[string]int{}, repairErr: map[string]string{}}
}

// run reads until ctx ends.
func (m *containerMonitor) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.Clock.After(m.Interval):
		}
		m.step(ctx)
	}
}

func (m *containerMonitor) streak(err error, what string) {
	if err == nil {
		m.failing = false
		return
	}
	if !m.failing {
		m.Log.Warn("container monitor cannot read "+what+"; retrying every cycle", "error", err)
	}
	m.failing = true
}

// step makes one reading and evaluates it.
func (m *containerMonitor) step(ctx context.Context) {
	dirs := m.StackDirs()
	if len(dirs) == 0 {
		// No binding or an unreadable working copy: nothing is known to be own, so nothing is touched.
		return
	}
	before, err := m.Engine.Snapshot()
	if err != nil {
		m.streak(err, "the engine state")
		return
	}
	lctx, cancel := context.WithTimeout(ctx, monitorListTimeout)
	all, err := m.Docker.List(lctx)
	cancel()
	if err != nil {
		m.streak(err, "the containers")
		return
	}
	// A run that ended between the two snapshots may have left the containers mid-change in the reading.
	after, err := m.Engine.Snapshot()
	if err != nil {
		m.streak(err, "the engine state")
		return
	}
	m.streak(nil, "")
	busy := map[string]bool{}
	for n := range before.Active {
		busy[n] = true
	}
	for n := range after.Active {
		busy[n] = true
	}
	// The attempts run after the evaluation, so a slow restart does not age the reading of the other stacks.
	for _, a := range m.evaluate(m.Clock.Now(), dirs, after.State.Stacks, busy, all) {
		m.attempt(ctx, a)
	}
}

func (m *containerMonitor) evaluate(now time.Time, dirs map[string]string, states map[string]engine.StackState,
	busy map[string]bool, all []docker.Container) []repairAttempt {
	running := make(map[string]bool, len(all))
	for _, c := range all {
		running[c.ID] = c.State == "running"
	}

	projects, _ := docker.Group(all)
	byStack := map[string][]docker.Container{}
	for _, p := range projects {
		if kind, _ := classify(p.Name, p.WorkingDir, dirs); kind == control.ContainerKindOwn {
			byStack[p.Name] = append(byStack[p.Name], p.Containers...)
		}
	}

	oldUnhealthy, oldRestarts := m.unhealthySince, m.restarts
	m.unhealthySince, m.restarts = map[string]time.Time{}, map[string][]restartSample{}

	var due []repairAttempt
	names := make([]string, 0, len(states))
	for n := range states {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		st := states[name]
		var stopped map[string]bool
		if !busy[name] {
			// A marker ends when the container runs again or is gone.
			var drop []string
			stopped = map[string]bool{}
			for _, id := range st.StoppedContainers {
				if run, ok := running[id]; ok && !run {
					stopped[id] = true
				} else {
					drop = append(drop, id)
				}
			}
			if len(drop) > 0 {
				if err := m.Engine.UpdateStoppedContainers(name, nil, drop); err != nil {
					m.Log.Warn("container monitor cannot store its record", "stack", name, "error", err)
					for _, id := range drop {
						stopped[id] = true
					}
				}
			}
		}
		if busy[name] {
			// What a run or action leaves behind is a new baseline; the open failure stays as it is.
			delete(m.clean, name)
			if len(st.Services) > 0 {
				m.record(name, st.Failure, nil)
			}
			continue
		}
		_, own := dirs[name]
		if !own || st.Deployed == "" || st.Removed || st.Stopped != nil || st.Paused != nil || st.OperatorPause != nil {
			delete(m.clean, name)
			if st.Failure != nil {
				m.record(name, nil, st.Services)
			}
			// The repair ends without a word; its count stays, so a stop and start does not lift the limit.
			if r := st.Repair; r != nil && r.Halted == nil && !r.Next.IsZero() {
				next := *r
				next.Next = time.Time{}
				m.recordRepair(name, &next)
			}
			continue
		}
		cs := byStack[name]
		kind, container, failed := m.detect(now, cs, st.Services, stopped, oldUnhealthy, oldRestarts)
		open := st.Failure
		switch {
		case kind != "" && st.Failure == nil:
			f := engine.Failure{Kind: kind, Container: container, Since: now.UTC()}
			if !m.record(name, &f, st.Services) {
				continue
			}
			m.Hook(MonitorEvent{Stack: name, Kind: kind, Container: container, Since: f.Since, Time: now.UTC()})
			open = &f
		case kind != "":
			m.clean[name] = 0
		case st.Failure != nil:
			m.clean[name]++
			if m.clean[name] >= recoverPolls && m.record(name, nil, st.Services) {
				delete(m.clean, name)
				f := st.Failure
				r := st.Repair
				restarted := r != nil && r.Halted == nil && r.Attempts > 0 && !r.Last.Before(f.Since)
				m.Hook(MonitorEvent{Stack: name, Kind: f.Kind, Container: f.Container, Recovered: true,
					Restarted: restarted, Since: f.Since, Time: now.UTC()})
				open = nil
			}
		default:
			if svc := m.healthyServices(cs, stopped); svc != nil && !slices.Equal(svc, st.Services) {
				m.record(name, nil, svc)
			}
		}
		switch {
		case kind != "":
			if a, ok := m.planRepair(now, name, st.Repair, *open, kind, container, failed); ok {
				for _, c := range cs {
					a.all = append(a.all, c.ID)
				}
				due = append(due, a)
			}
		case open == nil:
			m.settleRepair(now, name, st.Repair)
		}
	}
	return due
}

// planRepair handles the repair of a stack whose reading shows the failure f: it halts the repair once the
// attempts are used up, keeps the time of the next attempt and returns the attempt when it is due.
func (m *containerMonitor) planRepair(now time.Time, name string, cur *engine.Repair, f engine.Failure, kind,
	container string, failed []string) (repairAttempt, bool) {
	var r engine.Repair
	if cur != nil {
		r = *cur
	}
	if r.Halted != nil {
		return repairAttempt{}, false
	}
	r.StableSince = time.Time{}
	if r.Attempts >= engine.MaxRepairAttempts {
		r.Next, r.Halted = time.Time{}, &engine.Hold{Since: now.UTC()}
		if m.recordRepair(name, &r) {
			m.Hook(MonitorEvent{Stack: name, Kind: kind, Container: container, Halted: true, Attempts: r.Attempts,
				Since: f.Since, Time: now.UTC()})
		}
		return repairAttempt{}, false
	}
	next := f.Since.Add(repairDelays[0])
	if r.Attempts > 0 {
		if t := r.Last.Add(repairDelays[r.Attempts]); t.After(next) {
			next = t
		}
	}
	r.Next = next.UTC()
	if cur == nil || !sameRepair(*cur, r) {
		if !m.recordRepair(name, &r) {
			return repairAttempt{}, false
		}
	}
	if now.Before(next) {
		return repairAttempt{}, false
	}
	a := repairAttempt{stack: name, rec: engine.Repair{Attempts: r.Attempts + 1, Last: now.UTC()}}
	if a.rec.Attempts < engine.MaxRepairAttempts {
		a.rec.Next = now.Add(repairDelays[a.rec.Attempts]).UTC()
	}
	if r.Attempts == 0 {
		// Without a failed container, as for a missing service, the first attempt restarts the stack too.
		a.containers = failed
	}
	return a, true
}

// settleRepair handles the repair of a stack without an open failure: no attempt is due, and after
// repairStable without a failure the restarts no longer count.
func (m *containerMonitor) settleRepair(now time.Time, name string, cur *engine.Repair) {
	if cur == nil || cur.Halted != nil {
		return
	}
	r := *cur
	r.Next = time.Time{}
	if r.StableSince.IsZero() {
		r.StableSince = now.UTC()
	}
	if r.Attempts == 0 || now.Sub(r.StableSince) >= repairStable {
		m.recordRepair(name, nil)
		return
	}
	if !sameRepair(r, *cur) {
		m.recordRepair(name, &r)
	}
}

func sameRepair(a, b engine.Repair) bool {
	return a.Attempts == b.Attempts && a.Last.Equal(b.Last) && a.Next.Equal(b.Next) &&
		a.StableSince.Equal(b.StableSince) && (a.Halted == nil) == (b.Halted == nil)
}

// recordRepair stores the repair record of a stack and tells whether it was stored; a failed write is retried
// at the next reading.
func (m *containerMonitor) recordRepair(name string, r *engine.Repair) bool {
	stored, err := m.Engine.RecordRepair(name, r)
	if err != nil {
		m.Log.Warn("container monitor cannot store its record", "stack", name, "error", err)
	}
	return stored
}

// attempt makes one repair attempt. A refusal, such as a busy run slot, leaves it due for the next reading.
func (m *containerMonitor) attempt(ctx context.Context, a repairAttempt) {
	ctx, cancel := context.WithTimeout(ctx, control.StackActionTimeout)
	defer cancel()
	var restart func(ctx context.Context) error
	if len(a.containers) > 0 {
		restart = func(ctx context.Context) error { return m.restartContainers(ctx, a.containers) }
	}
	made, err := m.Engine.Repair(ctx, a.stack, a.rec, restart)
	if made {
		delete(m.repairErr, a.stack)
		for _, id := range a.all {
			delete(m.unhealthySince, id)
			delete(m.restarts, id)
		}
		what := "stack"
		if restart != nil {
			what = "containers"
		}
		if err != nil {
			m.Log.Warn("repair restart failed", "stack", a.stack, "attempt", a.rec.Attempts, "restarted", what, "error", err)
			return
		}
		m.Log.Info("repair restart made", "stack", a.stack, "attempt", a.rec.Attempts, "restarted", what)
		return
	}
	if err == nil || errors.Is(err, engine.ErrNotRepairable) || errors.Is(err, engine.ErrRunActive) || errors.Is(err, engine.ErrBusy) {
		return
	}
	if msg := err.Error(); m.repairErr[a.stack] != msg {
		m.repairErr[a.stack] = msg
		m.Log.Warn("repair attempt refused; retrying every cycle", "stack", a.stack, "error", err)
	}
}

// restartContainers restarts the containers and waits until they run and are healthy; whether they then stay
// so is for the next readings to tell.
func (m *containerMonitor) restartContainers(ctx context.Context, ids []string) error {
	var errs []error
	for _, id := range ids {
		if err := m.Docker.Restart(ctx, id, control.ContainerDefaultGraceSeconds*time.Second); err != nil {
			errs = append(errs, err)
		}
	}
	if _, err := m.Docker.WaitHealthy(ctx, ids, docker.WaitOptions{Deadline: containerReadyWait}); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// record stores the monitor's view of a stack and tells whether that worked; on a failed write the next
// reading tries again, so no transition is sent twice or lost.
func (m *containerMonitor) record(name string, f *engine.Failure, services []string) bool {
	if err := m.Engine.RecordMonitor(name, f, services); err != nil {
		m.Log.Warn("container monitor cannot store its record", "stack", name, "error", err)
		return false
	}
	return true
}

// healthyServices returns the sorted Compose services of the stack if every container that is not stopped
// through qdeploy and not a completed one-shot runs and is not unhealthy or starting; nil otherwise.
func (m *containerMonitor) healthyServices(cs []docker.Container, stopped map[string]bool) []string {
	set := map[string]bool{}
	for _, c := range cs {
		if !stopped[c.ID] && !completedOneShot(c) && (c.State != "running" || (c.Health != "" && c.Health != "healthy")) {
			return nil
		}
		if s := c.Labels[serviceLabel]; s != "" {
			set[s] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	if len(out) > engine.MaxMonitorServices {
		return nil
	}
	return out
}

// detect returns the failure of one stack's containers, by priority exited, restart loop, unhealthy, missing;
// empty if none. failed are the IDs of the containers that fail. It also carries the per-container timers
// forward.
func (m *containerMonitor) detect(now time.Time, cs []docker.Container, expected []string, stopped map[string]bool,
	oldUnhealthy map[string]time.Time, oldRestarts map[string][]restartSample) (kind, container string, failed []string) {
	sorted := slices.Clone(cs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	found := map[string]string{}
	set := func(k, name string) {
		if _, ok := found[k]; !ok {
			found[k] = maskControl(name)
		}
	}
	fail := func(k string, c docker.Container) {
		set(k, c.Name)
		if !slices.Contains(failed, c.ID) {
			failed = append(failed, c.ID)
		}
	}
	services := map[string]bool{}
	for _, c := range sorted {
		if s := c.Labels[serviceLabel]; s != "" {
			services[s] = true
		}
		if (c.State == "exited" || c.State == "dead") && !stopped[c.ID] && !completedOneShot(c) {
			fail(engine.FailureExited, c)
		}
		samples := oldRestarts[c.ID]
		if n := len(samples); n > 0 && c.RestartCount < samples[n-1].count {
			samples = nil
		}
		samples = slices.DeleteFunc(slices.Clone(samples), func(s restartSample) bool { return now.Sub(s.at) > restartWindow })
		samples = append(samples, restartSample{now, c.RestartCount})
		m.restarts[c.ID] = samples
		if c.RestartCount-samples[0].count >= restartRise {
			fail(engine.FailureRestartLoop, c)
		}
		if c.State == "running" && c.Health == "unhealthy" {
			since, ok := oldUnhealthy[c.ID]
			if !ok {
				since = now
			}
			m.unhealthySince[c.ID] = since
			if now.Sub(since) > unhealthyAfter {
				fail(engine.FailureUnhealthy, c)
			}
		}
	}
	switch {
	case len(cs) == 0 && len(expected) == 0:
		set(engine.FailureMissing, "")
	default:
		for _, s := range expected {
			if !services[s] {
				set(engine.FailureMissing, s)
				break
			}
		}
	}
	for _, k := range []string{engine.FailureExited, engine.FailureRestartLoop, engine.FailureUnhealthy, engine.FailureMissing} {
		if c, ok := found[k]; ok {
			return k, c, failed
		}
	}
	return "", "", nil
}
