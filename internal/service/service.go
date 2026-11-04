// Package service holds the long-running loop of the qdeploy service and the channel the command line talks to
// it over.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/castrowithcee/qatlas-kit/docker"
	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/notify"
	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/changelog"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	registryclient "github.com/castrowithcee/qatlas-deploy/internal/registry"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
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
	// Backups saves and restores the data of a stack. Nil means the backup commands answer that backups are
	// not available.
	Backups Backups
	// Registry configures the registry clients of the update detection; tests point it at a local server. Its
	// Credential is ignored: logins come from registryCredential.
	Registry registryclient.Options
	// Changelog configures the GitHub client of the changelogs; tests point it at a local server. Its Token and
	// CacheDir are ignored: the token comes from the credential "github.com" and the cache lives in StateDir.
	Changelog changelog.Options
	// DockerSocket is the socket of the container engine; empty means DefaultDockerSocket. It is read only
	// when Docker is nil.
	DockerSocket string
	// Docker reads the containers of the host. Nil means the container engine at DockerSocket; tests inject
	// their own.
	Docker Docker
	// PollInterval is the time between repository polls; zero means engine.DefaultInterval.
	PollInterval time.Duration
	// Clock drives the poll loop and the maintenance window; nil means the system clock. Tests inject their own.
	Clock engine.Clock
	// Notify configures the notification sender; tests give it the roots of a local server and short waits. Its
	// Redactor is ignored: the service's own redactor is used.
	Notify notify.Options
	// OnUpdateAvailable, when set, is told every stack in manual mode the maintenance window found an update
	// for. It runs in the window's goroutine and must return quickly.
	OnUpdateAvailable func(UpdateNotice)
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
	notifier, err := newNotifyService(cfg.StateDir, store, cfg.Notify, secrets, cfg.Version, log)
	if err != nil {
		return fmt.Errorf("prepare the notifications: %w", err)
	}
	// Deferred first, so it runs after the engine has stopped and queued its last events.
	defer notifier.close()
	if err := registerNotifyCommands(registry, notifier); err != nil {
		return err
	}
	deployer := cfg.Deployer
	if deployer != nil {
		deployer = redactingDeployer{next: deployer, r: secrets}
	}
	eng, err := engine.New(engine.Config{
		StateDir: cfg.StateDir, Source: repo.engineSource, Lock: &repo.mu, Deployer: deployer,
		Events: notifier.runEvent,
		Redact: secrets.Apply, Interval: cfg.PollInterval, Clock: cfg.Clock, Logger: log,
	})
	if err != nil {
		return fmt.Errorf("prepare the engine: %w", err)
	}
	notifier.bindRunLog(eng.AppendLog)
	repo.onBound = eng.Wake
	upd, err := registerUpdateCommands(registry, repo, eng, store, cfg.Registry, cfg.Changelog)
	if err != nil {
		return err
	}
	win, err := newWindowService(cfg.StateDir, upd, cfg.Clock, log, func(n UpdateNotice) {
		if cfg.OnUpdateAvailable != nil {
			cfg.OnUpdateAvailable(n)
		}
		notifier.updateAvailable(n)
	})
	if err != nil {
		return fmt.Errorf("prepare the maintenance window: %w", err)
	}
	if err := registerWindowCommands(registry, win); err != nil {
		return err
	}
	if err := registerRunCommands(registry, eng, cfg.Version, started, win.status); err != nil {
		return err
	}
	if err := registerBackupCommands(registry, eng, cfg.Backups, secrets.Apply,
		func(st stacks.Stack) { registerStackSecrets(secrets, st) }, log); err != nil {
		return err
	}
	if err := registerSecretCommands(registry, eng, repo.engineSource, secrets, log); err != nil {
		return err
	}
	dock := cfg.Docker
	if dock == nil {
		socket := cfg.DockerSocket
		if socket == "" {
			socket = DefaultDockerSocket
		}
		client, err := docker.New(docker.Options{Socket: socket})
		if err != nil {
			return fmt.Errorf("prepare the container engine client: %w", err)
		}
		defer client.Close()
		dock = client
	}
	if err := registerContainerCommands(registry, repo, dock, log); err != nil {
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
	windowDone := make(chan struct{})
	go func() {
		defer close(windowDone)
		win.sched.Run(ctx)
	}()
	defer func() { <-windowDone }()
	check := cfg.PeerCheck
	if check == nil {
		check = RequireRoot
	}
	log.Info("service started")
	serve(ctx, l, registry, check, log)
	log.Info("service stopped")
	return nil
}
