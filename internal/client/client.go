// Package client is how the command line talks to the service: over its channel and nowhere else.
package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// CallTimeout bounds one call that has no deadline of its own.
const CallTimeout = 30 * time.Second

// ErrNotRunning means nothing listens on the service channel.
var ErrNotRunning = errors.New("the qdeploy service is not running")

// ErrNoAccess means the operating system denies this user the service channel.
var ErrNoAccess = errors.New("this user may not use the service channel")

// Client is one connection to the service. It is not safe for concurrent calls.
type Client struct {
	conn net.Conn
	br   *bufio.Reader
}

// Dial connects to the channel of ep. The transport checks who listens before anything is written.
func Dial(ctx context.Context, ep localipc.ServiceEndpoint) (*Client, error) {
	conn, err := ep.Dial(ctx)
	if err != nil {
		switch {
		case errors.Is(err, syscall.ENOENT), errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.ENOTDIR):
			return nil, fmt.Errorf("%w: %w", ErrNotRunning, err)
		case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
			return nil, fmt.Errorf("%w: %w", ErrNoAccess, err)
		}
		return nil, err
	}
	return &Client{conn: conn, br: bufio.NewReaderSize(conn, 4096)}, nil
}

// Do dials ep, sends one command within limit and closes the connection again.
func Do(ctx context.Context, ep localipc.ServiceEndpoint, limit time.Duration, command string, args, result any) error {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	c, err := Dial(ctx, ep)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(ctx, command, args, result)
}

// Close ends the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Call sends one command and decodes its result into result, which may be nil. A failed answer is returned as
// a *control.Error.
func (c *Client) Call(ctx context.Context, command string, args, result any) error {
	req := control.Request{V: control.Version, Command: command}
	if args != nil {
		raw, err := json.Marshal(args)
		if err != nil {
			return fmt.Errorf("encode arguments: %w", err)
		}
		req.Args = raw
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(CallTimeout)
	}
	_ = c.conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = c.conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	// A service that refuses this peer answers and closes without reading, so a failed write still leaves
	// an answer to read.
	writeErr := control.WriteFrame(c.conn, req, control.MaxRequestBytes)
	raw, err := control.ReadFrame(c.br, control.MaxResponseBytes)
	if err != nil {
		if writeErr != nil {
			return fmt.Errorf("send the request: %w", writeErr)
		}
		return fmt.Errorf("read the answer: %w", err)
	}
	var resp control.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("the answer is malformed: %w", err)
	}
	if resp.V != control.Version {
		return fmt.Errorf("the service speaks protocol version %d, this client version %d", resp.V, control.Version)
	}
	if !resp.OK {
		if resp.Error == nil {
			return errors.New("the service failed without a reason")
		}
		return resp.Error
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Result, result); err != nil {
		return fmt.Errorf("the result is malformed: %w", err)
	}
	return nil
}
