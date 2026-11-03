package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/castrowithcee/qatlas-deploy/internal/envfile"
	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// envName is the versioned file of a stack that holds its image versions.
const envName = ".env"

// Errors an update is refused with before anything changes.
var (
	ErrNotDeployed = errors.New("the stack was never deployed; deploy it first")
	ErrUndeployed  = errors.New("the stack has changes on the branch that are not deployed; deploy them first")
	ErrCopyChanges = errors.New("the working copy has local changes outside the stack and cannot follow a new commit")
	ErrStalePlan   = errors.New("the stack changed since its update was planned")
)

// VersionStep is one step of an update: the image version variable moves from one version to the next.
type VersionStep struct {
	Variable, From, To string
}

// StepResult is the outcome of one step of an update. Run is empty for a step that was not attempted; Commit
// is the step's pushed commit, or the deployed one when the step failed before its push.
type StepResult struct {
	VersionStep
	Run, Commit, Result, Error string
}

// CheckUpdate tells whether the stack can be updated now, refusing it as Update would, and returns the stack
// as the working copy has it. It changes nothing and holds nothing.
func (e *Engine) CheckUpdate(ctx context.Context, name string) (stacks.Stack, error) {
	if e.cfg.Deployer == nil {
		return stacks.Stack{}, ErrNoDeployer
	}
	b, err := e.cfg.Source()
	if err != nil {
		return stacks.Stack{}, err
	}
	e.lock.Lock()
	defer e.lock.Unlock()
	st, head, err := e.updatable(ctx, b, name)
	if err != nil {
		return stacks.Stack{}, err
	}
	e.mu.Lock()
	_, busy := e.active[name]
	closed := e.closed
	e.mu.Unlock()
	switch {
	case closed:
		return stacks.Stack{}, ErrStopped
	case busy:
		return stacks.Stack{}, ErrRunActive
	}
	if _, err := e.updateBase(ctx, b, name, head); err != nil {
		return stacks.Stack{}, err
	}
	return st, nil
}

// Update runs the steps one after another on the stack, each as a run of its own with trigger update: stop,
// backup, a commit that sets the variable in the stack's .env pushed to the branch, the working copy moved to
// it, pull, up and check. A step that fails is rolled back like any run, and the later steps are not
// attempted; the steps before it stay deployed. The stack's slot is held from the first step to the last, so
// no other run of it starts in between.
//
// Each step's From must be the variable's value in the stack's .env, or the To of the step before it, else
// ErrStalePlan. The stack is refused as Trigger refuses it, and also when it is pinned (ErrPinned), when it
// was never deployed (ErrNotDeployed), when its folder differs between the deployed commit and the working
// copy (ErrUndeployed) or when the working copy has other local changes (ErrCopyChanges). The steps run in
// the caller's goroutine, each under the run time limit and independent of ctx.
func (e *Engine) Update(ctx context.Context, name string, steps []VersionStep) ([]StepResult, error) {
	if e.cfg.Deployer == nil {
		return nil, ErrNoDeployer
	}
	b, err := e.cfg.Source()
	if err != nil {
		return nil, err
	}
	var (
		st       stacks.Stack
		deployed string
	)
	err = func() error {
		e.lock.Lock()
		defer e.lock.Unlock()
		var head string
		var err error
		if st, head, err = e.updatable(ctx, b, name); err != nil {
			return err
		}
		if err := e.take(name, "update"); err != nil {
			return err
		}
		if deployed, err = e.updateBase(ctx, b, name, head); err == nil {
			err = checkPlan(st, steps)
		}
		if err != nil {
			e.free(name, true)
		}
		return err
	}()
	if err != nil {
		return nil, err
	}
	held := true
	defer func() { e.free(name, held) }()

	results := make([]StepResult, len(steps))
	for i, s := range steps {
		results[i].VersionStep = s
	}
	for i := range steps {
		if e.base.Err() != nil {
			break
		}
		r, err := e.stepRun(b, st, deployed)
		if err != nil {
			results[i].Error = "the run log cannot be created"
			e.cfg.Logger.Error("update step not started", "stack", name, "error", err)
			break
		}
		r.held, r.change = held, &steps[i]
		out := r.perform()
		held = r.held
		rec := r.record(out)
		e.notify(Event{Kind: rec.Result, Stack: rec.Stack, Run: rec.ID, Commit: rec.Commit, Error: rec.Error})
		results[i].Run, results[i].Commit, results[i].Result, results[i].Error = rec.ID, rec.Commit, rec.Result, rec.Error
		if rec.Result != ResultSucceeded {
			break
		}
		deployed, st = out.at, r.st
	}
	return results, nil
}

