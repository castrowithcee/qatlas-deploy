// Package control defines the wire protocol of the service channel: one JSON object per line, a request from
// the client and an answer from the service. It holds the frame, the fixed error codes, the limits, the
// command registry and the endpoint, and nothing that serves or calls.
package control

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

// Version is the protocol version every request and answer carries.
const Version = 1

// Endpoint returns the channel of the service. The service runs as the system user qdeploy, so the directory
// belongs to that user and its group and is closed to others; the service manager creates it. The transport
// admits root and members of that group; the service narrows this to UID 0 (see service.RequireRoot).
func Endpoint() localipc.ServiceEndpoint {
	return localipc.ServiceEndpoint{
		Dir: "/run/qdeploy", SocketBase: "control", ServiceUser: "qdeploy", Group: "qdeploy", ErrorName: "qdeploy service",
	}
}

// The size and time limits of a connection.
const (
	MaxRequestBytes  = 64 << 10
	MaxResponseBytes = 1 << 20

	// ReadTimeout bounds how long one request may take from its first byte to its end of line.
	ReadTimeout = 10 * time.Second
	// IdleTimeout bounds how long a connection may stay silent between requests.
	IdleTimeout = time.Minute
	// WriteTimeout bounds how long one answer may take to leave.
	WriteTimeout = 10 * time.Second
)

// The codes an answer's error carries. They are fixed words, never text built from a request, so an answer
// cannot echo what a peer sent.
const (
	CodeNotRoot        = "not-root"
	CodeBadRequest     = "bad-request"
	CodeVersion        = "version"
	CodeUnknownCommand = "unknown-command"
	CodeTooLarge       = "too-large"
	CodeBusy           = "busy"
	CodeInternal       = "internal"
	CodeInvalid        = "invalid-argument"
	CodeNotFound       = "not-found"
	CodeKeyExists      = "key-exists"
	CodeNoRepo         = "no-repo"
	CodeRepoBound      = "repo-bound"
	CodeHostKey        = "host-key"
	CodeRemote         = "remote-failed"
	CodeRunActive      = "run-active"
	CodeLocalChanges   = "local-changes"
	CodeNoSuchStack    = "no-such-stack"
	CodeNoSuchRun      = "no-such-run"
	CodeUnreadable     = "stack-unreadable"
	CodeNoDeployer     = "no-deployer"
	CodePaused         = "paused"
	CodeNotPaused      = "not-paused"

	CodeNoBackups     = "no-backups"
	CodeNoBackupRules = "no-backup-rules"
	CodeNoSuchBackup  = "no-such-backup"
	CodeConfirmation  = "confirmation-required"
	CodeBackupFailed  = "backup-failed"
)

// Request is what a client sends. Args is command specific and may be absent.
type Request struct {
	V       int             `json:"v"`
	Command string          `json:"command"`
	Args    json.RawMessage `json:"args,omitempty"`
}

