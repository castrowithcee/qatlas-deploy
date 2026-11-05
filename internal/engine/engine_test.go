package engine

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

func commit(n int) string { return fmt.Sprintf("%040x", n) }

// fakeCopy stands in for the git layer; FastForward writes into the directory so a forbidden call is visible.
// RevertStack and Push take their answers from revertErrs and pushErrs, one per call, nil once used up.
type fakeCopy struct {
	mu           sync.Mutex
	dir          string
	head, remote string
	changes      []gitrepo.Change
	folders      map[[2]string][]string
	fetchErr     error
	ffErr        error
	revertErrs   []error
	pushErrs     []error
	reverts      []revertCall
	pushed       []string
	commitErrs   []error
	diverged     bool // the working copy has commits the branch lacks
	commits      []commitCall
	// files holds what a fast-forward to a commit made by CommitFile or RevertStack writes, by path.
	files   map[string]map[string][]byte
	onFetch func(*fakeCopy) // called with mu held
	// fetchGate, when set, holds every Fetch until it is closed, outside mu; fetchIn gets a value per held
	// Fetch.
	fetchGate     chan struct{}
	fetchIn       chan struct{}
	onStatus      func(*fakeCopy) // called with mu held
	calls         []string
	fastForwarded int
	ffTo          []string
}

type revertCall struct{ Stack, Deployed, Failed, Reason, Base string }

type commitCall struct {
	Path, Message, Base string
	Old, New            []byte
}

func pop(errs *[]error) error {
	if len(*errs) == 0 {
		return nil
	}
	err := (*errs)[0]
	*errs = (*errs)[1:]
	return err
}

