package compose

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// DefaultMaxOutput is the number of output bytes a call keeps: the first and the last half.
const DefaultMaxOutput = 64 << 10

const defaultKillGrace = 2 * time.Second

// ExecRunner runs programs with os/exec. Each program gets its own process group, and the whole group ends
// when the context does, so a hanging program does not leave children behind.
type ExecRunner struct {
	// KillGrace is the time between SIGTERM and SIGKILL; zero means two seconds.
	KillGrace time.Duration
}

// Run implements Runner.
func (r *ExecRunner) Run(ctx context.Context, c Command) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	grace := r.KillGrace
	if grace <= 0 {
		grace = defaultKillGrace
	}
	limit := c.MaxOutput
	if limit <= 0 {
		limit = DefaultMaxOutput
	}

	out := &boundedWriter{limit: limit}
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = c.Env
	if cmd.Env == nil {
		cmd.Env = []string{} // an empty environment, not the service's
	}
	cmd.Stdout, cmd.Stderr = out, out
	if c.Stdout != nil {
		cmd.Stdout = c.Stdout
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var mu sync.Mutex
	var killer *time.Timer
	cmd.Cancel = func() error {
		pid := cmd.Process.Pid
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		mu.Lock()
		killer = time.AfterFunc(grace, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
		mu.Unlock()
		return nil
	}
	// A backstop for children that keep the output pipe open after the group was signalled.
	cmd.WaitDelay = grace + time.Second

	err := cmd.Run()
	if cmd.Process != nil {
		mu.Lock()
		if killer != nil {
			killer.Stop()
		}
		mu.Unlock()
		if ctx.Err() != nil {
			// The leader may have exited on SIGTERM while a child ignored it.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}

	res := Result{Output: out.bytes(), Omitted: out.omitted()}
	if ctxErr := ctx.Err(); ctxErr != nil && err != nil {
		return res, fmt.Errorf("%s: %w", c.Name, ctxErr)
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
		return res, nil
	case errors.As(err, &ee):
		return res, &ExitError{Code: ee.ExitCode()}
	}
	return res, fmt.Errorf("%s: %w", c.Name, err)
}

// boundedWriter keeps the first and the last half of the limit and counts what lies between. It accepts every
// write so the program is never blocked or failed by a full buffer.
type boundedWriter struct {
	mu    sync.Mutex
	limit int
	head  []byte
	tail  []byte
	drop  int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	headLimit := w.limit / 2
	if room := headLimit - len(w.head); room > 0 {
		take := min(room, len(p))
		w.head = append(w.head, p[:take]...)
		p = p[take:]
	}
	if len(p) == 0 {
		return n, nil
	}
	tailLimit := w.limit - headLimit
	w.tail = append(w.tail, p...)
	if over := len(w.tail) - tailLimit; over > 0 {
		w.drop += int64(over)
		w.tail = append(w.tail[:0], w.tail[over:]...)
	}
	return n, nil
}

func (w *boundedWriter) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := append([]byte(nil), w.head...)
	if w.drop > 0 {
		out = append(out, fmt.Sprintf("\n[%d bytes omitted]\n", w.drop)...)
	}
	return append(out, w.tail...)
}

func (w *boundedWriter) omitted() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.drop
}
