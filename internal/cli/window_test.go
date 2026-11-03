package cli

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
)

func objectOf(t *testing.T, out string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output = %q: %v", out, err)
	}
	return got
}

func TestWindowShowAndSet(t *testing.T) {
	startService(t, func(net.Conn) error { return nil })
	code, out, stderr := run(t, "window", "show", "--output", "json")
	if code != 0 {
		t.Fatalf("show: %d %s", code, stderr)
	}
	got := objectOf(t, out)
	if got["days"] != "mon,tue,wed,thu,fri,sat,sun" || got["start"] != "03:00" || got["duration"] != "2h" || got["zone"] != "local" {
		t.Fatalf("show = %v", got)
	}
	next, err := time.Parse(time.RFC3339, got["next_opening"].(string))
	if h, m, _ := next.In(time.Local).Clock(); err != nil || h != 3 || m != 0 || !next.After(time.Now()) {
		t.Fatalf("next_opening = %v, %v", got["next_opening"], err)
	}

	code, out, stderr = run(t, "window", "set", "--start", "04:30", "--zone", "UTC", "--duration", "1h30m", "--output", "json")
	if code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	got = objectOf(t, out)
	if got["days"] != "mon,tue,wed,thu,fri,sat,sun" || got["start"] != "04:30" || got["duration"] != "1h30m" || got["zone"] != "UTC" ||
		!strings.HasSuffix(got["next_opening"].(string), "T04:30:00Z") {
		t.Fatalf("set = %v", got)
	}
	code, out, stderr = run(t, "window", "set", "--days", "sat, Sunday")
	if code != 0 || !strings.Contains(out, "sat,sun") || !strings.Contains(out, "04:30") || !strings.Contains(out, "NEXT_OPENING") {
		t.Fatalf("set days: %d %q %s", code, out, stderr)
	}
	if code, out, _ := run(t, "window", "show"); code != 0 || !strings.Contains(out, "sat,sun") || !strings.Contains(out, "UTC") {
		t.Fatalf("show table: %d %q", code, out)
	}
	if code, out, _ := run(t, "status", "--output", "json"); code != 0 || objectOf(t, out)["window_next_opening"] == nil {
		t.Fatalf("status: %d %q", code, out)
	}
}

func TestWindowSetErrors(t *testing.T) {
	startService(t, func(net.Conn) error { return nil })
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"window", "set"}, "at least one"},
		{[]string{"window", "set", "--start", "25:00"}, "HH:MM"},
		{[]string{"window", "set", "--days", "funday"}, "weekday"},
		{[]string{"window", "set", "--days", ""}, "weekday"},
		{[]string{"window", "set", "--duration", "13h"}, "duration"},
		{[]string{"window", "set", "--zone", "Nowhere/Atlantis"}, "zone"},
		{[]string{"window", "set", "--zone", ""}, "--zone needs a value"},
		{[]string{"window", "show", "extra"}, ""},
	} {
		code, out, stderr := run(t, tc.args...)
		if code != 2 || out != "" || !strings.Contains(stderr, tc.want) {
			t.Fatalf("%v: %d %q %q", tc.args, code, out, stderr)
		}
	}
	if code, out, _ := run(t, "window", "show", "--output", "json"); code != 0 || objectOf(t, out)["start"] != "03:00" {
		t.Fatalf("refused changes were stored: %d %q", code, out)
	}
}

func TestWindowWithoutRunningService(t *testing.T) {
	useEndpoint(t, channeltest.Endpoint(t))
	if code, _, stderr := run(t, "window", "show"); code != ExitServiceNotRunning || !strings.Contains(stderr, "not running") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
}
