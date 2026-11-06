package engine

import (
	"context"
	"errors"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// ErrNotRepairable means a repair attempt was refused: the stack is unknown to the state, never deployed,
// removed, stopped, paused or its repair is halted.
var ErrNotRepairable = errors.New("the stack may not be repaired")

func repairable(st StackState) bool {
	return st.Deployed != "" && !st.Removed && st.Stopped == nil && st.Paused == nil && st.OperatorPause == nil &&
		(st.Repair == nil || st.Repair.Halted == nil)
}

func cleanRepair(r Repair) Repair {
	r.Last, r.Next, r.StableSince = r.Last.UTC(), r.Next.UTC(), r.StableSince.UTC()
	if r.Halted != nil {
		h := Hold{Since: r.Halted.Since.UTC()}
		r.Halted = &h
	}
	return r
}

// Repair makes one repair attempt of the container monitor under the stack's run slot, so it never overlaps a
// run or an action. It stores rec before it restarts, so an attempt counts even if the service ends during it,
// and then calls restart; nil restarts the whole stack and runs its check as StackAction does. made tells
// whether the attempt was stored. ErrNotRepairable when the stack may not be repaired; the refusals of
// WithStack otherwise.
func (e *Engine) Repair(ctx context.Context, name string, rec Repair, restart func(ctx context.Context) error) (made bool, err error) {
	d := e.cfg.Deployer
	if restart == nil && d == nil {
		return false, ErrNoDeployer
	}
	rec = cleanRepair(rec)
	if err := validateMonitor(StackState{Repair: &rec}); err != nil {
		return false, err
	}
	err = e.WithStack(ctx, name, "repair", func(ctx context.Context, st stacks.Stack, _ string) error {
		_, err := e.store.Update(func(s State) (State, error) {
			s = normalize(s)
			cur, ok := s.Stacks[name]
			if !ok || !repairable(cur) {
				return s, ErrNotRepairable
			}
			r := rec
			cur.Repair = &r
			s.Stacks[name] = cur
			return s, nil
		})
		if err != nil {
			return err
		}
		made = true
		if restart != nil {
			return restart(ctx)
		}
		if err := d.Restart(ctx, st, nil); err != nil {
			return err
		}
		return d.Check(ctx, st, nil)
	})
	return made, err
}

// RecordRepair stores the monitor's repair record of a stack outside an attempt; nil clears it. A halted record
// is kept as it is, since only Resume ends a halt; stored tells whether rec was stored. A stack the engine has
// no state for is ignored.
func (e *Engine) RecordRepair(stack string, rec *Repair) (stored bool, err error) {
	var r *Repair
	if rec != nil {
		c := cleanRepair(*rec)
		r = &c
	}
	if err := validateMonitor(StackState{Repair: r}); err != nil {
		return false, err
	}
	_, err = e.store.Update(func(s State) (State, error) {
		s = normalize(s)
		cur, ok := s.Stacks[stack]
		if !ok || (cur.Repair != nil && cur.Repair.Halted != nil) {
			return s, nil
		}
		cur.Repair, stored = r, true
		s.Stacks[stack] = cur
		return s, nil
	})
	if err != nil {
		stored = false
	}
	return stored, err
}
