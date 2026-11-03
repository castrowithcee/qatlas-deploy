// Package service holds the long-running loop of the qdeploy service and the channel the command line talks to
// it over.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
)

// DefaultStateDir is the state directory of the service under systemd's StateDirectory convention.
const DefaultStateDir = "/var/lib/qdeploy"

// Config is what Run needs from its caller.
type Config struct {
	// Version is reported by the status command.
	Version string
	// Endpoint is the channel to listen on; control.Endpoint() in production.
	Endpoint localipc.ServiceEndpoint
	// StateDir holds the credential store; it is required. DefaultStateDir in production.
	StateDir string
	// PeerCheck admits the peers of the channel. Nil means RequireRoot; tests inject their own.
	PeerCheck PeerCheck
}

// Run serves the channel until ctx ends and then returns nil: a stop request is the normal way for the service
// to end. It fails if the channel cannot be opened.
func Run(ctx context.Context, log *slog.Logger, cfg Config) error {
	secrets := &redact.Redactor{}
	log = slog.New(redactHandler{next: log.Handler(), r: secrets})
	store, err := credentials.Open(cfg.StateDir, secrets.Add)
	if err != nil {
		return fmt.Errorf("open the credential store: %w", err)
	}
	registry := control.NewRegistry()
	started := time.Now().UTC().Truncate(time.Second)
	err = registry.Register(control.StatusCommand, func(context.Context, json.RawMessage) (any, error) {
		return control.StatusResult{Version: cfg.Version, StartedAt: started}, nil
	})
	if err != nil {
		return err
	}
	if err := registerCredentialCommands(registry, store); err != nil {
		return err
	}
	check := cfg.PeerCheck
	if check == nil {
		check = RequireRoot
	}
	l, err := cfg.Endpoint.Listen()
	if err != nil {
		return fmt.Errorf("open the service channel: %w", err)
	}
	log.Info("service started")
	serve(ctx, l, registry, check, log)
	log.Info("service stopped")
	return nil
}
