package service_test

import (
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

func TestStackActionsMarkAndClearStopped(t *testing.T) {
	dep := &fakeDeployer{}
	h := bound(t, dep)
	dep.mu.Lock()
	dep.calls = nil // the initial deployments are not under test
	dep.mu.Unlock()
	var res control.StackActionResult
	if err := h.call(control.StackStopCommand, control.StackActionArgs{Stack: "plain-stack"}, &res); err != nil {
		t.Fatal(err)
	}
	if res != (control.StackActionResult{Stack: "plain-stack", Action: "stop", Stopped: true}) {
		t.Fatalf("stop = %+v", res)
	}
	if s := stackStatus(t, h, "plain-stack"); s.Stopped == nil {
		t.Fatalf("status = %+v", s)
	}
	if other := stackStatus(t, h, "demo-stack"); other.Stopped != nil {
		t.Fatalf("other stack stopped: %+v", other)
	}
	var list control.StacksResult
	if err := h.call(control.StacksListCommand, nil, &list); err != nil {
		t.Fatal(err)
	}
	for _, s := range list.Stacks {
		if (s.Name == "plain-stack") != (s.Stopped != nil) {
			t.Fatalf("stack list: %+v", s)
		}
	}
	wantCode(t, h.call(control.StackRestartCommand, control.StackActionArgs{Stack: "plain-stack"}, nil), control.CodeStackStopped)
	if err := h.call(control.StackStartCommand, control.StackActionArgs{Stack: "plain-stack"}, &res); err != nil || res.Stopped {
		t.Fatalf("start = %+v, %v", res, err)
	}
	if s := stackStatus(t, h, "plain-stack"); s.Stopped != nil {
		t.Fatalf("status after start = %+v", s)
	}
	if err := h.call(control.StackRestartCommand, control.StackActionArgs{Stack: "plain-stack"}, &res); err != nil || res.Action != "restart" {
		t.Fatalf("restart = %+v, %v", res, err)
	}
	if got := dep.callsOf("plain-stack"); !slices.Equal(got, []string{"stop plain-stack", "start plain-stack", "restart plain-stack"}) {
		t.Fatalf("calls = %v", got)
	}
	wantCode(t, h.call(control.StackStopCommand, control.StackActionArgs{Stack: "nope"}, nil), control.CodeNoSuchStack)
	wantCode(t, h.call(control.StackStopCommand, control.StackActionArgs{Stack: "../x"}, nil), control.CodeInvalid)
	wantCode(t, h.call(control.StackStopCommand, map[string]any{"stack": "plain-stack", "extra": 1}, nil), control.CodeBadRequest)
}

func TestStackActionRefusedWhileARunIsActive(t *testing.T) {
	dep := &fakeDeployer{}
	h := bound(t, dep)
	entered, release := dep.hold()
	var dr control.DeployResult
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &dr); err != nil {
		t.Fatal(err)
	}
	<-entered
	for _, c := range []string{control.StackStartCommand, control.StackStopCommand, control.StackRestartCommand} {
		wantCode(t, h.call(c, control.StackActionArgs{Stack: "plain-stack"}, nil), control.CodeRunActive)
	}
	release()
	waitRun(t, h, dr.ID)
}

func TestStoppedStackRefusesManualDeploy(t *testing.T) {
	dep := &fakeDeployer{}
	h := bound(t, dep)
	if err := h.call(control.StackStopCommand, control.StackActionArgs{Stack: "plain-stack"}, nil); err != nil {
		t.Fatal(err)
	}
	wantCode(t, h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, nil), control.CodeStackStopped)
	if err := h.call(control.StackStartCommand, control.StackActionArgs{Stack: "plain-stack"}, nil); err != nil {
		t.Fatal(err)
	}
	var dr control.DeployResult
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "plain-stack"}, &dr); err != nil {
		t.Fatal(err)
	}
	waitRun(t, h, dr.ID)
}
