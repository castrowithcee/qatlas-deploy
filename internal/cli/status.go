package cli

import (
	"context"
	"errors"
	"strings"
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
		Long: "status asks the running service for its version and start time, the state of its last repository poll\n" +
			"and, per stack, what is deployed, pending or running, the last run result, a pause after a failed rollback, the operator's pause and pin, and the findings,\n" +
			"and whether the maintenance window is open and when it opens next (see 'qdeploy window'). Only root may use the service\n" +
			"channel, so run it with sudo. Without a running service it ends with exit code 3.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var st control.StatusResult
			if err := call(c.Context(), serviceEndpoint(), control.StatusCommand, nil, &st); err != nil {
				if errors.Is(err, client.ErrNotRunning) {
					state.seen = true
				}
				return failureOf(err)
			}
			fields := []output.Field{
				{Name: "running", Value: true},
				{Name: "version", Value: st.Version},
				{Name: "started_at", Value: st.StartedAt.UTC().Format(time.RFC3339)},
			}
			fields = append(fields, engineFields(st.Engine)...)
			if w := st.Window; w != nil {
				fields = append(fields, output.Field{Name: "window_open", Value: w.Open},
					output.Field{Name: "window_next_opening", Value: w.NextOpening.Format(time.RFC3339)})
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: fields})
		},
	}
}

// engineFields flattens the engine part into scalar fields, which every output format can carry.
func engineFields(e control.EngineStatus) []output.Field {
	var out []output.Field
	if !e.LastPoll.IsZero() {
		out = append(out, output.Field{Name: "last_poll", Value: e.LastPoll.UTC().Format(time.RFC3339)})
	}
	for _, f := range [][2]string{{"poll_error", e.PollError}, {"note", e.Note}, {"head", e.Head}, {"remote_head", e.RemoteHead}} {
		if f[1] != "" {
			out = append(out, output.Field{Name: f[0], Value: f[1]})
		}
	}
	for _, s := range e.Stacks {
		p := "stack." + s.Name + "."
		out = append(out, output.Field{Name: p + "deployed", Value: s.Deployed}, output.Field{Name: p + "pending", Value: s.Pending})
		if s.ActiveRun != "" {
			out = append(out, output.Field{Name: p + "active_run", Value: s.ActiveRun})
		}
		if s.LastRun != "" {
			out = append(out, output.Field{Name: p + "last_run", Value: s.LastRun})
		}
		if len(s.Findings) > 0 {
			parts := make([]string, 0, len(s.Findings))
			for _, f := range s.Findings {
				part := f.Kind
				if f.Detail != "" {
					part += ": " + f.Detail
				}
				parts = append(parts, part)
			}
			out = append(out, output.Field{Name: p + "findings", Value: strings.Join(parts, "; ")})
		}
		if s.Removed {
			out = append(out, output.Field{Name: p + "removed", Value: true})
		}
		if s.Paused != nil {
			out = append(out, output.Field{Name: p + "paused",
				Value: "since " + s.Paused.Since.UTC().Format(time.RFC3339) + ", run " + s.Paused.Run})
		}
		if s.OperatorPause != nil {
			out = append(out, output.Field{Name: p + "operator_pause", Value: "since " + s.OperatorPause.Since.UTC().Format(time.RFC3339)})
		}
		if s.Pinned != nil {
			out = append(out, output.Field{Name: p + "pinned", Value: "since " + s.Pinned.Since.UTC().Format(time.RFC3339)})
		}
		if s.Stopped != nil {
			out = append(out, output.Field{Name: p + "stopped", Value: "since " + s.Stopped.Since.UTC().Format(time.RFC3339)})
		}
	}
	return out
}

func call(ctx context.Context, ep localipc.ServiceEndpoint, command string, args, result any) error {
	return callFor(ctx, client.CallTimeout, ep, command, args, result)
}

