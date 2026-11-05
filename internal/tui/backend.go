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
	// Secrets lists the key names of the secrets of one stack; never a value.
	Secrets(ctx context.Context, stack string) (control.SecretListResult, error)
	// Containers lists the containers of the host in groups, without readings of CPU and memory.
	Containers(ctx context.Context) (control.ContainerListResult, error)
	// Container reads one container by name; the result holds the names of its environment variables, never values.
	Container(ctx context.Context, name string) (control.ContainerDetail, error)
	// ContainerLogs reads a page of the output of one container or of all containers of an own stack.
	ContainerLogs(ctx context.Context, args control.ContainerLogsArgs) (control.ContainerLogsResult, error)

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
	// StackStart, StackStop and StackRestart act on the containers of a stack and return when the service is done.
	StackStart(ctx context.Context, stack string) (control.StackActionResult, error)
	StackStop(ctx context.Context, stack string) (control.StackActionResult, error)
	StackRestart(ctx context.Context, stack string) (control.StackActionResult, error)

	// The settings calls. Reads are free; the others the interface makes only after a confirmation. None of
	// the results carries a stored secret.

	// RepoProbe reads the host key the server of url presents; it stores nothing.
	RepoProbe(ctx context.Context, url string) (control.RepoProbeResult, error)
	RepoSet(ctx context.Context, args control.RepoSetArgs) (control.RepoSetResult, error)
	RepoFetch(ctx context.Context) (control.RepoFetchResult, error)
	KeyShow(ctx context.Context) (control.KeyResult, error)
	// KeyGenerate never replaces an existing key: the interface offers no replacement.
	KeyGenerate(ctx context.Context) (control.KeyResult, error)
	Credentials(ctx context.Context) (control.CredentialListResult, error)
	CredentialSet(ctx context.Context, name, value string) error
	CredentialDelete(ctx context.Context, name string) error
	WindowSet(ctx context.Context, args control.WindowSetArgs) (control.WindowResult, error)
	Notifications(ctx context.Context) (control.NotifyListResult, error)
	NotifyAdd(ctx context.Context, args control.NotifyAddArgs) (control.NotifyChannelInfo, error)
	NotifyRemove(ctx context.Context, name string) error
	NotifyTest(ctx context.Context, name string) (control.NotifyTestResult, error)

	// The stack-detail writes. The value of SecretSet travels only in this call.
	SecretSet(ctx context.Context, stack, key, value string) (control.SecretResult, error)
	SecretDelete(ctx context.Context, stack, key string) (control.SecretResult, error)
	// BackupRestore replaces the data of a stack with a backup and starts the stack again.
	BackupRestore(ctx context.Context, stack, id string) (control.BackupRestoreResult, error)
}

// updateCallLimit bounds one update call, which waits for every step of the stack; it is the limit of the
// update command.
const updateCallLimit = 24 * time.Hour

// remoteCallLimit bounds the calls that wait on the git server; it is the limit of the repo command.
const remoteCallLimit = 3 * time.Minute

// restoreCallLimit bounds a restore, which waits for the stack to stop, unpack and start again; it is the limit
// of the backup command.
const restoreCallLimit = 6*time.Hour + time.Minute

// containerCallLimit bounds the container reads. They are one request to the container engine and repeat every
// few seconds, so a call that hangs gives way early; the engine's own limit is longer and its answer, if any, is
// only seen when it comes sooner.
const containerCallLimit = 10 * time.Second

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

func (b *ChannelBackend) stackAction(ctx context.Context, command, stack string) (res control.StackActionResult, err error) {
	err = client.Do(ctx, b.endpoint, control.StackActionTimeout, command, control.StackActionArgs{Stack: stack}, &res)
	return res, err
}

func (b *ChannelBackend) StackStart(ctx context.Context, stack string) (control.StackActionResult, error) {
	return b.stackAction(ctx, control.StackStartCommand, stack)
}

func (b *ChannelBackend) StackStop(ctx context.Context, stack string) (control.StackActionResult, error) {
	return b.stackAction(ctx, control.StackStopCommand, stack)
}

func (b *ChannelBackend) StackRestart(ctx context.Context, stack string) (control.StackActionResult, error) {
	return b.stackAction(ctx, control.StackRestartCommand, stack)
}

func (b *ChannelBackend) RepoProbe(ctx context.Context, url string) (res control.RepoProbeResult, err error) {
	err = client.Do(ctx, b.endpoint, remoteCallLimit, control.RepoProbeCommand, control.RepoProbeArgs{URL: url}, &res)
	return res, err
}

func (b *ChannelBackend) RepoSet(ctx context.Context, args control.RepoSetArgs) (res control.RepoSetResult, err error) {
	err = client.Do(ctx, b.endpoint, remoteCallLimit, control.RepoSetCommand, args, &res)
	return res, err
}

