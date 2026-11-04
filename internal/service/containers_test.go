package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/docker"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
)

const (
	lblProject = "com.docker.compose.project"
	lblDir     = "com.docker.compose.project.working_dir"
	lblConfig  = "com.docker.compose.project.config_files"
)

// fakeDocker implements service.Docker.
type fakeDocker struct {
	list     []docker.Container
	details  map[string]docker.Details
	stats    map[string]docker.Stats
	listErr  error
	statsErr map[string]error

	inflight, peak atomic.Int32
}

func (f *fakeDocker) List(context.Context) ([]docker.Container, error) { return f.list, f.listErr }

func (f *fakeDocker) Inspect(_ context.Context, ref string) (docker.Details, error) {
	if d, ok := f.details[ref]; ok {
		return d, nil
	}
	return docker.Details{}, &docker.Error{Code: output.CodeNotFound, Message: "no such container"}
}

func (f *fakeDocker) Stats(_ context.Context, ref string) (docker.Stats, error) {
	n := f.inflight.Add(1)
	defer f.inflight.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond)
	if err := f.statsErr[ref]; err != nil {
		return docker.Stats{}, err
	}
	return f.stats[ref], nil
}

func withDocker(d service.Docker) func(*service.Config) {
	return func(c *service.Config) { c.Docker = d }
}

func running(id, name string, labels map[string]string) docker.Container {
	return docker.Container{ID: id, Name: name, Image: "registry.example.test/demo/" + name + ":1", State: "running",
		StartedAt: time.Now().Add(-90 * time.Second), Labels: labels}
}

func groupByName(t *testing.T, res control.ContainerListResult, kind, name string) control.ContainerGroup {
	t.Helper()
	for _, g := range res.Groups {
		if g.Kind == kind && g.Name == name {
			return g
		}
	}
	t.Fatalf("no %s group %q in %+v", kind, name, res.Groups)
	return control.ContainerGroup{}
}

func TestContainerListKindsAndStates(t *testing.T) {
	fd := &fakeDocker{}
	h := boundWith(t, nil, nil, withDocker(fd))
	own := filepath.Join(h.dir, "demo-stack")
	bad := unhealthyOf(running("c2", "demo-stack-db-1", map[string]string{lblProject: "demo-stack", lblDir: own}))
	bad.RestartCount = 3
	stopped := running("c3", "plain-stack-app-1", map[string]string{lblProject: "plain-stack", lblDir: "/srv/other/plain-stack"})
	stopped.State = "exited"
	fd.list = []docker.Container{
		running("c1", "demo-stack-app-1", map[string]string{lblProject: "demo-stack", lblDir: own}),
		bad,
		stopped,
		running("c4", "other-web-1", map[string]string{lblProject: "other", lblDir: "/srv/other", lblConfig: "/srv/other/a.yml,/srv/other/b.yml"}),
		running("c5", "lonely", nil),
	}
	var res control.ContainerListResult
	if err := h.call(control.ContainerListCommand, control.ContainerListArgs{}, &res); err != nil {
		t.Fatal(err)
	}
	g := groupByName(t, res, control.ContainerKindOwn, "demo-stack")
	if g.State != control.ContainerUnhealthy || g.Restarts != 3 || len(g.Containers) != 2 || g.Conflict || g.WorkingDir != "" {
		t.Fatalf("own group = %+v", g)
	}
	if g.UptimeSeconds < 85 || g.UptimeSeconds > 120 {
		t.Fatalf("uptime = %d", g.UptimeSeconds)
	}
	c := groupByName(t, res, control.ContainerKindForeign, "plain-stack")
	if !c.Conflict || c.State != control.ContainerStopped || c.WorkingDir != "/srv/other/plain-stack" {
		t.Fatalf("conflict group = %+v", c)
	}
	f := groupByName(t, res, control.ContainerKindForeign, "other")
	if f.Conflict || f.WorkingDir != "/srv/other" || strings.Join(f.ConfigFiles, "|") != "/srv/other/a.yml|/srv/other/b.yml" {
		t.Fatalf("foreign group = %+v", f)
	}
	s := groupByName(t, res, control.ContainerKindSingle, "lonely")
	if s.State != control.ContainerOK || len(s.Containers) != 1 {
		t.Fatalf("single group = %+v", s)
	}
	if res.Groups[0].Kind != control.ContainerKindOwn || res.Groups[len(res.Groups)-1].Kind != control.ContainerKindSingle {
		t.Fatalf("order = %+v", res.Groups)
	}
}

func unhealthyOf(c docker.Container) docker.Container { c.Health = "unhealthy"; return c }

