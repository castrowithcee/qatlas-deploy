package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

func TestRepairStoresTheAttemptAndRestartsUnderTheSlot(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, "web")
	r.deployAll()
	now := r.clock.Now()

	// the containers: the record is stored before the restart, which runs under the slot
	rec := Repair{Attempts: 1, Last: now, Next: now.Add(5 * time.Minute)}
	var seen Repair
	var active string
	made, err := r.eng.Repair(ctx, "web", rec, func(context.Context) error {
		if st := r.stack("web"); st.Repair != nil {
			seen = *st.Repair
		}
		snap, _ := r.eng.Snapshot()
		active = snap.Active["web"]
		return nil
	})
	if err != nil || !made || seen.Attempts != 1 || !seen.Next.Equal(rec.Next) || active != "repair" {
		t.Fatalf("repair = %v %v, stored %+v, active %q", made, err, seen, active)
	}
	if got := r.dep.stepsOf("web"); len(got) != 0 {
		t.Fatalf("a container repair ran deployer steps %v", got)
	}

	// the whole stack: restart and check through the deployer
	rec = Repair{Attempts: 2, Last: now, Next: now.Add(15 * time.Minute)}
	if made, err := r.eng.Repair(ctx, "web", rec, nil); err != nil || !made {
		t.Fatalf("stack repair = %v %v", made, err)
	}
	if got := r.dep.stepsOf("web"); !slices.Equal(got, []string{"restart", "check"}) {
		t.Fatalf("steps = %v", got)
	}

	// the count survives a restart of the service
	r.eng = r.start()
	if st := r.stack("web"); st.Repair == nil || st.Repair.Attempts != 2 || !st.Repair.Last.Equal(now) {
		t.Fatalf("after restart: %+v", st.Repair)
	}

	// a failed restart still counts
	r.dep.mu.Lock()
	r.dep.errs = map[string][]error{"restart": {errors.New("compose failed")}}
	r.dep.mu.Unlock()
	if made, err := r.eng.Repair(ctx, "web", Repair{Attempts: 3, Last: now}, nil); err == nil || !made {
		t.Fatalf("failed repair = %v %v", made, err)
	}
	if st := r.stack("web"); st.Repair == nil || st.Repair.Attempts != 3 {
		t.Fatalf("failed attempt not counted: %+v", st.Repair)
	}
}

func TestRepairIsRefusedWhereItMustNotRun(t *testing.T) {
	ctx := context.Background()
	mark := &Hold{Since: time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)}
	cases := map[string]func(*StackState){
		"stopped":        func(s *StackState) { s.Stopped = mark },
		"operator pause": func(s *StackState) { s.OperatorPause = mark },
		"rollback pause": func(s *StackState) { s.Paused = &Pause{Time: mark.Since, Run: "20260102T030405Z-0000abcd"} },
		"halted":         func(s *StackState) { s.Repair = &Repair{Attempts: 3, Halted: mark} },
		"removed":        func(s *StackState) { s.Removed = true },
		"never deployed": func(s *StackState) { s.Deployed = "" },
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, "web")
			r.deployAll()
			before := r.stack("web").Repair
			if _, err := r.eng.store.Update(func(s State) (State, error) {
				cur := s.Stacks["web"]
				set(&cur)
				before = cur.Repair
				s.Stacks["web"] = cur
				return s, nil
			}); err != nil {
				t.Fatal(err)
			}
			called := false
			made, err := r.eng.Repair(ctx, "web", Repair{Attempts: 1, Last: r.clock.Now()}, func(context.Context) error {
				called = true
				return nil
			})
			if !errors.Is(err, ErrNotRepairable) || made || called {
				t.Fatalf("repair = %v %v, restarted %v", made, err, called)
			}
			if got := r.stack("web").Repair; (got == nil) != (before == nil) || (got != nil && got.Attempts != before.Attempts) {
				t.Fatalf("record changed: %+v", got)
			}
		})
	}

	t.Run("busy slot", func(t *testing.T) {
		r := newRig(t, "web")
		r.deployAll()
		in, release := make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- r.eng.WithStack(ctx, "web", "backup", func(context.Context, stacks.Stack, string) error {
				close(in)
				<-release
				return nil
			})
		}()
		<-in
		made, err := r.eng.Repair(ctx, "web", Repair{Attempts: 1, Last: r.clock.Now()}, func(context.Context) error { return nil })
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if !errors.Is(err, ErrRunActive) || made || r.stack("web").Repair != nil {
			t.Fatalf("busy repair = %v %v, record %+v", made, err, r.stack("web").Repair)
		}
	})
}

