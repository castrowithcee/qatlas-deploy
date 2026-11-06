package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/backup"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// backupLimit bounds one backup or restore as a whole; the helper calls have their own limits.
const backupLimit = 6 * time.Hour

// Backups is what the backup commands need; *backup.Manager implements it. The service serializes the calls
// per stack with the stack's run slot, so an implementation does not lock.
type Backups interface {
	Create(ctx context.Context, st stacks.Stack, commit string, log backup.Logger) (backup.Result, error)
	List(stack string) ([]backup.Meta, error)
	Restore(ctx context.Context, st stacks.Stack, id string, log backup.Logger) (backup.Meta, error)
}

var _ Backups = (*backup.Manager)(nil)

// backupLog writes the steps of a backup to the service log, which redacts registered secrets.
type backupLog struct {
	log   *slog.Logger
	stack string
}

func (l backupLog) Step(name string) { l.log.Info("backup step", "stack", l.stack, "step", name) }
func (l backupLog) Printf(format string, args ...any) {
	l.log.Info("backup", "stack", l.stack, "detail", fmt.Sprintf(format, args...))
}

func registerBackupCommands(r *control.Registry, eng *sourceSet, backups Backups, redact func(string) string, register func(stacks.Stack), log *slog.Logger) error {
	answer := func(err error) error {
		switch {
		case errors.Is(err, backup.ErrNoRules):
			return control.NewError(control.CodeNoBackupRules, "the stack settings have no backup section")
		case errors.Is(err, backup.ErrNoSuchBackup):
			return control.NewError(control.CodeNoSuchBackup, "no such backup")
		}
		var ce *control.Error
		if ans := runsAnswer(err); errors.As(ans, &ce) {
			return ans
		}
		return control.NewError(control.CodeBackupFailed, printable(redact(err.Error())))
	}
	unavailable := func() error {
		return control.NewError(control.CodeNoBackups, "backups are not available in this version")
	}
	checkStack := func(name string) error {
		if !backup.ValidStack(name) {
			return control.NewError(control.CodeInvalid, "the stack name is not valid")
		}
		return nil
	}

	commands := map[string]control.Handler{
		control.BackupCreateCommand: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a control.BackupArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if backups == nil {
				return nil, unavailable()
			}
			if err := checkStack(a.Stack); err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(ctx, backupLimit)
			defer cancel()
			var res backup.Result
			err := eng.WithStack(ctx, a.Stack, "backup", func(ctx context.Context, st stacks.Stack, deployed string) error {
				register(st)
				var err error
				res, err = backups.Create(ctx, st, deployed, backupLog{log, a.Stack})
				return err
			})
			if err != nil {
				return nil, answer(err)
			}
			out := control.BackupCreateResult{Backup: backupInfo(res.Meta), Removed: []string{}}
			for _, id := range res.Removed {
				out.Removed = append(out.Removed, printable(id))
			}
			return out, nil
		},
		control.BackupListCommand: func(_ context.Context, raw json.RawMessage) (any, error) {
			var a control.BackupArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			if backups == nil {
				return nil, unavailable()
			}
			if err := checkStack(a.Stack); err != nil {
				return nil, err
			}
			list, err := backups.List(a.Stack)
			if err != nil {
				log.Error("backups cannot be listed", "stack", a.Stack, "error", redact(err.Error()))
				return nil, control.NewError(control.CodeBackupFailed, "the backups cannot be listed")
			}
			out := control.BackupListResult{Stack: printable(a.Stack), Backups: make([]control.BackupInfo, 0, len(list))}
			for _, m := range list {
				out.Backups = append(out.Backups, backupInfo(m))
			}
			return out, nil
		},
		control.BackupRestoreCommand: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a control.BackupRestoreArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			// A restore overwrites the current data: without explicit confirmation nothing else is looked at.
			if !a.Confirm {
				return nil, control.NewError(control.CodeConfirmation,
					"restoring overwrites the current data of the stack; the request must confirm it")
			}
			if backups == nil {
				return nil, unavailable()
			}
			if err := checkStack(a.Stack); err != nil {
				return nil, err
			}
			if !backup.ValidID(a.ID) {
				return nil, control.NewError(control.CodeNoSuchBackup, "no such backup")
			}
			ctx, cancel := context.WithTimeout(ctx, backupLimit)
			defer cancel()
			var meta backup.Meta
			err := eng.WithStack(ctx, a.Stack, "restore", func(ctx context.Context, st stacks.Stack, _ string) error {
				register(st)
				var err error
				meta, err = backups.Restore(ctx, st, a.ID, backupLog{log, a.Stack})
				return err
			})
			if err != nil {
				return nil, answer(err)
			}
			return control.BackupRestoreResult{Backup: backupInfo(meta)}, nil
		},
	}
	for name, h := range commands {
		if err := r.Register(name, h); err != nil {
			return err
		}
	}
	return nil
}

func backupInfo(m backup.Meta) control.BackupInfo {
	info := control.BackupInfo{
		ID: printable(m.ID), Stack: printable(m.Stack), Created: m.Created, Commit: printable(m.Commit),
		Size: m.Size(), Items: make([]control.BackupItemInfo, 0, len(m.Items)), Incomplete: m.Incomplete,
	}
	for _, it := range m.Items {
		info.Items = append(info.Items, control.BackupItemInfo{Kind: printable(it.Kind), Name: printable(it.Name), Size: it.Size})
	}
	return info
}
