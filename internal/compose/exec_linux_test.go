package compose

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// These tests start /bin/sh and sleep, never docker.

func TestExecRunnerEnvironmentIsExplicit(t *testing.T) {
	t.Setenv("DEMO_LEAK", "should-not-pass")
	res, err := (&ExecRunner{}).Run(context.Background(), Command{Name: "/bin/sh", Args: []string{"-c", "echo [$DEMO_LEAK][$KEPT]"}, Env: []string{"KEPT=yes"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(res.Output)); got != "[][yes]" {
		t.Errorf("output = %q", got)
	}
}

func TestExecRunnerExitCode(t *testing.T) {
	_, err := (&ExecRunner{}).Run(context.Background(), Command{Name: "/bin/sh", Args: []string{"-c", "exit 3"}})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 3 {
		t.Fatalf("err = %v", err)
	}
}

func TestExecRunnerBoundsOutput(t *testing.T) {
	res, err := (&ExecRunner{}).Run(context.Background(), Command{Name: "/bin/sh", MaxOutput: 100,
		Args: []string{"-c", "head -c 5000 /dev/zero | tr '\\0' a; echo END"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Output) > 200 || res.Omitted == 0 || !strings.HasSuffix(strings.TrimSpace(string(res.Output)), "END") {
		t.Errorf("len = %d omitted = %d", len(res.Output), res.Omitted)
	}
}

func TestExecRunnerKillsProcessGroupOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := (&ExecRunner{KillGrace: 100 * time.Millisecond}).Run(ctx, Command{Name: "/bin/sh",
		Args: []string{"-c", "trap '' TERM; sleep 30 & sleep 30; wait"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
}

func TestExecRunnerSendsStandardOutputToTheWriter(t *testing.T) {
	var out bytes.Buffer
	res, err := (&ExecRunner{}).Run(context.Background(), Command{
		Name: "/bin/sh", Args: []string{"-c", "echo data; echo warning >&2"}, Env: []string{}, Stdout: &out,
	})
	if err != nil || out.String() != "data\n" || string(res.Output) != "warning\n" {
		t.Fatalf("stdout %q, output %q, err %v", out.String(), res.Output, err)
	}
}