func (f *fakeCopy) rec(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

func (f *fakeCopy) Fetch(ctx context.Context) error {
	f.rec("fetch")
	f.mu.Lock()
	gate, in := f.fetchGate, f.fetchIn
	f.mu.Unlock()
	if gate != nil {
		select {
		case in <- struct{}{}:
		default:
		}
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.onFetch != nil {
		f.onFetch(f)
	}
	return f.fetchErr
}

// RevertStack answers with a new commit id per call, built on the remote head.
func (f *fakeCopy) RevertStack(_ context.Context, stack, deployed, failed, reason string) (string, error) {
	f.rec("revert")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reverts = append(f.reverts, revertCall{stack, deployed, failed, reason, f.remote})
	if err := pop(&f.revertErrs); err != nil {
		return "", err
	}
	id := commit(1000 + len(f.reverts))
	// Reverting a commit of CommitFile puts its file back.
	for i, c := range f.commits {
		if commit(2001+i) == failed {
			f.files[id] = map[string][]byte{c.Path: c.Old}
		}
	}
	return id, nil
}

func (f *fakeCopy) IsAncestor(_ context.Context, a, b string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.diverged || a == b, nil
}

// CommitFile edits the file as the working copy has it and answers with a new commit id per call; a
// fast-forward to the commit writes the result.
func (f *fakeCopy) CommitFile(_ context.Context, path string, edit func([]byte) ([]byte, error), message string) (string, error) {
	f.rec("commit")
	old, err := os.ReadFile(filepath.Join(f.dir, path))
	if err != nil {
		return "", err
	}
	content, err := edit(old)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := pop(&f.commitErrs); err != nil {
		return "", err
	}
	f.commits = append(f.commits, commitCall{Path: path, Message: message, Base: f.remote, Old: old, New: content})
	id := commit(2000 + len(f.commits))
	f.files[id] = map[string][]byte{path: content}
	return id, nil
}

func (f *fakeCopy) Push(_ context.Context, c string) error {
	f.rec("push")
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := pop(&f.pushErrs); err != nil {
		return err
	}
	f.pushed = append(f.pushed, c)
	f.remote = c
	return nil
}
func (f *fakeCopy) Head(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.head, nil
}
func (f *fakeCopy) RemoteHead(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.remote, nil
}
func (f *fakeCopy) Status(context.Context) ([]gitrepo.Change, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.onStatus != nil {
		f.onStatus(f)
	}
	return slices.Clone(f.changes), nil
}
func (f *fakeCopy) ChangedFolders(_ context.Context, from, to string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.folders[[2]string{from, to}], nil
}
func (f *fakeCopy) FastForward(_ context.Context, to string) error {
	f.rec("ff")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ffTo = append(f.ffTo, to)
	if f.ffErr != nil {
		return f.ffErr
	}
	f.fastForwarded++
	f.head = to
	for path, body := range f.files[f.head] {
		if err := os.WriteFile(filepath.Join(f.dir, path), body, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(f.dir, "fast-forwarded"), []byte("x"), 0o644)
}

func (f *fakeCopy) called(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == name {
			n++
		}
	}
	return n
}

func (f *fakeCopy) set(fn func(*fakeCopy)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

type call struct{ Stack, Commit string }

// fakeDeployer records every step as "<stack>:<step>". A run's pull is recorded in calls with the commit the
// working copy has, which is what the run deploys. errs holds the answers of a step, one per call, nil once
// used up; block, entered and action apply to pull.
type fakeDeployer struct {
	mu      sync.Mutex
	head    func() string
	calls   []call
	steps   []string
	errs    map[string][]error
	block   chan struct{} // when set, Pull waits for it
	entered chan struct{}
	action  func(*Log)
	// hook, when set, runs at the start of every step; an error from it is the step's.
	hook func(ctx context.Context, step string, st stacks.Stack) error
	// noRules makes Backup answer ErrNoBackup.
	noRules bool
	// trace, when set, gets every step as it starts.
	trace func(string)
	// backedUp lists the deployed commit of every backup.
	backedUp []string
}

func (d *fakeDeployer) do(ctx context.Context, step string, st stacks.Stack) error {
	d.mu.Lock()
	d.steps = append(d.steps, st.Name+":"+step)
	var err error
	if list := d.errs[step]; len(list) > 0 {
		err, d.errs[step] = list[0], list[1:]
	}
	hook, trace := d.hook, d.trace
	d.mu.Unlock()
	if trace != nil {
		trace(st.Name + ":" + step)
	}
	if hook != nil {
		if herr := hook(ctx, step, st); herr != nil {
			return herr
		}
	}
	return err
}

func (d *fakeDeployer) fail(step string, errs ...error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.errs == nil {
		d.errs = map[string][]error{}
	}
	d.errs[step] = append(d.errs[step], errs...)
}

func (d *fakeDeployer) Stop(ctx context.Context, st stacks.Stack, _ *Log) error {
	return d.do(ctx, "stop", st)
}

func (d *fakeDeployer) Backup(ctx context.Context, st stacks.Stack, deployed string, _ *Log) (string, error) {
	if err := d.do(ctx, "backup", st); err != nil {
		return "", err
	}
	d.mu.Lock()
	d.backedUp = append(d.backedUp, deployed)
	d.mu.Unlock()
	if d.noRules {
		return "", ErrNoBackup
	}
	return "backup-of-" + deployed[:8], nil
}

func (d *fakeDeployer) Pull(ctx context.Context, st stacks.Stack, log *Log) error {
	d.mu.Lock()
	commit := ""
	if d.head != nil {
		commit = d.head()
	}
	d.calls = append(d.calls, call{st.Name, commit})
	block, entered, action := d.block, d.entered, d.action
	d.mu.Unlock()
	if action != nil {
		action(log)
	}
	if entered != nil {
		entered <- struct{}{}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return d.do(ctx, "pull", st)
}

func (d *fakeDeployer) Up(ctx context.Context, st stacks.Stack, _ *Log) error {
	return d.do(ctx, "up", st)
}

func (d *fakeDeployer) Start(ctx context.Context, st stacks.Stack, _ *Log) error {
	return d.do(ctx, "start", st)
}

func (d *fakeDeployer) Restart(ctx context.Context, st stacks.Stack, _ *Log) error {
	return d.do(ctx, "restart", st)
}

func (d *fakeDeployer) Check(ctx context.Context, st stacks.Stack, _ *Log) error {
	return d.do(ctx, "check", st)
}

func (d *fakeDeployer) Restore(ctx context.Context, st stacks.Stack, id string, _ *Log) error {
	return d.do(ctx, "restore "+id, st)
}

func (d *fakeDeployer) got() []call {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.calls)
}

func (d *fakeDeployer) stepsOf(stack string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for _, s := range d.steps {
		if name, step, _ := strings.Cut(s, ":"); name == stack {
			out = append(out, step)
		}
	}
	return out
}

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, waiter{c.now.Add(d), ch})
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	keep := c.waiters[:0]
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			w.ch <- c.now
		} else {
			keep = append(keep, w)
		}
	}
	c.waiters = keep
}

