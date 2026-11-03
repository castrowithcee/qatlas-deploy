package cli

import (
	"context"
	"errors"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// ExitServiceNotRunning is the exit code of a command that found no running service. The kit's exit contract
// knows only success, runtime failure and usage, so Run maps this one case on top of it.
const ExitServiceNotRunning = 3

// serviceEndpoint names the channel the commands talk to; tests replace it.
var serviceEndpoint = control.Endpoint

// notRunning records that the command failed because no service listens.
type notRunning struct{ seen bool }

func newStatusCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether the qdeploy service is running",
		Long: "status asks the running service for its version and start time. Only root may use the service\n" +
			"channel, so run it with sudo. Without a running service it ends with exit code 3.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var st control.StatusResult
			if err := call(c.Context(), serviceEndpoint(), control.StatusCommand, &st); err != nil {
				if errors.Is(err, client.ErrNotRunning) {
					state.seen = true
				}
				return failureOf(err)
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
				{Name: "running", Value: true},
				{Name: "version", Value: st.Version},
				{Name: "started_at", Value: st.StartedAt.UTC().Format(time.RFC3339)},
			}})
		},
	}
}

func call(ctx context.Context, ep localipc.ServiceEndpoint, command string, result any) error {
	ctx, cancel := context.WithTimeout(ctx, client.CallTimeout)
	defer cancel()
	cl, err := client.Dial(ctx, ep)
	if err != nil {
		return err
	}
	defer cl.Close()
	return cl.Call(ctx, command, nil, result)
}

// failureOf turns a channel error into a diagnostic with a next step.
func failureOf(err error) error {
	var remote *control.Error
	switch {
	case errors.Is(err, client.ErrNotRunning):
		return &cli.Failure{Code: output.CodeUnreachable, Message: "the qdeploy service is not running",
			NextStep: "Start the service, then run the command again.", Cause: err}
	case errors.Is(err, client.ErrNoAccess), errors.As(err, &remote) && remote.Code == control.CodeNotRoot:
		return &cli.Failure{Code: output.CodePermission, Message: "only root may use the qdeploy service; run with sudo",
			NextStep: "Run the same command with sudo.", Cause: err}
	case errors.As(err, &remote):
		return &cli.Failure{Code: output.CodeRuntime, Message: "the service refused the request: " + remote.Message,
			NextStep: "Check that the client and the service are the same version.", Cause: err}
	}
	return &cli.Failure{Code: output.CodeRuntime, Message: "cannot talk to the service: " + err.Error(),
		NextStep: "Check that the service is healthy and run the command again.", Cause: err}
}
