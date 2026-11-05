package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

func summary(name, image, state, engine string, uptime int64, restarts int) control.ContainerSummary {
	return control.ContainerSummary{Name: name, Image: image, State: state, EngineState: engine, UptimeSeconds: uptime, Restarts: restarts}
}

// containersBackend is demoBackend with a host that has an own stack, two foreign projects, one of them in
// conflict with a stack name, and a single container.
func containersBackend() *fakeBackend {
	b := demoBackend()
	app := summary("demo-stack-app-1", "example.invalid/app:1.4.2", control.ContainerOK, "running", 3*3600+120, 0)
	db := summary("demo-stack-db-1", "example.invalid/db:16", control.ContainerUnhealthy, "running", 90, 2)
	db.Health = "unhealthy"
	web := summary("legacy-blog-web-1", "example.invalid/blog:2", control.ContainerOK, "running", 5*86400+3600, 0)
	blogDB := summary("legacy-blog-db-1", "example.invalid/db:15", control.ContainerStopped, "exited", 0, 0)
	wiki := summary("wiki-app-1", "example.invalid/wiki:1", control.ContainerRestarting, "restarting", 0, 14)
	cache := summary("example-cache", "example.invalid/cache:7", control.ContainerOK, "running", 45, 0)
	b.containers = control.ContainerListResult{Groups: []control.ContainerGroup{
		{Kind: control.ContainerKindOwn, Name: "demo-stack", State: control.ContainerUnhealthy, Containers: []control.ContainerSummary{app, db}},
		{Kind: control.ContainerKindForeign, Name: "legacy-blog", State: control.ContainerStopped,
			WorkingDir: "/srv/example/blog", Containers: []control.ContainerSummary{blogDB, web}},
		{Kind: control.ContainerKindForeign, Name: "wiki", Conflict: true, State: control.ContainerRestarting,
			Containers: []control.ContainerSummary{wiki}},
		{Kind: control.ContainerKindSingle, Name: "example-cache", State: control.ContainerOK, Containers: []control.ContainerSummary{cache}},
	}}
	b.containerDetails = map[string]control.ContainerDetail{
		"demo-stack-app-1": {ContainerSummary: app, ID: "0123456789ab", Kind: control.ContainerKindOwn, Project: "demo-stack",
			Ports:    []control.ContainerPort{{PrivatePort: 8080, PublicPort: 8080, Protocol: "tcp", HostIP: "127.0.0.1"}, {PrivatePort: 9000, Protocol: "tcp"}},
			Mounts:   []control.ContainerMount{{Type: "bind", Source: "/srv/example/data", Destination: "/data"}, {Type: "volume", Name: "demo-config", Destination: "/config", ReadOnly: true}},
			Networks: []control.ContainerNetwork{{Name: "demo-stack_default", IPAddress: "172.18.0.2"}},
			Labels:   map[string]string{"com.docker.compose.project": "demo-stack", "com.docker.compose.service": "app"},
			EnvNames: []string{"APP_MODE", "DB_PASSWORD"}},
		"legacy-blog-db-1": {ContainerSummary: blogDB, ID: "ba9876543210", Kind: control.ContainerKindForeign, Project: "legacy-blog",
			Ports: []control.ContainerPort{}, Mounts: []control.ContainerMount{}, Networks: []control.ContainerNetwork{},
			Labels: map[string]string{}, EnvNames: []string{}},
		"example-cache": {ContainerSummary: cache, ID: "feedfacecafe", Kind: control.ContainerKindSingle},
	}
	return b
}

// The goldens of the Containers area at the sizes of a small and a large terminal.
const (
	ctNarrow, ctNarrowRows = 80, 24
	ctWide, ctWideRows     = 120, 40
)

func goldenContainers(t *testing.T, prefix string, setup func() *fakeBackend, keys ...string) {
	t.Helper()
	for name, lang := range goldenLangs {
		for size, dim := range map[string][2]int{"narrow": {ctNarrow, ctNarrowRows}, "wide": {ctWide, ctWideRows}} {
			t.Run(name+"."+size, func(t *testing.T) {
				m := newTestModel(t, lang, setup(), dim[0], dim[1])
				send(m, keys...)
				screen := m.render()
				checkScreen(t, screen, dim[0], dim[1])
				checkGolden(t, prefix+"."+name+"."+size+".golden", screen+"\n")
			})
		}
	}
}

