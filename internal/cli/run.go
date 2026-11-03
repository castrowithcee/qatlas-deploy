package cli

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-deploy/internal/service"
)

func newRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the qdeploy service in the foreground",
		Long: "run is what the service unit starts: it runs until it receives SIGTERM or SIGINT, then ends with\n" +
			"exit code 0. It logs to stderr.",
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(c.Context(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()
			return service.Run(ctx, slog.New(slog.NewTextHandler(c.ErrOrStderr(), nil)).With("pid", os.Getpid()),
				service.Config{Version: version, Endpoint: serviceEndpoint()})
		},
	}
}
