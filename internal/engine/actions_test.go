package engine

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestStopMarksStackAndOnlyStartClearsIt(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, "web")
	r.deployAll()

	if err := r.eng.StackAction(ctx, "web", ActionStop); err != nil {
		t.Fatal(err)
	}
	if st := r.stack("web"); st.Stopped == nil {
		t.Fatalf("no mark after stop: %+v", st)
	}
	if got := r.dep.stepsOf("web"); !slices.Equal(got, []string{"stop"}) {
		t.Fatalf("steps = %v", got)
	}

	// the mark survives a reload of the state
	r.eng = r.start()
	if r.stack("web").Stopped == nil {
		t.Fatal("mark lost after restart")
	}

	// a new commit starts no run and is reported, and the window refuses the stack
	r.copy.set(func(f *fakeCopy) {
		f.head, f.remote = commit(2), commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
	})
	if res := r.poll(); len(res.Started) != 0 {
		t.Fatalf("poll started %v", res.Started)
	}
	st := r.stack("web")
	if !hasFinding(st, FindingStopped) || st.Pending != commit(2) || st.Deployed != commit(1) {
		t.Fatalf("after poll: %+v", st)
	}
	if v, err := r.eng.WindowEligibility("web"); err != nil || v.Allowed || !slices.Equal(v.Reasons, []string{BlockStopped}) {
		t.Fatalf("window = %+v, %v", v, err)
	}

	// restart is refused and runs nothing
	if err := r.eng.StackAction(ctx, "web", ActionRestart); !errors.Is(err, ErrStackStopped) {
		t.Fatalf("restart: %v", err)
	}
	if got := r.dep.stepsOf("web"); slices.Contains(got, "restart") {
		t.Fatalf("steps = %v", got)
	}

	// start clears the mark and checks; the next poll deploys the pending commit
	if err := r.eng.StackAction(ctx, "web", ActionStart); err != nil {
		t.Fatal(err)
	}
	if got := r.dep.stepsOf("web"); !slices.Equal(got[len(got)-2:], []string{"start", "check"}) {
		t.Fatalf("steps = %v", got)
	}
	if r.stack("web").Stopped != nil {
		t.Fatal("mark left after start")
	}
	if res := r.poll(); !slices.Equal(res.Started, []string{"web"}) {
		t.Fatalf("poll after start: %+v", res)
	}
}

func TestStackActionFailureKeepsStateAndRestartChecks(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, "web")
	r.deployAll()

	r.dep.fail("stop", errors.New("stop failed"))
	if err := r.eng.StackAction(ctx, "web", ActionStop); err == nil {
		t.Fatal("stop failure not reported")
	}
	if r.stack("web").Stopped != nil {
		t.Fatal("mark set by a failed stop")
	}
	if err := r.eng.StackAction(ctx, "web", ActionRestart); err != nil {
		t.Fatal(err)
	}
	if got := r.dep.stepsOf("web"); !slices.Equal(got[len(got)-2:], []string{"restart", "check"}) {
		t.Fatalf("steps = %v", got)
	}
	if err := r.eng.StackAction(ctx, "nope", ActionStop); !errors.Is(err, ErrUnknownStack) {
		t.Fatalf("unknown: %v", err)
	}
	if err := r.eng.StackAction(ctx, "web", "reboot"); err == nil {
		t.Fatal("unknown action accepted")
	}
}

func TestStoppedStackRefusesEveryRunUntilStarted(t *testing.T) {
	ctx := context.Background()
	r := updateRig(t)
	if err := r.eng.StackAction(ctx, "web", ActionStop); err != nil {
		t.Fatal(err)
	}
	r.dep.mu.Lock()
	r.dep.steps = nil
	r.dep.mu.Unlock()
	envBefore := r.env()

	if _, err := r.eng.Trigger(ctx, "web"); !errors.Is(err, ErrStackStopped) {
		t.Fatalf("trigger: %v", err)
	}
	if _, err := r.eng.Update(ctx, "web", []VersionStep{step("1.0.0", "1.0.1")}); !errors.Is(err, ErrStackStopped) {
		t.Fatalf("update: %v", err)
	}
	if got := r.dep.stepsOf("web"); len(got) != 0 {
		t.Fatalf("deployer called: %v", got)
	}
	if r.env() != envBefore {
		t.Fatal("a refused update changed the working copy")
	}

	if err := r.eng.StackAction(ctx, "web", ActionStart); err != nil {
		t.Fatal(err)
	}
	if _, err := r.eng.Trigger(ctx, "web"); err != nil {
		t.Fatalf("trigger after start: %v", err)
	}
	r.eng.Wait()
	if res := r.update(step("1.0.0", "1.0.1")); len(res) == 0 {
		t.Fatal("update after start did nothing")
	}
}