func (c *fakeClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

type rig struct {
	t     *testing.T
	wc    string // working copy directory
	state string
	copy  *fakeCopy
	dep   *fakeDeployer
	clock *fakeClock
	eng   *Engine
	cfg   Config
	redac *redact.Redactor
}

func writeStack(t *testing.T, wc, name string) {
	t.Helper()
	dir := filepath.Join(wc, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "services:\n  app:\n    image: registry.example.test/demo/app:${APP_VERSION}\n"
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_VERSION=1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRig(t *testing.T, names ...string) *rig {
	t.Helper()
	r := &rig{t: t, wc: t.TempDir(), state: t.TempDir(), redac: &redact.Redactor{}}
	for _, n := range names {
		writeStack(t, r.wc, n)
	}
	r.copy = &fakeCopy{dir: r.wc, head: commit(1), remote: commit(1), folders: map[[2]string][]string{}, files: map[string]map[string][]byte{}}
	r.dep = &fakeDeployer{head: func() string {
		h, _ := r.copy.Head(context.Background())
		return h
	}}
	r.clock = &fakeClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	r.cfg = Config{
		StateDir: r.state,
		Source:   func() (Binding, error) { return Binding{Dir: r.wc, Copy: r.copy}, nil },
		Deployer: r.dep, Redact: r.redac.Apply, Clock: r.clock,
		Jitter: func(time.Duration) time.Duration { return 0 },
	}
	r.eng = r.start()
	return r
}

func (r *rig) start() *Engine {
	r.t.Helper()
	e, err := New(r.cfg)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(e.Close)
	return e
}

func (r *rig) poll() PollResult {
	r.t.Helper()
	res, err := r.eng.Poll(context.Background())
	if err != nil {
		r.t.Fatalf("poll: %v", err)
	}
	r.eng.Wait()
	return res
}

func (r *rig) stack(name string) StackState {
	r.t.Helper()
	st, _, err := r.eng.Stack(name)
	if err != nil {
		r.t.Fatal(err)
	}
	return st
}

// deployAll brings every stack to the head.
func (r *rig) deployAll() {
	r.poll()
	r.dep.mu.Lock()
	r.dep.calls, r.dep.steps = nil, nil
	r.dep.mu.Unlock()
}

func hasFinding(st StackState, kind string) bool {
	return slices.ContainsFunc(st.Findings, func(f Finding) bool { return f.Kind == kind })
}

func TestChangedFolderRunsExactlyOneStack(t *testing.T) {
	r := newRig(t, "web", "db")
	r.deployAll()
	if got := r.stack("web").Deployed; got != commit(1) {
		t.Fatalf("first poll deployed %q", got)
	}

	// unchanged: nothing runs
	if res := r.poll(); len(res.Started) != 0 || len(r.dep.got()) != 0 {
		t.Fatalf("unchanged repo started %v / %v", res.Started, r.dep.got())
	}

	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
	})
	res := r.poll()
	if !res.FastForwarded || !slices.Equal(res.Started, []string{"web"}) {
		t.Fatalf("result = %+v", res)
	}
	if got := r.dep.got(); len(got) != 1 || got[0] != (call{"web", commit(2)}) {
		t.Fatalf("deploy calls = %v", got)
	}
	if r.stack("web").Deployed != commit(2) || r.stack("db").Deployed != commit(1) {
		t.Fatalf("deployed web=%s db=%s", r.stack("web").Deployed, r.stack("db").Deployed)
	}
	if r.stack("web").LastRun.Result != ResultSucceeded {
		t.Fatalf("last run = %+v", r.stack("web").LastRun)
	}
	if again := r.poll(); len(again.Started) != 0 {
		t.Fatalf("second poll started %v", again.Started)
	}
}

func TestNewStackIsDetected(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	writeStack(t, r.wc, "fresh")
	res := r.poll()
	if !slices.Equal(res.Started, []string{"fresh"}) {
		t.Fatalf("started %v", res.Started)
	}
	if got := r.dep.got(); len(got) != 1 || got[0] != (call{"fresh", commit(1)}) {
		t.Fatalf("calls %v", got)
	}
}

func TestRemovedStackIsReportedAndNotTouched(t *testing.T) {
	r := newRig(t, "web", "old")
	r.deployAll()
	if err := os.RemoveAll(filepath.Join(r.wc, "old")); err != nil {
		t.Fatal(err)
	}
	r.poll()
	st := r.stack("old")
	if !st.Removed || !hasFinding(st, FindingRemoved) || st.Deployed != commit(1) {
		t.Fatalf("old = %+v", st)
	}
	if len(r.dep.got()) != 0 {
		t.Fatalf("deployer called for a removed stack: %v", r.dep.got())
	}
	writeStack(t, r.wc, "old")
	r.poll()
	if r.stack("old").Removed {
		t.Fatal("removed flag stays after the folder returned")
	}
}

func treeDigest(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		fmt.Fprintf(h, "%s|%v\n", rel, d.IsDir())
		if !d.IsDir() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			h.Write(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func TestLocalChangesSkipTheStackAndLeaveTheCopyUntouched(t *testing.T) {
	r := newRig(t, "web", "db")
	r.deployAll()

	// local edits in web: modified, deleted and untracked
	if err := os.WriteFile(filepath.Join(r.wc, "web", "compose.yaml"), []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.wc, "web", ".env")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.wc, "web", "extra.txt"), []byte("u"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.copy.set(func(f *fakeCopy) {
		f.changes = []gitrepo.Change{{Code: " M", Path: "web/compose.yaml"}, {Code: " D", Path: "web/.env"}, {Code: "??", Path: "web/extra.txt"}}
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web", "db"}
	})
	before := treeDigest(t, r.wc)

	res := r.poll()
	if r.copy.called("ff") != 0 || res.FastForwarded {
		t.Fatal("fast-forwarded a working copy with local changes")
	}
	if after := treeDigest(t, r.wc); after != before {
		t.Fatal("the working copy changed")
	}
	web := r.stack("web")
	if !hasFinding(web, FindingLocalChanges) || web.Deployed != commit(1) || web.Pending != commit(2) {
		t.Fatalf("web = %+v", web)
	}
	// db is clean and pending at the fetched commit, but the copy is still at commit 1: nothing to run yet
	if db := r.stack("db"); db.Pending != commit(2) || db.Deployed != commit(1) || len(r.dep.got()) != 0 {
		t.Fatalf("db = %+v calls %v", db, r.dep.got())
	}
	snap, _ := r.eng.Snapshot()
	if !strings.Contains(snap.State.Note, "local changes") {
		t.Fatalf("note = %q", snap.State.Note)
	}

	// a manual run of the dirty stack is refused too
	if _, err := r.eng.Trigger(context.Background(), "web"); !errors.Is(err, ErrLocalChanges) {
		t.Fatalf("trigger err = %v", err)
	}
	// the clean stack may be run manually at the commit the copy has
	if _, err := r.eng.Trigger(context.Background(), "db"); err != nil {
		t.Fatal(err)
	}
	r.eng.Wait()
	if after := treeDigest(t, r.wc); after != before {
		t.Fatal("the working copy changed")
	}
}

func TestOwnFilesDoNotBlockTheFastForward(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	r.copy.set(func(f *fakeCopy) {
		f.changes = []gitrepo.Change{{Code: "??", Path: "web/secrets.env"}, {Code: "??", Path: "web/data/db/file"}}
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
	})
	res := r.poll()
	if !res.FastForwarded || !slices.Equal(res.Started, []string{"web"}) {
		t.Fatalf("result = %+v", res)
	}
	if st := r.stack("web"); len(st.Findings) != 0 {
		t.Fatalf("findings = %v", st.Findings)
	}
}

func TestChangeOutsideStacksBlocksTheFastForwardOnly(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	r.copy.set(func(f *fakeCopy) {
		f.changes = []gitrepo.Change{{Code: " M", Path: "README.md"}}
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
	})
	r.poll()
	if r.copy.called("ff") != 0 {
		t.Fatal("fast-forwarded a dirty copy")
	}
	if st := r.stack("web"); st.Pending != commit(2) || len(st.Findings) != 0 {
		t.Fatalf("web = %+v", st)
	}
}

func TestParallelTriggerIsDropped(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	r.dep.block = make(chan struct{})
	r.dep.entered = make(chan struct{}, 4)

	info, err := r.eng.Trigger(context.Background(), "web")
	if err != nil {
		t.Fatal(err)
	}
	<-r.dep.entered
	if _, err := r.eng.Trigger(context.Background(), "web"); !errors.Is(err, ErrRunActive) {
		t.Fatalf("second trigger err = %v", err)
	}
	snap, _ := r.eng.Snapshot()
	if snap.Active["web"] != info.ID {
		t.Fatalf("active = %v", snap.Active)
	}
	if d := r.stack("web").Dropped; d == nil || d.Trigger != TriggerManual {
		t.Fatalf("dropped = %+v", d)
	}
	// a poll that wants the same stack is dropped as well, and does not fast-forward under the run
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
	})
	res, err := r.eng.Poll(context.Background())
	if err != nil || r.copy.called("ff") != 0 || len(res.Started) != 0 {
		t.Fatalf("poll during run: %+v %v ff=%d", res, err, r.copy.called("ff"))
	}
	close(r.dep.block)
	r.eng.Wait()
	if n := len(r.dep.got()); n != 1 {
		t.Fatalf("deploy ran %d times", n)
	}
	if runs, _ := r.eng.Runs("web"); len(runs) != 2 { // the first poll's run and the manual one
		t.Fatalf("runs = %d", len(runs))
	}
}

func TestPollTriggerDroppedWhileRunActive(t *testing.T) {
	r := newRig(t, "web")
	r.dep.block = make(chan struct{})
	r.dep.entered = make(chan struct{}, 4)
	if _, err := r.eng.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-r.dep.entered
	writeStack(t, r.wc, "other")
	res, err := r.eng.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// web is new and active -> dropped; other is new and starts
	if !slices.Equal(res.Dropped, []string{"web"}) || !slices.Equal(res.Started, []string{"other"}) {
		t.Fatalf("result = %+v", res)
	}
	<-r.dep.entered
	close(r.dep.block)
	r.eng.Wait()
}

func TestStateSurvivesRestart(t *testing.T) {
	r := newRig(t, "web", "db")
	r.deployAll()
	r.eng.Close()

	r.eng = r.start()
	if got := r.stack("web"); got.Deployed != commit(1) || got.LastRun == nil {
		t.Fatalf("web after restart = %+v", got)
	}
	if res := r.poll(); len(res.Started) != 0 {
		t.Fatalf("restart redeployed %v", res.Started)
	}
	runs, err := r.eng.Runs("")
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs = %v %v", runs, err)
	}
	if _, err := r.eng.Log(runs[0].ID); err != nil {
		t.Fatalf("log after restart: %v", err)
	}
}

