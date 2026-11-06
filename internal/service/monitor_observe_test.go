package service

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/docker"

	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/notifier"
)

// observeRig is a monitor rig whose stack web runs the update of commit a…, under observation from since for
// an hour. Its app container exits at the first reading after the call, t0.
func observeRig(t *testing.T, since func(t0 time.Time) time.Time) (r *monRig, t0 time.Time) {
	t.Helper()
	r = newMonRig(t)
	r.eng.now = func() time.Time { return r.clock.now }
	r.dock.list = []docker.Container{ctr("1", "app"), ctr("2", "db")}
	r.poll(30 * time.Second)
	st := r.eng.states["web"]
	s := since(r.clock.now.Add(30 * time.Second)).UTC()
	st.Observe = &engine.Observation{Run: "20260504T025500Z-00000001", From: strings.Repeat("b", 40), Commit: st.Deployed,
		Backup: "20260504T025400Z-00000002", Since: s, Until: s.Add(time.Hour)}
	r.eng.states["web"] = st
	r.setApp("exited")
	r.poll(30 * time.Second)
	r.wantEvents(1, 0)
	return r, r.clock.now
}

func TestMonitorRollsBackAnUpdateThatFailsInItsObservationTime(t *testing.T) {
	r, t0 := observeRig(t, func(t0 time.Time) time.Time { return t0.Add(-59 * time.Minute) })
	r.pollUntil(t0.Add(time.Minute))
	if len(r.eng.repairs) != 1 || !slices.Equal(r.dock.restarted, []string{"1"}) || len(r.eng.rollbacks) != 0 {
		t.Fatalf("attempt 1: repairs %+v, containers %v, rollbacks %v", r.eng.repairs, r.dock.restarted, r.eng.rollbacks)
	}
	a1 := r.eng.repairs[0]
	if a1.Attempts != 1 || !a1.Next.Equal(a1.Last.Add(5*time.Minute)) {
		t.Fatalf("attempt 1 record %+v", a1)
	}
	// the decision survives a restart of the service; the failure began before the end of the observation
	// time, which has passed by the rollback
	r.restart()
	r.pollUntil(a1.Last.Add(5 * time.Minute).Add(-time.Second))
	if len(r.eng.rollbacks) != 0 {
		t.Fatalf("rollback too early: %v", r.eng.rollbacks)
	}
	r.poll(30 * time.Second)
	if !slices.Equal(r.eng.rollbacks, []string{strings.Repeat("a", 40)}) || len(r.eng.repairs) != 1 || r.eng.stackRestarts != 0 ||
		r.halts() != 0 {
		t.Fatalf("rollback %v, repairs %+v, stack restarts %d", r.eng.rollbacks, r.eng.repairs, r.eng.stackRestarts)
	}
	if !strings.Contains(r.logs.String(), "update rollback started") {
		t.Fatalf("log:\n%s", r.logs)
	}
	// the rolled back stack runs again: that is no recovery by a restart
	r.setApp("running")
	r.poll(30 * time.Second)
	r.poll(30 * time.Second)
	r.wantEvents(1, 1)
	if rec := r.events[len(r.events)-1]; !rec.Recovered || rec.Restarted {
		t.Fatalf("recovery %+v", rec)
	}
	if len(r.eng.rollbacks) != 1 {
		t.Fatalf("rollbacks %v", r.eng.rollbacks)
	}
}

func TestMonitorOutsideTheObservationTimeOnlyRestarts(t *testing.T) {
	for name, since := range map[string]func(time.Time) time.Time{
		"failure after it":  func(t0 time.Time) time.Time { return t0.Add(-time.Hour) },
		"failure before it": func(t0 time.Time) time.Time { return t0.Add(time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			r, t0 := observeRig(t, since)
			r.pollUntil(t0.Add(time.Minute + 5*time.Minute + 15*time.Minute))
			if len(r.eng.rollbacks) != 0 || len(r.eng.repairs) != 3 || r.eng.stackRestarts != 2 {
				t.Fatalf("rollbacks %v, repairs %+v, stack restarts %d", r.eng.rollbacks, r.eng.repairs, r.eng.stackRestarts)
			}
		})
	}
}

func TestMonitorCountsOnlyRestartsSinceTheUpdate(t *testing.T) {
	r, t0 := observeRig(t, func(t0 time.Time) time.Time { return t0.Add(-10 * time.Minute) })
	st := r.eng.states["web"]
	st.Repair = &engine.Repair{Attempts: 2, Last: st.Observe.Since.Add(-20 * time.Minute)}
	r.eng.states["web"] = st
	r.pollUntil(t0.Add(time.Minute))
	if len(r.eng.repairs) != 1 || r.eng.repairs[0].Attempts != 3 || !slices.Equal(r.dock.restarted, []string{"1"}) {
		t.Fatalf("attempt after the update: %+v, containers %v", r.eng.repairs, r.dock.restarted)
	}
	r.pollUntil(r.eng.repairs[0].Last.Add(5 * time.Minute))
	if len(r.eng.rollbacks) != 1 || r.halts() != 0 || len(r.eng.repairs) != 1 {
		t.Fatalf("rollbacks %v, halts %d, repairs %+v", r.eng.rollbacks, r.halts(), r.eng.repairs)
	}
}

func TestMonitorHaltsWhenTheBackupOfTheUpdateIsMissing(t *testing.T) {
	r, t0 := observeRig(t, func(t0 time.Time) time.Time { return t0.Add(-time.Minute) })
	r.eng.rollbackErr = fmt.Errorf("%w: 20260504T025400Z-00000002", engine.ErrBackupMissing)
	r.pollUntil(t0.Add(time.Minute + 5*time.Minute))
	if r.halts() != 1 || len(r.eng.repairs) != 1 {
		t.Fatalf("halts %d, repairs %+v: %+v", r.halts(), r.eng.repairs, r.events)
	}
	ev := r.events[len(r.events)-1]
	if !ev.Halted || ev.Cause != notifier.CauseBackupMissing || ev.Attempts != 1 || !strings.Contains(ev.Error, "backup of the update is missing") ||
		!ev.Since.Equal(t0) || ev.Kind != engine.FailureExited {
		t.Fatalf("halt event %+v", ev)
	}
	if rep := r.repair(); rep == nil || rep.Halted == nil || rep.Attempts != 1 || !rep.Next.IsZero() {
		t.Fatalf("repair %+v", rep)
	}
	for range 60 {
		r.poll(30 * time.Second)
	}
	if r.halts() != 1 || len(r.eng.repairs) != 1 || r.eng.stackRestarts != 0 {
		t.Fatalf("after the halt: halts %d, repairs %d", r.halts(), len(r.eng.repairs))
	}
}

func TestMonitorRetriesARefusedRollback(t *testing.T) {
	r, t0 := observeRig(t, func(t0 time.Time) time.Time { return t0.Add(-time.Minute) })
	r.eng.rollbackErr = engine.ErrRunActive
	r.pollUntil(t0.Add(time.Minute + 5*time.Minute))
	r.poll(30 * time.Second)
	if len(r.eng.rollbacks) != 0 || r.halts() != 0 || len(r.eng.repairs) != 1 {
		t.Fatalf("rollbacks %v, halts %d, repairs %d", r.eng.rollbacks, r.halts(), len(r.eng.repairs))
	}
	r.eng.rollbackErr = nil
	r.poll(30 * time.Second)
	if len(r.eng.rollbacks) != 1 {
		t.Fatalf("rollbacks %v", r.eng.rollbacks)
	}
}
