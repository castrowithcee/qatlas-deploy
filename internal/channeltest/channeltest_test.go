package channeltest

import (
	"testing"
	"time"
)

// A channel that starts listening only after a delay is waited for, not failed.
func TestWaitReadyWaitsForALateListener(t *testing.T) {
	ep := Endpoint(t)
	ready := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		l, err := ep.Listen()
		if err != nil {
			t.Error(err)
			close(ready)
			return
		}
		t.Cleanup(func() { _ = l.Close() })
		close(ready)
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	waitReady(t, ep, 5*time.Second)
	<-ready
}
