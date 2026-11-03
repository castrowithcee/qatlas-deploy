// Package service holds the long-running loop of the qdeploy service.
package service

import (
	"context"
	"log/slog"
)

// Run blocks until ctx ends and then returns nil: a stop request is the normal way for the service to end.
func Run(ctx context.Context, log *slog.Logger) error {
	log.Info("service started")
	<-ctx.Done()
	log.Info("service stopped")
	return nil
}
