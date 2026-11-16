package service

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/docker"

	"github.com/castrowithcee/qatlas-deploy/internal/engine"
)

// failingRig is a monitor rig whose app container exited at the first reading, at failedAt.
func failingRig(t *testing.T) (r *monRig, failedAt time.Time) {
	t.Helper()
	r = newMonRig(t)
	r.dock.list = []docker.Container{ctr("1", "app"), ctr("2", "db")}
	r.poll(30 * time.Second)
	r.setApp("exited")
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
	return r, r.clock.now
}

func (r *monRig) setApp(state string) {
	for i := range r.dock.list {
		if r.dock.list[i].ID == "1" {
			r.dock.list[i].State = state
		}
	}
}

// pollUntil reads every 30 seconds as long as the reading is not later than at.
func (r *monRig) pollUntil(at time.Time) {
	r.t.Helper()
	for !r.clock.now.Add(30 * time.Second).After(at) {
		r.poll(30 * time.Second)
	}
}

func (r *monRig) repair() *engine.Repair { return r.eng.states["web"].Repair }

func (r *monRig) halts() (n int) {
	for _, ev := range r.events {
		if ev.Halted {
			n++
		}
	}
	return n
}

func TestMonitorRepairRestartsContainersThenTheStackAndHaltsAfterThree(t *testing.T) {
	r, t0 := failingRig(t)
	if rep := r.repair(); rep == nil || rep.Attempts != 0 || !rep.Next.Equal(t0.Add(time.Minute)) {
		t.Fatalf("first attempt not planned: %+v", rep)
	}
	r.pollUntil(t0.Add(time.Minute).Add(-time.Second))
	if len(r.eng.repairs) != 0 {
		t.Fatalf("attempt before a minute: %+v", r.eng.repairs)
	}
	r.poll(30 * time.Second)
	if len(r.eng.repairs) != 1 || !slices.Equal(r.dock.restarted, []string{"1"}) || r.eng.stackRestarts != 0 {
		t.Fatalf("attempt 1: repairs %+v, containers %v, stack %d", r.eng.repairs, r.dock.restarted, r.eng.stackRestarts)
	}
	a1 := r.eng.repairs[0]
	if a1.Attempts != 1 || !a1.Last.Equal(t0.Add(time.Minute)) || !a1.Next.Equal(a1.Last.Add(5*time.Minute)) {
		t.Fatalf("attempt 1 record %+v", a1)
	}

	r.pollUntil(a1.Last.Add(5 * time.Minute).Add(-time.Second))
	if len(r.eng.repairs) != 1 {
		t.Fatalf("attempt 2 too early: %+v", r.eng.repairs)
	}
	r.poll(30 * time.Second)
	if len(r.eng.repairs) != 2 || r.eng.stackRestarts != 1 || len(r.dock.restarted) != 1 {
		t.Fatalf("attempt 2: repairs %+v, stack %d", r.eng.repairs, r.eng.stackRestarts)
	}
	a2 := r.eng.repairs[1]
	if a2.Attempts != 2 || !a2.Last.Equal(a1.Last.Add(5*time.Minute)) || !a2.Next.Equal(a2.Last.Add(15*time.Minute)) {
		t.Fatalf("attempt 2 record %+v", a2)
	}

	r.pollUntil(a2.Last.Add(15 * time.Minute).Add(-time.Second))
	if len(r.eng.repairs) != 2 {
		t.Fatalf("attempt 3 too early: %+v", r.eng.repairs)
	}
	r.poll(30 * time.Second)
	if len(r.eng.repairs) != 3 || r.eng.stackRestarts != 2 {
		t.Fatalf("attempt 3: repairs %+v, stack %d", r.eng.repairs, r.eng.stackRestarts)
	}
	if a3 := r.eng.repairs[2]; a3.Attempts != 3 || !a3.Next.IsZero() || r.halts() != 0 {
		t.Fatalf("attempt 3 record %+v, halts %d", a3, r.halts())
	}

	// the next reading still fails: the repair halts and says so once
	r.poll(30 * time.Second)
	if r.halts() != 1 {
		t.Fatalf("halts = %d: %+v", r.halts(), r.events)
	}
	ev := r.events[len(r.events)-1]
	if !ev.Halted || ev.Attempts != 3 || ev.Stack != "web" || ev.Kind != engine.FailureExited || ev.Container != "web-app-1" ||
		!ev.Since.Equal(t0) {
		t.Fatalf("halt event %+v", ev)
	}
	if rep := r.repair(); rep == nil || rep.Halted == nil || rep.Attempts != 3 || !rep.Next.IsZero() {
		t.Fatalf("halt record %+v", rep)
	}
	for range 200 {
		r.poll(30 * time.Second)
	}
	if len(r.eng.repairs) != 3 || r.halts() != 1 {
		t.Fatalf("after the halt: repairs %d, halts %d", len(r.eng.repairs), r.halts())
	}
	// a recovery and a new failure of the halted stack are reported, but not repaired
	r.setApp("running")
	r.poll(30 * time.Second)
	r.poll(30 * time.Second)
	r.wantEvents(1, 1)
	if rec := r.events[len(r.events)-1]; !rec.Recovered || rec.Restarted {
		t.Fatalf("recovery of a halted stack %+v", rec)
	}
	r.setApp("exited")
	for range 40 {
		r.poll(30 * time.Second)
	}
	r.wantEvents(2, 1)
	if len(r.eng.repairs) != 3 || r.halts() != 1 {
		t.Fatalf("halted stack repaired: repairs %d, halts %d", len(r.eng.repairs), r.halts())
	}
}

