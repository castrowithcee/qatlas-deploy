package engine

import (
	"context"
	"errors"
	"io/fs"
	"os"
)

var (
	errNotExist = fs.ErrNotExist
	errExists   = os.ErrExist
)

// Run polls until ctx ends and returns nil once the active runs have finished or been cancelled. The first
// poll happens at once; later polls follow after the interval plus a small random spread, so hosts with the
// same interval do not all hit the remote together. Wake and a failed poll never shorten the interval except
// as stated: a missing binding is looked at again after a minute or on Wake.
func (e *Engine) Run(ctx context.Context) error {
	e.running.Lock()
	if e.looping {
		e.running.Unlock()
		return errAlreadyRunning
	}
	e.looping = true
	e.running.Unlock()
	defer func() {
		e.Close()
		e.running.Lock()
		e.looping = false
		e.running.Unlock()
	}()

	for {
		_, err := e.Poll(ctx)
		wait := e.cfg.Interval + e.cfg.Jitter(min(e.cfg.Interval/10, maxSpread))
		switch {
		case errors.Is(err, ErrNoRepo):
			wait = idleWait
		case err != nil && ctx.Err() == nil:
			e.cfg.Logger.Warn("poll failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-e.wake:
		case <-e.cfg.Clock.After(wait):
		}
	}
}