// updatable reads the stack from the working copy and refuses it when it or the working copy has local
// changes. It returns the stack and the working copy's HEAD. The caller holds the working-copy lock.
func (e *Engine) updatable(ctx context.Context, b Binding, name string) (stacks.Stack, string, error) {
	changes, err := b.Copy.Status(ctx)
	if err != nil {
		return stacks.Stack{}, "", err
	}
	local, dirty := classify(changes)
	switch {
	case len(local[name]) > 0:
		return stacks.Stack{}, "", fmt.Errorf("%w: %s", ErrLocalChanges, e.localDetail(local[name]))
	case dirty:
		return stacks.Stack{}, "", ErrCopyChanges
	}
	head, err := b.Copy.Head(ctx)
	if err != nil {
		return stacks.Stack{}, "", err
	}
	list, err := stacks.Load(b.Dir)
	if err != nil {
		return stacks.Stack{}, "", fmt.Errorf("read stacks: %w", err)
	}
	i := slices.IndexFunc(list, func(s stacks.Stack) bool { return s.Name == name })
	switch {
	case i < 0:
		return stacks.Stack{}, "", ErrUnknownStack
	case list[i].Err != nil:
		return stacks.Stack{}, "", fmt.Errorf("%w: %s", ErrUnreadable, e.text(list[i].Err.Error()))
	}
	return list[i], head, nil
}

// updateBase returns the deployed commit of the stack, refusing a paused or pinned stack, one never deployed
// and one whose folder at head differs from the deployed one. The caller holds the working-copy lock.
func (e *Engine) updateBase(ctx context.Context, b Binding, name, head string) (string, error) {
	cur, _, err := e.Stack(name)
	switch {
	case err != nil:
		return "", err
	case cur.Paused != nil:
		return "", ErrPaused
	case cur.Pin != nil:
		return "", ErrPinned
	case cur.Deployed == "":
		return "", ErrNotDeployed
	case cur.Deployed == head:
		return cur.Deployed, nil
	}
	folders, err := b.Copy.ChangedFolders(ctx, cur.Deployed, head)
	if err != nil {
		return "", err
	}
	if slices.Contains(folders, name) {
		return "", ErrUndeployed
	}
	return cur.Deployed, nil
}

// checkPlan makes sure the steps start at the values the stack has and follow each other.
func checkPlan(st stacks.Stack, steps []VersionStep) error {
	values := map[string]string{}
	for _, img := range st.Images {
		if img.Source == stacks.SourceEnv {
			values[img.Var] = img.Value
		}
	}
	for _, s := range steps {
		if v, ok := values[s.Variable]; !ok || v != s.From || s.To == "" || s.To == s.From {
			return ErrStalePlan
		}
		values[s.Variable] = s.To
	}
	return nil
}

// stepRun prepares the run of one update step and shows it as the stack's active run. The caller holds the
// stack's slot.
func (e *Engine) stepRun(b Binding, st stacks.Stack, deployed string) (*run, error) {
	now := e.cfg.Clock.Now().UTC()
	id, w, err := e.newLog(now)
	if err != nil {
		return nil, fmt.Errorf("create the run log: %w", err)
	}
	e.mu.Lock()
	e.active[st.Name] = id
	e.mu.Unlock()
	return &run{
		e: e, b: b, st: st, deployed: deployed,
		log: &Log{w: w, redact: e.cfg.Redact, now: e.cfg.Clock.Now},
		rec: RunRecord{ID: id, Stack: st.Name, Commit: deployed, Trigger: TriggerUpdate, Started: now},
	}, nil
}