func TestGoldenContainersList(t *testing.T) {
	goldenContainers(t, "containers.list", containersBackend, "5")
}

func TestGoldenContainersFolded(t *testing.T) {
	goldenContainers(t, "containers.folded", containersBackend, "5", "left")
}

func TestGoldenContainerDetail(t *testing.T) {
	// The kit orders the groups by title: in English the foreign projects come first, so the app of the own
	// stack is the sixth row below the first container.
	goldenContainers(t, "containers.detail", containersBackend, "5", "down", "down", "down", "down", "down", "enter")
}

func TestGoldenContainerDetailEmptySections(t *testing.T) {
	goldenContainers(t, "containers.detail.empty", containersBackend, "5", "enter")
}

func dockerDown(code string) func() *fakeBackend {
	return func() *fakeBackend {
		b := containersBackend()
		b.errs["containers:"] = control.NewError(code, "engine message that is not shown")
		return b
	}
}

func TestGoldenContainersDockerUnreachable(t *testing.T) {
	goldenContainers(t, "containers.unreachable", dockerDown(control.CodeDockerUnreachable), "5")
}

func TestGoldenContainersEmpty(t *testing.T) {
	goldenContainers(t, "containers.empty", func() *fakeBackend {
		b := containersBackend()
		b.containers = control.ContainerListResult{}
		return b
	}, "5")
}

func TestGoldenStackDetailContainers(t *testing.T) {
	goldenContainers(t, "detail.containers", containersBackend, "enter")
}

// selected is the ID of the marked row of the Containers list.
func selected(t *testing.T, m *Model) string {
	t.Helper()
	id, ok := m.ctList.Selected()
	if !ok {
		t.Fatal("no container is marked")
	}
	return string(id)
}

func TestContainersRefreshKeepsSelectionAndFold(t *testing.T) {
	b := containersBackend()
	m := newTestModel(t, i18n.English, b, ctNarrow, ctNarrowRows)
	send(m, "5")
	// Mark the second container of the own stack, then fold the first group.
	send(m, "down", "down", "down", "down", "down", "down")
	want := selected(t, m)
	if want != "demo-stack-db-1" {
		t.Fatalf("marked %q, want demo-stack-db-1", want)
	}
	send(m, "home", "left")
	if strings.Contains(m.render(), "legacy-blog-web-1") {
		t.Fatal("the folded group still shows its containers")
	}
	send(m, "down", "down", "down", "down", "down")
	if want = selected(t, m); want != "demo-stack-db-1" {
		t.Fatalf("marked %q, want demo-stack-db-1", want)
	}

	// The engine now reports the container as stopped; the timer refreshes without a key.
	b.containers.Groups[0].Containers[1].State = control.ContainerStopped
	b.containers.Groups[0].Containers[1].EngineState = "exited"
	timersOf[m].fire(t, m)

	screen := m.render()
	if got := selected(t, m); got != want {
		t.Errorf("marked %q after the refresh, want %q", got, want)
	}
	if strings.Contains(screen, "legacy-blog-web-1") || strings.Contains(screen, "legacy-blog-db-1") {
		t.Errorf("the fold did not survive the refresh:\n%s", screen)
	}
	if !strings.Contains(screen, "stopped") {
		t.Errorf("the new state is not shown:\n%s", screen)
	}
}

func TestContainersTimerRunsOnlyWhileShown(t *testing.T) {
	b := containersBackend()
	m := newTestModel(t, i18n.English, b, ctNarrow, ctNarrowRows)
	tm := timersOf[m]
	if len(tm.fns) != 0 || b.called("containers") != 0 {
		t.Fatal("the container list is read before the area is shown")
	}
	send(m, "5")
	if len(tm.fns) != 1 || tm.waits[0] != containerInterval || b.called("containers") != 1 {
		t.Fatalf("timers %v, reads %d", tm.waits, b.called("containers"))
	}
	// Away and back before the timer fires: the first timer must not run a second chain.
	send(m, "1", "5")
	reads := b.called("containers")
	tm.fire(t, m)
	if b.called("containers") != reads || len(tm.fns) != 1 {
		t.Fatalf("a timer of an earlier visit refreshed: reads %d, timers %d", b.called("containers")-reads, len(tm.fns))
	}
	tm.fire(t, m)
	if b.called("containers") != reads+1 || len(tm.fns) != 1 {
		t.Fatalf("the timer did not refresh and set the next: reads %d, timers %d", b.called("containers")-reads, len(tm.fns))
	}
	// Leave the area: the next timer reads nothing and sets none.
	send(m, "1")
	reads = b.called("containers")
	tm.fire(t, m)
	if b.called("containers") != reads || len(tm.fns) != 0 {
		t.Fatalf("the area is hidden but reads %d, timers %d", b.called("containers")-reads, len(tm.fns))
	}
}