func TestFailedFirstDeployStopsAndIsNotRetriedAutomatically(t *testing.T) {
	r := newRig(t, "web")
	r.dep.fail("up", errors.New("compose exploded"))
	r.poll()
	st := r.stack("web")
	if st.Deployed != "" || st.FailedCommit != commit(1) || st.LastRun.Result != ResultFailed || st.Paused != nil ||
		!strings.Contains(st.LastRun.Error, "exploded") {
		t.Fatalf("web = %+v / %+v", st, st.LastRun)
	}
	// no backup of a stack that was never deployed, no revert, and the new stack is left stopped
	if got := r.dep.stepsOf("web"); !slices.Equal(got, []string{"stop", "pull", "up", "stop"}) {
		t.Fatalf("steps = %v", got)
	}
	if r.copy.called("revert")+r.copy.called("push") != 0 {
		t.Fatal("a first deployment was reverted")
	}
	r.poll()
	if n := len(r.dep.got()); n != 1 { // the failed run only, not retried
		t.Fatalf("calls = %d", n)
	}
	if !hasFinding(r.stack("web"), FindingFailedCommit) {
		t.Fatal("no failed-commit finding")
	}
	// a manual run retries
	if _, err := r.eng.Trigger(context.Background(), "web"); err != nil {
		t.Fatal(err)
	}
	r.eng.Wait()
	if st := r.stack("web"); st.Deployed != commit(1) || st.FailedCommit != "" {
		t.Fatalf("after manual retry = %+v", st)
	}
}

