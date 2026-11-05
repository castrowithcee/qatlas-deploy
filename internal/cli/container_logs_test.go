package cli

import (
	"encoding/json"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/docker"
)

func logLine(ago time.Duration, src docker.Source, text string) docker.LogLine {
	return docker.LogLine{Time: time.Now().Add(-ago).UTC().Truncate(time.Millisecond), Source: src, Text: text}
}

func TestParseLogTime(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	for in, want := range map[string]time.Time{
		"":                     {},
		"90s":                  now.Add(-90 * time.Second),
		"2h":                   now.Add(-2 * time.Hour),
		"2030-01-01T00:00:00Z": time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		got, err := parseLogTime("--since", in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("%q: %v %v, want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"yesterday", "-5m", "12", "2030-01-01"} {
		if _, err := parseLogTime("--since", in, now); err == nil || !strings.Contains(err.Error(), "--since") {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func TestContainerLogsOptionsAndOutput(t *testing.T) {
	f := sampleEngine()
	f.logs = map[string][]docker.LogLine{"c2": {
		logLine(3*time.Hour, docker.SourceStdout, "old"),
		logLine(30*time.Minute, docker.SourceStdout, "first"),
		logLine(20*time.Minute, docker.SourceStderr, "second\x1b[31m"),
		logLine(10*time.Minute, docker.SourceStdout, "third"),
	}}
	startContainers(t, f)

	code, out, stderr := run(t, "container", "logs", "lonely", "--since", "1h", "--until", "15m", "--tail", "2")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 2 || !strings.HasSuffix(lines[0], " first") || !strings.Contains(lines[1], " [stderr] second") ||
		strings.Contains(out, "\x1b") || strings.Contains(out, "lonely") {
		t.Fatalf("%d %q %s", code, out, stderr)
	}
	if code, out, _ = run(t, "container", "logs", "lonely", "--tail", "0"); code != 0 || strings.Count(out, "\n") != 4 {
		t.Fatalf("tail 0: %d %q", code, out)
	}
	code, out, stderr = run(t, "container", "logs", "lonely", "--tail", "1", "--output", "json")
	var got struct{ Time, Source, Stream, Text string }
	if err := json.Unmarshal([]byte(out), &got); code != 0 || err != nil || got.Text != "third" || got.Source != "lonely" || got.Stream != "stdout" {
		t.Fatalf("json: %d %q %s %v", code, out, stderr, err)
	}
}

func TestContainerLogsArgumentErrors(t *testing.T) {
	startContainers(t, sampleEngine())
	for _, args := range [][]string{
		{"container", "logs"},
		{"container", "logs", "lonely", "--stack", "x"},
		{"container", "logs", "lonely", "--since", "never"},
		{"container", "logs", "lonely", "--tail", "99999"},
	} {
		if code, _, stderr := run(t, args...); code == 0 || stderr == "" {
			t.Errorf("%v: %d %q", args, code, stderr)
		}
	}
	if code, _, stderr := run(t, "container", "logs", "--stack", "nope"); code == 0 || !strings.Contains(stderr, "no such stack") {
		t.Errorf("unknown stack: %d %q", code, stderr)
	}
	if code, _, stderr := run(t, "container", "logs", "nope"); code == 0 || !strings.Contains(stderr, "no such container") {
		t.Errorf("unknown container: %d %q", code, stderr)
	}
}

func TestContainerLogsFollowContinuesAtCursorAndEndsOnInterrupt(t *testing.T) {
	old := logsPollInterval
	logsPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { logsPollInterval = old })
	f := sampleEngine()
	f.logs = map[string][]docker.LogLine{"c2": {logLine(time.Minute, docker.SourceStdout, "one"), logLine(time.Minute-time.Second, docker.SourceStdout, "two")}}
	calls := 0
	f.onLogs = func() {
		calls++
		switch calls {
		case 2:
			f.logs["c2"] = append(f.logs["c2"], logLine(0, docker.SourceStdout, "three"))
		case 4:
			_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
		}
	}
	startContainers(t, f)
	code, out, stderr := run(t, "container", "logs", "lonely", "--follow")
	if code != 0 || stderr != "" || !strings.HasSuffix(strings.TrimSpace(out), " three") || strings.Count(out, "\n") != 3 ||
		strings.Count(out, " one\n") != 1 || strings.Count(out, " three\n") != 1 {
		t.Fatalf("%d %q %q", code, out, stderr)
	}
}