func TestMonitorRepairSuccessIsReportedAsRecoveredByARestart(t *testing.T) {
	r, t0 := failingRig(t)
	r.eng.onRestart = func() { r.setApp("running") }
	r.pollUntil(t0.Add(time.Minute))
	if len(r.eng.repairs) != 1 {
		t.Fatalf("repairs %+v", r.eng.repairs)
	}
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
	r.poll(30 * time.Second)
	r.wantEvents(1, 1)
	rec := r.events[1]
	if !rec.Recovered || !rec.Restarted || !rec.Since.Equal(t0) {
		t.Fatalf("recovery %+v", rec)
	}
	if rep := r.repair(); rep == nil || rep.Attempts != 1 || !rep.Next.IsZero() || !rep.StableSince.Equal(r.clock.now) {
		t.Fatalf("after success %+v", rep)
	}
}

func TestMonitorRecoveryWithoutAttemptIsNoRestart(t *testing.T) {
	r, _ := failingRig(t)
	r.setApp("running")
	r.poll(30 * time.Second)
	r.poll(30 * time.Second)
	r.wantEvents(1, 1)
	if rec := r.events[1]; rec.Restarted || len(r.eng.repairs) != 0 || r.repair() != nil {
		t.Fatalf("recovery %+v, repairs %+v, record %+v", rec, r.eng.repairs, r.repair())
	}
}

func TestMonitorRepairCountResetsOnlyAfterAnHourWithoutFailure(t *testing.T) {
	r, t0 := failingRig(t)
	r.eng.onRestart = func() { r.setApp("running") }
	r.pollUntil(t0.Add(2 * time.Minute))
	r.wantEvents(1, 1)
	stable := r.repair().StableSince

	// a new failure within the hour goes on with the stack, five minutes after the last attempt at the earliest
	r.eng.onRestart = nil
	r.pollUntil(stable.Add(30 * time.Minute))
	r.setApp("exited")
	r.poll(30 * time.Second)
	r.wantEvents(2, 1)
	t1 := r.clock.now
	r.pollUntil(t1.Add(time.Minute))
	if len(r.eng.repairs) != 2 || r.eng.stackRestarts != 1 || r.eng.repairs[1].Attempts != 2 {
		t.Fatalf("second failure: repairs %+v, stack %d", r.eng.repairs, r.eng.stackRestarts)
	}

	// it ends on its own; the hour counts from the end of the failure
	r.setApp("running")
	r.poll(30 * time.Second)
	r.poll(30 * time.Second)
	r.wantEvents(2, 2)
	if !r.events[3].Restarted {
		t.Fatalf("recovery after attempt 2 %+v", r.events[3])
	}
	end := r.repair().StableSince
	r.pollUntil(end.Add(time.Hour).Add(-time.Second))
	if rep := r.repair(); rep == nil || rep.Attempts != 2 {
		t.Fatalf("reset before the hour: %+v", rep)
	}
	r.poll(30 * time.Second)
	if rep := r.repair(); rep != nil {
		t.Fatalf("not reset after the hour: %+v", rep)
	}

	// the count starts afresh: the containers again, after a minute
	r.dock.restarted = nil
	r.setApp("exited")
	r.poll(30 * time.Second)
	t2 := r.clock.now
	r.pollUntil(t2.Add(time.Minute))
	if len(r.eng.repairs) != 3 || r.eng.repairs[2].Attempts != 1 || !slices.Equal(r.dock.restarted, []string{"1"}) {
		t.Fatalf("after reset: repairs %+v, containers %v", r.eng.repairs, r.dock.restarted)
	}
}