func (b *ChannelBackend) RepoFetch(ctx context.Context) (res control.RepoFetchResult, err error) {
	err = client.Do(ctx, b.endpoint, remoteCallLimit, control.RepoFetchCommand, nil, &res)
	return res, err
}

func (b *ChannelBackend) KeyShow(ctx context.Context) (res control.KeyResult, err error) {
	err = b.call(ctx, control.KeyShowCommand, nil, &res)
	return res, err
}

func (b *ChannelBackend) KeyGenerate(ctx context.Context) (res control.KeyResult, err error) {
	err = b.call(ctx, control.KeyGenerateCommand, control.KeyGenerateArgs{}, &res)
	return res, err
}

func (b *ChannelBackend) Credentials(ctx context.Context) (res control.CredentialListResult, err error) {
	err = b.call(ctx, control.CredentialListCommand, nil, &res)
	return res, err
}

func (b *ChannelBackend) CredentialSet(ctx context.Context, name, value string) error {
	return b.call(ctx, control.CredentialSetCommand, control.CredentialSetArgs{Name: name, Value: value}, nil)
}

func (b *ChannelBackend) CredentialDelete(ctx context.Context, name string) error {
	return b.call(ctx, control.CredentialDeleteCommand, control.CredentialNameArgs{Name: name}, nil)
}

func (b *ChannelBackend) WindowSet(ctx context.Context, args control.WindowSetArgs) (res control.WindowResult, err error) {
	err = b.call(ctx, control.WindowSetCommand, args, &res)
	return res, err
}

func (b *ChannelBackend) Notifications(ctx context.Context) (res control.NotifyListResult, err error) {
	err = b.call(ctx, control.NotifyListCommand, nil, &res)
	return res, err
}

func (b *ChannelBackend) NotifyAdd(ctx context.Context, args control.NotifyAddArgs) (res control.NotifyChannelInfo, err error) {
	err = b.call(ctx, control.NotifyAddCommand, args, &res)
	return res, err
}

func (b *ChannelBackend) NotifyRemove(ctx context.Context, name string) error {
	return b.call(ctx, control.NotifyRemoveCommand, control.NotifyNameArgs{Name: name}, nil)
}

func (b *ChannelBackend) NotifyTest(ctx context.Context, name string) (res control.NotifyTestResult, err error) {
	err = client.Do(ctx, b.endpoint, client.CallTimeout+control.NotifyTestTimeout, control.NotifyTestCommand,
		control.NotifyNameArgs{Name: name}, &res)
	return res, err
}

func (b *ChannelBackend) Secrets(ctx context.Context, stack string) (res control.SecretListResult, err error) {
	err = b.call(ctx, control.SecretListCommand, control.SecretListArgs{Stack: stack}, &res)
	return res, err
}

func (b *ChannelBackend) Containers(ctx context.Context) (res control.ContainerListResult, err error) {
	err = client.Do(ctx, b.endpoint, containerCallLimit, control.ContainerListCommand, control.ContainerListArgs{}, &res)
	return res, err
}

func (b *ChannelBackend) Container(ctx context.Context, name string) (res control.ContainerDetail, err error) {
	err = client.Do(ctx, b.endpoint, containerCallLimit, control.ContainerGetCommand, control.ContainerGetArgs{Name: name}, &res)
	return res, err
}

func (b *ChannelBackend) ContainerLogs(ctx context.Context, args control.ContainerLogsArgs) (res control.ContainerLogsResult, err error) {
	err = client.Do(ctx, b.endpoint, control.ContainerLogsTimeout+client.CallTimeout, control.ContainerLogsCommand, args, &res)
	return res, err
}

func (b *ChannelBackend) SecretSet(ctx context.Context, stack, key, value string) (res control.SecretResult, err error) {
	err = b.call(ctx, control.SecretSetCommand, control.SecretSetArgs{Stack: stack, Key: key, Value: value}, &res)
	return res, err
}

func (b *ChannelBackend) SecretDelete(ctx context.Context, stack, key string) (res control.SecretResult, err error) {
	err = b.call(ctx, control.SecretDeleteCommand, control.SecretKeyArgs{Stack: stack, Key: key}, &res)
	return res, err
}

func (b *ChannelBackend) BackupRestore(ctx context.Context, stack, id string) (res control.BackupRestoreResult, err error) {
	err = client.Do(ctx, b.endpoint, restoreCallLimit, control.BackupRestoreCommand,
		control.BackupRestoreArgs{Stack: stack, ID: id, Confirm: true}, &res)
	return res, err
}

// Folders is the host side of binding a stack folder. It acts on the host, not through the service, because the
// service runs sandboxed and never creates a folder itself.
type Folders interface {
	// Missing reports whether the folder does not exist yet.
	Missing(dir string) (bool, error)
	// Grant creates the folder when create is set, which the person confirmed, and lets the service write to it.
	Grant(ctx context.Context, dir string, create bool) error
}