// Response is what the service answers: a result on success, an error otherwise.
type Response struct {
	V      int             `json:"v"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Error is a failed answer: a fixed code and a message for a person.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

// NewError returns an error a command handler can return to answer with a code of its own.
func NewError(code, message string) *Error { return &Error{Code: code, Message: message} }

// ErrNotRoot is the answer to a peer that is not UID 0.
func ErrNotRoot() *Error {
	return NewError(CodeNotRoot, "only root may use the service; run the command with sudo")
}

// ErrTooLarge reports a frame beyond its limit.
var ErrTooLarge = errors.New("message exceeds the size limit")

// ReadFrame reads one line of at most max bytes, without its line end. An end of input before any byte is
// io.EOF; a frame beyond max is ErrTooLarge and leaves the stream unusable.
func ReadFrame(br *bufio.Reader, max int) ([]byte, error) {
	var frame []byte
	for {
		// Waiting for one byte first keeps the loop below from blocking on a line that is not complete.
		if _, err := br.Peek(1); err != nil {
			if errors.Is(err, io.EOF) {
				if len(frame) == 0 {
					return nil, io.EOF
				}
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		data, _ := br.Peek(br.Buffered())
		end := bytes.IndexByte(data, '\n')
		take := len(data)
		if end >= 0 {
			take = end
		}
		if len(frame)+take > max {
			return nil, ErrTooLarge
		}
		frame = append(frame, data[:take]...)
		if end >= 0 {
			_, _ = br.Discard(end + 1)
			return bytes.TrimSuffix(frame, []byte("\r")), nil
		}
		_, _ = br.Discard(take)
	}
}

// WriteFrame writes v as one line of at most max bytes.
func WriteFrame(w io.Writer, v any, max int) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	if len(raw) > max {
		return ErrTooLarge
	}
	_, err = w.Write(append(raw, '\n'))
	return err
}

// StatusCommand asks for the state of the running service.
const StatusCommand = "status"

// StatusResult answers StatusCommand.
type StatusResult struct {
	Version   string       `json:"version"`
	StartedAt time.Time    `json:"started_at"`
	Engine    EngineStatus `json:"engine"`
}

// EngineStatus is the engine's part of StatusResult. Stacks is empty while no repository is bound.
type EngineStatus struct {
	LastPoll   time.Time     `json:"last_poll,omitzero"`
	PollError  string        `json:"poll_error,omitempty"`
	Note       string        `json:"note,omitempty"`
	Head       string        `json:"head,omitempty"`
	RemoteHead string        `json:"remote_head,omitempty"`
	Stacks     []StackStatus `json:"stacks"`
}

// StackStatus is the engine's view of one stack. ActiveRun is the id of the run in progress, LastRun the
// result of the last finished one. Paused is set while the stack gets no runs after a failed rollback.
type StackStatus struct {
	Name      string        `json:"name"`
	Deployed  string        `json:"deployed,omitempty"`
	Pending   string        `json:"pending,omitempty"`
	ActiveRun string        `json:"active_run,omitempty"`
	LastRun   string        `json:"last_run,omitempty"`
	Findings  []FindingInfo `json:"findings"`
	Removed   bool          `json:"removed,omitempty"`
	Paused    *PauseInfo    `json:"paused,omitempty"`
}

// PauseInfo says since when a stack is paused, after which run and why.
type PauseInfo struct {
	Since  time.Time `json:"since"`
	Run    string    `json:"run"`
	Reason string    `json:"reason,omitempty"`
}

// FindingInfo is one finding of the engine about a stack.
type FindingInfo struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

// The commands that control runs. ResumeCommand clears the pause of a stack and starts no run.
const (
	DeployCommand = "deploy"
	RunsCommand   = "runs"
	LogCommand    = "log"
	ResumeCommand = "resume"
)

// ResumeArgs are the arguments of ResumeCommand.
type ResumeArgs struct {
	Stack string `json:"stack"`
}

// ResumeResult answers ResumeCommand with the pause that was cleared.
type ResumeResult struct {
	Stack string    `json:"stack"`
	Pause PauseInfo `json:"pause"`
}

// DeployArgs are the arguments of DeployCommand.
type DeployArgs struct {
	Stack string `json:"stack"`
}

// DeployResult answers DeployCommand with the run that was started.
type DeployResult struct {
	ID     string `json:"id"`
	Stack  string `json:"stack"`
	Commit string `json:"commit"`
}

// RunsArgs are the arguments of RunsCommand; an empty Stack means all stacks.
type RunsArgs struct {
	Stack string `json:"stack,omitempty"`
}

// RunInfo describes one recorded run. DurationMS is in milliseconds.
type RunInfo struct {
	ID         string    `json:"id"`
	Stack      string    `json:"stack"`
	Commit     string    `json:"commit"`
	Trigger    string    `json:"trigger"`
	Started    time.Time `json:"started"`
	DurationMS int64     `json:"duration_ms"`
	Result     string    `json:"result"`
	Error      string    `json:"error,omitempty"`
}

// RunsResult answers RunsCommand, newest run first.
type RunsResult struct {
	Runs []RunInfo `json:"runs"`
}

// LogArgs are the arguments of LogCommand.
type LogArgs struct {
	ID string `json:"id"`
}

// LogResult answers LogCommand.
type LogResult struct {
	Text string `json:"text"`
}

// The commands that manage the deploy key and the named credentials. The private key and stored values never
// travel back over the channel.
const (
	KeyGenerateCommand      = "key.generate"
	KeyShowCommand          = "key.show"
	CredentialSetCommand    = "credential.set"
	CredentialListCommand   = "credential.list"
	CredentialDeleteCommand = "credential.delete"
)

// KeyGenerateArgs are the arguments of KeyGenerateCommand. An existing key is replaced only if Replace is set.
type KeyGenerateArgs struct {
	Replace bool `json:"replace"`
}

// KeyResult answers KeyGenerateCommand and KeyShowCommand with the public part of the deploy key.
type KeyResult struct {
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
}

// CredentialSetArgs are the arguments of CredentialSetCommand.
type CredentialSetArgs struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CredentialNameArgs are the arguments of CredentialDeleteCommand.
type CredentialNameArgs struct {
	Name string `json:"name"`
}

// CredentialInfo describes one stored credential; it never carries the value.
type CredentialInfo struct {
	Name     string    `json:"name"`
	Modified time.Time `json:"modified"`
}

// CredentialListResult answers CredentialListCommand.
type CredentialListResult struct {
	Credentials []CredentialInfo `json:"credentials"`
}

// The commands that bind and read the stacks repository.
const (
	RepoProbeCommand  = "repo.probe"
	RepoSetCommand    = "repo.set"
	RepoFetchCommand  = "repo.fetch"
	StacksListCommand = "stacks.list"
	// StacksCheckCommand compares the stacks of the working copy with the standard.
	StacksCheckCommand = "stacks.check"
)

// RepoProbeArgs are the arguments of RepoProbeCommand.
type RepoProbeArgs struct {
	URL string `json:"url"`
}

// RepoProbeResult answers RepoProbeCommand with the host key the server presents. Pinned says the service
// already trusts exactly this key for the host.
type RepoProbeResult struct {
	Host        string `json:"host"`
	Port        string `json:"port"`
	KeyType     string `json:"key_type"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
	Pinned      bool   `json:"pinned"`
}