func TestContainerWithoutBindingIsNeverOwn(t *testing.T) {
	fd := &fakeDocker{list: []docker.Container{running("c1", "demo-stack-app-1", map[string]string{lblProject: "demo-stack", lblDir: "/x/demo-stack"})}}
	h := startWith(t, nil, withDocker(fd))
	var res control.ContainerListResult
	if err := h.call(control.ContainerListCommand, control.ContainerListArgs{}, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Kind != control.ContainerKindForeign || res.Groups[0].Conflict {
		t.Fatalf("groups = %+v", res.Groups)
	}
}

func TestContainerStateOrder(t *testing.T) {
	fd := &fakeDocker{}
	h := startWith(t, nil, withDocker(fd))
	mk := func(id, state, health string) docker.Container {
		c := running(id, id, map[string]string{lblProject: "p", lblDir: "/p"})
		c.State, c.Health = state, health
		return c
	}
	for _, tc := range []struct {
		name string
		cs   []docker.Container
		want string
	}{
		{"ok", []docker.Container{mk("a", "running", "healthy")}, control.ContainerOK},
		{"starting", []docker.Container{mk("a", "running", "starting"), mk("b", "running", "")}, control.ContainerStarting},
		{"stopped", []docker.Container{mk("a", "running", "starting"), mk("b", "exited", "")}, control.ContainerStopped},
		{"restarting", []docker.Container{mk("a", "exited", ""), mk("b", "restarting", "")}, control.ContainerRestarting},
		{"unhealthy", []docker.Container{mk("a", "restarting", ""), mk("b", "running", "unhealthy")}, control.ContainerUnhealthy},
	} {
		fd.list = tc.cs
		var res control.ContainerListResult
		if err := h.call(control.ContainerListCommand, control.ContainerListArgs{}, &res); err != nil {
			t.Fatal(err)
		}
		if got := res.Groups[0].State; got != tc.want {
			t.Errorf("%s: state = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestContainerMetricsAreBoundedAndOptIn(t *testing.T) {
	fd := &fakeDocker{stats: map[string]docker.Stats{}, statsErr: map[string]error{"c3": &docker.Error{Code: output.CodeTimeout, Message: "x"}}}
	for i := range 40 {
		id := fmt.Sprintf("c%d", i)
		fd.list = append(fd.list, running(id, id, nil))
		fd.stats[id] = docker.Stats{CPUPercent: 1.5, MemoryUsed: 100, MemoryLimit: 1000}
	}
	stopped := running("stopped", "stopped", nil)
	stopped.State = "exited"
	fd.list = append(fd.list, stopped)
	h := startWith(t, nil, withDocker(fd))

	var plain control.ContainerListResult
	if err := h.call(control.ContainerListCommand, control.ContainerListArgs{}, &plain); err != nil {
		t.Fatal(err)
	}
	if fd.peak.Load() != 0 || plain.MetricsIncomplete || plain.Groups[0].Containers[0].Metrics != nil {
		t.Fatalf("metrics read without being asked: peak %d", fd.peak.Load())
	}
	var res control.ContainerListResult
	if err := h.call(control.ContainerListCommand, control.ContainerListArgs{Metrics: true}, &res); err != nil {
		t.Fatal(err)
	}
	if p := fd.peak.Load(); p < 2 || p > 8 {
		t.Fatalf("peak parallelism = %d, want 2..8", p)
	}
	if !res.MetricsIncomplete {
		t.Fatal("a failed reading must be reported")
	}
	withMetrics, without := 0, 0
	for _, g := range res.Groups {
		for _, c := range g.Containers {
			switch {
			case c.Metrics != nil:
				withMetrics++
				if g.Metrics == nil || g.Metrics.MemoryUsed != 100 {
					t.Fatalf("group metrics = %+v", g.Metrics)
				}
			default:
				without++
			}
		}
	}
	if withMetrics != 39 || without != 2 {
		t.Fatalf("with %d without %d", withMetrics, without)
	}
}

func TestContainerGetNeverShowsEnvValues(t *testing.T) {
	socket := fakeEngine(t)
	h := startWith(t, nil, func(c *service.Config) { c.DockerSocket = socket })
	var list control.ContainerListResult
	if err := h.call(control.ContainerListCommand, control.ContainerListArgs{Metrics: true}, &list); err != nil {
		t.Fatal(err)
	}
	var d control.ContainerDetail
	if err := h.call(control.ContainerGetCommand, control.ContainerGetArgs{Name: "demo-app-1"}, &d); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal([]any{list, d})
	if strings.Contains(string(raw), "invented-secret-value") {
		t.Fatalf("an environment value left the service: %s", raw)
	}
	if strings.Join(d.EnvNames, ",") != "DEMO_TOKEN,PATH" {
		t.Fatalf("env names = %v", d.EnvNames)
	}
	if len(d.Ports) != 1 || d.Ports[0].PublicPort != 8080 || len(d.Mounts) != 1 || len(d.Networks) != 1 || d.Labels["tier"] != "web" {
		t.Fatalf("detail = %+v", d)
	}
	if d.Kind != control.ContainerKindForeign || d.Project != "demo" {
		t.Fatalf("kind = %s project = %s", d.Kind, d.Project)
	}
	if len(list.Groups) != 1 || list.Groups[0].Containers[0].Metrics == nil || list.Groups[0].Containers[0].Metrics.MemoryUsed != 4096 {
		t.Fatalf("list through the real client = %+v", list)
	}
	err := h.call(control.ContainerGetCommand, control.ContainerGetArgs{Name: "no-such-one"}, &d)
	wantCode(t, err, control.CodeNotFound)
	err = h.call(control.ContainerGetCommand, control.ContainerGetArgs{Name: "bad name;"}, &d)
	wantCode(t, err, control.CodeInvalid)
}

// fakeEngine serves a minimal Engine API on a Unix socket and returns its path.
func fakeEngine(t *testing.T) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "e.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Api-Version", "1.45")
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/v1.45/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"Id":"abc123","Names":["/demo-app-1"],"Image":"registry.example.test/demo/app:1","State":"running","Status":"Up",
			"Labels":{"` + lblProject + `":"demo","` + lblDir + `":"/srv/demo"}}]`))
	})
	inspect := `{"Id":"abc123","Name":"/demo-app-1","RestartCount":2,
		"State":{"Status":"running","StartedAt":"2026-01-01T00:00:00Z"},
		"Config":{"Image":"registry.example.test/demo/app:1","Env":["PATH=/bin","DEMO_TOKEN=invented-secret-value"],
			"Labels":{"` + lblProject + `":"demo","` + lblDir + `":"/srv/demo","tier":"web"}},
		"Mounts":[{"Type":"bind","Source":"/srv/demo/data","Destination":"/data","RW":true}],
		"NetworkSettings":{"Ports":{"80/tcp":[{"HostIp":"0.0.0.0","HostPort":"8080"}]},"Networks":{"demo_default":{"IPAddress":"10.0.0.2"}}}}`
	mux.HandleFunc("/v1.45/containers/abc123/json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(inspect)) })
	mux.HandleFunc("/v1.45/containers/demo-app-1/json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(inspect)) })
	mux.HandleFunc("/v1.45/containers/abc123/stats", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"memory_stats":{"usage":4096,"limit":8192}}`))
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return socket
}

