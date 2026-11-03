// Package service holds the long-running loop of the qdeploy service and the channel the command line talks to
// it over.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
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
	// Git is the git layer of the repository commands. Nil means the system git through internal/gitrepo;
	// tests inject their own.
	Git Git
	// Deployer brings a stack to a commit. Nil means the engine only polls, updates the working copy and
	// reports pending stacks; it never deploys.
	Deployer engine.Deployer
	// PollInterval is the time between repository polls; zero means engine.DefaultInterval.
	PollInterval time.Duration
}

// Run serves the channel until ctx ends and then returns nil: a stop request is the normal way for the service
// to end. It fails if the channel cannot be opened.
func Run(ctx context.Context, log *slog.Logger, cfg Config) error {
	secrets := &redact.Redactor{}
	log = slog.New(redactHandler{next: log.Handler(), r: secrets})
	// The channel's lock must keep a second instance away before opening the store sweeps the temporary
	// files of a write the first instance may be doing.
	l, err := cfg.Endpoint.Listen()
	if err != nil {
		return fmt.Errorf("open the service channel: %w", err)
	}
	defer func() { _ = l.Close() }()
	store, err := credentials.Open(cfg.StateDir, secrets.Add)
	if err != nil {
		return fmt.Errorf("open the credential store: %w", err)
	}
	registry := control.NewRegistry()
	started := time.Now().UTC().Truncate(time.Second)
	if err := registerCredentialCommands(registry, store); err != nil {
		return err
	}
	repo, err := newRepoService(cfg.StateDir, store, cfg.Git)
	if err != nil {
		return fmt.Errorf("prepare the repository commands: %w", err)
	}
	if err := registerRepoCommands(registry, repo); err != nil {
		return err
	}
	eng, err := engine.New(engine.Config{
		StateDir: cfg.StateDir, Source: repo.engineSource, Lock: &repo.mu, Deployer: cfg.Deployer,
		Redact: secrets.Apply, Interval: cfg.PollInterval, Logger: log,
	})
	if err != nil {
		return fmt.Errorf("prepare the engine: %w", err)
	}
	repo.onBound = eng.Wake
	if err := registerRunCommands(registry, eng, cfg.Version, started); err != nil {
		return err
	}
	engineDone := make(chan struct{})
	go func() {
		defer close(engineDone)
		if err := eng.Run(ctx); err != nil {
			log.Error("engine stopped", "error", err)
		}
	}()
	defer func() { <-engineDone }()
	check := cfg.PeerCheck
	if check == nil {
		check = RequireRoot
	}
	log.Info("service started")
	serve(ctx, l, registry, check, log)
	log.Info("service stopped")
	return nil
}