func TestRecordRepairKeepsAHaltAndResumeEndsIt(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	now := r.clock.Now()
	if stored, err := r.eng.RecordRepair("unseen", &Repair{Attempts: 1, Last: now}); err != nil || stored {
		t.Fatalf("unseen = %v %v", stored, err)
	}
	if _, err := r.eng.RecordRepair("web", &Repair{Attempts: 4}); err == nil {
		t.Fatal("more attempts than allowed accepted")
	}
	halt := &Repair{Attempts: 3, Last: now, Halted: &Hold{Since: now}}
	if stored, err := r.eng.RecordRepair("web", halt); err != nil || !stored {
		t.Fatalf("halt = %v %v", stored, err)
	}
	// only resume ends a halt
	for _, rec := range []*Repair{nil, {Attempts: 1, Last: now}, halt} {
		if stored, err := r.eng.RecordRepair("web", rec); err != nil || stored {
			t.Fatalf("overwrote the halt with %+v: %v %v", rec, stored, err)
		}
	}
	// the halt blocks no run
	if _, err := r.eng.Trigger(context.Background(), "web"); err != nil {
		t.Fatalf("trigger of a halted stack: %v", err)
	}
	r.eng.Wait()
	if st := r.stack("web"); st.Repair == nil || st.Repair.Halted == nil {
		t.Fatalf("a run ended the halt: %+v", st.Repair)
	}

	p, err := r.eng.Resume("web")
	if err != nil || p != (Pause{}) {
		t.Fatalf("resume = %+v, %v", p, err)
	}
	if st := r.stack("web"); st.Repair != nil {
		t.Fatalf("after resume: %+v", st.Repair)
	}
	if _, err := r.eng.Resume("web"); !errors.Is(err, ErrNotPaused) {
		t.Fatalf("second resume: %v", err)
	}
	// a repair that is not halted is not resumed
	if _, err := r.eng.RecordRepair("web", &Repair{Attempts: 2, Last: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.eng.Resume("web"); !errors.Is(err, ErrNotPaused) || r.stack("web").Repair == nil {
		t.Fatalf("resume without halt: %v %+v", err, r.stack("web").Repair)
	}
}

func TestResumeClearsBothPauseAndHalt(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	now := r.clock.Now()
	pause := Pause{Time: now, Run: "20260102T030405Z-0000abcd", Reason: "rollback failed"}
	if _, err := r.eng.store.Update(func(s State) (State, error) {
		cur := s.Stacks["web"]
		cur.Paused, cur.Repair = &pause, &Repair{Attempts: 3, Last: now, Halted: &Hold{Since: now}}
		s.Stacks["web"] = cur
		return s, nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := r.eng.Resume("web")
	if err != nil || got != pause {
		t.Fatalf("resume = %+v, %v", got, err)
	}
	if st := r.stack("web"); st.Paused != nil || st.Repair != nil {
		t.Fatalf("after resume: %+v", st)
	}
}

func TestStateWithoutRepairLoadsAsNoRepair(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	raw, err := os.ReadFile(filepath.Join(r.state, engineDir, stateFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"repair"`) {
		t.Fatalf("a stack without repair wrote one: %s", raw)
	}
	r.eng = r.start()
	if st := r.stack("web"); st.Repair != nil {
		t.Fatalf("repair = %+v", st.Repair)
	}
}