func TestRunLogIsRedactedBoundedAndSafelyNamed(t *testing.T) {
	r := newRig(t, "web")
	r.redac.Add("hunter2-demo-secret")
	r.dep.action = func(l *Log) {
		l.Step("pull")
		l.Printf("token hunter2-demo-secret\nsecond\x1b[31m line")
		for range 5000 {
			l.Printf("%s", strings.Repeat("x", 100))
		}
	}
	r.deployAll()
	runs, _ := r.eng.Runs("web")
	text, err := r.eng.Log(runs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "hunter2") || !strings.Contains(text, "[redacted]") || strings.Contains(text, "\x1b") {
		t.Fatalf("log not clean: %.200q", text)
	}
	if len(text) > maxLogBytes+maxLogLine || !strings.Contains(text, "log truncated") {
		t.Fatalf("log size %d", len(text))
	}
	fi, err := os.Stat(filepath.Join(r.state, engineDir, runsDir, runs[0].ID+logExt))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("log file: %v %v", fi, err)
	}
	for _, bad := range []string{"", "../state", "../../etc/passwd", runs[0].ID + "/../x", "web", "20060102T150405Z-zzzzzzzz"} {
		if _, err := r.eng.Log(bad); !errors.Is(err, ErrNoSuchRun) {
			t.Fatalf("Log(%q) err = %v", bad, err)
		}
	}
}

