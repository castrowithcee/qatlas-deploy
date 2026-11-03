package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/ptytest"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
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

const (
	ptySecretChild = "QDEPLOY_TUI_PTY_SECRET_DIR"
	ptyValue       = "pty-value-Q7Zx-9031-example"
)

// fileSecretBackend keeps the secrets the way the service does, in a file of its own, and logs the name, never
// the value, of what it did.
type fileSecretBackend struct {
	*fakeBackend
	dir string
}

func (f fileSecretBackend) SecretSet(ctx context.Context, stack, key, value string) (control.SecretResult, error) {
	res, err := f.fakeBackend.SecretSet(ctx, stack, key, value)
	if err != nil {
		return res, err
	}
	if err := os.WriteFile(filepath.Join(f.dir, "secrets.env"), []byte(key+"="+value+"\n"), 0o600); err != nil {
		return res, err
	}
	line := "secret set " + stack + "/" + key + "\n"
	return res, os.WriteFile(filepath.Join(f.dir, "service.log"), []byte(line), 0o600)
}

// A secret set through the interface at a terminal stands in the secrets file and nowhere else: not on a
// screen the terminal got, not in the log.
func TestSecretSetAtATerminalLeavesTheValueOnlyInTheSecretsFile(t *testing.T) {
	if dir := os.Getenv(ptySecretChild); dir != "" {
		b := fileSecretBackend{fakeBackend: demoBackend(), dir: dir}
		if err := Run(Options{Backend: b, In: os.Stdin, Out: os.Stdout}); err != nil {
			t.Fatal(err)
		}
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	term, err := ptytest.Start(ptytest.Command{Path: exe, Args: []string{"-test.run=^TestSecretSetAtATerminalLeavesTheValueOnlyInTheSecretsFile$"},
		Env: []string{ptySecretChild + "=" + dir, "TERM=xterm-256color", "LANG=C.UTF-8"}, Size: ptytest.Size{Columns: 100, Rows: 40}})
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	step := func(wait string, input string) {
		t.Helper()
		if wait != "" {
			if err := term.WaitForText(10*time.Second, wait); err != nil {
				t.Fatalf("%v\n%q", err, term.Output())
			}
		}
		if input != "" {
			if _, err := term.Write([]byte(input)); err != nil {
				t.Fatal(err)
			}
		}
	}
	step("photo-album", "\r")
	step("API_SIGNING_KEY", "a")
	step("Set a secret", "PTY_SECRET\t"+ptyValue)
	step("••••••••", "\x1bOQ") // F2
	step("Set the secret PTY_SECRET", "y")
	step("The change applies at the next deployment", "\r")
	step("PTY_SECRET", "")
	step("", "q")
	if code, err := term.Wait(10 * time.Second); err != nil || code != 0 {
		t.Fatalf("exit code %d, %v\n%q", code, err, term.Output())
	}
	if strings.Contains(string(term.Output()), ptyValue) {
		t.Fatal("the value reached the terminal")
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if has := strings.Contains(string(raw), ptyValue); has != (f.Name() == "secrets.env") {
			t.Fatalf("%s holds the value: %v", f.Name(), has)
		}
	}
}
