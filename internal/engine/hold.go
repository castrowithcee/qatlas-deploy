package engine

import (
	"context"
	"slices"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// Reasons a stack is kept out of the maintenance window, see WindowVerdict.
const (
	// BlockRollback is the pause a failed rollback left; only Resume clears it.
	BlockRollback = "rollback-paused"
	// BlockPause is the operator's pause: the window starts no runs for the stack.
	BlockPause = "paused"
	// BlockPin is the operator's pin: the window never changes the stack's versions.
	BlockPin = "pinned"
	// BlockStopped is a stack the operator stopped through qdeploy; only a start clears it.
	BlockStopped = "stopped"
	// BlockRemoved is a stack whose folder is gone from the repository.
	BlockRemoved = "removed"
)

// WindowVerdict tells whether the maintenance window may update a stack. Reasons lists every mark that
// forbids it, in a fixed order; Allowed is true exactly when there is none. The verdict covers only what the
// engine remembers; a window still has to check the working copy and the run slot as Trigger does.
type WindowVerdict struct {
	Allowed bool
	Reasons []string
}

// WindowEligibility returns the verdict for a stack. A stack the engine has no state for has no marks and is
// allowed.
func (e *Engine) WindowEligibility(name string) (WindowVerdict, error) {
	st, _, err := e.Stack(name)
	if err != nil {
		return WindowVerdict{}, err
	}
	return windowVerdict(st), nil
}

func windowVerdict(st StackState) WindowVerdict {
	var reasons []string
	if st.Paused != nil {
		reasons = append(reasons, BlockRollback)
	}
	if st.OperatorPause != nil {
		reasons = append(reasons, BlockPause)
	}
	if st.Pin != nil {
		reasons = append(reasons, BlockPin)
	}
	if st.Stopped != nil {
		reasons = append(reasons, BlockStopped)
	}
	if st.Removed {
		reasons = append(reasons, BlockRemoved)
	}
	return WindowVerdict{Allowed: len(reasons) == 0, Reasons: reasons}
}

// SetPause sets or clears the operator pause of a stack and tells whether that changed anything; repeating it
// is no error and keeps the original time. Setting needs the stack in the working copy, clearing also accepts
// a stack the engine only remembers, such as one removed from the repository. ErrUnknownStack otherwise.
func (e *Engine) SetPause(ctx context.Context, name string, on bool) (changed bool, err error) {
	return e.setHold(ctx, name, on, func(s *StackState) **Hold { return &s.OperatorPause })
}

// SetPin sets or clears the pin of a stack, with the same rules as SetPause.
func (e *Engine) SetPin(ctx context.Context, name string, on bool) (changed bool, err error) {
	return e.setHold(ctx, name, on, func(s *StackState) **Hold { return &s.Pin })
}

func (e *Engine) setHold(ctx context.Context, name string, on bool, field func(*StackState) **Hold) (bool, error) {
	if err := e.holdable(ctx, name, on); err != nil {
		return false, err
	}
	now := e.cfg.Clock.Now().UTC()
	var changed bool
	_, err := e.store.Update(func(s State) (State, error) {
		s = normalize(s)
		cur, ok := s.Stacks[name]
		if !ok && !on {
			return s, nil
		}
		slot := field(&cur)
		switch {
		case on && *slot == nil:
			*slot, changed = &Hold{Since: now}, true
		case !on && *slot != nil:
			*slot, changed = nil, true
		default:
			return s, nil
		}
		s.Stacks[name] = cur
		return s, nil
	})
	return changed, err
}

// holdable checks that the stack may carry a mark. The working-copy lock keeps the stack list from changing
// under a poll.
func (e *Engine) holdable(ctx context.Context, name string, on bool) error {
	if !on {
		if _, ok, err := e.Stack(name); err != nil || ok {
			return err
		}
	}
	b, err := e.cfg.Source()
	if err != nil {
		if !on {
			return ErrUnknownStack
		}
		return err
	}
	e.lock.Lock()
	defer e.lock.Unlock()
	list, err := stacks.Load(b.Dir)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(list, func(st stacks.Stack) bool { return st.Name == name }) {
		return ErrUnknownStack
	}
	return nil
}