func TestHostileStackNameStaysOutOfPaths(t *testing.T) {
	r := newRig(t)
	name := "we b\x1b[2J"
	writeStack(t, r.wc, name)
	r.deployAll()
	runs, _ := r.eng.Runs("")
	if len(runs) != 1 {
		t.Fatalf("runs = %v", runs)
	}
	text, err := r.eng.Log(runs[0].ID)
	if err != nil || strings.Contains(text, "\x1b") {
		t.Fatalf("log %q %v", text, err)
	}
	entries, _ := os.ReadDir(filepath.Join(r.state, engineDir, runsDir))
	if len(entries) != 1 || !runIDRe.MatchString(strings.TrimSuffix(entries[0].Name(), logExt)) {
		t.Fatalf("entries = %v", entries)
	}
}

func TestHistoryIsBounded(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	for range keepRuns + 5 {
		if _, err := r.eng.Trigger(context.Background(), "web"); err != nil {
			t.Fatal(err)
		}
		r.eng.Wait()
	}
	runs, _ := r.eng.Runs("web")
	if len(runs) != keepRuns {
		t.Fatalf("runs = %d", len(runs))
	}
	entries, _ := os.ReadDir(filepath.Join(r.state, engineDir, runsDir))
	if len(entries) != keepRuns {
		t.Fatalf("log files = %d", len(entries))
	}
}

func TestWithoutDeployerOnlyReports(t *testing.T) {
	r := newRig(t, "web")
	r.cfg.Deployer = nil
	r.eng = r.start()
	r.copy.set(func(f *fakeCopy) { f.remote = commit(2) })
	res := r.poll()
	st := r.stack("web")
	if len(res.Started) != 0 || st.Deployed != "" || st.Pending != commit(2) {
		t.Fatalf("res %+v st %+v", res, st)
	}
	if _, err := r.eng.Trigger(context.Background(), "web"); !errors.Is(err, ErrNoDeployer) {
		t.Fatalf("err = %v", err)
	}
}

