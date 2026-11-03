// Package engine polls the stacks repository, finds the stacks that changed since their last deployed commit
// and runs at most one deployment per stack. It decides when and what; how a stack is brought up is the
// Deployer's job.
//
// The working copy is shared with the repository commands of the service and belongs to the operator: the
// engine fetches, fast-forwards only a copy without local changes, and otherwise only reads. It never resets,
// stashes, checks out or writes into it. Its content is data; stack names from it never become file names.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-kit/configfile"

	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// Defaults and limits.
const (
	DefaultInterval   = 5 * time.Minute
	MinInterval       = 10 * time.Second
	DefaultRunTimeout = 30 * time.Minute
	idleWait          = time.Minute
	maxSpread         = time.Minute
	maxFindingPaths   = 5

	stateFile = "state.json"
	engineDir = "deploy"
	runsDir   = "runs"
)

// Errors the callers of Trigger can tell apart.
var (
	ErrNoRepo         = errors.New("no stacks repository is bound")
	ErrNoDeployer     = errors.New("deployments are not available")
	ErrRunActive      = errors.New("a run of this stack is active")
	ErrUnknownStack   = errors.New("no such stack in the working copy")
	ErrUnreadable     = errors.New("the stack cannot be read")
	ErrLocalChanges   = errors.New("the stack has local changes in the working copy")
	ErrStopped        = errors.New("the engine is stopped")
	errAlreadyRunning = errors.New("the poll loop is already running")
)

// WorkingCopy is the git layer the engine needs.
type WorkingCopy interface {
	Fetch(ctx context.Context) error
	Head(ctx context.Context) (string, error)
	RemoteHead(ctx context.Context) (string, error)
	Status(ctx context.Context) ([]gitrepo.Change, error)
	ChangedFolders(ctx context.Context, from, to string) ([]string, error)
	FastForward(ctx context.Context) error
}

// Binding is the working copy to watch and the directory it lives in.
type Binding struct {
	Dir  string
	Copy WorkingCopy
}

// Deployer brings one stack to a commit. The working copy is at commit and is not moved while the call runs.
// An error is a failed run; the deployed commit then stays.
type Deployer interface {
	Deploy(ctx context.Context, stack stacks.Stack, commit string, log *Log) error
}

// Clock is the time source of the poll loop.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Config is what New needs.
type Config struct {
	// StateDir is the service state directory; the engine keeps its files in a subdirectory.
	StateDir string
	// Source returns the current binding, ErrNoRepo while none exists. It is called without Lock held and
	// again in every cycle, so a binding made later is picked up.
	Source func() (Binding, error)
	// Lock serializes all use of the working copy with the other users of it. Nil means a private mutex.
	Lock sync.Locker
	// Deployer is nil in a service that cannot deploy yet: the engine then only polls, fast-forwards and
	// reports pending stacks, and never starts a run or records a deployed commit.
	Deployer Deployer
	// Redact is applied to every text stored in the state and the run logs. Nil means none.
	Redact func(string) string
	// Interval between polls, DefaultInterval when zero, at least MinInterval.
	Interval time.Duration
	// RunTimeout bounds one run, DefaultRunTimeout when zero.
	RunTimeout time.Duration
	// Clock and Jitter are replaced by tests. Jitter returns a delay in [0, max).
	Clock  Clock
	Jitter func(max time.Duration) time.Duration
	Logger *slog.Logger
}

// Engine is the poll loop and the run control of the service.
type Engine struct {
	cfg   Config
	lock  sync.Locker
	store *configfile.Store[State]
	logs  logStore

	base   context.Context
	cancel context.CancelFunc
	wake   chan struct{}

	pollMu  sync.Mutex // one poll at a time
	running sync.Mutex // guards the loop flag
	looping bool

	mu     sync.Mutex // guards active, stopped and wg.Add
	active map[string]string
	closed bool
	wg     sync.WaitGroup
}