// advance commits the version change of an update step, pushes it and moves the working copy to it. The
// commit is built on the freshly fetched branch and changes only the variable's line in the stack's .env; a
// push the remote rejects because the branch moved is built and pushed once more. Like revert it waits until
// no other run relies on the working copy and returns holding the run's hold again. r.pushed is set as soon
// as the commit is on the branch: from then on a failure is rolled back with a revert.
func (r *run) advance(ctx context.Context) error {
	e, log, wc, ch := r.e, r.log, r.b.Copy, r.change
	log.Step("commit")
	e.releaseHold()
	r.held = false
	if err := e.exclusive(ctx); err != nil {
		return fmt.Errorf("wait for the other runs: %w", err)
	}
	defer func() {
		e.share()
		r.held = true
	}()

	name := r.st.Name
	message := fmt.Sprintf("Update %s %s from %s to %s", name, ch.Variable, ch.From, ch.To)
	edit := func(old []byte) ([]byte, error) { return envfile.Set(old, ch.Variable, ch.To) }
	var at string
	for attempt := 1; at == ""; attempt++ {
		if err := r.unchangedOnBranch(ctx); err != nil {
			return err
		}
		c, err := wc.CommitFile(ctx, name+"/"+envName, edit, message)
		if err != nil {
			return fmt.Errorf("commit: %w", err)
		}
		log.Printf("commit %s", c)
		err = r.step("push", func() error { return wc.Push(ctx, c) })
		switch {
		case err == nil:
			at = c
		case errors.Is(err, gitrepo.ErrPushRejected) && attempt == 1:
			log.Printf("rejected: the branch moved; fetching and committing again")
		case r.landed(ctx, c):
			// The push may fail after the remote took the commit; the branch is what counts.
			log.Printf("the push reported an error, but the branch has the commit")
			at = c
		default:
			return fmt.Errorf("push: %w; nothing was pushed", err)
		}
	}
	r.pushed, r.rec.Commit = true, at
	return r.follow(ctx, at, "update")
}

// landed fetches and reports whether the branch is at commit c.
func (r *run) landed(ctx context.Context, c string) bool {
	if r.b.Copy.Fetch(ctx) != nil {
		return false
	}
	remote, err := r.b.Copy.RemoteHead(ctx)
	return err == nil && remote == c
}

// unchangedOnBranch fetches and makes sure that the working copy can follow a new commit and that the stack
// folder on the branch is still the deployed one, so the commit carries the version change and nothing else
// of the stack.
func (r *run) unchangedOnBranch(ctx context.Context) error {
	wc := r.b.Copy
	if err := wc.Fetch(ctx); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	changes, err := wc.Status(ctx)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	if _, dirty := classify(changes); dirty {
		return errors.New("the working copy has local changes and cannot follow a new commit; nothing was pushed")
	}
	remote, err := wc.RemoteHead(ctx)
	if err != nil {
		return err
	}
	head, err := wc.Head(ctx)
	if err != nil {
		return err
	}
	if ok, err := wc.IsAncestor(ctx, head, remote); err != nil || !ok {
		return errors.Join(errors.New("the working copy has commits the branch lacks and cannot follow a new commit; nothing was pushed"), err)
	}
	if remote == r.deployed {
		return nil
	}
	folders, err := wc.ChangedFolders(ctx, r.deployed, remote)
	if err != nil {
		return err
	}
	if slices.Contains(folders, r.st.Name) {
		return errors.New("the stack changed on the branch since its deployed commit; nothing was pushed")
	}
	return nil
}
