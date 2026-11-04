package cli

import (
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-deploy/internal/backup"
	"github.com/castrowithcee/qatlas-deploy/internal/compose"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
)

// stateDir names the credential store of the service; tests replace it.
var stateDir = func() string { return service.DefaultStateDir }

func newRunCommand() *cobra.Command {
	var backupImage, dockerSocket string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the qdeploy service in the foreground",
		Long: "run is what the service unit starts: it runs until it receives SIGTERM or SIGINT, then ends with\n" +
			"exit code 0. It logs to stderr.",
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(c.Context(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()
			backups := backup.NewManager(filepath.Join(stateDir(), "backups"))
			backups.Image = backupImage
			return service.Run(ctx, slog.New(slog.NewTextHandler(c.ErrOrStderr(), nil)).With("pid", os.Getpid()),
				service.Config{Version: version, Endpoint: serviceEndpoint(), StateDir: stateDir(),
					Deployer: compose.New(backups.ForRuns()), Backups: backups, DockerSocket: dockerSocket})
		},
	}
	cmd.Flags().StringVar(&backupImage, "backup-image", backup.DefaultImage,
		"helper image of backups and restores; it needs a POSIX shell, find, rm and a tar that knows --numeric-owner")
	cmd.Flags().StringVar(&dockerSocket, "docker-socket", service.DefaultDockerSocket,
		"socket of the container engine the service reads containers from")
	return cmd
}
