package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/castrowithcee/qatlas-deploy/internal/backup"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// registerSettingsCommands registers stack.settings.get and stack.settings.set.
func registerSettingsCommands(r *control.Registry, eng *engine.Engine, redact func(string) string, log *slog.Logger) error {
	commands := map[string]control.Handler{
		control.StackSettingsGetCommand: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a control.StackSettingsArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if !backup.ValidStack(a.Stack) {
				return nil, control.NewError(control.CodeInvalid, "the stack name is not valid")
			}
			rules, err := eng.StackSettings(ctx, a.Stack)
			if err != nil {
				return nil, settingsAnswer(err, redact)
			}
			res := control.StackSettingsResult{Stack: a.Stack}
			if rules != nil {
				s, err := settingsOf(rules)
				if err != nil {
					return nil, err
				}
				res.Settings = &s
			}
			return res, nil
		},
		control.StackSettingsSetCommand: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a control.StackSettingsSetArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if !backup.ValidStack(a.Stack) {
				return nil, control.NewError(control.CodeInvalid, "the stack name is not valid")
			}
			data, err := json.Marshal(a.Settings)
			if err != nil {
				return nil, control.NewError(control.CodeBadRequest, "the arguments are not valid")
			}
			rules, err := stacks.ParseSettings(data)
			if err != nil {
				return nil, control.NewError(control.CodeInvalid, printable(err.Error()))
			}
			ctx, cancel := context.WithTimeout(ctx, control.StackSettingsTimeout)
			defer cancel()
			commit, err := eng.SetSettings(ctx, a.Stack, rules)
			if err != nil {
				ans := settingsAnswer(err, redact)
				log.Info("stack settings", "target", a.Stack, "result", "failed", "error", printable(redact(err.Error())))
				return nil, ans
			}
			log.Info("stack settings", "target", a.Stack, "result", "ok", "changed", commit != "", "commit", commit)
			s, err := settingsOf(rules)
			if err != nil {
				return nil, err
			}
			return control.StackSettingsSetResult{Stack: a.Stack, Settings: s, Changed: commit != "", Commit: commit}, nil
		},
	}
	for name, h := range commands {
		if err := r.Register(name, h); err != nil {
			return err
		}
	}
	return nil
}

// settingsOf turns rules into their wire form, which has the field names of the file.
func settingsOf(rules *stacks.Rules) (control.StackSettings, error) {
	var out control.StackSettings
	data, err := json.Marshal(rules.Settings())
	if err == nil {
		err = json.Unmarshal(data, &out)
	}
	if err != nil {
		return control.StackSettings{}, errors.New("the stack settings cannot be encoded")
	}
	return out, nil
}

// settingsAnswer maps the refusals of the engine to fixed answers. Settings that do not fit and a failed git
// call keep their redacted text, which names the field or the remote's answer.
func settingsAnswer(err error, redact func(string) string) error {
	switch {
	case errors.Is(err, engine.ErrInvalidSettings):
		return control.NewError(control.CodeInvalid, printable(redact(err.Error())))
	case errors.Is(err, engine.ErrUnreadable):
		return control.NewError(control.CodeUnreadable, "the stack cannot be read; 'qdeploy stack check' names the problem")
	case errors.Is(err, engine.ErrCopyChanges):
		return control.NewError(control.CodeLocalChanges, "the working copy has local changes and cannot follow a new commit")
	case errors.Is(err, context.DeadlineExceeded):
		return control.NewError(control.CodeBusy, "the stacks repository did not answer in time; try again")
	}
	var ce *control.Error
	if ans := runsAnswer(err); errors.As(ans, &ce) {
		return ans
	}
	return control.NewError(control.CodeRemote, printable(redact(err.Error())))
}