func TestContainersErrorStates(t *testing.T) {
	for code, want := range map[string]string{
		control.CodeDockerUnreachable: "container engine is not reachable",
		control.CodeDockerPermission:  "may not use the container engine",
		control.CodeDockerTimeout:     "did not answer in time",
	} {
		b := containersBackend()
		b.errs["containers:"] = control.NewError(code, "engine message that is not shown")
		m := newTestModel(t, i18n.English, b, ctNarrow, ctNarrowRows)
		send(m, "5")
		contains(t, m, want)
		noLeak(t, m.render(), "engine message that is not shown")

		// The engine comes back: the next refresh shows the containers and drops the error.
		delete(b.errs, "containers:")
		timersOf[m].fire(t, m)
		contains(t, m, "demo-stack-app-1")
		noLeak(t, m.render(), want)
	}
}

func TestContainersErrorKeepsTheRowsRead(t *testing.T) {
	b := containersBackend()
	m := newTestModel(t, i18n.English, b, ctNarrow, ctNarrowRows)
	send(m, "5")
	b.errs["containers:"] = control.NewError(control.CodeDockerUnreachable, "gone")
	timersOf[m].fire(t, m)
	contains(t, m, "demo-stack-app-1", "container engine is not reachable")
	if msg := m.message(); msg == nil || msg.Kind != kittui.MessageError {
		t.Fatalf("message = %+v", msg)
	}
}

func TestContainerDetailRefreshesAndNamesOnly(t *testing.T) {
	b := containersBackend()
	m := newTestModel(t, i18n.English, b, ctWide, ctWideRows)
	send(m, "5", "down", "down", "down", "down", "down", "enter")
	contains(t, m, "demo-stack-app-1", "APP_MODE", "DB_PASSWORD", "127.0.0.1:8080->8080/tcp", "com.docker.compose.project=demo-stack")
	reads := b.called("container")
	timersOf[m].fire(t, m)
	if b.called("container") != reads+1 {
		t.Errorf("the open detail was not read again")
	}
	// Esc closes the detail, a second esc leaves the area.
	send(m, "esc")
	if m.ctv.open || m.area != areaContainers {
		t.Fatalf("open %v, area %s", m.ctv.open, m.area)
	}
	send(m, "esc")
	if m.area != areaStacks {
		t.Fatalf("area %s", m.area)
	}
}

func TestContainerNamesAreNeutralised(t *testing.T) {
	b := containersBackend()
	evil := summary("\x1b[31mevil\x1b[0m", "example.invalid/\x1b]0;title\x07img", control.ContainerOK, "running", 1, 0)
	b.containers.Groups[3].Containers = append(b.containers.Groups[3].Containers, evil)
	b.containers.Groups = append(b.containers.Groups, control.ContainerGroup{Kind: control.ContainerKindForeign, Name: "\x1b[2Jproj",
		Containers: []control.ContainerSummary{summary("proj-1", "x", control.ContainerOK, "running", 1, 0)}})
	m := newTestModel(t, i18n.English, b, ctWide, ctWideRows)
	send(m, "5")
	if screen := m.render(); strings.ContainsAny(screen, "\x1b\x07") {
		t.Errorf("control characters reached the screen: %q", screen)
	}
}

func TestStackDetailListsItsContainers(t *testing.T) {
	m := newTestModel(t, i18n.English, containersBackend(), ctNarrow, ctNarrowRows)
	send(m, "enter")
	contains(t, m, "Containers", "demo-stack-app-1", "demo-stack-db-1", "unhealthy")
	noLeak(t, m.render(), "legacy-blog", "example-cache")

	// A stack without containers says so.
	m = newTestModel(t, i18n.English, demoBackend(), ctNarrow, ctNarrowRows)
	send(m, "enter")
	contains(t, m, "No containers of this stack.")
}