// RepoSetArgs are the arguments of RepoSetCommand. Fingerprint is the host key the operator confirmed.
type RepoSetArgs struct {
	URL         string `json:"url"`
	Dir         string `json:"dir"`
	Fingerprint string `json:"fingerprint"`
}

// RepoSetResult answers RepoSetCommand.
type RepoSetResult struct {
	URL         string `json:"url"`
	Dir         string `json:"dir"`
	Fingerprint string `json:"fingerprint"`
	Head        string `json:"head"`
	Cloned      bool   `json:"cloned"`
}

// RepoFetchResult answers RepoFetchCommand. UpdatesAvailable says the fetched remote branch differs from the
// working copy's HEAD; the working copy itself is not touched.
type RepoFetchResult struct {
	Dir              string `json:"dir"`
	Head             string `json:"head"`
	RemoteHead       string `json:"remote_head"`
	UpdatesAvailable bool   `json:"updates_available"`
}

// StacksResult answers StacksListCommand.
type StacksResult struct {
	Dir    string      `json:"dir"`
	Stacks []StackInfo `json:"stacks"`
}

// StackInfo describes one stack of the working copy. Error is set when the stack cannot be read.
type StackInfo struct {
	Name     string      `json:"name"`
	Update   string      `json:"update"`
	Images   []ImageInfo `json:"images"`
	Findings int         `json:"findings"`
	Error    string      `json:"error,omitempty"`
}

// ImageInfo is a service image whose tag is a variable; Version is empty when no value is set.
type ImageInfo struct {
	Service string `json:"service"`
	Image   string `json:"image"`
	Var     string `json:"var"`
	Version string `json:"version"`
}

// StacksCheckArgs are the arguments of StacksCheckCommand. An empty Stack checks every stack.
type StacksCheckArgs struct {
	Stack string `json:"stack,omitempty"`
}

// StacksCheckResult answers StacksCheckCommand. Stacks names every stack that was checked, so a stack without
// findings is visible; Findings is sorted by stack, severity and rule.
type StacksCheckResult struct {
	Dir      string         `json:"dir"`
	Stacks   []string       `json:"stacks"`
	Findings []CheckFinding `json:"findings"`
	Errors   int            `json:"errors"`
	Warnings int            `json:"warnings"`
}

// CheckFinding is one deviation from the standard. Severity is "error" or "warning"; Rule is a stable word.
type CheckFinding struct {
	Stack    string `json:"stack"`
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Cause    string `json:"cause"`
	Fix      string `json:"fix"`
}

// The commands that save and restore the data of a stack.
const (
	BackupCreateCommand  = "backup.create"
	BackupListCommand    = "backup.list"
	BackupRestoreCommand = "backup.restore"
)

// BackupArgs are the arguments of BackupCreateCommand and BackupListCommand.
type BackupArgs struct {
	Stack string `json:"stack"`
}

// BackupRestoreArgs are the arguments of BackupRestoreCommand. Confirm must be true: a restore overwrites the
// current data, so a call without it is refused.
type BackupRestoreArgs struct {
	Stack   string `json:"stack"`
	ID      string `json:"id"`
	Confirm bool   `json:"confirm"`
}

// BackupItemInfo is one part of a backup: a data directory, a volume or the command output.
type BackupItemInfo struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	Size int64  `json:"size"`
}

// BackupInfo describes one backup. Incomplete marks a directory of the store without valid metadata.
type BackupInfo struct {
	ID         string           `json:"id"`
	Stack      string           `json:"stack"`
	Created    time.Time        `json:"created,omitzero"`
	Commit     string           `json:"commit,omitempty"`
	Size       int64            `json:"size"`
	Items      []BackupItemInfo `json:"items"`
	Incomplete bool             `json:"incomplete,omitempty"`
}

// BackupCreateResult answers BackupCreateCommand; Removed lists the ids retention deleted.
type BackupCreateResult struct {
	Backup  BackupInfo `json:"backup"`
	Removed []string   `json:"removed"`
}

// BackupListResult answers BackupListCommand, newest backup first.
type BackupListResult struct {
	Stack   string       `json:"stack"`
	Backups []BackupInfo `json:"backups"`
}

// BackupRestoreResult answers BackupRestoreCommand with the backup that was restored.
type BackupRestoreResult struct {
	Backup BackupInfo `json:"backup"`
}
