package service_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/docker"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

var logBase = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

func logAt(ms int, src docker.Source, text string) docker.LogLine {
	return docker.LogLine{Time: logBase.Add(time.Duration(ms) * time.Millisecond), Source: src, Text: text}
}

// readAllLogs follows the cursor until the service says nothing is left and returns every line.
func readAllLogs(t *testing.T, h *harness, a control.ContainerLogsArgs) (lines []control.ContainerLogLine, pages int) {
	t.Helper()
	for {
		var res control.ContainerLogsResult
		if err := h.call(control.ContainerLogsCommand, a, &res); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(res)
		if len(raw) > control.MaxResponseBytes*3/4 {
			t.Fatalf("page of %d bytes is too close to the limit", len(raw))
		}
		pages++
		lines = append(lines, res.Lines...)
		if !res.More {
			return lines, pages
		}
		if n := len(res.Lines); n == 0 || !res.Cursor.Equal(res.Lines[n-1].Time.Add(time.Nanosecond)) {
			t.Fatalf("cursor %v does not follow the last line of %d", res.Cursor, n)
		}
		a.Cursor, a.Tail = res.Cursor, 0
	}
}

func TestContainerLogsStackMergesRedactsAndMasks(t *testing.T) {
	const planted = "invented-value-0002"
	fd := &fakeDocker{}
	files := map[string]string{
		"logs-stack/compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:1\n",
		"logs-stack/secrets.env":  "PLANTED=" + planted + "\n",
	}
	h := boundWith(t, nil, files, withDocker(fd))
	own := filepath.Join(h.dir, "logs-stack")
	labels := map[string]string{lblProject: "logs-stack", lblDir: own}
	app, db := running("c1", "logs-stack-app-1", labels), running("c2", "logs-stack-db-1", labels)
	foreign := running("c3", "logs-stack-old-1", map[string]string{lblProject: "logs-stack", lblDir: "/srv/elsewhere"})
	other := running("c4", "other-web-1", map[string]string{lblProject: "other", lblDir: "/srv/other"})
	fd.list = []docker.Container{db, foreign, app, other}
	fd.logs = map[string][]docker.LogLine{
		"c1": {logAt(10, docker.SourceStdout, "app first"), logAt(40, docker.SourceStderr, "token "+planted+" \x1b]0;x\x07end")},
		"c2": {logAt(20, docker.SourceStdout, "db second"), logAt(30, docker.SourceStdout, "db third")},
		"c3": {logAt(25, docker.SourceStdout, "foreign must not appear")},
		"c4": {logAt(26, docker.SourceStdout, "other must not appear")},
	}
	var res control.ContainerLogsResult
	if err := h.call(control.ContainerLogsCommand, control.ContainerLogsArgs{Stack: "logs-stack"}, &res); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range res.Lines {
		got = append(got, fmt.Sprintf("%s/%s/%s", l.Source, l.Stream, l.Text))
	}
	const f = "�"
	want := []string{
		"logs-stack-app-1/stdout/app first", "logs-stack-db-1/stdout/db second", "logs-stack-db-1/stdout/db third",
	}
	if len(got) != 4 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("lines = %q", got)
	}
	if strings.Contains(got[3], planted) || !strings.HasPrefix(got[3], "logs-stack-app-1/stderr/token ") || !strings.HasSuffix(got[3], "]0;x"+f+"end") {
		t.Fatalf("last line = %q", got[3])
	}
	raw, _ := json.Marshal(res)
	for _, r := range string(raw) {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("control character %q in %s", r, raw)
		}
	}
	if res.More || !res.Cursor.Equal(logAt(40, "", "").Time.Add(time.Nanosecond)) {
		t.Fatalf("more %v, cursor %v", res.More, res.Cursor)
	}

	for _, tc := range []struct {
		a    control.ContainerLogsArgs
		code string
	}{
		{control.ContainerLogsArgs{Stack: "nope"}, control.CodeNoSuchStack},
		{control.ContainerLogsArgs{Stack: "other"}, control.CodeNoSuchStack},
		{control.ContainerLogsArgs{}, control.CodeBadRequest},
		{control.ContainerLogsArgs{Name: "x", Stack: "logs-stack"}, control.CodeBadRequest},
		{control.ContainerLogsArgs{Stack: "logs-stack", Tail: 5, Cursor: logBase}, control.CodeInvalid},
		{control.ContainerLogsArgs{Stack: "logs-stack", Tail: -1}, control.CodeInvalid},
		{control.ContainerLogsArgs{Name: "missing"}, control.CodeNotFound},
	} {
		wantCode(t, h.call(control.ContainerLogsCommand, tc.a, nil), tc.code)
	}
}

