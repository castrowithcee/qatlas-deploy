package cli

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/tui"
)

// fakeTerminal makes standard input and the returned stdout count as terminals and records whether the
// interface was started, with which options.
func fakeTerminal(t *testing.T) (stdout *os.File, started *[]tui.Options) {
	t.Helper()
	dir := t.TempDir()
	open := func(name string) *os.File {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}
	in, out := open("in"), open("out")
	t.Setenv("HOME", dir)
	oldIn, oldTerm, oldRun := stdinStream, isTerminal, runTUI
	stdinStream = func() io.Reader { return in }
	isTerminal = func(f *os.File) bool { return f == in || f == out }
	var calls []tui.Options
	runTUI = func(opts tui.Options) error { calls = append(calls, opts); return nil }
	t.Cleanup(func() { stdinStream, isTerminal, runTUI = oldIn, oldTerm, oldRun })
	return out, &calls
}

func runOn(stdout io.Writer, args ...string) (code int, stderr string) {
	var errOut bytes.Buffer
	code = Run(args, stdout, &errOut)
	return code, errOut.String()
}

func TestNoArgumentsAtATerminalStartsTheInterface(t *testing.T) {
	startService(t, func(net.Conn) error { return nil })
	out, started := fakeTerminal(t)
	if code, stderr := runOn(out); code != 0 || len(*started) != 1 {
		t.Fatalf("code %d, started %d, stderr %q", code, len(*started), stderr)
	}
	opts := (*started)[0]
	if opts.Out != out || opts.In == nil || opts.Backend == nil || opts.Prefs.Language != "english" || len(opts.PrefsWarnings) != 0 {
		t.Fatalf("options = %+v", opts)
	}
}

func TestTUICommandReadsThePreferences(t *testing.T) {
	startService(t, func(net.Conn) error { return nil })
	out, started := fakeTerminal(t)
	dir := filepath.Join(os.Getenv("HOME"), ".qatlas", "qdeploy")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tui.yaml"), []byte("language: deutsch\nstart: runs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, stderr := runOn(out, "tui"); code != 0 || len(*started) != 1 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if p := (*started)[0].Prefs; p.Language != "deutsch" || p.Start != "runs" {
		t.Fatalf("prefs = %+v", p)
	}
}

func TestNoArgumentsWithoutATerminalShowsHelp(t *testing.T) {
	_, started := fakeTerminal(t)
	var out bytes.Buffer
	if code, _ := runOn(&out); code != 0 || !strings.Contains(out.String(), "Usage:") || len(*started) != 0 {
		t.Fatalf("code %d, stdout %q, started %d", code, out.String(), len(*started))
	}
}

func TestTUICommandNeedsATerminal(t *testing.T) {
	_, started := fakeTerminal(t)
	var out bytes.Buffer
	code, stderr := runOn(&out, "tui")
	if code != 2 || !strings.Contains(stderr, "needs a terminal") || len(*started) != 0 {
		t.Fatalf("code %d, stderr %q, started %d", code, stderr, len(*started))
	}
}

func TestInterfaceWithoutServiceEndsWithCode3(t *testing.T) {
	useEndpoint(t, channeltest.Endpoint(t))
	out, started := fakeTerminal(t)
	code, stderr := runOn(out)
	if code != ExitServiceNotRunning || !strings.Contains(stderr, "not running") || len(*started) != 0 {
		t.Fatalf("code %d, stderr %q, started %d", code, stderr, len(*started))
	}
}

func TestInterfaceAsNonRootNamesSudo(t *testing.T) {
	startService(t, func(net.Conn) error { return control.ErrNotRoot() })
	out, started := fakeTerminal(t)
	code, stderr := runOn(out, "tui")
	if code != 1 || !strings.Contains(stderr, "sudo") || len(*started) != 0 {
		t.Fatalf("code %d, stderr %q, started %d", code, stderr, len(*started))
	}
}
