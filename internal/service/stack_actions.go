package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/castrowithcee/qatlas-deploy/internal/backup"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
)

// registerStackActionCommands registers stack.start, stack.stop and stack.restart. Only stacks of the working
// copy are accepted; anything else is answered as no such stack.
func registerStackActionCommands(r *control.Registry, eng *engine.Engine, redact func(string) string, log *slog.Logger) error {
	handler := func(action string) control.Handler {
		return func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a control.StackActionArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if !backup.ValidStack(a.Stack) {
				return nil, control.NewError(control.CodeInvalid, "the stack name is not valid")
			}
			ctx, cancel := context.WithTimeout(ctx, control.StackActionTimeout)
			defer cancel()
			err := eng.StackAction(ctx, a.Stack, action)
			if err != nil {
				ans := stackActionAnswer(err, redact)
				result := "failed"
				var ce *control.Error
				if errors.As(ans, &ce) {
					result = ce.Code
				}
				log.Info("stack action", "action", action, "target", a.Stack, "result", result)
				return nil, ans
			}
			log.Info("stack action", "action", action, "target", a.Stack, "result", "ok")
			return control.StackActionResult{Stack: printable(a.Stack), Action: action, Stopped: action == engine.ActionStop}, nil
		}
	}
	for command, action := range map[string]string{
		control.StackStartCommand: engine.ActionStart, control.StackStopCommand: engine.ActionStop,
		control.StackRestartCommand: engine.ActionRestart,
	} {
		if err := r.Register(command, handler(action)); err != nil {
			return err
		}
	}
	return nil
}

// stackActionAnswer maps the refusals of the engine to their fixed answers and a failed Compose call to
// CodeStackAction with its redacted text.
func stackActionAnswer(err error, redact func(string) string) error {
	var ce *control.Error
	if ans := runsAnswer(err); errors.As(ans, &ce) {
		return ans
	}
	return control.NewError(control.CodeStackAction, printable(redact(err.Error())))
}