// New prepares the engine; it creates no files before the first state change.
func New(cfg Config) (*Engine, error) {
	if cfg.StateDir == "" || cfg.Source == nil {
		return nil, errors.New("engine: state directory and source are required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	cfg.Interval = max(cfg.Interval, MinInterval)
	if cfg.RunTimeout <= 0 {
		cfg.RunTimeout = DefaultRunTimeout
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	if cfg.Jitter == nil {
		cfg.Jitter = randomDelay
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if cfg.Redact == nil {
		cfg.Redact = func(s string) string { return s }
	}
	dir := filepath.Join(cfg.StateDir, engineDir)
	store, err := configfile.NewStore(filepath.Join(dir, stateFile), configfile.Codec[State]{
		Validate: validateState, Encode: encodeState, Decode: decodeState,
	})
	if err != nil {
		return nil, err
	}
	e := &Engine{
		cfg: cfg, lock: cfg.Lock, store: store, logs: logStore{dir: filepath.Join(dir, runsDir)},
		wake: make(chan struct{}, 1), active: map[string]string{},
	}
	if e.lock == nil {
		e.lock = &sync.Mutex{}
	}
	e.base, e.cancel = context.WithCancel(context.Background())
	return e, nil
}

func randomDelay(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	var n uint64
	for _, x := range b {
		n = n<<8 | uint64(x)
	}
	return time.Duration(n % uint64(max))
}

// Wake makes the loop poll at once, for example after the repository was bound. It never blocks.
func (e *Engine) Wake() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Close cancels the active runs, waits for them and refuses further runs. It is safe to call twice.
func (e *Engine) Close() {
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
	e.cancel()
	e.wg.Wait()
}

// Wait returns when no run is active. It is meant for tests and shutdown.
func (e *Engine) Wait() { e.wg.Wait() }

// PollResult tells what one poll did.
type PollResult struct {
	Head, RemoteHead string
	FastForwarded    bool
	// Started lists the stacks a run was started for, Dropped those whose trigger found a run active.
	Started, Dropped []string
}

// Poll runs one cycle: fetch, bring the working copy up to date when that is safe, find the stacks that
// changed since their deployed commit and start a run for each. Runs continue in the background.
//
// When the working copy has local changes outside secrets.env and data/ (anywhere in it, also in files
// outside the stacks), the fast-forward is not attempted at all: git would either refuse or merge around the
// changes. The stacks with local changes are skipped with a finding; the other stacks are still reported as
// pending at the fetched commit and run only up to the commit the working copy has.
func (e *Engine) Poll(ctx context.Context) (PollResult, error) {
	e.pollMu.Lock()
	defer e.pollMu.Unlock()
	b, err := e.cfg.Source()
	if err != nil {
		return PollResult{}, err
	}
	e.lock.Lock()
	defer e.lock.Unlock()
	res, perr := e.poll(ctx, b)
	if perr != nil {
		e.recordPollError(perr)
	}
	return res, perr
}

func (e *Engine) poll(ctx context.Context, b Binding) (PollResult, error) {
	var res PollResult
	if err := b.Copy.Fetch(ctx); err != nil {
		return res, fmt.Errorf("fetch: %w", err)
	}
	head, err := b.Copy.Head(ctx)
	if err != nil {
		return res, err
	}
	remote, err := b.Copy.RemoteHead(ctx)
	if err != nil {
		return res, err
	}
	changes, err := b.Copy.Status(ctx)
	if err != nil {
		return res, err
	}
	local, dirty := classify(changes)

	var note string
	var ffErr error
	if head != remote {
		switch {
		case dirty:
			note = "fast-forward skipped: the working copy has local changes"
		case e.activeCount() > 0:
			note = "fast-forward skipped: a run is active"
		default:
			if ffErr = b.Copy.FastForward(ctx); ffErr == nil {
				res.FastForwarded = true
				if head, err = b.Copy.Head(ctx); err != nil {
					return res, err
				}
			}
		}
	}
	res.Head, res.RemoteHead = head, remote

	list, err := stacks.Load(b.Dir)
	if err != nil {
		return res, fmt.Errorf("read stacks: %w", err)
	}
	snap, err := e.snapshot()
	if err != nil {
		return res, err
	}
	folders := &diffCache{ctx: ctx, copy: b.Copy, memo: map[[2]string][]string{}}

	type verdict struct {
		findings []Finding
		pending  string
		start    bool
	}
	verdicts := map[string]verdict{}
	present := map[string]bool{}
	for _, st := range list {
		present[st.Name] = true
		old := snap.Stacks[st.Name]
		v := verdict{}
		changedAtHead, errHead := folders.changed(old.Deployed, head, st.Name)
		changedAtRemote := changedAtHead
		var errRemote error
		if remote != head {
			changedAtRemote, errRemote = folders.changed(old.Deployed, remote, st.Name)
		}
		diffErr := errors.Join(errHead, errRemote)
		if diffErr == nil && changedAtRemote {
			v.pending = remote
		}
		// A local edit can itself make a stack unreadable, so it is reported first.
		switch paths := local[st.Name]; {
		case len(paths) > 0:
			v.findings = append(v.findings, Finding{FindingLocalChanges, e.localDetail(paths)})
		case st.Err != nil:
			v.findings = append(v.findings, Finding{FindingUnreadable, e.text(st.Err.Error())})
		case diffErr != nil:
			v.findings = append(v.findings, Finding{FindingDiff, e.text(diffErr.Error())})
		case changedAtHead && old.FailedCommit == head:
			v.findings = append(v.findings, Finding{FindingFailedCommit,
				"the last run of this commit failed; a new commit or a manual run retries"})
		case changedAtHead:
			v.start = true
		}
		verdicts[st.Name] = v
	}

	// The state is written before runs start, so a run that ends at once never races a stale write.
	now := e.cfg.Clock.Now().UTC()
	_, err = e.store.Update(func(s State) (State, error) {
		s = normalize(s)
		for name, v := range verdicts {
			cur := s.Stacks[name]
			cur.Findings, cur.Pending, cur.Removed = v.findings, v.pending, false
			s.Stacks[name] = cur
		}
		for name, cur := range s.Stacks {
			if present[name] {
				continue
			}
			if cur.Deployed == "" && cur.LastRun == nil {
				delete(s.Stacks, name)
				continue
			}
			cur.Removed, cur.Pending = true, ""
			cur.Findings = []Finding{{FindingRemoved, "the folder is gone from the repository; nothing is stopped or removed"}}
			s.Stacks[name] = cur
		}
		s.LastPoll, s.Head, s.RemoteHead, s.Note, s.PollError = now, head, remote, note, ""
		if ffErr != nil {
			s.PollError = e.text(ffErr.Error())
		}
		return s, nil
	})
	if err != nil {
		return res, fmt.Errorf("write state: %w", err)
	}

	if e.cfg.Deployer != nil {
		names := make([]string, 0, len(verdicts))
		for name, v := range verdicts {
			if v.start {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		byName := map[string]stacks.Stack{}
		for _, st := range list {
			byName[st.Name] = st
		}
		for _, name := range names {
			_, err := e.start(byName[name], head, TriggerPoll)
			switch {
			case err == nil:
				res.Started = append(res.Started, name)
			case errors.Is(err, ErrRunActive):
				res.Dropped = append(res.Dropped, name)
			default:
				e.cfg.Logger.Warn("run not started", "stack", name, "error", err)
			}
		}
	}
	if ffErr != nil {
		return res, fmt.Errorf("fast-forward: %w", ffErr)
	}
	return res, nil
}

func (e *Engine) recordPollError(perr error) {
	now := e.cfg.Clock.Now().UTC()
	_, err := e.store.Update(func(s State) (State, error) {
		s = normalize(s)
		s.LastPoll, s.PollError = now, e.text(perr.Error())
		return s, nil
	})
	if err != nil {
		e.cfg.Logger.Warn("poll error not recorded", "error", err)
	}
}

// text makes a text from an error or the working copy fit for the state: redacted, printable and short.
func (e *Engine) text(s string) string {
	s = printableLine(e.cfg.Redact(s))
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 400 {
		s = s[:400] + "..."
	}
	return s
}

func (e *Engine) localDetail(paths []string) string {
	shown := paths
	if len(shown) > maxFindingPaths {
		shown = shown[:maxFindingPaths]
	}
	parts := make([]string, len(shown))
	for i, p := range shown {
		parts[i] = e.text(p)
	}
	detail := fmt.Sprintf("%d local change(s): %s", len(paths), strings.Join(parts, ", "))
	if len(paths) > len(shown) {
		detail += ", ..."
	}
	return detail
}

func normalize(s State) State {
	s.Schema = stateSchema
	if s.Stacks == nil {
		s.Stacks = map[string]StackState{}
	}
	return s
}

// classify maps the local changes to the stack folders they lie in. Changes to <stack>/secrets.env and below
// <stack>/data/ are the host's own and count as neither. dirty is true when any other change exists, also
// outside every stack.
func classify(changes []gitrepo.Change) (byStack map[string][]string, dirty bool) {
	byStack = map[string][]string{}
	for _, c := range changes {
		top, rel, nested := strings.Cut(c.Path, "/")
		if nested && (rel == "secrets.env" || strings.HasPrefix(rel, "data/")) {
			continue
		}
		dirty = true
		if nested {
			byStack[top] = append(byStack[top], c.Path)
		}
	}
	return byStack, dirty
}

// diffCache asks git once per pair of commits.
type diffCache struct {
	ctx  context.Context
	copy WorkingCopy
	memo map[[2]string][]string
}

// changed reports whether the stack differs between its deployed commit and to; a stack without a deployed
// commit always does.
func (d *diffCache) changed(deployed, to, name string) (bool, error) {
	if deployed == "" {
		return true, nil
	}
	if deployed == to {
		return false, nil
	}
	key := [2]string{deployed, to}
	list, ok := d.memo[key]
	if !ok {
		var err error
		if list, err = d.copy.ChangedFolders(d.ctx, deployed, to); err != nil {
			return false, err
		}
		d.memo[key] = list
	}
	return slices.Contains(list, name), nil
}

func (e *Engine) activeCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.active)
}

// RunInfo identifies a started run.
type RunInfo struct {
	ID, Stack, Commit string
}

// Trigger starts a manual run of the stack at the commit the working copy has; it does not fetch. It returns
// as soon as the run is started. A second trigger while a run of the stack is active is dropped with
// ErrRunActive and noted in the state. A stack with local changes is refused with ErrLocalChanges.
func (e *Engine) Trigger(ctx context.Context, name string) (RunInfo, error) {
	if e.cfg.Deployer == nil {
		return RunInfo{}, ErrNoDeployer
	}
	b, err := e.cfg.Source()
	if err != nil {
		return RunInfo{}, err
	}
	e.lock.Lock()
	defer e.lock.Unlock()
	changes, err := b.Copy.Status(ctx)
	if err != nil {
		return RunInfo{}, err
	}
	if local, _ := classify(changes); len(local[name]) > 0 {
		return RunInfo{}, fmt.Errorf("%w: %s", ErrLocalChanges, e.localDetail(local[name]))
	}
	head, err := b.Copy.Head(ctx)
	if err != nil {
		return RunInfo{}, err
	}
	list, err := stacks.Load(b.Dir)
	if err != nil {
		return RunInfo{}, fmt.Errorf("read stacks: %w", err)
	}
	for _, st := range list {
		if st.Name != name {
			continue
		}
		if st.Err != nil {
			return RunInfo{}, fmt.Errorf("%w: %s", ErrUnreadable, e.text(st.Err.Error()))
		}
		return e.start(st, head, TriggerManual)
	}
	return RunInfo{}, ErrUnknownStack
}

// start takes the stack's lock and launches the run. The caller holds the working-copy lock.
func (e *Engine) start(st stacks.Stack, commit, trigger string) (RunInfo, error) {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return RunInfo{}, ErrStopped
	}
	if _, busy := e.active[st.Name]; busy {
		e.mu.Unlock()
		e.recordDropped(st.Name, trigger)
		return RunInfo{}, ErrRunActive
	}
	now := e.cfg.Clock.Now().UTC()
	id, w, err := e.newLog(now)
	if err != nil {
		e.mu.Unlock()
		return RunInfo{}, fmt.Errorf("create the run log: %w", err)
	}
	e.active[st.Name] = id
	e.wg.Add(1)
	e.mu.Unlock()

	log := &Log{w: w, redact: e.cfg.Redact, now: e.cfg.Clock.Now}
	rec := RunRecord{ID: id, Stack: st.Name, Commit: commit, Trigger: trigger, Started: now}
	go e.execute(st, rec, log)
	return RunInfo{ID: id, Stack: st.Name, Commit: commit}, nil
}

func (e *Engine) newLog(now time.Time) (string, io.WriteCloser, error) {
	for range 8 {
		var r [4]byte
		if _, err := rand.Read(r[:]); err != nil {
			return "", nil, err
		}
		id := now.Format("20060102T150405Z") + "-" + hex.EncodeToString(r[:])
		w, err := e.logs.create(id)
		if err == nil {
			return id, w, nil
		}
		if !errors.Is(err, errExists) {
			return "", nil, err
		}
	}
	return "", nil, errors.New("no free run id")
}

func (e *Engine) execute(st stacks.Stack, rec RunRecord, log *Log) {
	defer e.wg.Done()
	ctx, cancel := context.WithTimeout(e.base, e.cfg.RunTimeout)
	defer cancel()

	log.Step("run " + e.text(st.Name) + " at " + rec.Commit + " (" + rec.Trigger + ")")
	err := e.callDeployer(ctx, st, rec.Commit, log)
	rec.Duration = e.cfg.Clock.Now().Sub(rec.Started)
	rec.Result = ResultSucceeded
	if err != nil {
		rec.Result, rec.Error = ResultFailed, e.text(err.Error())
	}
	log.line("result", rec.Result+" "+rec.Error)
	log.close()

	var dropped []RunRecord
	_, uerr := e.store.Update(func(s State) (State, error) {
		s = normalize(s)
		cur := s.Stacks[rec.Stack]
		r := rec
		cur.LastRun = &r
		if rec.Result == ResultSucceeded {
			cur.Deployed, cur.FailedCommit = rec.Commit, ""
			if cur.Pending == rec.Commit {
				cur.Pending = ""
			}
		} else {
			cur.FailedCommit = rec.Commit
		}
		s.Stacks[rec.Stack] = cur
		dropped = s.addRun(rec)
		return s, nil
	})
	if uerr != nil {
		e.cfg.Logger.Error("run result not recorded", "run", rec.ID, "error", uerr)
	}
	ids := make([]string, len(dropped))
	for i, d := range dropped {
		ids[i] = d.ID
	}
	e.logs.remove(ids...)

	e.mu.Lock()
	delete(e.active, rec.Stack)
	e.mu.Unlock()
}

func (e *Engine) callDeployer(ctx context.Context, st stacks.Stack, commit string, log *Log) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the deployer panicked: %v", r)
		}
	}()
	return e.cfg.Deployer.Deploy(ctx, st, commit, log)
}

