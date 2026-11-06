// Package engine polls the stacks repository, finds the stacks that changed since their last deployed commit
// and runs at most one deployment per stack. It decides when and what; how a stack is stopped, backed up and
// brought up is the Deployer's job. A failed run is rolled back: the data of the run's backup is restored, a
// commit that puts the stack folder back to its deployed state is pushed, and that state is started again.
//
// The working copy is shared with the repository commands of the service and belongs to the operator: the
// engine fetches, fast-forwards only a copy without local changes, and otherwise only reads. Its only own
// changes are a rollback's revert commit and an update's version commit, each built outside the working copy,
// pushed as a plain fast-forward and then followed by a fast-forward. A working copy without a remote (local
// mode of gitrepo) fetches nothing and lands such a commit on its branch in place of the push. It never
// resets, stashes, checks out or writes into the working copy. Its content is data; stack names from it never
// become file names.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
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
	// DefaultLockWait stays below the call limit of the command line, so a command answers ErrBusy in time.
	DefaultLockWait = 20 * time.Second
	idleWait        = time.Minute
	maxSpread       = time.Minute
	maxFindingPaths = 5

	stateFile = "state.json"
	engineDir = "deploy"
	runsDir   = "runs"
)

// Errors the callers can tell apart.
var (
	ErrNoRepo       = errors.New("no stacks repository is bound")
	ErrNoDeployer   = errors.New("deployments are not available")
	ErrRunActive    = errors.New("a run of this stack is active")
	ErrUnknownStack = errors.New("no such stack in the working copy")
	ErrUnreadable   = errors.New("the stack cannot be read")
	// ErrNotUpdatable is a stack that can be deployed but not updated, because its .env cannot be parsed.
	ErrNotUpdatable = errors.New("the stack is deploy-only and cannot be updated")
	ErrLocalChanges = errors.New("the stack has local changes in the working copy")
	ErrStopped      = errors.New("the engine is stopped")
	ErrPaused       = errors.New("the stack is paused after a failed rollback")
	ErrNotPaused    = errors.New("the stack is not paused")
	ErrPinned       = errors.New("the stack is pinned")
	// ErrNameCollision is a stack whose name a stack of another source uses; it is never deployed.
	ErrNameCollision = errors.New("another stack folder already uses the name of this stack")
	// ErrBusy means a command gave up waiting for the working-copy lock, which an update's commit and push or a
	// rollback's revert may hold for minutes.
	ErrBusy = errors.New("the working copy is busy; try again")
	// ErrNoBackup is what Deployer.Backup returns for a stack without backup rules: the run goes on without a
	// backup, and a rollback without a data restore.
	ErrNoBackup       = errors.New("the stack has no backup rules")
	errAlreadyRunning = errors.New("the poll loop is already running")
)

// WorkingCopy is the git layer the engine needs; *gitrepo.Repo implements it. RevertStack, CommitFile and
// Push follow the contract and the errors of gitrepo, also of its local mode, in which Fetch does nothing,
// RemoteHead is the branch itself and Push moves the working copy to the commit.
type WorkingCopy interface {
	Fetch(ctx context.Context) error
	Head(ctx context.Context) (string, error)
	RemoteHead(ctx context.Context) (string, error)
	Status(ctx context.Context) ([]gitrepo.Change, error)
	ChangedFolders(ctx context.Context, from, to string) ([]string, error)
	FastForward(ctx context.Context, commit string) error
	RevertStack(ctx context.Context, stack, deployed, failed, reason string) (string, error)
	CommitFile(ctx context.Context, path string, edit func(old []byte) ([]byte, error), message string) (string, error)
	IsAncestor(ctx context.Context, a, b string) (bool, error)
	Push(ctx context.Context, commit string) error
}

// Binding is the working copy to watch and the directory it lives in. Blocked, when set, maps the names of the
// stacks in it that another source already uses to the reason: such a stack gets a name-collision finding and
// no run. It is asked after the stack list is read, so it can judge the list as the engine sees it.
type Binding struct {
	Dir     string
	Copy    WorkingCopy
	Blocked func() map[string]string
}

func (b Binding) blockedNames() map[string]string {
	if b.Blocked == nil {
		return nil
	}
	return b.Blocked()
}

