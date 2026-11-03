package cli

import (
	"bytes"
	"errors"
	"flag"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/ptytest"
)

// The service channel of qdeploy is fixed, so a separately built binary cannot be pointed at a test service.
// The test binary therefore runs the command line itself: the parent re-executes it with this variable set,
// and the child calls Run on its own standard streams the way main does, against a service it starts.
const cliChild = "QDEPLOY_CLI_PTY_CHILD"

// TestCommandLineChild is the child of the tests below. The arguments after -- are the command line.
func TestCommandLineChild(t *testing.T) {
	if os.Getenv(cliChild) != "1" {
		t.Skip("runs only as the child of the terminal tests")
	}
	code := cli.ExitRuntime
	t.Run("run", func(t *testing.T) {
		startService(t, func(net.Conn) error { return nil })
		code = Run(flag.Args(), os.Stdout, os.Stderr)
	})
	os.Exit(code)
}

func childCommand(t *testing.T, args ...string) (path string, argv, env []string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	return exe, append([]string{"-test.run=^TestCommandLineChild$", "--"}, args...),
		[]string{cliChild + "=1", "TERM=xterm-256color", "LANG=C.UTF-8", "HOME=" + home}
}

func TestBinaryWithoutArgumentsAtATerminalRunsTheInterface(t *testing.T) {
	path, args, env := childCommand(t)
	term, err := ptytest.Start(ptytest.Command{Path: path, Args: args, Env: env, Size: ptytest.Size{Columns: 100, Rows: 24}})
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if err := term.WaitForText(10*time.Second, "1 Stacks"); err != nil {
		t.Fatalf("%v\n%q", err, term.Output())
	}
	if _, err := term.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	if code, err := term.Wait(10 * time.Second); err != nil || code != 0 {
		t.Fatalf("exit code %d, %v\n%q", code, err, term.Output())
	}
}

// runPiped runs the child with standard input and output as pipes and returns the exit code and both outputs.
func runPiped(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	path, argv, env := childCommand(t, args...)
	cmd := exec.Command(path, argv...)
	cmd.Env = env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("the child did not end\n%q %q", out.String(), errOut.String())
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), out.String(), errOut.String()
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0, out.String(), errOut.String()
}

func TestBinaryWithoutArgumentsOnPipesShowsTheHelp(t *testing.T) {
	code, stdout, stderr := runPiped(t)
	if code != 0 || !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "tui") {
		t.Fatalf("code %d\nstdout %q\nstderr %q", code, stdout, stderr)
	}
}

func TestBinaryTUICommandOnPipesFailsWithTheUsageCode(t *testing.T) {
	code, stdout, stderr := runPiped(t, "tui")
	if code != cli.ExitUsage || !strings.Contains(stderr, "needs a terminal") {
		t.Fatalf("code %d\nstdout %q\nstderr %q", code, stdout, stderr)
	}
}