func (e *Engine) recordDropped(name, trigger string) {
	now := e.cfg.Clock.Now().UTC()
	e.cfg.Logger.Info("trigger dropped: a run is active", "stack", name, "trigger", trigger)
	_, err := e.store.Update(func(s State) (State, error) {
		s = normalize(s)
		cur := s.Stacks[name]
		cur.Dropped = &DroppedTrig{Time: now, Trigger: trigger}
		s.Stacks[name] = cur
		return s, nil
	})
	if err != nil {
		e.cfg.Logger.Warn("dropped trigger not recorded", "error", err)
	}
}

// Snapshot is the state plus the runs active right now (stack to run id).
type Snapshot struct {
	State  State
	Active map[string]string
}

// Snapshot returns a copy of the engine's state.
func (e *Engine) Snapshot() (Snapshot, error) {
	e.mu.Lock()
	active := make(map[string]string, len(e.active))
	for k, v := range e.active {
		active[k] = v
	}
	e.mu.Unlock()
	s, err := e.snapshot()
	return Snapshot{State: s, Active: active}, err
}

func (e *Engine) snapshot() (State, error) {
	s, _, err := e.store.LoadVersioned()
	if err != nil && !errors.Is(err, errNotExist) {
		return State{}, err
	}
	return normalize(s).clone(), nil
}

// Stack returns the state of one stack.
func (e *Engine) Stack(name string) (StackState, bool, error) {
	s, err := e.snapshot()
	if err != nil {
		return StackState{}, false, err
	}
	st, ok := s.Stacks[name]
	return st, ok, nil
}

// Runs lists the recorded runs, newest first; an empty stack means all stacks.
func (e *Engine) Runs(stack string) ([]RunRecord, error) {
	s, err := e.snapshot()
	if err != nil {
		return nil, err
	}
	var out []RunRecord
	for _, r := range slices.Backward(s.Runs) {
		if stack == "" || r.Stack == stack {
			out = append(out, r)
		}
	}
	return out, nil
}

// Log returns the run log of a run id; ErrNoSuchRun when the id is malformed or has no log.
func (e *Engine) Log(id string) (string, error) { return e.logs.read(id) }
