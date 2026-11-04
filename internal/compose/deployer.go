package compose

import (
	"context"
	"fmt"
	"os"

	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// Backups saves and restores the data of a stack around a run; the backup package provides it.
type Backups interface {
	// Create backs up the stopped stack, whose data belongs to commit, and returns the backup's id.
	Create(ctx context.Context, st stacks.Stack, commit string, log Logger) (string, error)
	// Restore puts the data of backup id back into the stopped stack and leaves it stopped.
	Restore(ctx context.Context, st stacks.Stack, id string, log Logger) error
}

// Deployer implements engine.Deployer with Docker Compose and, when Backups is set, the stack's backup rules.
type Deployer struct {
	Exec    *Executor
	Checker *Checker
	// Backups is nil when the service cannot back up: every stack then runs as one without backup rules.
	Backups Backups
}

var _ engine.Deployer = (*Deployer)(nil)

// New returns the production deployer: docker through os/exec with ProductionEnv, real clock.
func New(backups Backups) *Deployer {
	x := &Executor{Runner: &ExecRunner{}, Env: ProductionEnv(os.Getenv)}
	return &Deployer{Exec: x, Checker: &Checker{Exec: x}, Backups: backups}
}

// Stop implements engine.Deployer.
func (d *Deployer) Stop(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	return d.Exec.Stop(ctx, st, log)
}

// Backup implements engine.Deployer; a stack without backup rules is engine.ErrNoBackup.
func (d *Deployer) Backup(ctx context.Context, st stacks.Stack, deployed string, log *engine.Log) (string, error) {
	if d.Backups == nil || st.Rules == nil || st.Rules.Backup == nil {
		return "", engine.ErrNoBackup
	}
	return d.Backups.Create(ctx, st, deployed, log)
}

// Pull implements engine.Deployer.
func (d *Deployer) Pull(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	return d.Exec.Pull(ctx, st, log)
}

// Up implements engine.Deployer.
func (d *Deployer) Up(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	return d.Exec.Up(ctx, st, log)
}

// Start implements engine.Deployer.
func (d *Deployer) Start(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	return d.Exec.Start(ctx, st, log)
}

// Restart implements engine.Deployer.
func (d *Deployer) Restart(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	return d.Exec.Restart(ctx, st, log)
}

// Check implements engine.Deployer.
func (d *Deployer) Check(ctx context.Context, st stacks.Stack, log *engine.Log) error {
	if err := d.Checker.Check(ctx, st, log); err != nil {
		return fmt.Errorf("check failed: %w", err)
	}
	return nil
}

// Restore implements engine.Deployer.
func (d *Deployer) Restore(ctx context.Context, st stacks.Stack, id string, log *engine.Log) error {
	if d.Backups == nil {
		return engine.ErrNoBackup
	}
	return d.Backups.Restore(ctx, st, id, log)
}