// blocked refuses a stack whose name another source uses.
func (b Binding) blocked(name string) error {
	if _, ok := b.blockedNames()[name]; ok {
		return ErrNameCollision
	}
	return nil
}

// Deployer carries out the steps of a run on one stack. A run calls Stop, Backup, Pull, Up and Check in this
// order and stops at the first error; its rollback calls Stop, Restore, Up and Check. While a step runs, the
// working copy is not moved, so the stack folder is the one the step was given.
type Deployer interface {
	// Stop stops the containers of the stack; a stack that does not run is no error.
	Stop(ctx context.Context, stack stacks.Stack, log *Log) error
	// Backup saves the data of the stopped stack, which belongs to the commit deployed, and returns the id of
	// the backup; ErrNoBackup when the stack has no backup rules. It leaves the stack stopped.
	Backup(ctx context.Context, stack stacks.Stack, deployed string, log *Log) (string, error)
	// Pull fetches the images of the stack; nothing is started.
	Pull(ctx context.Context, stack stacks.Stack, log *Log) error
	// Up starts the stack.
	Up(ctx context.Context, stack stacks.Stack, log *Log) error
	// Start starts the existing, stopped containers of the stack without recreating them.
	Start(ctx context.Context, stack stacks.Stack, log *Log) error
	// Restart restarts the containers of the stack without recreating them.
	Restart(ctx context.Context, stack stacks.Stack, log *Log) error
	// Check tells whether the started stack works.
	Check(ctx context.Context, stack stacks.Stack, log *Log) error
	// Restore puts the data of backup id back into the stopped stack and leaves it stopped.
	Restore(ctx context.Context, stack stacks.Stack, id string, log *Log) error
}

