package main

import (
	"bufio"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunEndsCleanlyOnSignal(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "qdeploy")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			cmd := exec.Command(bin, "run")
			stderr, err := cmd.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })

			started := make(chan struct{})
			drained := make(chan struct{})
			go func() {
				defer close(drained)
				scanner := bufio.NewScanner(stderr)
				signaled := false
				// Reading on to the end keeps the child from blocking on a full pipe.
				for scanner.Scan() {
					if !signaled && strings.Contains(scanner.Text(), "service started") {
						signaled = true
						close(started)
					}
				}
			}()

			// The service must be up, with its handler installed, before the signal is sent.
			select {
			case <-started:
			case <-time.After(10 * time.Second):
				t.Fatal("service did not start")
			}
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				<-drained // Wait closes the pipe, so all reads must be finished first.
				done <- cmd.Wait()
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("exit: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("service did not end after the signal")
			}
		})
	}
}