func TestTriggerUnknownAndUnreadable(t *testing.T) {
	r := newRig(t, "web")
	if _, err := r.eng.Trigger(context.Background(), "nope"); !errors.Is(err, ErrUnknownStack) {
		t.Fatalf("err = %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.wc, "web", "qdeploy.yaml"), []byte("version: 1\nupdate:\n  mode: sometimes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.eng.Trigger(context.Background(), "web"); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("err = %v", err)
	}
	r.poll()
	if !hasFinding(r.stack("web"), FindingUnreadable) || len(r.dep.got()) != 0 {
		t.Fatal("unreadable stack was not skipped with a finding")
	}
}

func TestFetchFailureIsRecorded(t *testing.T) {
	r := newRig(t, "web")
	r.copy.fetchErr = errors.New("network down")
	if _, err := r.eng.Poll(context.Background()); err == nil {
		t.Fatal("no error")
	}
	snap, _ := r.eng.Snapshot()
	if !strings.Contains(snap.State.PollError, "network down") || len(r.dep.got()) != 0 {
		t.Fatalf("state = %+v", snap.State)
	}
	r.copy.fetchErr = nil
	r.poll()
	if snap, _ := r.eng.Snapshot(); snap.State.PollError != "" {
		t.Fatalf("poll error stays: %q", snap.State.PollError)
	}
}

func TestNoBinding(t *testing.T) {
	r := newRig(t, "web")
	r.cfg.Source = func() (Binding, error) { return Binding{}, ErrNoRepo }
	r.eng = r.start()
	if _, err := r.eng.Poll(context.Background()); !errors.Is(err, ErrNoRepo) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.state, engineDir)); err == nil {
		t.Fatal("state written without a binding")
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLoopPollsOnIntervalAndWakesOnBinding(t *testing.T) {
	r := newRig(t, "web")
	var mu sync.Mutex
	bound := false
	r.cfg.Interval = 5 * time.Minute
	r.cfg.Source = func() (Binding, error) {
		mu.Lock()
		defer mu.Unlock()
		if !bound {
			return Binding{}, ErrNoRepo
		}
		return Binding{Dir: r.wc, Copy: r.copy}, nil
	}
	r.eng = r.start()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.eng.Run(ctx) }()

	eventually(t, "idle wait", func() bool { return r.clock.pending() == 1 })
	if r.copy.called("fetch") != 0 {
		t.Fatal("polled without binding")
	}
	mu.Lock()
	bound = true
	mu.Unlock()
	r.eng.Wake()
	eventually(t, "poll after wake", func() bool { return r.copy.called("fetch") == 1 })
	eventually(t, "interval wait", func() bool { return r.clock.pending() >= 2 })

	r.clock.Advance(4 * time.Minute)
	time.Sleep(5 * time.Millisecond)
	if r.copy.called("fetch") != 1 {
		t.Fatal("polled before the interval elapsed")
	}
	r.clock.Advance(time.Minute)
	eventually(t, "poll after interval", func() bool { return r.copy.called("fetch") == 2 })
	eventually(t, "deployed once", func() bool { return r.stack("web").Deployed == commit(1) })

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestLoopStopCancelsActiveRun(t *testing.T) {
	r := newRig(t, "web")
	r.dep.block = make(chan struct{})
	r.dep.entered = make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.eng.Run(ctx) }()
	<-r.dep.entered
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	st := r.stack("web")
	if st.Deployed != "" || st.LastRun == nil || st.LastRun.Result != ResultFailed {
		t.Fatalf("web = %+v", st)
	}
}

func TestRaceManyTriggers(t *testing.T) {
	r := newRig(t, "web", "db")
	r.deployAll()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := []string{"web", "db"}[i%2]
			_, _ = r.eng.Trigger(context.Background(), name)
			_, _ = r.eng.Poll(context.Background())
			_, _ = r.eng.Snapshot()
		}()
	}
	wg.Wait()
	r.eng.Wait()
	if _, err := r.eng.Runs(""); err != nil {
		t.Fatal(err)
	}
}

func TestWithStackExcludesRunsAndPolls(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	var gotCommit string
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- r.eng.WithStack(context.Background(), "web", "backup", func(_ context.Context, st stacks.Stack, deployed string) error {
			gotCommit = deployed
			if st.Name != "web" {
				t.Errorf("stack = %q", st.Name)
			}
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	if _, err := r.eng.Trigger(context.Background(), "web"); !errors.Is(err, ErrRunActive) {
		t.Fatalf("trigger err = %v", err)
	}
	if err := r.eng.WithStack(context.Background(), "web", "backup", nil); !errors.Is(err, ErrRunActive) {
		t.Fatalf("second WithStack err = %v", err)
	}
	if snap, _ := r.eng.Snapshot(); snap.Active["web"] != "backup" {
		t.Fatalf("active = %v", snap.Active)
	}
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
	})
	res, err := r.eng.Poll(context.Background())
	if err != nil || r.copy.called("ff") != 0 || len(res.Started) != 0 {
		t.Fatalf("poll during backup: %+v %v ff=%d", res, err, r.copy.called("ff"))
	}
	close(release)
	if err := <-done; err != nil || gotCommit != commit(1) {
		t.Fatalf("err %v, commit %q", err, gotCommit)
	}
	if snap, _ := r.eng.Snapshot(); len(snap.Active) != 0 {
		t.Fatalf("slot not released: %v", snap.Active)
	}
}

