package service_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/docker"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
)

// listDocker lists the single container held in details, as the engine would show it after the actions.
type listDocker struct{ *fakeDocker }

func (l listDocker) List(_ context.Context) ([]docker.Container, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return []docker.Container{l.details["c1"].Container}, nil
}

func TestMonitorInServiceQdeployStopIsNoFailureButAnExitIs(t *testing.T) {
	fd := &fakeDocker{details: map[string]docker.Details{}}
	var mu sync.Mutex
	var events []service.MonitorEvent
	h := boundWith(t, &fakeDeployer{}, nil, withDocker(listDocker{fd}), func(c *service.Config) {
		c.MonitorInterval = 10 * time.Millisecond
		c.OnMonitorEvent = func(ev service.MonitorEvent) {
			mu.Lock()
			defer mu.Unlock()
			// The fake engine shows no container until the test adds one; those "missing" events are not under test.
			if ev.Kind != "missing" {
				events = append(events, ev)
			}
		}
	})
	labels := map[string]string{lblProject: "plain-stack", lblDir: filepath.Join(h.dir, "plain-stack"), "com.docker.compose.service": "app"}
	c := running("c1", "plain-stack-app-1", labels)
	fd.mu.Lock()
	fd.details["plain-stack-app-1"], fd.details["c1"] = docker.Details{Container: c}, docker.Details{Container: c}
	fd.mu.Unlock()

	if err := h.call(control.ContainerStopCommand, control.ContainerActionArgs{Name: "plain-stack-app-1"}, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	n := len(events)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("a stop through qdeploy was reported: %+v", events)
	}

	// started and then exited on its own: one failure, shown in the status
	if err := h.call(control.ContainerStartCommand, control.ContainerActionArgs{Name: "plain-stack-app-1"}, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	fd.setStateLocked("exited")
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		var st control.StatusResult
		if err := h.call(control.StatusCommand, nil, &st); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, s := range st.Engine.Stacks {
			for _, f := range s.Findings {
				found = found || (s.Name == "plain-stack" && f.Kind == "container-failed")
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no finding: %+v", st.Engine)
		}
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || events[0].Stack != "plain-stack" || events[0].Kind != "exited" || events[0].Recovered {
		t.Fatalf("events = %+v", events)
	}
}

func (f *fakeDocker) setStateLocked(state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setState(state)("c1")
}
