package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func allowAll(net.Conn) error { return nil }

func runService(t *testing.T, cfg Config) localipc.ServiceEndpoint {
	t.Helper()
	if cfg.Endpoint == (localipc.ServiceEndpoint{}) {
		cfg.Endpoint = channeltest.Endpoint(t)
	}
	if cfg.StateDir == "" {
		cfg.StateDir = filepath.Join(t.TempDir(), "state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, quiet, cfg) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run = %v", err)
		}
	})
	channeltest.WaitReady(t, cfg.Endpoint)
	return cfg.Endpoint
}

func rawExchange(t *testing.T, ep localipc.ServiceEndpoint, payload string) control.Response {
	t.Helper()
	conn, err := ep.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	go func() { _, _ = io.WriteString(conn, payload) }()
	raw, err := control.ReadFrame(bufio.NewReader(conn), control.MaxResponseBytes)
	if err != nil {
		t.Fatalf("read answer: %v", err)
	}
	var resp control.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestStatusAnsweredForAdmittedPeer(t *testing.T) {
	ep := runService(t, Config{Version: "1.2.3", PeerCheck: allowAll})
	cl, err := client.Dial(context.Background(), ep)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	before := time.Now().Add(-2 * time.Second)
	var st control.StatusResult
	if err := cl.Call(context.Background(), control.StatusCommand, nil, &st); err != nil {
		t.Fatal(err)
	}
	if st.Version != "1.2.3" || st.StartedAt.Before(before) || st.StartedAt.After(time.Now()) {
		t.Fatalf("status = %+v", st)
	}
	// The connection serves a second request.
	if err := cl.Call(context.Background(), control.StatusCommand, nil, &st); err != nil {
		t.Fatal(err)
	}
}

func callError(t *testing.T, ep localipc.ServiceEndpoint) *control.Error {
	t.Helper()
	cl, err := client.Dial(context.Background(), ep)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	var ce *control.Error
	if err := cl.Call(context.Background(), control.StatusCommand, nil, nil); !errors.As(err, &ce) {
		t.Fatalf("Call = %v, want a service error", err)
	}
	return ce
}

func TestPeerWithoutRootIsRejectedWithSudoMessage(t *testing.T) {
	ep := runService(t, Config{Version: "x", PeerCheck: func(net.Conn) error { return control.ErrNotRoot() }})
	ce := callError(t, ep)
	if ce.Code != control.CodeNotRoot || !strings.Contains(ce.Message, "sudo") {
		t.Fatalf("error = %+v", ce)
	}
}

func TestDefaultPeerCheckRefusesNonRoot(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("the test needs a non-root user")
	}
	ep := runService(t, Config{Version: "x"})
	if ce := callError(t, ep); ce.Code != control.CodeNotRoot {
		t.Fatalf("error = %+v", ce)
	}
}

func TestPeerCheckFailureWithoutServiceErrorStillRefuses(t *testing.T) {
	ep := runService(t, Config{Version: "x", PeerCheck: func(net.Conn) error { return errors.New("cannot tell") }})
	if ce := callError(t, ep); ce.Code != control.CodeNotRoot {
		t.Fatalf("error = %+v", ce)
	}
}

func TestMalformedRequestsAreAnsweredAndNeverPanic(t *testing.T) {
	ep := runService(t, Config{Version: "x", PeerCheck: allowAll})
	for name, tc := range map[string]struct {
		payload string
		code    string
	}{
		"unknown command":  {`{"v":1,"command":"nope"}` + "\n", control.CodeUnknownCommand},
		"wrong version":    {`{"v":2,"command":"status"}` + "\n", control.CodeVersion},
		"missing version":  {`{"command":"status"}` + "\n", control.CodeVersion},
		"not json":         {"\x00\xff garbage\n", control.CodeBadRequest},
		"json array":       {"[1,2]\n", control.CodeBadRequest},
		"no command":       {`{"v":1}` + "\n", control.CodeBadRequest},
		"too large":        {`{"v":1,"command":"` + strings.Repeat("a", control.MaxRequestBytes) + "\"}\n", control.CodeTooLarge},
		"too large no eol": {strings.Repeat("a", control.MaxRequestBytes+10), control.CodeTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			resp := rawExchange(t, ep, tc.payload)
			if resp.OK || resp.Error == nil || resp.Error.Code != tc.code || resp.V != control.Version {
				t.Fatalf("answer = %+v", resp)
			}
		})
	}
	// The service is still alive afterwards.
	if resp := rawExchange(t, ep, `{"v":1,"command":"status"}`+"\n"); !resp.OK {
		t.Fatalf("status after abuse = %+v", resp)
	}
}

func TestHandlerFailuresDoNotLeakOrCrash(t *testing.T) {
	reg := control.NewRegistry()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(reg.Register("boom", func(context.Context, json.RawMessage) (any, error) { panic("secret detail") }))
	must(reg.Register("fail", func(context.Context, json.RawMessage) (any, error) { return nil, errors.New("secret detail") }))
	must(reg.Register("typed", func(context.Context, json.RawMessage) (any, error) {
		return nil, control.NewError("nothing-open", "nothing is open")
	}))
	ep := channeltest.Endpoint(t)
	l, err := ep.Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { serve(ctx, l, reg, allowAll, quiet); close(done) }()
	defer func() { cancel(); <-done }()
	for command, code := range map[string]string{"boom": control.CodeInternal, "fail": control.CodeInternal, "typed": "nothing-open"} {
		resp := rawExchange(t, ep, `{"v":1,"command":"`+command+"\"}\n")
		if resp.Error == nil || resp.Error.Code != code || strings.Contains(resp.Error.Message, "secret") {
			t.Fatalf("%s answer = %+v", command, resp)
		}
	}
}
