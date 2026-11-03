package control

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
)

// Handler serves one command. It receives the raw arguments of the request, which it must treat as untrusted,
// and returns a value that is encoded as the result. An *Error becomes the answer's error as it is; any other
// error is answered as an internal error without its text.
type Handler func(ctx context.Context, args json.RawMessage) (any, error)

var commandName = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9-]*)*$`)

// Registry maps command names to handlers. Features register their commands before the service serves.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{handlers: map[string]Handler{}} }

// Register adds a command. A name registers once, and must be lower-case words joined by dots.
func (r *Registry) Register(name string, h Handler) error {
	if !commandName.MatchString(name) || h == nil {
		return fmt.Errorf("invalid command registration %q", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.handlers[name]; ok {
		return fmt.Errorf("command %q is already registered", name)
	}
	r.handlers[name] = h
	return nil
}

// Lookup returns the handler of a command.
func (r *Registry) Lookup(name string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[name]
	return h, ok
}
