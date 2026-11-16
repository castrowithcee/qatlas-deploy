package engine

import (
	"context"
	"testing"
)

// watchResult installs the engine's after-result seam. At that point the result is stored; a result a client
// can see must not leave the slot occupied for anyone who could look at it, so either the slot is free or
// the writer still holds e.mu. steps is the number of results the test's runs store; only the last frees the slot.
func watchResult(t *testing.T, e *Engine, stack string, steps int) {
	t.Helper()
	calls := 0
	e.afterResult = func(name string) {
		calls++
		runs, err := e.Runs(name)
		if err != nil || len(runs) == 0 || runs[0].Result != ResultSucceeded {
			t.Errorf("call %d: result not stored: %v %+v", calls, err, runs)
			return
		}
		if !e.mu.TryLock() {
			return // the writer frees the slot in the same critical section
		}
		_, busy := e.active[name]
		e.mu.Unlock()
		if last := calls == steps; last && busy {
			t.Errorf("call %d: the result is visible while the slot is still taken", calls)
		} else if !last && !busy {
			t.Errorf("call %d: an intermediate step gave up the slot", calls)
		}
	}
}

func TestRunFreesSlotWithItsVisibleResult(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	watchResult(t, r.eng, "web", 1)
	if _, err := r.eng.Trigger(context.Background(), "web"); err != nil {
		t.Fatal(err)
	}
	r.eng.Wait()
	if _, err := r.eng.Trigger(context.Background(), "web"); err != nil {
		t.Fatalf("trigger after the result: %v", err)
	}
	r.eng.Wait()
	if d := r.stack("web").Dropped; d != nil {
		t.Fatalf("dropped = %+v", d)
	}
}

func TestUpdateFreesSlotWithItsLastVisibleResult(t *testing.T) {
	r := updateRig(t)
	watchResult(t, r.eng, "web", 2)
	res := r.update(step("1.0.0", "1.2.0"), step("1.2.0", "2.3.0"))
	if res[1].Result != ResultSucceeded {
		t.Fatalf("res = %+v", res)
	}
	if _, err := r.eng.Trigger(context.Background(), "web"); err != nil {
		t.Fatalf("trigger after the update: %v", err)
	}
	r.eng.Wait()
}
