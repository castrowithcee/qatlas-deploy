package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// blockFetch makes every Fetch of the rig wait until the returned release is called; entered gets a value
// for each Fetch that waits.
func (r *rig) blockFetch() (entered <-chan struct{}, release func()) {
	gate, in := make(chan struct{}), make(chan struct{}, 16)
	r.copy.set(func(f *fakeCopy) { f.fetchGate, f.fetchIn = gate, in })
	released := false
	release = func() {
		if !released {
			released = true
			r.copy.set(func(f *fakeCopy) { f.fetchGate, f.fetchIn = nil, nil })
			close(gate)
		}
	}
	r.t.Cleanup(release)
	return in, release
}

// within fails the test unless fn returns within a few seconds.
func within(t *testing.T, what string, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("%s waited for the fetch", what)
		return nil
	}
}

func TestBlockedPollFetchBlocksNoCommand(t *testing.T) {
	r := newRig(t, "web", "db")
	r.deployAll()
	entered, release := r.blockFetch()
	polled := make(chan error, 1)
	go func() {
		_, err := r.eng.Poll(context.Background())
		polled <- err
	}()
	<-entered

	ctx := context.Background()
	if err := within(t, "trigger", func() error { _, err := r.eng.Trigger(ctx, "web"); return err }); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if err := within(t, "pause", func() error { _, err := r.eng.SetPause(ctx, "db", true); return err }); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := within(t, "pin", func() error { _, err := r.eng.SetPin(ctx, "db", true); return err }); err != nil {
		t.Fatalf("pin: %v", err)
	}
	ran := false
	err := within(t, "WithStack", func() error {
		return r.eng.WithStack(ctx, "db", "backup", func(context.Context, stacks.Stack, string) error {
			ran = true
			return nil
		})
	})
	if err != nil || !ran {
		t.Fatalf("WithStack: ran %v, %v", ran, err)
	}
	select {
	case err := <-polled:
		t.Fatalf("the poll ended while its fetch was held: %v", err)
	default:
	}
	release()
	if err := <-polled; err != nil {
		t.Fatalf("poll: %v", err)
	}
	r.eng.Wait()
}

// TestParallelFetchDoesNotMoveTheFastForward: a fetch that moves the remote-tracking ref after the poll read
// it, as "repo fetch" may, does not carry the fast-forward further than the commit the poll evaluated.
func TestParallelFetchDoesNotMoveTheFastForward(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
		f.onStatus = func(f *fakeCopy) {
			f.remote, f.onStatus = commit(3), nil
		}
	})
	res := r.poll()
	if res.RemoteHead != commit(2) || res.Head != commit(2) || !res.FastForwarded {
		t.Fatalf("poll = %+v", res)
	}
	r.copy.mu.Lock()
	head, ffTo := r.copy.head, r.copy.ffTo
	r.copy.mu.Unlock()
	if head != commit(2) || len(ffTo) != 1 || ffTo[0] != commit(2) {
		t.Fatalf("head %s, fast-forwards to %v; want only %s", head, ffTo, commit(2))
	}
	if got := r.dep.got(); len(got) != 1 || got[0].Commit != commit(2) {
		t.Fatalf("runs = %v", got)
	}
	snap, _ := r.eng.Snapshot()
	if snap.State.Head != commit(2) || snap.State.RemoteHead != commit(2) {
		t.Fatalf("state head %s remote %s", snap.State.Head, snap.State.RemoteHead)
	}
}

// TestCommandsAnswerBusyWhileAnUpdateCommits: an update's commit holds the working-copy lock over its fetch
// and push; a command gives up after LockWait with ErrBusy, and a repo-level fetch waits for the ref lock.
func TestCommandsAnswerBusyWhileAnUpdateCommits(t *testing.T) {
	r := updateRig(t)
	r.cfg.LockWait = 50 * time.Millisecond
	r.eng = r.start()
	entered, release := r.blockFetch()
	updated := make(chan []StepResult, 1)
	go func() {
		res, err := r.eng.Update(context.Background(), "web", []VersionStep{step("1.0.0", "1.1.0")})
		if err != nil {
			t.Errorf("update: %v", err)
		}
		updated <- res
	}()
	<-entered // the commit's fetch, with the working-copy lock and the ref lock held

	ctx := context.Background()
	if _, err := r.eng.Trigger(ctx, "web"); !errors.Is(err, ErrBusy) {
		t.Fatalf("trigger: %v", err)
	}
	if _, err := r.eng.SetPin(ctx, "web", true); !errors.Is(err, ErrBusy) {
		t.Fatalf("pin: %v", err)
	}
	if _, err := r.eng.CheckUpdate(ctx, "web"); !errors.Is(err, ErrBusy) {
		t.Fatalf("check update: %v", err)
	}
	// the poll's fetch waits for the ref lock, not for the working copy
	fetches := r.copy.called("fetch")
	polled := make(chan error, 1)
	go func() {
		_, err := r.eng.Poll(ctx)
		polled <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if n := r.copy.called("fetch"); n != fetches {
		t.Fatalf("fetches = %d, want %d; the poll fetched beside the commit", n, fetches)
	}
	release()
	if res := <-updated; len(res) != 1 || res[0].Result != ResultSucceeded {
		t.Fatalf("update = %+v", res)
	}
	if err := <-polled; err != nil {
		t.Fatalf("poll: %v", err)
	}
	r.eng.Wait()
	if st := r.stack("web"); st.Deployed != commit(2001) {
		t.Fatalf("web = %+v", st)
	}
}
