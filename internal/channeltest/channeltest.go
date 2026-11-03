// Package channeltest gives tests a service channel in a private temporary directory, so no test touches the
// system's runtime directory.
package channeltest

import (
	"os"
	"os/user"
	"strconv"
	"testing"

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
