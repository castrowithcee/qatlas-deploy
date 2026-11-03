package tui

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/ptytest"
)

// ptyChild makes the test binary run the interface itself, at the terminal the parent test opened.
const ptyChild = "QDEPLOY_TUI_PTY_CHILD"

// The interface starts on a real terminal, draws the stacks and ends cleanly on q.
func TestInterfaceRunsAtATerminal(t *testing.T) {
	if os.Getenv(ptyChild) == "1" {
		if err := Run(Options{Backend: demoBackend(), In: os.Stdin, Out: os.Stdout}); err != nil {
			t.Fatal(err)
		}
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	term, err := ptytest.Start(ptytest.Command{Path: exe, Args: []string{"-test.run=^TestInterfaceRunsAtATerminal$"},
		Env: []string{ptyChild + "=1", "TERM=xterm-256color", "LANG=C.UTF-8"}, Size: ptytest.Size{Columns: 100, Rows: 20}})
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if err := term.WaitForText(10*time.Second, "photo-album"); err != nil {
		t.Fatalf("%v\n%q", err, term.Output())
	}
	if _, err := term.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	if code, err := term.Wait(10 * time.Second); err != nil || code != 0 {
		t.Fatalf("exit code %d, %v\n%q", code, err, term.Output())
	}
}
