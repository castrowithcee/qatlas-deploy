package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
)

// syncBuffer lets the test read the log while the service still writes it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestRunEndsCleanlyOnSignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			useEndpoint(t, channeltest.Endpoint(t))
			useStateDir(t)
			var stderr syncBuffer
			done := make(chan int, 1)
			go func() { done <- Run([]string{"run"}, io.Discard, &stderr) }()

			// The handler is installed before the service logs its start, so the signal is safe afterwards.
			deadline := time.Now().Add(10 * time.Second)
			for !strings.Contains(stderr.String(), "service started") {
				select {
				case code := <-done:
					t.Fatalf("run ended early with %d: %s", code, stderr.String())
				default:
				}
				if time.Now().After(deadline) {
					t.Fatalf("service did not start: %s", stderr.String())
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err := syscall.Kill(os.Getpid(), sig); err != nil {
				t.Fatal(err)
			}
			select {
			case code := <-done:
				if code != 0 {
					t.Fatalf("exit code %d: %s", code, stderr.String())
				}
			case <-time.After(10 * time.Second):
				t.Fatal("service did not end after the signal")
			}
		})
	}
}
