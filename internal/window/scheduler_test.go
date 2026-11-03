package window

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeClock tells waits about every timer the scheduler sets, so a test knows when the scheduler waits.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []timer
	waits  chan time.Time
}

type timer struct {
	at time.Time
	ch chan time.Time
}

func newClock(now time.Time) *fakeClock { return &fakeClock{now: now, waits: make(chan time.Time, 64)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, timer{c.now.Add(d), ch})
	c.waits <- c.now.Add(d)
	return ch
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
	keep := c.timers[:0]
	for _, tm := range c.timers {
		if !tm.at.After(t) {
			tm.ch <- t
		} else {
			keep = append(keep, tm)
		}
	}
	c.timers = keep
}

// nextWait returns the time of the next timer the scheduler sets.
func (c *fakeClock) nextWait(t *testing.T) time.Time {
	t.Helper()
	select {
	case w := <-c.waits:
		return w
	case <-time.After(5 * time.Second):
		t.Fatal("the scheduler set no timer")
		return time.Time{}
	}
}

// jobRig is a job that hands each call's open to the test and waits until the test releases it.
type jobRig struct {
	calls   chan func() bool
	release chan struct{}
}

func newJob() *jobRig {
	return &jobRig{calls: make(chan func() bool, 8), release: make(chan struct{})}
}

func (j *jobRig) run(ctx context.Context, open func() bool) {
	j.calls <- open
	select {
	case <-j.release:
	case <-ctx.Done():
	}
}

func (j *jobRig) called(t *testing.T) func() bool {
	t.Helper()
	select {
	case open := <-j.calls:
		return open
	case <-time.After(5 * time.Second):
		t.Fatal("the job did not run")
		return nil
	}
}

func (j *jobRig) idle(t *testing.T) {
	t.Helper()
	select {
	case <-j.calls:
		t.Fatal("the job ran")
	default:
	}
}

func (j *jobRig) done() { j.release <- struct{}{} }

