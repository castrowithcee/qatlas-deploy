package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// selectContainer moves the mark of the Containers list to the container name.
func selectContainer(t *testing.T, m *Model, name string) {
	t.Helper()
	send(m, "home")
	for range 20 {
		if id, ok := m.ctList.Selected(); ok && string(id) == name {
			return
		}
		send(m, "down")
	}
	t.Fatalf("container %s is not reachable in the list", name)
}

func ctModel(t *testing.T, b *fakeBackend) *Model {
	t.Helper()
	m := newTestModel(t, i18n.English, b, ctWide, ctWideRows)
	send(m, "5")
	return m
}

func TestContainerActionCallsTheServiceByIDAfterConfirmation(t *testing.T) {
	for _, tt := range []struct{ key, want string }{
		{"S", "ctstart:0123456789ab"}, {"X", "ctstop:0123456789ab"}, {"Z", "ctrestart:0123456789ab"},
	} {
		t.Run(tt.key, func(t *testing.T) {
			b := containersBackend()
			m := ctModel(t, b)
			selectContainer(t, m, "demo-stack-app-1")
			for _, no := range []string{"n", "esc", "enter"} {
				send(m, tt.key)
				if m.modal == nil || m.modal.kind != modalAsk {
					t.Fatalf("%s opened no question", tt.key)
				}
				send(m, no)
				if got := b.actions(); len(got) != 0 {
					t.Fatalf("answer %q called %v", no, got)
				}
			}
			send(m, tt.key, "y")
			if got := b.actions(); !slices.Equal(got, []string{tt.want}) {
				t.Fatalf("calls = %v, want %s", got, tt.want)
			}
		})
	}
}

func TestContainerActionOnAGroupRowDoesNothing(t *testing.T) {
	b := containersBackend()
	m := ctModel(t, b)
	send(m, "home", "left") // folds the first group, so the mark rests on its header
	if _, ok := m.ctList.Selected(); ok {
		t.Fatal("the mark is not on a group row")
	}
	send(m, "S", "X", "Z", "y")
	if m.modal != nil || len(b.actions()) != 0 {
		t.Fatalf("a group row acted: modal=%v calls=%v", m.modal != nil, b.actions())
	}
}