func TestContainerTextIsMasked(t *testing.T) {
	evil := "x\x1b[31mred\x07\r\nname"
	fd := &fakeDocker{
		list: []docker.Container{running("c1", evil, map[string]string{lblProject: evil, lblDir: "/srv/a\x1b]0;t\x07b", lblConfig: "/srv/\x1b[2Jc.yml"})},
		details: map[string]docker.Details{"c1": {
			Container: docker.Container{ID: "c1", Name: evil, Image: evil, State: "running", Labels: map[string]string{"k\x1b": "v\x1b[0m"}},
			Mounts:    []docker.Mount{{Type: "bind", Source: "/a\x1bb", Destination: "/c\x00d"}},
			Networks:  []docker.Network{{Name: "n\x1b", IPAddress: "1.2.3.4"}},
			Ports:     []docker.Port{{PrivatePort: 1, Protocol: "tcp", HostIP: "h\x1b"}},
			EnvNames:  []string{"A\x1bB"},
		}},
	}
	h := startWith(t, nil, withDocker(fd))
	var list control.ContainerListResult
	var d control.ContainerDetail
	if err := h.call(control.ContainerListCommand, control.ContainerListArgs{}, &list); err != nil {
		t.Fatal(err)
	}
	if err := h.call(control.ContainerGetCommand, control.ContainerGetArgs{Name: "c1"}, &d); err != nil {
		t.Fatal(err)
	}
	const f = "\ufffd"
	wantName := "x" + f + "[31mred" + f + f + f + "name"
	g := list.Groups[0]
	if g.Name != wantName || g.WorkingDir != "/srv/a"+f+"]0;t"+f+"b" || len(g.ConfigFiles) != 1 || g.ConfigFiles[0] != "/srv/"+f+"[2Jc.yml" ||
		g.Containers[0].Name != wantName || g.Containers[0].Image != "registry.example.test/demo/"+wantName+":1" {
		t.Fatalf("list not masked exactly: %+v", g)
	}
	if d.Name != wantName || d.Image != wantName || d.Labels["k"+f] != "v"+f+"[0m" || len(d.Labels) != 1 ||
		d.Mounts[0].Source != "/a"+f+"b" || d.Mounts[0].Destination != "/c"+f+"d" || d.Networks[0].Name != "n"+f ||
		d.Ports[0].HostIP != "h"+f || d.EnvNames[0] != "A"+f+"B" {
		t.Fatalf("detail not masked exactly: %+v", d)
	}
	for _, v := range []any{list, d} {
		raw, _ := json.Marshal(v)
		for _, r := range string(raw) {
			if r < 0x20 || r == 0x7f {
				t.Fatalf("control character %q in %s", r, raw)
			}
		}
	}
}