// DataDirChecker is implemented by a Deployer that can tell, before a run touches the stack, which bind-mount
// source directories below the stack folder are missing although the image runs as a non-root user. A
// Deployer without it makes the run skip that check.
type DataDirChecker interface {
	// MissingDataDirs returns the directories Docker would create as root. versioned reports whether a path
	// relative to the stack folder is tracked in the working copy; such a path is never a candidate. It
	// creates nothing.
	MissingDataDirs(ctx context.Context, stack stacks.Stack, versioned func(ctx context.Context, rel string) (bool, error), log *Log) ([]MissingDir, error)
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
	// Lock serializes moving and evaluating the working copy with the other users of it; the engine holds it
	// over a fetch or a push only in a run's commit or revert. Nil means a private mutex.
	Lock sync.Locker
	// Refs serializes every fetch and every update of the remote-tracking ref with the other users of the
	// working copy. Whoever needs both takes Lock first. Nil means a private mutex.
	Refs sync.Locker
	// LockWait bounds how long a command waits for Lock before it answers ErrBusy; DefaultLockWait when zero.
	LockWait time.Duration
	// Deployer is nil in a service that cannot deploy yet: the engine then only polls, fast-forwards and
	// reports pending stacks, and never starts a run or records a deployed commit.
	Deployer Deployer
	// Events, when set, is told the outcome of every finished run. It is called after the result is stored,
	// for a run that is no update step also after the stack's slot is free, and must return quickly: Close
	// waits for it.
	Events func(Event)
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
	refs  sync.Locker
	store *configfile.Store[State]
	logs  logStore

	base   context.Context
	cancel context.CancelFunc
	wake   chan struct{}

	pollMu  sync.Mutex // one poll at a time; taken before refs and lock, never while holding either
	running sync.Mutex // guards the loop flag
	looping bool

	stateMu sync.Mutex // one state write of this engine at a time, see update

	mu     sync.Mutex // guards active, closed, holds, idle and wg.Add
	active map[string]string
	closed bool
	wg     sync.WaitGroup
	// holds counts the runs and claims that rely on the working copy staying where it is; idle is closed and
	// replaced whenever holds drops to zero. See exclusive.
	holds int
	idle  chan struct{}
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
	if cfg.LockWait <= 0 {
		cfg.LockWait = DefaultLockWait
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
		cfg: cfg, lock: cfg.Lock, refs: cfg.Refs, store: store, logs: logStore{dir: filepath.Join(dir, runsDir)},
		wake: make(chan struct{}, 1), active: map[string]string{}, idle: make(chan struct{}),
	}
	if e.lock == nil {
		e.lock = &sync.Mutex{}
	}
	if e.refs == nil {
		e.refs = &sync.Mutex{}
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
// changed since their deployed commit and start a run for each. Runs continue in the background. The fetch
// holds only the ref lock and ends before the working-copy lock is taken, so a slow remote keeps no command
// waiting.
//
// When the working copy has local changes other than the host's own (see classify), anywhere in it, also in
// files outside the stacks, the fast-forward is not attempted at all: git would either refuse or merge around
// the changes. The stacks with local changes are skipped with a finding; the other stacks are still reported
// as pending at the fetched commit and run only up to the commit the working copy has.
func (e *Engine) Poll(ctx context.Context) (PollResult, error) {
	e.pollMu.Lock()
	defer e.pollMu.Unlock()
	b, err := e.cfg.Source()
	if err != nil {
		return PollResult{}, err
	}
	if err := e.fetch(ctx, b.Copy); err != nil {
		perr := fmt.Errorf("fetch: %w", err)
		e.recordPollError(perr)
		return PollResult{}, perr
	}
	e.lock.Lock()
	defer e.lock.Unlock()
	res, perr := e.poll(ctx, b)
	if perr != nil {
		e.recordPollError(perr)
	}
	return res, perr
}

// fetch fetches under the ref lock alone.
func (e *Engine) fetch(ctx context.Context, wc WorkingCopy) error {
	e.refs.Lock()
	defer e.refs.Unlock()
	return wc.Fetch(ctx)
}

// poll evaluates the fetched branch. The caller holds the working-copy lock: only a fetch can move the
// remote-tracking ref meanwhile, and the fast-forward goes to the commit read here, not further.
func (e *Engine) poll(ctx context.Context, b Binding) (PollResult, error) {
	var res PollResult
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
	// A list that cannot be read here is read again below, where its error counts.
	list, _ := stacks.Load(b.Dir)
	local, dirty := classify(changes, list)

	var note string
	var ffErr error
	if head != remote {
		switch {
		case dirty:
			note = "fast-forward skipped: the working copy has local changes"
		case e.activeCount() > 0:
			note = "fast-forward skipped: a run is active"
		default:
			if ffErr = b.Copy.FastForward(ctx, remote); ffErr == nil {
				res.FastForwarded = true
				if head, err = b.Copy.Head(ctx); err != nil {
					return res, err
				}
			}
		}
	}
	res.Head, res.RemoteHead = head, remote

	list, err = stacks.Load(b.Dir)
	if err != nil {
		return res, fmt.Errorf("read stacks: %w", err)
	}
	snap, err := e.snapshot()
	if err != nil {
		return res, err
	}
	blocked := b.blockedNames()
	folders := &diffCache{ctx: ctx, copy: b.Copy, memo: map[[2]string][]string{}}

	type verdict struct {
		findings []Finding
		pending  string
		start    bool
		folder   string
		// clearWait says the stack no longer waits for data directories.
		clearWait bool
	}
	verdicts := map[string]verdict{}
	present := map[string]bool{}
	// renamed maps a folder to the listed name it now has; an unreadable stack's name is not its project name.
	renamed := map[string]string{}
	// unreadable holds the folders that are listed but cannot be read; their old entry is not gone.
	unreadable := map[string]bool{}
	for _, st := range list {
		present[st.Name] = true
		if st.Err == nil {
			renamed[st.Folder] = st.Name
		} else {
			unreadable[st.Folder] = true
		}
	}
	// gone maps a folder to the deployed stacks no longer listed, whose Compose projects are still there.
	gone := map[string][]string{}
	for name, s := range snap.Stacks {
		if !present[name] && s.Deployed != "" {
			gone[s.folder(name)] = append(gone[s.folder(name)], name)
		}
	}
	for _, st := range list {
		old := snap.Stacks[st.Name]
		v := verdict{folder: folderField(st)}
		if detail, ok := blocked[st.Name]; ok {
			v.findings = []Finding{{Kind: FindingNameCollision, Detail: e.text(detail)}}
			verdicts[st.Name] = v
			continue
		}
		changedAtHead, errHead := folders.changed(old.Deployed, head, st.Folder)
		changedAtRemote := changedAtHead
		var errRemote error
		if remote != head {
			changedAtRemote, errRemote = folders.changed(old.Deployed, remote, st.Folder)
		}
		diffErr := errors.Join(errHead, errRemote)
		if diffErr == nil && changedAtRemote {
			v.pending = remote
		}
		// A local edit can itself make a stack unreadable, so it is reported first.
		switch paths := local[st.Folder]; {
		case len(paths) > 0:
			v.findings = append(v.findings, Finding{Kind: FindingLocalChanges, Detail: e.localDetail(paths)})
		case st.Err != nil:
			v.findings = append(v.findings, Finding{Kind: FindingUnreadable, Detail: e.text(st.Err.Error())})
		case diffErr != nil:
			v.findings = append(v.findings, Finding{Kind: FindingDiff, Detail: e.text(diffErr.Error())})
		case changedAtHead && old.FailedCommit == head:
			v.findings = append(v.findings, Finding{Kind: FindingFailedCommit, Detail: "the last run of this commit failed; a new commit or a manual run retries"})
		case changedAtHead:
			v.start = true
		}
		if w := old.DataDirs; w != nil {
			if w.Commit == head && dirsMissing(st.Dir, w.Dirs) {
				v.start = false
				v.findings = append([]Finding{dataDirFinding(w.Dirs)}, v.findings...)
			} else {
				v.clearWait = true
			}
		}
		// Deploying the new name would start a second copy of the stack next to the old project.
		if from := gone[st.Folder]; st.Err == nil && old.Deployed == "" && len(from) > 0 {
			slices.Sort(from)
			v.start = false
			v.findings = append([]Finding{{Kind: FindingProjectRenamed, Detail: e.text(fmt.Sprintf(
				"the folder %s was deployed as project %s and is now project %s; nothing is deployed until a person "+
					"decides: a manual deploy starts %s next to the old project, which is not stopped",
				st.Folder, strings.Join(from, ", "), st.Name, st.Name))}}, v.findings...)
		}
		if old.Paused != nil {
			v.start = false
			v.findings = append([]Finding{{Kind: FindingPaused, Detail: e.pauseDetail(old.Paused)}}, v.findings...)
		}
		if old.Stopped != nil {
			v.start = false
			v.findings = append([]Finding{{Kind: FindingStopped, Detail: "stopped by the operator since " +
				old.Stopped.Since.UTC().Format(time.RFC3339) + "; no run starts until 'qdeploy stack start'"}}, v.findings...)
		}
		verdicts[st.Name] = v
	}

	// The state is written before runs start, so a run that ends at once never races a stale write.
	now := e.cfg.Clock.Now().UTC()
	err = e.update(func(s State) (State, error) {
		s = normalize(s)
		for name, v := range verdicts {
			cur := s.Stacks[name]
			cur.Findings, cur.Pending, cur.Removed, cur.Folder = v.findings, v.pending, false, v.folder
			if v.clearWait {
				cur.DataDirs = nil
			}
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
			cur.Findings = []Finding{{Kind: FindingRemoved, Detail: "the folder is gone from the repository; nothing is stopped or removed"}}
			if unreadable[cur.folder(name)] {
				cur.Findings = []Finding{{Kind: FindingRemoved, Detail: "the stack cannot be read; nothing is stopped or removed"}}
			}
			if to := renamed[cur.folder(name)]; to != "" && cur.Deployed != "" {
				cur.Findings = []Finding{{Kind: FindingProjectRenamed, Detail: e.text(fmt.Sprintf(
					"the folder %s is now project %s; this project is not stopped or removed", cur.folder(name), to))}}
			}
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
			_, err := e.start(b, byName[name], head, TriggerPoll)
			switch {
			case err == nil:
				res.Started = append(res.Started, name)
			case errors.Is(err, ErrRunActive):
				res.Dropped = append(res.Dropped, name)
			case errors.Is(err, ErrPaused), errors.Is(err, ErrStackStopped):
				// Paused by a run that ended after the state above was read; the next poll reports it.
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
	err := e.update(func(s State) (State, error) {
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

// dataDirFinding is the finding for a stack that waits for its data directories.
func dataDirFinding(dirs []MissingDir) Finding {
	return Finding{Kind: FindingDataDirMissing, Detail: dataDirDetail(dirs), Dirs: slices.Clone(dirs)}
}

func dataDirDetail(dirs []MissingDir) string {
	parts := make([]string, len(dirs))
	for i, d := range dirs {
		parts[i] = d.Path
		if d.Owner != "" {
			parts[i] += " (owner " + d.Owner + ")"
		}
	}
	return "data directories are missing and the image does not run as root: " + strings.Join(parts, ", ") +
		"; the stack starts once they exist"
}

// dirsMissing reports whether any of the directories, relative to the stack folder, is still absent.
func dirsMissing(stackDir string, dirs []MissingDir) bool {
	for _, d := range dirs {
		if !ValidDirPath(d.Path) {
			continue
		}
		if _, err := os.Lstat(filepath.Join(stackDir, filepath.FromSlash(d.Path))); errors.Is(err, fs.ErrNotExist) {
			return true
		}
	}
	return false
}

func (e *Engine) pauseDetail(p *Pause) string {
	detail := "the rollback of run " + p.Run + " failed; the stack stays stopped and gets no runs until it is resumed"
	if p.Reason != "" {
		detail += ": " + p.Reason
	}
	return e.text(detail)
}

func normalize(s State) State {
	s.Schema = stateSchema
	if s.Stacks == nil {
		s.Stacks = map[string]StackState{}
	}
	return s
}

// classify maps the local changes to the stack folders they lie in. Changes to <stack>/secrets.env and below
// <stack>/data/ are the host's own and count as neither, and so are untracked files below a bind mount of the
// stack in list (Stack.BindMounts); a changed tracked file there still counts. dirty is true when any other
// change exists, also outside every stack. A root stack in list holds every path but those below
// stacks.SettingsTop.
func classify(changes []gitrepo.Change, list []stacks.Stack) (byStack map[string][]string, dirty bool) {
	mounts := map[string][]string{}
	root := false
	for _, st := range list {
		mounts[st.Folder] = st.BindMounts
		root = root || st.Folder == stacks.RootFolder
	}
	byStack = map[string][]string{}
	for _, c := range changes {
		folder, rel, inStack := strings.Cut(c.Path, "/")
		if root {
			folder, rel, inStack = stacks.RootFolder, c.Path, folder != stacks.SettingsTop
		}
		if inStack && (rel == "secrets.env" || strings.HasPrefix(rel, "data/")) {
			continue
		}
		if inStack && c.Code == "??" && slices.ContainsFunc(mounts[folder], func(m string) bool {
			return rel == m || strings.HasPrefix(rel, m+"/")
		}) {
			continue
		}
		dirty = true
		if inStack {
			byStack[folder] = append(byStack[folder], c.Path)
		}
	}
	return byStack, dirty
}

// folderOf returns the folder of the stack called name; an unknown name is taken as a folder.
func folderOf(list []stacks.Stack, name string) string {
	if i := slices.IndexFunc(list, func(s stacks.Stack) bool { return s.Name == name }); i >= 0 {
		return list[i].Folder
	}
	return name
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
// ErrRunActive and noted in the state. A stack with local changes is refused with ErrLocalChanges, a paused
// stack with ErrPaused, a stack stopped through qdeploy with ErrStackStopped.
func (e *Engine) Trigger(ctx context.Context, name string) (RunInfo, error) {
	if e.cfg.Deployer == nil {
		return RunInfo{}, ErrNoDeployer
	}
	b, err := e.cfg.Source()
	if err != nil {
		return RunInfo{}, err
	}
	if err := e.lockCommand(ctx); err != nil {
		return RunInfo{}, err
	}
	defer e.lock.Unlock()
	changes, err := b.Copy.Status(ctx)
	if err != nil {
		return RunInfo{}, err
	}
	list, loadErr := stacks.Load(b.Dir)
	if local, _ := classify(changes, list); len(local[folderOf(list, name)]) > 0 {
		return RunInfo{}, fmt.Errorf("%w: %s", ErrLocalChanges, e.localDetail(local[folderOf(list, name)]))
	}
	head, err := b.Copy.Head(ctx)
	if err != nil {
		return RunInfo{}, err
	}
	if loadErr != nil {
		return RunInfo{}, fmt.Errorf("read stacks: %w", loadErr)
	}
	for _, st := range list {
		if st.Name != name {
			continue
		}
		if st.Err != nil {
			return RunInfo{}, fmt.Errorf("%w: %s", ErrUnreadable, e.text(st.Err.Error()))
		}
		if err := b.blocked(name); err != nil {
			return RunInfo{}, err
		}
		return e.start(b, st, head, TriggerManual)
	}
	return RunInfo{}, ErrUnknownStack
}

// start takes the stack's slot and a hold on the working copy and launches the run. The caller holds the
// working-copy lock.
func (e *Engine) start(b Binding, st stacks.Stack, commit, trigger string) (RunInfo, error) {
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
	// Read with mu held: a run stores its pause before it frees the slot, so none can slip in between.
	cur, _, err := e.Stack(st.Name)
	if err != nil {
		e.mu.Unlock()
		return RunInfo{}, err
	}
	if cur.Paused != nil {
		e.mu.Unlock()
		return RunInfo{}, ErrPaused
	}
	// Every run ends in up, which would start a stack the operator stopped; only a start clears the mark.
	if cur.Stopped != nil {
		e.mu.Unlock()
		return RunInfo{}, ErrStackStopped
	}
	now := e.cfg.Clock.Now().UTC()
	id, w, err := e.newLog(now)
	if err != nil {
		e.mu.Unlock()
		return RunInfo{}, fmt.Errorf("create the run log: %w", err)
	}
	e.active[st.Name] = id
	e.holds++
	e.wg.Add(1)
	e.mu.Unlock()

	r := &run{
		e: e, b: b, st: st, deployed: cur.Deployed, held: true,
		log: &Log{w: w, redact: e.cfg.Redact, now: e.cfg.Clock.Now},
		rec: RunRecord{ID: id, Stack: st.Name, Commit: commit, Trigger: trigger, Started: now},
	}
	go r.execute()
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

func (e *Engine) recordDropped(name, trigger string) {
	now := e.cfg.Clock.Now().UTC()
	e.cfg.Logger.Info("trigger dropped: a run is active", "stack", name, "trigger", trigger)
	err := e.update(func(s State) (State, error) {
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

// RecordUpdates stores the result of an update detection for a stack, replacing the earlier one. It touches
// nothing else of the stack's state, so it never overwrites a run's result and a run never drops it. The
// result is clipped to the stored limits.
func (e *Engine) RecordUpdates(stack string, check UpdateCheck) error {
	check.Checked = check.Checked.UTC()
	if len(check.Items) > MaxUpdateItems {
		check.Items = check.Items[:MaxUpdateItems]
	}
	check.Items = append([]UpdateItem(nil), check.Items...)
	for i := range check.Items {
		check.Items[i].Error = e.text(check.Items[i].Error)
	}
	if err := validateUpdates(check); err != nil {
		return err
	}
	err := e.update(func(s State) (State, error) {
		s = normalize(s)
		cur := s.Stacks[stack]
		cur.Updates = &check
		s.Stacks[stack] = cur
		return s, nil
	})
	return err
}

// RecordMonitor stores what the container monitor knows about a stack: its open failure (nil for none) and the
// services seen healthy. It touches nothing else of the stack's state. A stack the engine has no state for is
// ignored, so the monitor never creates one.
func (e *Engine) RecordMonitor(stack string, failure *Failure, services []string) error {
	var f *Failure
	if failure != nil {
		c := *failure
		c.Since = c.Since.UTC()
		f = &c
	}
	services = append([]string(nil), services...)
	if err := validateMonitor(StackState{Failure: f, Services: services}); err != nil {
		return err
	}
	_, err := e.store.Update(func(s State) (State, error) {
		s = normalize(s)
		cur, ok := s.Stacks[stack]
		if !ok {
			return s, nil
		}
		cur.Failure, cur.Services = f, services
		s.Stacks[stack] = cur
		return s, nil
	})
	return err
}

// UpdateStoppedContainers adds and removes container IDs of the stack's stop marker in one step, so a stop
// through qdeploy and the monitor's cleanup never overwrite each other. A stack the engine has no state for is
// ignored.
func (e *Engine) UpdateStoppedContainers(stack string, add, remove []string) error {
	_, err := e.store.Update(func(s State) (State, error) {
		s = normalize(s)
		cur, ok := s.Stacks[stack]
		if !ok {
			return s, nil
		}
		ids := slices.DeleteFunc(slices.Clone(cur.StoppedContainers), func(id string) bool {
			return slices.Contains(remove, id) || slices.Contains(add, id)
		})
		ids = append(ids, add...)
		if err := validateMonitor(StackState{StoppedContainers: ids}); err != nil {
			return s, err
		}
		cur.StoppedContainers = ids
		s.Stacks[stack] = cur
		return s, nil
	})
	return err
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

// update changes the state. The store's lock orders writers too, but the first write creates the state
// directory, and two writers doing that at once can fail; the engine's own writers therefore go one at a time.
func (e *Engine) update(change func(State) (State, error)) error {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	_, err := e.store.Update(change)
	return err
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

// AppendLog adds one info entry to the log of an existing run, for facts that arise after the run has ended,
// such as a notification that was not delivered. The text is redacted like every other entry. It fails with
// ErrNoSuchRun when the id is malformed or the log is missing or not a regular file, and with ErrLogFull
// when the log has reached its size bound.
func (e *Engine) AppendLog(id, text string) error {
	if e.cfg.Redact != nil {
		text = e.cfg.Redact(text)
	}
	return e.logs.appendLine(id, e.cfg.Clock.Now().UTC().Format(time.RFC3339), "info", text)
}

// WithStack calls fn with the stack, read from the working copy, and the commit last deployed (empty if
// none), while holding the stack's run slot: no run of the stack starts or is active meanwhile, and neither a
// poll nor a rollback moves the working copy. It is for work that must not overlap a deployment, such as a
// backup. label is shown as the active run of the stack and is no run id. A stack that is busy, unknown,
// unreadable or has local changes is refused as Trigger refuses it; a paused stack is accepted. fn runs in the
// caller's goroutine; its context also ends when the engine closes.
func (e *Engine) WithStack(ctx context.Context, name, label string,
	fn func(ctx context.Context, st stacks.Stack, deployed string) error) error {
	b, err := e.cfg.Source()
	if err != nil {
		return err
	}
	var (
		st      stacks.Stack
		release func()
	)
	err = func() error {
		if err := e.lockCommand(ctx); err != nil {
			return err
		}
		defer e.lock.Unlock()
		changes, err := b.Copy.Status(ctx)
		if err != nil {
			return err
		}
		list, err := stacks.Load(b.Dir)
		if local, _ := classify(changes, list); len(local[folderOf(list, name)]) > 0 {
			return fmt.Errorf("%w: %s", ErrLocalChanges, e.localDetail(local[folderOf(list, name)]))
		}
		if err != nil {
			return fmt.Errorf("read stacks: %w", err)
		}
		i := slices.IndexFunc(list, func(s stacks.Stack) bool { return s.Name == name })
		switch {
		case i < 0:
			return ErrUnknownStack
		case list[i].Err != nil:
			return fmt.Errorf("%w: %s", ErrUnreadable, e.text(list[i].Err.Error()))
		}
		if err := b.blocked(name); err != nil {
			return err
		}
		st = list[i]
		release, err = e.claim(name, label)
		return err
	}()
	if err != nil {
		return err
	}
	defer release()
	cur, _, err := e.Stack(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(e.base, cancel)()
	return fn(ctx, st, cur.Deployed)
}

// claim takes the run slot of a stack for work that is no deployment run.
func (e *Engine) claim(name, label string) (release func(), err error) {
	if err := e.take(name, label); err != nil {
		return nil, err
	}
	return func() { e.free(name, true) }, nil
}

// take takes the run slot of a stack and a hold on the working copy.
func (e *Engine) take(name, label string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrStopped
	}
	if _, busy := e.active[name]; busy {
		return ErrRunActive
	}
	e.active[name] = label
	e.holds++
	e.wg.Add(1)
	return nil
}

// free gives up what take took; held is false when the hold was given up already.
func (e *Engine) free(name string, held bool) {
	e.mu.Lock()
	delete(e.active, name)
	e.mu.Unlock()
	if held {
		e.releaseHold()
	}
	e.wg.Done()
}
