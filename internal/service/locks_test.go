package service_test

import (
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// TestReadsAnswerWhileRepoFetchWaits: a "repo fetch" that hangs at the remote holds only the ref lock; the
// read commands and the operator marks answer meanwhile.
func TestReadsAnswerWhileRepoFetchWaits(t *testing.T) {
	h, _ := updatesHarness(t)
	// The binding's first poll is over before the fetch is held, so the held fetch is the command's.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		var st control.StatusResult
		if err := h.call(control.StatusCommand, nil, &st); err != nil {
			t.Fatal(err)
		}
		if !st.Engine.LastPoll.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first poll did not end")
		}
	}
	entered, release := h.git.BlockFetch()
	t.Cleanup(release)
	fetched := make(chan error, 1)
	go func() {
		var res control.RepoFetchResult
		fetched <- h.call(control.RepoFetchCommand, nil, &res)
	}()
	<-entered

	answer := func(command string, args, result any) {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- h.call(command, args, result) }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s: %v", command, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s waited for the fetch", command)
		}
	}
	var list control.StacksResult
	answer(control.StacksListCommand, nil, &list)
	if len(list.Stacks) != 3 {
		t.Fatalf("stacks = %+v", list.Stacks)
	}
	answer(control.StacksCheckCommand, control.StacksCheckArgs{}, &control.StacksCheckResult{})
	var upd control.UpdatesResult
	answer(control.UpdatesListCommand, control.UpdatesArgs{}, &upd)
	if len(upd.Stacks) != 3 {
		t.Fatalf("updates = %+v", upd.Stacks)
	}
	answer(control.PinCommand, control.HoldArgs{Stack: "demo-stack"}, &control.HoldResult{})
	answer(control.UnpinCommand, control.HoldArgs{Stack: "demo-stack"}, &control.HoldResult{})
	answer(control.StatusCommand, nil, &control.StatusResult{})

	select {
	case err := <-fetched:
		t.Fatalf("repo fetch ended while held: %v", err)
	default:
	}
	release()
	if err := <-fetched; err != nil {
		t.Fatalf("repo fetch: %v", err)
	}
}