func TestContainerActionFromTheDetailActsOnTheOpenContainer(t *testing.T) {
	b := containersBackend()
	m := ctModel(t, b)
	selectContainer(t, m, "legacy-blog-db-1")
	send(m, "enter", "S")
	screen := m.render()
	if !strings.Contains(screen, "Start container legacy-blog-db-1 (foreign project legacy-") {
		t.Fatalf("question:\n%s", screen)
	}
	send(m, "y")
	if got := b.actions(); !slices.Equal(got, []string{"ctstart:ba9876543210"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestContainerActionOfAnOwnStackSaysItIsNotMarkedAsStopped(t *testing.T) {
	m := ctModel(t, containersBackend())
	selectContainer(t, m, "demo-stack-app-1")
	send(m, "X")
	screen := m.render()
	for _, want := range []string{"Stop container demo-stack-app-1 (own stack demo-stack)?", "not marked as stopped", "can start the"} {
		if !strings.Contains(screen, want) {
			t.Errorf("question misses %q:\n%s", want, screen)
		}
	}
	send(m, "n")
	selectContainer(t, m, "example-cache")
	send(m, "X")
	if strings.Contains(m.render(), "not marked as stopped") {
		t.Errorf("a single container is told about a stack:\n%s", m.render())
	}
}

func TestContainerActionShowsTheStateAfterwardsAndReadsAgain(t *testing.T) {
	b := containersBackend()
	m := ctModel(t, b)
	selectContainer(t, m, "demo-stack-app-1")
	send(m, "enter")
	lists, details := b.called("containers"), b.called("container")
	send(m, "X", "y")
	screen := m.render()
	for _, want := range []string{"stopped", "State afterwards: stopped (exited)", "1 ok, 0 failed"} {
		if !strings.Contains(screen, want) {
			t.Errorf("result misses %q:\n%s", want, screen)
		}
	}
	// One read for the question, one for the open detail after the result.
	if b.called("containers") <= lists || b.called("container") < details+2 {
		t.Errorf("reads after the result: containers %d->%d, container %d->%d",
			lists, b.called("containers"), details, b.called("container"))
	}
}

func TestContainerNotReadyCountsAsFailure(t *testing.T) {
	b := containersBackend()
	b.ctNotReady = "unhealthy"
	m := ctModel(t, b)
	selectContainer(t, m, "demo-stack-app-1")
	send(m, "Z", "y")
	screen := m.render()
	for _, want := range []string{"0 ok, 1 failed", "restarted, but the container is not ready: its health check fails"} {
		if !strings.Contains(screen, want) {
			t.Errorf("result misses %q:\n%s", want, screen)
		}
	}
}

func TestContainerActionErrorsAreUnderstandable(t *testing.T) {
	for _, tt := range []struct{ code, want string }{
		{control.CodeRunActive, "a run of the stack is active"},
		{control.CodeNotFound, "this container no longer exists"},
		{control.CodeDockerUnreachable, "the container engine is not reachable"},
		{control.CodeDockerPermission, "the service may not use the container engine"},
		{control.CodeDockerTimeout, "the container engine did not answer in time"},
	} {
		t.Run(tt.code, func(t *testing.T) {
			b := containersBackend()
			b.errs["ctstart:0123456789ab"] = control.NewError(tt.code, "message of the engine")
			m := ctModel(t, b)
			selectContainer(t, m, "demo-stack-app-1")
			send(m, "S", "y")
			screen := m.render()
			if !strings.Contains(screen, tt.want) || !strings.Contains(screen, "failed") || strings.Contains(screen, "message of the engine") {
				t.Errorf("result:\n%s", screen)
			}
		})
	}
}

func TestContainerGoneBeforeTheQuestionIsAWarning(t *testing.T) {
	b := containersBackend()
	b.errs["container:demo-stack-app-1"] = control.NewError(control.CodeNotFound, "no such container")
	m := ctModel(t, b)
	selectContainer(t, m, "demo-stack-app-1")
	send(m, "S")
	if m.modal != nil || !strings.Contains(m.render(), "this container no longer exists") || len(b.actions()) != 0 {
		t.Errorf("modal=%v calls=%v:\n%s", m.modal != nil, b.actions(), m.render())
	}
}

func TestContainerActionIsRefusedWhileAnotherRuns(t *testing.T) {
	b := containersBackend()
	m := ctModel(t, b)
	selectContainer(t, m, "demo-stack-app-1")
	m.run = &running{gen: 1, plan: plan{kind: kindStart, stacks: []string{"x"}}}
	send(m, "S")
	if m.modal != nil || !strings.Contains(m.render(), "Another action is still running") {
		t.Errorf("busy:\n%s", m.render())
	}
}

// goldenContainerAction marks the container name in the Containers list, since the order of the groups depends on
// the language, then sends keys.
func goldenContainerAction(t *testing.T, prefix string, setup func() *fakeBackend, name string, keys ...string) {
	t.Helper()
	for lname, lang := range goldenLangs {
		for size, dim := range map[string][2]int{"narrow": {ctNarrow, ctNarrowRows}, "wide": {ctWide, ctWideRows}} {
			t.Run(lname+"."+size, func(t *testing.T) {
				m := newTestModel(t, lang, setup(), dim[0], dim[1])
				send(m, "5")
				selectContainer(t, m, name)
				send(m, keys...)
				screen := m.render()
				checkScreen(t, screen, dim[0], dim[1])
				checkGolden(t, prefix+"."+lname+"."+size+".golden", screen+"\n")
			})
		}
	}
}

func TestGoldenContainerStartConfirmation(t *testing.T) {
	goldenContainerAction(t, "containers.start.confirm", containersBackend, "legacy-blog-db-1", "S")
}

func TestGoldenContainerStopConfirmationOfAnOwnStack(t *testing.T) {
	goldenContainerAction(t, "containers.stop.confirm", containersBackend, "demo-stack-app-1", "X")
}

func TestGoldenContainerRestartConfirmation(t *testing.T) {
	goldenContainerAction(t, "containers.restart.confirm", containersBackend, "example-cache", "Z")
}

func TestGoldenContainerStopSummary(t *testing.T) {
	goldenContainerAction(t, "containers.stop.summary", containersBackend, "demo-stack-app-1", "X", "y")
}

func TestGoldenContainerRestartNotReady(t *testing.T) {
	goldenContainerAction(t, "containers.restart.notready", func() *fakeBackend {
		b := containersBackend()
		b.ctNotReady = "unhealthy"
		return b
	}, "demo-stack-app-1", "Z", "y")
}

func TestGoldenContainerActionError(t *testing.T) {
	goldenContainerAction(t, "containers.start.error", func() *fakeBackend {
		b := containersBackend()
		b.errs["ctstart:0123456789ab"] = control.NewError(control.CodeRunActive, "run active")
		return b
	}, "demo-stack-app-1", "S", "y")
}
