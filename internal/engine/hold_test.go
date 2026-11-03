package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestHoldsAreIdempotentAndUnknownStacksRefused(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, "web")
	for name, set := range map[string]func(context.Context, string, bool) (bool, error){"pause": r.eng.SetPause, "pin": r.eng.SetPin} {
		if _, err := set(ctx, "nope", true); !errors.Is(err, ErrUnknownStack) {
			t.Fatalf("%s unknown: %v", name, err)
		}
		if _, err := set(ctx, "nope", false); !errors.Is(err, ErrUnknownStack) {
			t.Fatalf("un%s unknown: %v", name, err)
		}
		if _, err := set(ctx, "../web", true); !errors.Is(err, ErrUnknownStack) {
			t.Fatalf("%s odd name: %v", name, err)
		}
		if ch, err := set(ctx, "web", false); err != nil || ch {
			t.Fatalf("un%s unset: %v %v", name, ch, err)
		}
		if ch, err := set(ctx, "web", true); err != nil || !ch {
			t.Fatalf("%s: %v %v", name, ch, err)
		}
		since := r.stack("web").OperatorPause
		if name == "pin" {
			since = r.stack("web").Pin
		}
		r.clock.Advance(1)
		if ch, err := set(ctx, "web", true); err != nil || ch {
			t.Fatalf("%s again: %v %v", name, ch, err)
		}
		again := r.stack("web").OperatorPause
		if name == "pin" {
			again = r.stack("web").Pin
		}
		if since == nil || again == nil || !since.Since.Equal(again.Since) {
			t.Fatalf("%s time changed: %v %v", name, since, again)
		}
		if ch, err := set(ctx, "web", false); err != nil || !ch {
			t.Fatalf("un%s: %v %v", name, ch, err)
		}
	}
	if st := r.stack("web"); st.OperatorPause != nil || st.Pin != nil {
		t.Fatalf("marks left: %+v", st)
	}
}

func TestHoldsSurviveRestartAndRunsAndAreIndependentOfRollbackPause(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, "web")
	r.deployAll()
	if _, err := r.eng.SetPause(ctx, "web", true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.eng.SetPin(ctx, "web", true); err != nil {
		t.Fatal(err)
	}
	// a new commit is still deployed by the poll, and a manual run still works
	r.copy.set(func(f *fakeCopy) {
		f.head, f.remote = commit(2), commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
	})
	if res := r.poll(); !slices.Equal(res.Started, []string{"web"}) {
		t.Fatalf("poll: %+v", res)
	}
	if _, err := r.eng.Trigger(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	r.eng.Wait()
	r.eng = r.start()
	st := r.stack("web")
	if st.OperatorPause == nil || st.Pin == nil || st.Paused != nil || hasFinding(st, FindingPaused) {
		t.Fatalf("after restart: %+v", st)
	}
	if _, err := r.eng.Resume("web"); !errors.Is(err, ErrNotPaused) {
		t.Fatalf("resume must not touch the operator pause: %v", err)
	}
	if r.stack("web").OperatorPause == nil {
		t.Fatal("resume cleared the operator pause")
	}
}

func TestWindowEligibility(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, "web")
	r.deployAll()
	check := func(want ...string) {
		t.Helper()
		v, err := r.eng.WindowEligibility("web")
		if err != nil || v.Allowed != (len(want) == 0) || !slices.Equal(v.Reasons, want) {
			t.Fatalf("verdict = %+v, %v; want %v", v, err, want)
		}
	}
	check()
	if v, err := r.eng.WindowEligibility("unseen"); err != nil || !v.Allowed {
		t.Fatalf("unseen = %+v, %v", v, err)
	}
	_, _ = r.eng.SetPin(ctx, "web", true)
	check(BlockPin)
	_, _ = r.eng.SetPause(ctx, "web", true)
	check(BlockPause, BlockPin)
	_, err := r.eng.store.Update(func(s State) (State, error) {
		cur := s.Stacks["web"]
		cur.Paused = &Pause{Time: r.clock.Now(), Run: "20260102T030405Z-0000abcd"}
		s.Stacks["web"] = cur
		return s, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	check(BlockRollback, BlockPause, BlockPin)
	if _, err := r.eng.Resume("web"); err != nil {
		t.Fatal(err)
	}
	check(BlockPause, BlockPin)
	_, _ = r.eng.SetPause(ctx, "web", false)
	_, _ = r.eng.SetPin(ctx, "web", false)
	check()
}

func TestHoldsOfARemovedStack(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, "web", "old")
	r.deployAll()
	if _, err := r.eng.SetPause(ctx, "old", true); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(r.wc, "old")); err != nil {
		t.Fatal(err)
	}
	r.poll()
	st := r.stack("old")
	if !st.Removed || st.OperatorPause == nil {
		t.Fatalf("old = %+v", st)
	}
	if v, _ := r.eng.WindowEligibility("old"); v.Allowed || !slices.Contains(v.Reasons, BlockRemoved) {
		t.Fatalf("verdict = %+v", v)
	}
	if _, err := r.eng.SetPin(ctx, "old", true); !errors.Is(err, ErrUnknownStack) {
		t.Fatalf("pin of a removed stack: %v", err)
	}
	if ch, err := r.eng.SetPause(ctx, "old", false); err != nil || !ch {
		t.Fatalf("unpause of a removed stack: %v %v", ch, err)
	}
}

func TestInvalidHoldIsRejectedByValidation(t *testing.T) {
	s := State{Schema: stateSchema, Stacks: map[string]StackState{"web": {Pin: &Hold{}}}}
	if err := validateState(s); err == nil {
		t.Fatal("zero hold accepted")
	}
}
