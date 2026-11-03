package service_test

import (
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
)

func TestOperatorMarksAreSetClearedAndShownInStatus(t *testing.T) {
	h := bound(t, &fakeDeployer{})
	for _, tc := range []struct {
		command, other string
	}{
		{control.PauseCommand, control.UnpauseCommand}, {control.PinCommand, control.UnpinCommand},
	} {
		var res control.HoldResult
		if err := h.call(tc.command, control.HoldArgs{Stack: "plain-stack"}, &res); err != nil {
			t.Fatal(err)
		}
		if res != (control.HoldResult{Stack: "plain-stack", Active: true, Changed: true}) {
			t.Fatalf("%s = %+v", tc.command, res)
		}
		if err := h.call(tc.command, control.HoldArgs{Stack: "plain-stack"}, &res); err != nil || res.Changed || !res.Active {
			t.Fatalf("%s again = %+v, %v", tc.command, res, err)
		}
		wantCode(t, h.call(tc.command, control.HoldArgs{Stack: "nope"}, nil), control.CodeNoSuchStack)
		wantCode(t, h.call(tc.other, control.HoldArgs{Stack: "nope"}, nil), control.CodeNoSuchStack)
		wantCode(t, h.call(tc.command, map[string]any{"stack": "plain-stack", "extra": 1}, nil), control.CodeBadRequest)
	}
	s := stackStatus(t, h, "plain-stack")
	if s.OperatorPause == nil || s.Pinned == nil || s.Paused != nil || len(s.Findings) != 0 {
		t.Fatalf("status = %+v", s)
	}
	if other := stackStatus(t, h, "demo-stack"); other.OperatorPause != nil || other.Pinned != nil {
		t.Fatalf("other stack marked: %+v", other)
	}

	// manual runs are not blocked by either mark
	var dep control.DeployResult
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &dep); err != nil {
		t.Fatal(err)
	}
	if r := waitRun(t, h, dep.ID); r.Result != engine.ResultSucceeded {
		t.Fatalf("run = %+v", r)
	}

	for _, command := range []string{control.UnpauseCommand, control.UnpinCommand} {
		var res control.HoldResult
		if err := h.call(command, control.HoldArgs{Stack: "plain-stack"}, &res); err != nil || res.Active || !res.Changed {
			t.Fatalf("%s = %+v, %v", command, res, err)
		}
	}
	if s := stackStatus(t, h, "plain-stack"); s.OperatorPause != nil || s.Pinned != nil {
		t.Fatalf("status after clearing = %+v", s)
	}
}

func TestUpdateRefusesAPinnedStack(t *testing.T) {
	h := updateHarness(t, &fakeDeployer{})
	if err := h.call(control.PinCommand, control.HoldArgs{Stack: "demo-stack"}, nil); err != nil {
		t.Fatal(err)
	}
	_, err := h.update(control.UpdateRunArgs{Stacks: []string{"demo-stack"}})
	wantCode(t, err, control.CodePinned)
	if len(h.git.Pushed()) != 0 {
		t.Fatalf("pushed = %q", h.git.Pushed())
	}
	// the operator pause does not stop a manual update
	if err := h.call(control.UnpinCommand, control.HoldArgs{Stack: "demo-stack"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.call(control.PauseCommand, control.HoldArgs{Stack: "demo-stack"}, nil); err != nil {
		t.Fatal(err)
	}
	res, err := h.update(control.UpdateRunArgs{Stacks: []string{"demo-stack"}})
	if err != nil || len(res.Stacks) != 1 || res.Stacks[0].Error != "" || len(res.Stacks[0].Steps) == 0 {
		t.Fatalf("update = %+v, %v", res, err)
	}
}
