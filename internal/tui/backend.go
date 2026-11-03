// Package tui is the terminal interface of qdeploy. It acts only through a Backend, which reaches the service
// over its channel; it never touches the stacks repository, Docker or the files of the service. It changes
// something only after the person confirmed it.
package tui

import (
	"context"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// Backend is what the interface asks of the service, one method per channel command. Calls run off the event
// loop and may block; a failure the service reports is a *control.Error.
type Backend interface {
	Status(ctx context.Context) (control.StatusResult, error)
	Stacks(ctx context.Context) (control.StacksResult, error)
	// Check checks one stack, or every stack for an empty name.
	Check(ctx context.Context, stack string) (control.StacksCheckResult, error)
	// Updates reads the stored update detection; args.Refresh asks the registries first.
	Updates(ctx context.Context, args control.UpdatesArgs) (control.UpdatesResult, error)
	// Runs lists the runs of one stack, or of every stack for an empty name, newest first.
	Runs(ctx context.Context, stack string) (control.RunsResult, error)
	Log(ctx context.Context, run string) (control.LogResult, error)
	Changelog(ctx context.Context, stack string) (control.ChangelogResult, error)
	// Backups lists the backups of one stack, newest first.
	Backups(ctx context.Context, stack string) (control.BackupListResult, error)
	Window(ctx context.Context) (control.WindowResult, error)

	// The calls below change something in the service; the interface makes them only after a confirmation.

	// Deploy starts a manual run of one stack and returns as soon as it is started.
	Deploy(ctx context.Context, stack string) (control.DeployResult, error)
	// Update moves the image versions of the stacks and returns when the last step is done.
	Update(ctx context.Context, stacks []string) (control.UpdateRunResult, error)
	Pause(ctx context.Context, stack string) (control.HoldResult, error)
	Unpause(ctx context.Context, stack string) (control.HoldResult, error)
	Pin(ctx context.Context, stack string) (control.HoldResult, error)
	Unpin(ctx context.Context, stack string) (control.HoldResult, error)
	// Resume clears the pause a failed rollback left on a stack.
	Resume(ctx context.Context, stack string) (control.ResumeResult, error)
}

// updateCallLimit bounds one update call, which waits for every step of the stack; it is the limit of the
// update command.
const updateCallLimit = 24 * time.Hour

// ChannelBackend is the Backend over the service channel. Every call dials a connection of its own, so calls
// may run concurrently.
type ChannelBackend struct {
	endpoint localipc.ServiceEndpoint
}

// NewBackend returns the Backend over the channel at ep.
func NewBackend(ep localipc.ServiceEndpoint) *ChannelBackend { return &ChannelBackend{endpoint: ep} }

func (b *ChannelBackend) call(ctx context.Context, command string, args, result any) error {
	return client.Do(ctx, b.endpoint, client.CallTimeout, command, args, result)
}

func (b *ChannelBackend) Status(ctx context.Context) (res control.StatusResult, err error) {
	err = b.call(ctx, control.StatusCommand, nil, &res)
	return res, err
}

func (b *ChannelBackend) Stacks(ctx context.Context) (res control.StacksResult, err error) {
	err = b.call(ctx, control.StacksListCommand, nil, &res)
	return res, err
}

func (b *ChannelBackend) Check(ctx context.Context, stack string) (res control.StacksCheckResult, err error) {
	err = b.call(ctx, control.StacksCheckCommand, control.StacksCheckArgs{Stack: stack}, &res)
	return res, err
}

func (b *ChannelBackend) Updates(ctx context.Context, args control.UpdatesArgs) (res control.UpdatesResult, err error) {
	limit := client.CallTimeout
	if args.Refresh {
		limit += control.UpdatesRefreshTimeout
	}
	if args.Changelog {
		limit += control.ChangelogTimeout
	}
	err = client.Do(ctx, b.endpoint, limit, control.UpdatesListCommand, args, &res)
	return res, err
}

func (b *ChannelBackend) Runs(ctx context.Context, stack string) (res control.RunsResult, err error) {
	err = b.call(ctx, control.RunsCommand, control.RunsArgs{Stack: stack}, &res)
	return res, err
}

func (b *ChannelBackend) Log(ctx context.Context, run string) (res control.LogResult, err error) {
	err = b.call(ctx, control.LogCommand, control.LogArgs{ID: run}, &res)
	return res, err
}

func (b *ChannelBackend) Changelog(ctx context.Context, stack string) (res control.ChangelogResult, err error) {
	err = client.Do(ctx, b.endpoint, client.CallTimeout+control.ChangelogTimeout, control.ChangelogShowCommand,
		control.ChangelogArgs{Stack: stack}, &res)
	return res, err
}

func (b *ChannelBackend) Window(ctx context.Context) (res control.WindowResult, err error) {
	err = b.call(ctx, control.WindowShowCommand, nil, &res)
	return res, err
}

func (b *ChannelBackend) Backups(ctx context.Context, stack string) (res control.BackupListResult, err error) {
	err = b.call(ctx, control.BackupListCommand, control.BackupArgs{Stack: stack}, &res)
	return res, err
}

func (b *ChannelBackend) Deploy(ctx context.Context, stack string) (res control.DeployResult, err error) {
	err = b.call(ctx, control.DeployCommand, control.DeployArgs{Stack: stack}, &res)
	return res, err
}

func (b *ChannelBackend) Update(ctx context.Context, stacks []string) (res control.UpdateRunResult, err error) {
	err = client.Do(ctx, b.endpoint, updateCallLimit, control.UpdateRunCommand, control.UpdateRunArgs{Stacks: stacks}, &res)
	return res, err
}

func (b *ChannelBackend) hold(ctx context.Context, command, stack string) (res control.HoldResult, err error) {
	err = b.call(ctx, command, control.HoldArgs{Stack: stack}, &res)
	return res, err
}

func (b *ChannelBackend) Pause(ctx context.Context, stack string) (control.HoldResult, error) {
	return b.hold(ctx, control.PauseCommand, stack)
}

func (b *ChannelBackend) Unpause(ctx context.Context, stack string) (control.HoldResult, error) {
	return b.hold(ctx, control.UnpauseCommand, stack)
}

func (b *ChannelBackend) Pin(ctx context.Context, stack string) (control.HoldResult, error) {
	return b.hold(ctx, control.PinCommand, stack)
}

func (b *ChannelBackend) Unpin(ctx context.Context, stack string) (control.HoldResult, error) {
	return b.hold(ctx, control.UnpinCommand, stack)
}

func (b *ChannelBackend) Resume(ctx context.Context, stack string) (res control.ResumeResult, err error) {
	err = b.call(ctx, control.ResumeCommand, control.ResumeArgs{Stack: stack}, &res)
	return res, err
}