func TestWithStackRefusals(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	noop := func(context.Context, stacks.Stack, string) error { t.Error("fn ran"); return nil }
	if err := r.eng.WithStack(context.Background(), "nope", "backup", noop); !errors.Is(err, ErrUnknownStack) {
		t.Fatalf("err = %v", err)
	}
	r.copy.set(func(f *fakeCopy) { f.changes = []gitrepo.Change{{Path: "web/compose.yaml"}} })
	if err := r.eng.WithStack(context.Background(), "web", "backup", noop); !errors.Is(err, ErrLocalChanges) {
		t.Fatalf("err = %v", err)
	}
	r.copy.set(func(f *fakeCopy) { f.changes = nil })
	if err := os.WriteFile(filepath.Join(r.wc, "web", "qdeploy.yaml"), []byte("version: 1\nupdate:\n  mode: sometimes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.eng.WithStack(context.Background(), "web", "backup", noop); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("err = %v", err)
	}
	if err := os.Remove(filepath.Join(r.wc, "web", "qdeploy.yaml")); err != nil {
		t.Fatal(err)
	}
	r.eng.Close()
	if err := r.eng.WithStack(context.Background(), "web", "backup", noop); !errors.Is(err, ErrStopped) {
		t.Fatalf("err = %v", err)
	}
}

func TestWithStackEndsWithTheEngine(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- r.eng.WithStack(context.Background(), "web", "backup", func(ctx context.Context, _ stacks.Stack, _ string) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	<-entered
	r.eng.Close()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

const appendedSecret = "appended-value-not-real"

func TestAppendLogAddsToAFinishedRunLog(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	runs, _ := r.eng.Runs("web")
	id := runs[0].ID
	r.redac.Add(appendedSecret)
	if err := r.eng.AppendLog(id, "late fact "+appendedSecret+"\nsecond\x1b[2J"); err != nil {
		t.Fatal(err)
	}
	text, err := r.eng.Log(id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, " info late fact ") || !strings.Contains(text, " info second") {
		t.Fatalf("log:\n%s", text)
	}
	if strings.Contains(text, appendedSecret) || strings.Contains(text, "\x1b") {
		t.Fatalf("log carries a secret or a control character:\n%s", text)
	}
	if !strings.HasSuffix(text, "\n") {
		t.Fatalf("log does not end in a newline: %q", text)
	}
}

func TestAppendLogRefusesWhatIsNotAnExistingRegularLog(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	runs, _ := r.eng.Runs("web")
	dir := filepath.Join(r.state, engineDir, runsDir)
	for _, bad := range []string{"", "../x", runs[0].ID + "/..", "20260102T030405Z-00000000"} {
		if err := r.eng.AppendLog(bad, "x"); !errors.Is(err, ErrNoSuchRun) {
			t.Errorf("AppendLog(%q) err = %v", bad, err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.log")
	if err := os.WriteFile(outside, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := "20260102T030405Z-00000001"
	if err := os.Symlink(outside, filepath.Join(dir, link+logExt)); err != nil {
		t.Fatal(err)
	}
	if err := r.eng.AppendLog(link, "x"); !errors.Is(err, ErrNoSuchRun) {
		t.Errorf("symlink err = %v", err)
	}
	if got, _ := os.ReadFile(outside); string(got) != "keep\n" {
		t.Errorf("the symlink target changed: %q", got)
	}
	dirID := "20260102T030405Z-00000002"
	if err := os.Mkdir(filepath.Join(dir, dirID+logExt), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := r.eng.AppendLog(dirID, "x"); !errors.Is(err, ErrNoSuchRun) {
		t.Errorf("directory err = %v", err)
	}
	if err := r.eng.AppendLog(runs[0].ID, "x"); err != nil {
		t.Fatal(err)
	}
	r.eng.logs.remove(runs[0].ID)
	if err := r.eng.AppendLog(runs[0].ID, "x"); !errors.Is(err, ErrNoSuchRun) {
		t.Errorf("removed log err = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, runs[0].ID+logExt)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("AppendLog created a log: %v", err)
	}
}

func TestAppendLogStaysWithinTheSizeBound(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	runs, _ := r.eng.Runs("web")
	id := runs[0].ID
	p := filepath.Join(r.state, engineDir, runsDir, id+logExt)
	line := strings.Repeat("a", maxLogLine-100)
	var err error
	for i := 0; i < maxLogBytes/len(line)+2 && err == nil; i++ {
		err = r.eng.AppendLog(id, line)
	}
	if !errors.Is(err, ErrLogFull) {
		t.Fatalf("err = %v", err)
	}
	fi, statErr := os.Stat(p)
	if statErr != nil || fi.Size() > maxLogBytes {
		t.Fatalf("size = %v, %v", fi, statErr)
	}
	if err := r.eng.AppendLog(id, line); !errors.Is(err, ErrLogFull) {
		t.Fatalf("err = %v", err)
	}
}