// callFor is call with its own time limit, for commands that wait on a remote server.
func callFor(ctx context.Context, limit time.Duration, ep localipc.ServiceEndpoint, command string, args, result any) error {
	return client.Do(ctx, ep, limit, command, args, result)
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
	case errors.As(err, &remote) && remote.Code == control.CodeKeyExists:
		return &cli.Failure{Code: output.CodeConfirmationRequired, Message: remote.Message, Usage: true,
			NextStep: "Run 'qdeploy key generate --replace' to replace it; the old key stops working at once.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeInvalid:
		return &cli.Failure{Code: output.CodeInvalidRequest, Message: remote.Message, Usage: true,
			NextStep: "Fix the argument and run the command again.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeNotFound:
		return &cli.Failure{Code: output.CodeNotFound, Message: remote.Message,
			NextStep: "Create it first with 'qdeploy key generate' or 'qdeploy credential set <name>'.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeNoRepo:
		return &cli.Failure{Code: output.CodeNotFound, Message: remote.Message,
			NextStep: "Bind the stacks repository with 'qdeploy repo set <ssh-url>'.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeRepoBound:
		return &cli.Failure{Code: output.CodeInvalidRequest, Message: remote.Message, Usage: true,
			NextStep: "Run 'qdeploy repo set' with the URL and directory already bound, or remove the service's stacks-repo.json while the service is stopped.",
			Cause:    err}
	case errors.As(err, &remote) && remote.Code == control.CodeHostKey:
		return &cli.Failure{Code: output.CodeAuth, Message: remote.Message,
			NextStep: "Verify the new host key with the git platform's operator before changing anything; the pinned key is never replaced by this command.",
			Cause:    err}
	case errors.As(err, &remote) && remote.Code == control.CodeRemote:
		return &cli.Failure{Code: output.CodeUnreachable, Message: remote.Message,
			NextStep: "Check the URL, the network and that the deploy key is entered in the git platform ('qdeploy key show').", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeRunActive:
		return &cli.Failure{Code: output.CodeRuntime, Message: remote.Message,
			NextStep: "Wait for the active run to end ('qdeploy stack runs'), then run the command again.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodePaused:
		return &cli.Failure{Code: output.CodeRuntime, Message: remote.Message,
			NextStep: "Find out why the rollback failed ('qdeploy stack runs <stack>', 'qdeploy stack log <run>'), fix the stack, then run 'qdeploy stack resume <stack>'.",
			Cause:    err}
	case errors.As(err, &remote) && remote.Code == control.CodePinned:
		return &cli.Failure{Code: output.CodeRuntime, Message: remote.Message,
			NextStep: "Unpin the stack with 'qdeploy stack unpin <stack>', then run the command again.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeStackStopped:
		return &cli.Failure{Code: output.CodeInvalidRequest, Message: remote.Message, Usage: true,
			NextStep: "Run 'qdeploy stack start <stack>' to start it and clear the mark, then run the command again.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeStackAction:
		return &cli.Failure{Code: output.CodeRuntime, Message: remote.Message,
			NextStep: "Check the containers with 'qdeploy container list' and the service log; a start does not create missing containers.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeNotPaused:
		return &cli.Failure{Code: output.CodeInvalidRequest, Message: remote.Message,
			NextStep: "Check the stack with 'qdeploy status'; only a paused stack can be resumed.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeLocalChanges:
		return &cli.Failure{Code: output.CodeRuntime, Message: remote.Message,
			NextStep: "Resolve the local changes in the working copy of the stack; the stack is skipped until then.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeNoSuchStack:
		return &cli.Failure{Code: output.CodeNotFound, Message: remote.Message,
			NextStep: "List the stacks with 'qdeploy stack list'.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeNoSuchRun:
		return &cli.Failure{Code: output.CodeNotFound, Message: remote.Message,
			NextStep: "List the runs with 'qdeploy stack runs'.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeUnreadable:
		return &cli.Failure{Code: output.CodeConfigInvalid, Message: remote.Message,
			NextStep: "Find the problem with 'qdeploy stack check', fix it in the stacks repository and run the command again.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeNoDeployer:
		return &cli.Failure{Code: output.CodeRuntime, Message: remote.Message,
			NextStep: "Stacks are only reported in this version; nothing can be deployed yet.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeConfirmation:
		return &cli.Failure{Code: output.CodeConfirmationRequired, Message: remote.Message, Usage: true,
			NextStep: "Confirm the action explicitly.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeNoBackups:
		return &cli.Failure{Code: output.CodeRuntime, Message: remote.Message,
			NextStep: "Update the service to a version that can back up stacks.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeNoBackupRules:
		return &cli.Failure{Code: output.CodeInvalidRequest, Message: remote.Message, Usage: true,
			NextStep: "Add a backup section to the stack settings in the qdeploy TUI or with 'qdeploy stack settings set' (see 'qdeploy help stack-settings').", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeNoSuchBackup:
		return &cli.Failure{Code: output.CodeNotFound, Message: remote.Message,
			NextStep: "List the backups with 'qdeploy stack backup list <stack>'.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeBackupFailed:
		return &cli.Failure{Code: output.CodeRuntime, Message: remote.Message,
			NextStep: "Check the service log, then check that the stack runs; a failed backup is removed and a stopped stack is started again.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeBadVersion:
		return &cli.Failure{Code: output.CodeInvalidRequest, Message: remote.Message, Usage: true,
			NextStep: "Choose a version of the same variant above the current one; 'qdeploy stack updates <stack> --refresh' shows them.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeRegistry:
		return &cli.Failure{Code: output.CodeUnreachable, Message: remote.Message,
			NextStep: "Check the network and the registry login (see 'qdeploy help credentials'), then run the command again.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeNotUpdatable:
		return &cli.Failure{Code: output.CodeRuntime, Message: remote.Message,
			NextStep: "Resolve the cause named in the message (see 'qdeploy status' and 'qdeploy help stack-settings'), then run the command again.", Cause: err}
	case errors.As(err, &remote) && remote.Code == control.CodeBusy:
		return &cli.Failure{Code: output.CodeRuntime, Message: remote.Message,
			NextStep: "Run the command again in a moment.", Cause: err}
	case errors.As(err, &remote):
		return &cli.Failure{Code: output.CodeRuntime, Message: "the service refused the request: " + remote.Message,
			NextStep: "Check that the client and the service are the same version.", Cause: err}
	}
	return &cli.Failure{Code: output.CodeRuntime, Message: "cannot talk to the service: " + err.Error(),
		NextStep: "Check that the service is healthy and run the command again.", Cause: err}
}