func TestMonitorRepairCountSurvivesAMonitorRestart(t *testing.T) {
	r, t0 := failingRig(t)
	r.pollUntil(t0.Add(time.Minute))
	if len(r.eng.repairs) != 1 {
		t.Fatalf("repairs %+v", r.eng.repairs)
	}
	last := r.eng.repairs[0].Last
	r.restart()
	r.pollUntil(last.Add(5 * time.Minute).Add(-time.Second))
	if len(r.eng.repairs) != 1 {
		t.Fatalf("restart repeated an attempt: %+v", r.eng.repairs)
	}
	r.poll(30 * time.Second)
	if len(r.eng.repairs) != 2 || r.eng.repairs[1].Attempts != 2 || r.eng.stackRestarts != 1 {
		t.Fatalf("after restart: repairs %+v, stack %d", r.eng.repairs, r.eng.stackRestarts)
	}
	r.restart()
	r.pollUntil(r.eng.repairs[1].Last.Add(15 * time.Minute))
	r.restart()
	r.poll(30 * time.Second)
	if len(r.eng.repairs) != 3 || r.halts() != 1 {
		t.Fatalf("limit across restarts: repairs %d, halts %d", len(r.eng.repairs), r.halts())
	}
	r.restart()
	for range 10 {
		r.poll(30 * time.Second)
	}
	if len(r.eng.repairs) != 3 || r.halts() != 1 {
		t.Fatalf("halt across a restart: repairs %d, halts %d", len(r.eng.repairs), r.halts())
	}
}

func TestMonitorRepairWaitsForAFreeSlot(t *testing.T) {
	r, t0 := failingRig(t)
	// a run is active at the time the attempt is due: the stack is not evaluated
	r.pollUntil(t0.Add(30 * time.Second))
	r.eng.active["web"] = "20260504T030000Z-deadbeef"
	r.pollUntil(t0.Add(2 * time.Minute))
	if len(r.eng.repairs) != 0 {
		t.Fatalf("attempt during a run: %+v", r.eng.repairs)
	}
	delete(r.eng.active, "web")
	r.poll(30 * time.Second)
	if len(r.eng.repairs) != 1 {
		t.Fatalf("attempt not made once the slot was free: %+v", r.eng.repairs)
	}

	// the slot is taken between the reading and the attempt: refused, nothing counted, tried again
	r2, u0 := failingRig(t)
	r2.pollUntil(u0.Add(30 * time.Second))
	refused := 0
	r2.mon.Engine = &busyOnce{monEngine: r2.eng, left: 2, refused: &refused}
	r2.pollUntil(u0.Add(2 * time.Minute))
	if refused != 2 || len(r2.eng.repairs) != 1 || r2.eng.repairs[0].Attempts != 1 {
		t.Fatalf("refused %d, repairs %+v", refused, r2.eng.repairs)
	}
	if strings.Contains(r2.logs.String(), "refused") {
		t.Fatalf("a busy slot was logged as a refusal:\n%s", r2.logs.String())
	}
}

// busyOnce answers the first left repair attempts as a busy slot.
type busyOnce struct {
	*monEngine
	left    int
	refused *int
}

func (b *busyOnce) Repair(ctx context.Context, stack string, rec engine.Repair, restart func(context.Context) error) (bool, error) {
	if b.left > 0 {
		b.left--
		*b.refused++
		return false, engine.ErrRunActive
	}
	return b.monEngine.Repair(ctx, stack, rec, restart)
}

