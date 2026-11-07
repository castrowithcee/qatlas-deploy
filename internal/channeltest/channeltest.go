// Package channeltest gives tests a service channel in a private temporary directory, so no test touches the
// system's runtime directory.
package channeltest

import (
	"context"
	"os"
	"os/user"
	"strconv"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

// Endpoint returns a channel of the current user in a fresh directory that the test removes.
func Endpoint(t testing.TB) localipc.ServiceEndpoint {
	t.Helper()
	me, err := user.Current()
	if err != nil {
		t.Skipf("current user unknown: %v", err)
	}
	group, err := user.LookupGroupId(strconv.Itoa(os.Getgid()))
	if err != nil {
		t.Skipf("primary group unknown: %v", err)
	}
	// A short path keeps the socket below the address limit.
	dir, err := os.MkdirTemp("", "qd-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	return localipc.ServiceEndpoint{
		Dir: dir, SocketBase: "control", ServiceUser: me.Username, Group: group.Name, ErrorName: "qdeploy service",
	}
}

// readyLimit bounds how long WaitReady keeps dialing.
const readyLimit = 5 * time.Second

// WaitReady blocks until a dial of ep succeeds. The socket file exists before the listener has fixed its owner and
// mode, and the client refuses such a socket, so the file alone does not mean the channel is ready. Every successful
// probe connection is closed at once; every dial error is retried until the limit, then the test fails with the last one.
func WaitReady(t testing.TB, ep localipc.ServiceEndpoint) {
	t.Helper()
	waitReady(t, ep, readyLimit)
}

func waitReady(t testing.TB, ep localipc.ServiceEndpoint, limit time.Duration) {
	t.Helper()
	var last error
	for deadline := time.Now().Add(limit); ; time.Sleep(5 * time.Millisecond) {
		conn, err := ep.Dial(context.Background())
		if err == nil {
			_ = conn.Close()
			return
		}
		last = err
		if time.Now().After(deadline) {
			t.Fatalf("service channel not ready: %v", last)
		}
	}
}
