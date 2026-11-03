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
type fakeCopy struct {
	mu            sync.Mutex
	dir           string
	head, remote  string
	changes       []gitrepo.Change
	folders       map[[2]string][]string
	fetchErr      error
	calls         []string
	fastForwarded int
}

func (f *fakeCopy) rec(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

func (f *fakeCopy) Fetch(context.Context) error { f.rec("fetch"); return f.fetchErr }
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
	return slices.Clone(f.changes), nil
}
func (f *fakeCopy) ChangedFolders(_ context.Context, from, to string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.folders[[2]string{from, to}], nil
}
func (f *fakeCopy) FastForward(context.Context) error {
	f.rec("ff")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fastForwarded++
	f.head = f.remote
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

type fakeDeployer struct {
	mu      sync.Mutex
	calls   []call
	err     error
	block   chan struct{} // when set, Deploy waits for it
	entered chan struct{}
	action  func(*Log)
}

func (d *fakeDeployer) Deploy(ctx context.Context, st stacks.Stack, commit string, log *Log) error {
	d.mu.Lock()
	d.calls = append(d.calls, call{st.Name, commit})
	block, entered, err, action := d.block, d.entered, d.err, d.action
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
	return err
}

func (d *fakeDeployer) got() []call {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.calls)
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
	r.copy = &fakeCopy{dir: r.wc, head: commit(1), remote: commit(1), folders: map[[2]string][]string{}}
	r.dep = &fakeDeployer{}
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
	r.dep.calls = nil
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

func TestFailedRunKeepsDeployedAndIsNotRetriedAutomatically(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	r.dep.err = errors.New("compose exploded")
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
	})
	r.poll()
	st := r.stack("web")
	if st.Deployed != commit(1) || st.FailedCommit != commit(2) || st.LastRun.Result != ResultFailed ||
		!strings.Contains(st.LastRun.Error, "exploded") {
		t.Fatalf("web = %+v / %+v", st, st.LastRun)
	}
	r.poll()
	if n := len(r.dep.got()); n != 1 { // the failed run only, not retried
		t.Fatalf("calls = %d", n)
	}
	if !hasFinding(r.stack("web"), FindingFailedCommit) {
		t.Fatal("no failed-commit finding")
	}
	// a manual run retries
	r.dep.err = nil
	if _, err := r.eng.Trigger(context.Background(), "web"); err != nil {
		t.Fatal(err)
	}
	r.eng.Wait()
	if st := r.stack("web"); st.Deployed != commit(2) || st.FailedCommit != "" {
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