func TestContainerTextKeepsLengthAndSpaces(t *testing.T) {
	long := strings.Repeat("Host(`a.example.test`) || ", 40) // over 300 characters, with spaces
	dir := "/srv/two  spaces/app"
	fd := &fakeDocker{
		list: []docker.Container{running("c1", "web", map[string]string{lblProject: "p", lblDir: dir, lblConfig: dir + "/a  b.yml"})},
		details: map[string]docker.Details{"web": {
			Container: docker.Container{ID: "c1", Name: "web", State: "running", Labels: map[string]string{"rule": long}},
		}},
	}
	h := startWith(t, nil, withDocker(fd))
	var list control.ContainerListResult
	var d control.ContainerDetail
	if err := h.call(control.ContainerListCommand, control.ContainerListArgs{}, &list); err != nil {
		t.Fatal(err)
	}
	if err := h.call(control.ContainerGetCommand, control.ContainerGetArgs{Name: "web"}, &d); err != nil {
		t.Fatal(err)
	}
	if g := list.Groups[0]; g.WorkingDir != dir || g.ConfigFiles[0] != dir+"/a  b.yml" {
		t.Fatalf("paths changed: %+v", g)
	}
	if len(long) <= 300 || d.Labels["rule"] != long {
		t.Fatalf("label changed: %d of %d characters", len(d.Labels["rule"]), len(long))
	}
}

func TestContainerEngineFailuresAreDistinguishable(t *testing.T) {
	fd := &fakeDocker{}
	h := startWith(t, nil, withDocker(fd))
	seen := map[string]bool{}
	for code, want := range map[output.Code]string{
		output.CodeUnreachable: control.CodeDockerUnreachable,
		output.CodePermission:  control.CodeDockerPermission,
		output.CodeTimeout:     control.CodeDockerTimeout,
	} {
		fd.listErr = &docker.Error{Code: code, Message: "secret engine text"}
		err := h.call(control.ContainerListCommand, control.ContainerListArgs{}, nil)
		wantCode(t, err, want)
		if strings.Contains(err.Error(), "secret engine text") {
			t.Fatalf("engine text echoed: %v", err)
		}
		seen[want] = true
	}
	if len(seen) != 3 {
		t.Fatalf("codes not distinct: %v", seen)
	}
	// The same through the real client: nothing listens on the socket.
	h2 := startWith(t, nil, func(c *service.Config) { c.DockerSocket = filepath.Join(t.TempDir(), "none.sock") })
	wantCode(t, h2.call(control.ContainerListCommand, control.ContainerListArgs{}, nil), control.CodeDockerUnreachable)
	wantCode(t, h2.call(control.ContainerGetCommand, control.ContainerGetArgs{Name: "x"}, nil), control.CodeDockerUnreachable)
}

func TestContainerAnswerBeyondTheBoundIsRefused(t *testing.T) {
	fd := &fakeDocker{}
	long := strings.Repeat("i", 250)
	for i := range 5000 {
		c := running(fmt.Sprintf("id%d", i), fmt.Sprintf("name-%d-%s", i, long), nil)
		c.Image = long
		fd.list = append(fd.list, c)
	}
	h := startWith(t, nil, withDocker(fd))
	wantCode(t, h.call(control.ContainerListCommand, control.ContainerListArgs{}, nil), control.CodeTooLarge)
	fd.list = fd.list[:20]
	var res control.ContainerListResult
	if err := h.call(control.ContainerListCommand, control.ContainerListArgs{}, &res); err != nil || len(res.Groups) != 20 {
		t.Fatalf("small list: %v, %d groups", err, len(res.Groups))
	}
}