func TestContainerLogsSingleContainerWindowAndTail(t *testing.T) {
	c := running("c1", "lonely", nil)
	fd := &fakeDocker{details: map[string]docker.Details{"lonely": {Container: c}}, logs: map[string][]docker.LogLine{}}
	for i := 0; i < 10; i++ {
		fd.logs["c1"] = append(fd.logs["c1"], logAt(i*10, docker.SourceStdout, fmt.Sprint("line ", i)))
	}
	h := startWith(t, nil, withDocker(fd))
	texts := func(a control.ContainerLogsArgs) string {
		var res control.ContainerLogsResult
		if err := h.call(control.ContainerLogsCommand, a, &res); err != nil {
			t.Fatal(err)
		}
		var s []string
		for _, l := range res.Lines {
			s = append(s, strings.TrimPrefix(l.Text, "line "))
		}
		return strings.Join(s, ",")
	}
	if got := texts(control.ContainerLogsArgs{Name: "lonely", Tail: 3}); got != "7,8,9" {
		t.Errorf("tail = %s", got)
	}
	if got := texts(control.ContainerLogsArgs{Name: "lonely", Since: logAt(20, "", "").Time, Until: logAt(50, "", "").Time}); got != "2,3,4,5" {
		t.Errorf("window = %s", got)
	}
	if got := texts(control.ContainerLogsArgs{Name: "lonely", Cursor: logAt(80, "", "").Time.Add(time.Nanosecond)}); got != "9" {
		t.Errorf("cursor = %s", got)
	}
	if got := texts(control.ContainerLogsArgs{Name: "lonely", Cursor: logAt(90, "", "").Time.Add(time.Nanosecond)}); got != "" {
		t.Errorf("past the end = %s", got)
	}
}

func TestContainerLogsBudgetPagesWithoutRepeatsOrGaps(t *testing.T) {
	c := running("c1", "chatty", nil)
	fd := &fakeDocker{details: map[string]docker.Details{"chatty": {Container: c}}, logs: map[string][]docker.LogLine{}}
	const total = 400
	for i := 0; i < total; i++ {
		fd.logs["c1"] = append(fd.logs["c1"], logAt(i, docker.SourceStdout, fmt.Sprintf("%04d %s", i, strings.Repeat("x", 4000))))
	}
	h := startWith(t, nil, withDocker(fd))
	lines, pages := readAllLogs(t, h, control.ContainerLogsArgs{Name: "chatty"})
	if pages < 2 || len(lines) != total {
		t.Fatalf("%d lines on %d pages", len(lines), pages)
	}
	for i, l := range lines {
		if !strings.HasPrefix(l.Text, fmt.Sprintf("%04d ", i)) {
			t.Fatalf("line %d is %q", i, l.Text[:8])
		}
	}
}

func TestContainerLogsNeverSplitLinesOfEqualTime(t *testing.T) {
	c := running("c1", "bursty", nil)
	fd := &fakeDocker{details: map[string]docker.Details{"bursty": {Container: c}}, logs: map[string][]docker.LogLine{}}
	const groups, size = 60, 7
	for g := 0; g < groups; g++ {
		for k := 0; k < size; k++ {
			fd.logs["c1"] = append(fd.logs["c1"], logAt(g, docker.SourceStdout, fmt.Sprintf("%02d-%d %s", g, k, strings.Repeat("y", 4000))))
		}
	}
	h := startWith(t, nil, withDocker(fd))
	var first control.ContainerLogsResult
	if err := h.call(control.ContainerLogsCommand, control.ContainerLogsArgs{Name: "bursty"}, &first); err != nil {
		t.Fatal(err)
	}
	if !first.More || len(first.Lines)%size != 0 || len(first.Lines) == 0 {
		t.Fatalf("first page of %d lines, more %v: a group was split", len(first.Lines), first.More)
	}
	lines, _ := readAllLogs(t, h, control.ContainerLogsArgs{Name: "bursty"})
	if len(lines) != groups*size {
		t.Fatalf("%d lines, want %d", len(lines), groups*size)
	}
	for i, l := range lines {
		if !strings.HasPrefix(l.Text, fmt.Sprintf("%02d-%d ", i/size, i%size)) {
			t.Fatalf("line %d is %q", i, l.Text[:5])
		}
	}
}
