package compose

import (
	"context"
	"fmt"
	"os"

	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// Deployer implements engine.Deployer: pull, up, check. A failed step returns its error and ends the run;
// bringing the previous version back is not part of it.
type Deployer struct {
	Exec    *Executor
	Checker *Checker
}

var _ engine.Deployer = (*Deployer)(nil)

// New returns the production deployer: docker through os/exec with ProductionEnv, real clock.
func New() *Deployer {
	x := &Executor{Runner: &ExecRunner{}, Env: ProductionEnv(os.Getenv)}
	return &Deployer{Exec: x, Checker: &Checker{Exec: x}}
}

// Deploy implements engine.Deployer.
func (d *Deployer) Deploy(ctx context.Context, st stacks.Stack, _ string, log *engine.Log) error {
	return d.deploy(ctx, st, log)
}

func (d *Deployer) deploy(ctx context.Context, st stacks.Stack, log Logger) error {
	// Fail on a missing secrets.env or an unusable name before the first Compose call.
	if _, err := d.Exec.command(st, 0, "pull"); err != nil {
		log.Step("prepare")
		return err
	}
	log.Step("pull")
	if err := d.Exec.Pull(ctx, st, log); err != nil {
		return err
	}
	log.Step("up")
	if err := d.Exec.Up(ctx, st, log); err != nil {
		return err
	}
	log.Step("check")
	if err := d.Checker.Check(ctx, st, log); err != nil {
		return fmt.Errorf("check failed: %w", err)
	}
	return nil
}
