package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/backup"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// registerStackSecrets puts the values of the stack's secrets.env and of its .env variables with a secret-like
// name into the redactor. The values stay in memory; a file that cannot be read adds nothing, and the Compose
// call reports the problem itself.
func registerStackSecrets(r *redact.Redactor, st stacks.Stack) {
	for k, v := range st.Env {
		if stacks.SecretLikeKey(k) {
			r.Add(v)
		}
	}
	values, err := stacks.SecretValues(st.Dir)
	if err != nil {
		return
	}
	for _, v := range values {
		r.Add(v)
	}
}

// redactingDeployer registers the secrets of the stack before every step, so a value Compose prints is
// redacted in the run log.
type redactingDeployer struct {
	next engine.Deployer
	r    *redact.Redactor
}

func (d redactingDeployer) Stop(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	registerStackSecrets(d.r, st)
	return d.next.Stop(ctx, st, log)
}

func (d redactingDeployer) Backup(ctx context.Context, st stacks.Stack, deployed string, log *engine.Log) (string, error) {
	registerStackSecrets(d.r, st)
	return d.next.Backup(ctx, st, deployed, log)
}

func (d redactingDeployer) Pull(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	registerStackSecrets(d.r, st)
	return d.next.Pull(ctx, st, log)
}

func (d redactingDeployer) Up(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	registerStackSecrets(d.r, st)
	return d.next.Up(ctx, st, log)
}

// MissingDataDirs implements engine.DataDirChecker; a deployer without the check finds nothing.
func (d redactingDeployer) MissingDataDirs(ctx context.Context, st stacks.Stack,
	versioned func(context.Context, string) (bool, error), log *engine.Log) ([]engine.MissingDir, error) {
	c, ok := d.next.(engine.DataDirChecker)
	if !ok {
		return nil, nil
	}
	registerStackSecrets(d.r, st)
	return c.MissingDataDirs(ctx, st, versioned, log)
}

func (d redactingDeployer) Start(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	registerStackSecrets(d.r, st)
	return d.next.Start(ctx, st, log)
}

func (d redactingDeployer) Restart(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	registerStackSecrets(d.r, st)
	return d.next.Restart(ctx, st, log)
}

func (d redactingDeployer) Check(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	registerStackSecrets(d.r, st)
	return d.next.Check(ctx, st, log)
}

func (d redactingDeployer) Restore(ctx context.Context, st stacks.Stack, id string, log *engine.Log) error {
	registerStackSecrets(d.r, st)
	return d.next.Restore(ctx, st, id, log)
}

func registerSecretCommands(r *control.Registry, eng *sourceSet,
	secrets *redact.Redactor, log *slog.Logger) error {
	checkStack := func(name string) error {
		if !backup.ValidStack(name) {
			return control.NewError(control.CodeInvalid, "the stack name is not valid")
		}
		return nil
	}
	// answer maps an error to a fixed text: nothing from the file or the request is passed on.
	answer := func(stack string, err error) error {
		switch {
		case errors.Is(err, stacks.ErrInvalidSecretKey), errors.Is(err, stacks.ErrInvalidSecretValue):
			return control.NewError(control.CodeInvalid, err.Error())
		case errors.Is(err, stacks.ErrKeyInEnv):
			return control.NewError(control.CodeKeyExists, err.Error())
		case errors.Is(err, stacks.ErrNoSecret):
			return control.NewError(control.CodeNotFound, err.Error())
		}
		var ce *control.Error
		if ans := runsAnswer(err); errors.As(ans, &ce) {
			return ans
		}
		log.Error("secrets.env cannot be changed", "stack", stack, "error", secrets.Apply(err.Error()))
		return control.NewError(control.CodeInternal, "secrets.env of the stack cannot be changed")
	}

	commands := map[string]control.Handler{
		control.SecretSetCommand: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a control.SecretSetArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			// A value that reached the service is never logged, even if the request is refused.
			secrets.Add(a.Value)
			if err := checkStack(a.Stack); err != nil {
				return nil, err
			}
			replaced := false
			err := eng.WithStack(ctx, a.Stack, "secret", func(_ context.Context, st stacks.Stack, _ string) error {
				registerStackSecrets(secrets, st)
				var err error
				replaced, err = stacks.SetSecret(st, a.Key, a.Value)
				return err
			})
			if err != nil {
				return nil, answer(a.Stack, err)
			}
			return control.SecretResult{Stack: a.Stack, Key: a.Key, Replaced: replaced}, nil
		},
		control.SecretDeleteCommand: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a control.SecretKeyArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if err := checkStack(a.Stack); err != nil {
				return nil, err
			}
			err := eng.WithStack(ctx, a.Stack, "secret", func(_ context.Context, st stacks.Stack, _ string) error {
				return stacks.DeleteSecret(st, a.Key)
			})
			if err != nil {
				return nil, answer(a.Stack, err)
			}
			return control.SecretResult{Stack: a.Stack, Key: a.Key}, nil
		},
		control.SecretListCommand: func(_ context.Context, raw json.RawMessage) (any, error) {
			var a control.SecretListArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if err := checkStack(a.Stack); err != nil {
				return nil, err
			}
			// The file is replaced atomically, so reading without the stack's run slot never sees a half
			// written file and works while a run is active.
			// The stack is found by its project name, which may differ from its folder. A stack with Err is
			// still answered: only key names are read.
			owned, _, err := eng.ownedStacks()
			if err != nil {
				return nil, err
			}
			dir := ""
			for _, ref := range owned {
				if ref.st.Name == a.Stack {
					dir = ref.st.Dir
					break
				}
			}
			if dir == "" {
				return nil, control.NewError(control.CodeNoSuchStack, "no such stack in the working copy")
			}
			keys, _, err := stacks.SecretKeys(dir)
			if err != nil {
				return nil, answer(a.Stack, err)
			}
			if keys == nil {
				keys = []string{}
			}
			return control.SecretListResult{Stack: a.Stack, Keys: keys}, nil
		},
	}
	for name, h := range commands {
		if err := r.Register(name, h); err != nil {
			return err
		}
	}
	return nil
}