func newScheduler(t *testing.T, dir string, clock *fakeClock, job *jobRig) *Scheduler {
	t.Helper()
	s, err := New(Config{StateDir: dir, Job: job.run, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// start runs the scheduler until the returned stop is called or the test ends.
func start(t *testing.T, s *Scheduler) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	var once sync.Once
	stop = func() { once.Do(func() { cancel(); <-done }) }
	t.Cleanup(stop)
	return stop
}

func set(t *testing.T, s *Scheduler, c Change) Status {
	t.Helper()
	st, err := s.Set(c)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func ptr[T any](v T) *T { return &v }

// Monday, 2 November 2026; the window is set to UTC so the host zone plays no part.
var monday = utc(2026, 11, 2, 2, 0)

func utcScheduler(t *testing.T, now time.Time) (*Scheduler, *fakeClock, *jobRig, string) {
	t.Helper()
	dir := t.TempDir()
	clock, job := newClock(now), newJob()
	s := newScheduler(t, dir, clock, job)
	set(t, s, Change{Zone: ptr("UTC")})
	<-s.changed // a change before Run would only cost one more timer
	return s, clock, job, dir
}

func TestWindowOpensRunsOnceAndCloses(t *testing.T) {
	s, clock, job, _ := utcScheduler(t, monday)
	start(t, s)
	if w := clock.nextWait(t); !w.Equal(utc(2026, 11, 2, 3, 0)) {
		t.Fatalf("waits until %v", w)
	}
	clock.Set(utc(2026, 11, 2, 2, 59))
	job.idle(t)

	clock.Set(utc(2026, 11, 2, 3, 0))
	open := job.called(t)
	st, err := s.Status()
	if err != nil || !st.Open || !st.Running || !st.LastRun.Equal(utc(2026, 11, 2, 3, 0)) ||
		!st.OpenUntil.Equal(utc(2026, 11, 2, 5, 0)) || !st.Next.Equal(utc(2026, 11, 3, 3, 0)) {
		t.Fatalf("status while running = %+v, %v", st, err)
	}
	if !open() {
		t.Fatal("closed at its start")
	}
	clock.Set(utc(2026, 11, 2, 4, 59))
	if !open() {
		t.Fatal("closed before its end")
	}
	clock.Set(utc(2026, 11, 2, 5, 0))
	if open() {
		t.Fatal("open at its end")
	}
	job.done()

	// the opening ran: the next wait aims at the next opening, a wait never longer than an hour
	if w := clock.nextWait(t); !w.Equal(utc(2026, 11, 2, 6, 0)) {
		t.Fatalf("waits until %v", w)
	}
	if st, _ := s.Status(); st.Open || st.Running {
		t.Fatalf("status after = %+v", st)
	}
	clock.Set(utc(2026, 11, 3, 3, 0))
	job.called(t)
	job.done()
}

func TestAJobFinishedWithinTheOpeningIsNotRepeated(t *testing.T) {
	s, clock, job, _ := utcScheduler(t, utc(2026, 11, 2, 3, 0))
	start(t, s)
	job.called(t)
	job.done()
	if w := clock.nextWait(t); !w.Equal(utc(2026, 11, 2, 4, 0)) {
		t.Fatalf("waits until %v", w)
	}
	clock.Set(utc(2026, 11, 2, 4, 0))
	if w := clock.nextWait(t); !w.Equal(utc(2026, 11, 2, 5, 0)) {
		t.Fatalf("waits until %v", w)
	}
	job.idle(t)
}

func TestAChangeAppliesWhileWaiting(t *testing.T) {
	s, clock, job, _ := utcScheduler(t, monday)
	start(t, s)
	clock.nextWait(t)

	// earlier: the wait is computed again at once
	set(t, s, Change{Start: ptr(150)})
	if w := clock.nextWait(t); !w.Equal(utc(2026, 11, 2, 2, 30)) {
		t.Fatalf("waits until %v", w)
	}
	// later: the old opening passes without a run
	st := set(t, s, Change{Start: ptr(240)})
	if !st.Next.Equal(utc(2026, 11, 2, 4, 0)) {
		t.Fatalf("next = %v", st.Next)
	}
	if w := clock.nextWait(t); !w.Equal(utc(2026, 11, 2, 3, 0)) {
		t.Fatalf("waits until %v", w)
	}
	clock.Set(utc(2026, 11, 2, 3, 0))
	if w := clock.nextWait(t); !w.Equal(utc(2026, 11, 2, 4, 0)) {
		t.Fatalf("waits until %v", w)
	}
	job.idle(t)
	// another weekday only: Monday is skipped
	set(t, s, Change{Days: ptr(mustDays(t, "tue"))})
	clock.nextWait(t)
	clock.Set(utc(2026, 11, 2, 4, 0))
	clock.nextWait(t)
	job.idle(t)
	if st, _ := s.Status(); !st.Next.Equal(utc(2026, 11, 3, 4, 0)) {
		t.Fatalf("next = %v", st.Next)
	}
}

func TestAChangeAppliesWhileOpen(t *testing.T) {
	s, clock, job, _ := utcScheduler(t, utc(2026, 11, 2, 3, 10))
	start(t, s)
	open := job.called(t)
	if !open() {
		t.Fatal("closed")
	}
	// shortened past now: nothing new may start
	st := set(t, s, Change{Duration: ptr(5 * time.Minute)})
	if open() || st.Open {
		t.Fatalf("still open after shortening: %+v", st)
	}
	// lengthened again, or moved so that it holds now
	set(t, s, Change{Duration: ptr(3 * time.Hour)})
	if !open() {
		t.Fatal("closed after lengthening")
	}
	set(t, s, Change{Zone: ptr("Asia/Tokyo")})
	if open() {
		t.Fatal("open in a zone where it is noon")
	}
	set(t, s, Change{Zone: ptr("UTC")})
	clock.Set(utc(2026, 11, 2, 5, 30))
	if !open() {
		t.Fatal("closed within the lengthened window")
	}
	job.done()
	// the opening already ran: no second run after the change
	clock.nextWait(t)
	job.idle(t)
}

func TestOncePerOpeningAcrossARestart(t *testing.T) {
	s, clock, job, dir := utcScheduler(t, utc(2026, 11, 2, 3, 0))
	stop := start(t, s)
	job.called(t)
	// the service stops while the job runs
	stop()

	clock.Set(utc(2026, 11, 2, 3, 30))
	again := newScheduler(t, dir, clock, job)
	start(t, again)
	if w := clock.nextWait(t); !w.Equal(utc(2026, 11, 2, 4, 30)) {
		t.Fatalf("waits until %v", w)
	}
	job.idle(t)
	if st, err := again.Status(); err != nil || st.Window.Zone != "UTC" || !st.LastRun.Equal(utc(2026, 11, 2, 3, 0)) {
		t.Fatalf("restored status = %+v, %v", st, err)
	}

	// the next opening runs
	clock.Set(utc(2026, 11, 3, 3, 0))
	job.called(t)
	job.done()
}

func TestAStartWithinAnOpeningThatDidNotRunRunsIt(t *testing.T) {
	s, _, job, _ := utcScheduler(t, utc(2026, 11, 2, 4, 50))
	start(t, s)
	job.called(t)
	job.done()
}

func TestTheSettingIsKeptAndValidated(t *testing.T) {
	dir := t.TempDir()
	clock := newClock(monday)
	s := newScheduler(t, dir, clock, newJob())
	if _, err := os.Stat(filepath.Join(dir, fileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a file exists before the first change")
	}
	st, err := s.Status()
	if err != nil || st.Window != Default() || !st.LastRun.IsZero() {
		t.Fatalf("default status = %+v, %v", st, err)
	}

	_, err = s.Set(Change{})
	wantInvalid(t, "empty change", err)
	for _, c := range []Change{
		{Days: ptr(Days(0))}, {Days: ptr(Days(1 << 7))}, {Start: ptr(-1)}, {Start: ptr(24 * 60)},
		{Duration: ptr(time.Duration(0))}, {Duration: ptr(MaxDuration + time.Minute)}, {Duration: ptr(90 * time.Second)},
		{Zone: ptr("Local")}, {Zone: ptr("Nowhere/Atlantis")}, {Zone: ptr("")},
	} {
		_, err := s.Set(c)
		wantInvalid(t, "change", err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused change was stored")
	}

	want := Window{Days: mustDays(t, "sat", "sun"), Start: 90, Duration: 45 * time.Minute, Zone: "Europe/Berlin"}
	set(t, s, Change{Days: &want.Days, Start: &want.Start})
	set(t, s, Change{Duration: &want.Duration, Zone: &want.Zone})
	again := newScheduler(t, dir, clock, newJob())
	if st, err := again.Status(); err != nil || st.Window != want {
		t.Fatalf("restored = %+v, %v", st.Window, err)
	}
}

func TestAMalformedSettingRunsNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(`{"schema":1,"days":["mon"],"start":"99:00","duration":"2h","zone":"UTC"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	clock, job := newClock(utc(2026, 11, 2, 3, 0)), newJob()
	s := newScheduler(t, dir, clock, job)
	if _, err := s.Status(); err == nil {
		t.Fatal("status of a malformed setting")
	}
	var ie *InvalidError
	if _, err := s.Set(Change{Start: ptr(60)}); err == nil || errors.As(err, &ie) {
		t.Fatalf("set over a malformed setting = %v", err)
	}
	start(t, s)
	if w := clock.nextWait(t); !w.Equal(utc(2026, 11, 2, 3, 1)) {
		t.Fatalf("retry at %v", w)
	}
	job.idle(t)
}