func TestMonitorRepairEndsQuietlyWhenTheStackIsStoppedOrPaused(t *testing.T) {
	for name, set := range map[string]func(*engine.StackState, time.Time){
		"stopped":        func(s *engine.StackState, at time.Time) { s.Stopped = &engine.Hold{Since: at} },
		"operator pause": func(s *engine.StackState, at time.Time) { s.OperatorPause = &engine.Hold{Since: at} },
		"rollback pause": func(s *engine.StackState, at time.Time) { s.Paused = &engine.Pause{Time: at} },
		"removed":        func(s *engine.StackState, _ time.Time) { s.Removed = true },
	} {
		t.Run(name, func(t *testing.T) {
			r, t0 := failingRig(t)
			r.pollUntil(t0.Add(time.Minute))
			if len(r.eng.repairs) != 1 {
				t.Fatalf("repairs %+v", r.eng.repairs)
			}
			s := r.eng.states["web"]
			set(&s, r.clock.now)
			r.eng.states["web"] = s
			for range 60 {
				r.poll(30 * time.Second)
			}
			if len(r.eng.repairs) != 1 || r.halts() != 0 || len(r.events) != 1 {
				t.Fatalf("repairs %d, events %+v", len(r.eng.repairs), r.events)
			}
			if rep := r.repair(); rep == nil || rep.Attempts != 1 || !rep.Next.IsZero() {
				t.Fatalf("record %+v", rep)
			}
		})
	}
}

func TestMonitorNeverRepairsWhatItMustNot(t *testing.T) {
	mark := &engine.Hold{Since: time.Date(2026, 5, 4, 2, 0, 0, 0, time.UTC)}
	for name, set := range map[string]func(*monRig){
		"stopped":        func(r *monRig) { s := r.eng.states["web"]; s.Stopped = mark; r.eng.states["web"] = s },
		"operator pause": func(r *monRig) { s := r.eng.states["web"]; s.OperatorPause = mark; r.eng.states["web"] = s },
		"rollback pause": func(r *monRig) {
			s := r.eng.states["web"]
			s.Paused = &engine.Pause{Time: mark.Since}
			r.eng.states["web"] = s
		},
		"foreign": func(r *monRig) {
			r.dock.list[0].Labels["com.docker.compose.project.working_dir"] = "/elsewhere/web"
			r.dock.list = append(r.dock.list, ctr("2", "db"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newMonRig(t)
			a := ctr("1", "app")
			a.State = "exited"
			r.dock.list = []docker.Container{a}
			set(r)
			for range 60 {
				r.poll(30 * time.Second)
			}
			if len(r.eng.repairs) != 0 || len(r.dock.restarted) != 0 || r.repair() != nil || len(r.events) != 0 {
				t.Fatalf("repairs %+v, restarted %v, record %+v, events %+v", r.eng.repairs, r.dock.restarted, r.repair(), r.events)
			}
		})
	}
}

func TestMonitorRepairWorksAgainAfterResume(t *testing.T) {
	r, t0 := failingRig(t)
	r.pollUntil(t0.Add(22 * time.Minute))
	if r.halts() != 1 || len(r.eng.repairs) != 3 {
		t.Fatalf("not halted: repairs %d, halts %d", len(r.eng.repairs), r.halts())
	}
	// what Resume does to a halted repair
	s := r.eng.states["web"]
	s.Repair = nil
	r.eng.states["web"] = s
	r.dock.restarted = nil
	r.poll(30 * time.Second)
	if len(r.eng.repairs) != 4 || r.eng.repairs[3].Attempts != 1 || !slices.Equal(r.dock.restarted, []string{"1"}) {
		t.Fatalf("after resume: repairs %+v, containers %v", r.eng.repairs, r.dock.restarted)
	}
}

func TestMonitorRepairOfAMissingServiceRestartsTheStack(t *testing.T) {
	r := newMonRig(t)
	r.dock.list = []docker.Container{ctr("1", "app"), ctr("2", "db")}
	r.poll(30 * time.Second)
	r.dock.list = []docker.Container{ctr("1", "app")}
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
	r.poll(time.Minute)
	if len(r.eng.repairs) != 1 || r.eng.stackRestarts != 1 || len(r.dock.restarted) != 0 {
		t.Fatalf("repairs %+v, stack %d, containers %v", r.eng.repairs, r.eng.stackRestarts, r.dock.restarted)
	}
}
