package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// maxConns bounds the connections served at once; further ones are closed unanswered.
const maxConns = 32

// PeerCheck decides whether the peer of a connection may use the service. It runs before anything is read.
type PeerCheck func(conn net.Conn) error

// RequireRoot admits UID 0 only. A peer whose user cannot be told is refused.
func RequireRoot(conn net.Conn) error {
	uid, err := localipc.PeerUID(conn)
	if err != nil || uid != 0 {
		return control.ErrNotRoot()
	}
	return nil
}

type server struct {
	registry *control.Registry
	check    PeerCheck
	log      *slog.Logger

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	wg    sync.WaitGroup
}

// serve accepts connections on l until ctx ends, then closes l and every open connection and waits for the
// handlers. It owns l.
func serve(ctx context.Context, l net.Listener, registry *control.Registry, check PeerCheck, log *slog.Logger) {
	s := &server{registry: registry, check: check, log: log, conns: map[net.Conn]struct{}{}}
	stop := context.AfterFunc(ctx, func() {
		_ = l.Close()
		s.mu.Lock()
		for c := range s.conns {
			_ = c.SetDeadline(time.Unix(1, 0))
		}
		s.mu.Unlock()
	})
	defer stop()
	defer s.wg.Wait()
	for {
		conn, err := l.Accept()
		if err != nil {
			var refused *localipc.PeerError
			switch {
			case errors.As(err, &refused):
				log.Warn("refused peer", "pid", refused.PID)
				continue
			case ctx.Err() != nil || errors.Is(err, net.ErrClosed):
				return
			}
			log.Error("accept failed", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		if ctx.Err() != nil || !s.track(conn) {
			_ = conn.Close()
			continue
		}
		go s.handle(ctx, conn)
	}
}

func (s *server) track(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.conns) >= maxConns {
		return false
	}
	s.conns[conn] = struct{}{}
	s.wg.Add(1)
	return true
}

func (s *server) handle(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		_ = conn.Close()
	}()
	if err := s.check(conn); err != nil {
		var ce *control.Error
		if !errors.As(err, &ce) {
			ce = control.ErrNotRoot()
		}
		s.respond(conn, errorResponse(ce))
		linger(conn)
		return
	}
	br := bufio.NewReaderSize(conn, 4096)
	for first := true; ; first = false {
		wait := control.IdleTimeout
		if first {
			wait = control.ReadTimeout
		}
		_ = conn.SetReadDeadline(time.Now().Add(wait))
		raw, err := control.ReadFrame(br, control.MaxRequestBytes)
		if err != nil {
			if errors.Is(err, control.ErrTooLarge) {
				s.respond(conn, errorResponse(control.NewError(control.CodeTooLarge, "request is too large")))
				linger(conn)
			} else if !errors.Is(err, io.EOF) && ctx.Err() == nil && !errors.Is(err, os.ErrDeadlineExceeded) {
				s.respond(conn, errorResponse(control.NewError(control.CodeBadRequest, "request is incomplete")))
				linger(conn)
			}
			return
		}
		resp, keep := s.dispatch(ctx, raw)
		if !s.respond(conn, resp) {
			return
		}
		if !keep {
			linger(conn)
			return
		}
	}
}

// dispatch answers one request. keep is false when the stream cannot be trusted any longer.
func (s *server) dispatch(ctx context.Context, raw []byte) (resp control.Response, keep bool) {
	var req control.Request
	if err := json.Unmarshal(raw, &req); err != nil || req.Command == "" {
		return errorResponse(control.NewError(control.CodeBadRequest, "request is not a valid command")), false
	}
	if req.V != control.Version {
		return errorResponse(control.NewError(control.CodeVersion,
			fmt.Sprintf("protocol version %d is not supported; this service speaks version %d", req.V, control.Version))), false
	}
	handler, ok := s.registry.Lookup(req.Command)
	if !ok {
		return errorResponse(control.NewError(control.CodeUnknownCommand, "unknown command")), true
	}
	return s.run(ctx, handler, req.Args), true
}

func (s *server) run(ctx context.Context, handler control.Handler, args json.RawMessage) (resp control.Response) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("command panicked", "panic", fmt.Sprint(r))
			resp = errorResponse(control.NewError(control.CodeInternal, "internal error"))
		}
	}()
	value, err := handler(ctx, args)
	if err != nil {
		var ce *control.Error
		if errors.As(err, &ce) {
			return errorResponse(ce)
		}
		s.log.Error("command failed", "error", err)
		return errorResponse(control.NewError(control.CodeInternal, "internal error"))
	}
	result, err := json.Marshal(value)
	if err != nil {
		s.log.Error("command result cannot be encoded", "error", err)
		return errorResponse(control.NewError(control.CodeInternal, "internal error"))
	}
	return control.Response{V: control.Version, OK: true, Result: result}
}

func errorResponse(e *control.Error) control.Response {
	return control.Response{V: control.Version, Error: e}
}

func (s *server) respond(conn net.Conn, resp control.Response) bool {
	_ = conn.SetWriteDeadline(time.Now().Add(control.WriteTimeout))
	if err := control.WriteFrame(conn, resp, control.MaxResponseBytes); err != nil {
		if errors.Is(err, control.ErrTooLarge) {
			err = control.WriteFrame(conn, errorResponse(control.NewError(control.CodeTooLarge, "answer is too large")),
				control.MaxResponseBytes)
		}
		return err == nil
	}
	return true
}

// lingerWait bounds how long a refused or broken peer may keep sending after its answer.
const lingerWait = time.Second

// linger ends the answer and discards what the peer still sends for a moment. Closing at once would reset
// the connection and could destroy the answer before the peer reads it.
func linger(conn net.Conn) {
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
	_ = conn.SetReadDeadline(time.Now().Add(lingerWait))
	_, _ = io.Copy(io.Discard, io.LimitReader(conn, control.MaxRequestBytes))
}
