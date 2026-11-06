// Package control defines the wire protocol of the service channel: one JSON object per line, a request from
// the client and an answer from the service. It holds the frame, the fixed error codes, the limits, the
// command registry and the endpoint, and nothing that serves or calls.
package control

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
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
	CodePinned         = "pinned"
	CodeStackStopped   = "stack-stopped"
	CodeStackAction    = "stack-action-failed"
	CodeNameCollision  = "name-collision"

	CodeNoBackups     = "no-backups"
	CodeNoBackupRules = "no-backup-rules"
	CodeNoSuchBackup  = "no-such-backup"
	CodeConfirmation  = "confirmation-required"
	CodeBackupFailed  = "backup-failed"

	CodeBadVersion   = "bad-version"
	CodeRegistry     = "registry-failed"
	CodeNotUpdatable = "not-updatable"

	CodeChannelExists = "channel-exists"
	CodeNoSuchChannel = "no-such-channel"
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
	// Window is the maintenance window; nil when its setting cannot be read.
	Window *WindowResult `json:"window,omitempty"`
}

// EngineStatus is the engine's part of StatusResult. Sources has every stack folder in the order they were
// bound; LastPoll, PollError, Note, Head and RemoteHead repeat those of the first one. Stacks has the stacks
// of every source by name, and for one name by source; both are empty while no repository is bound.
type EngineStatus struct {
	LastPoll   time.Time      `json:"last_poll,omitzero"`
	PollError  string         `json:"poll_error,omitempty"`
	Note       string         `json:"note,omitempty"`
	Head       string         `json:"head,omitempty"`
	RemoteHead string         `json:"remote_head,omitempty"`
	Sources    []SourceStatus `json:"sources"`
	Stacks     []StackStatus  `json:"stacks"`
}

// SourceStatus is the state of one stack folder as of its last poll. PollError tells why the poll, its fetch
// included, could not complete; Note why the working copy was not brought up to date.
type SourceStatus struct {
	ID         string    `json:"id"`
	Path       string    `json:"path"`
	Kind       string    `json:"kind"`
	LastPoll   time.Time `json:"last_poll,omitzero"`
	PollError  string    `json:"poll_error,omitempty"`
	Note       string    `json:"note,omitempty"`
	Head       string    `json:"head,omitempty"`
	RemoteHead string    `json:"remote_head,omitempty"`
}

// FindingNameCollision is the finding of a stack whose name a stack of another source already uses; it is
// never deployed.
const FindingNameCollision = "name-collision"

// Primary reports whether stacks[i] stands for its name: when several sources list a name, the entry that is
// neither blocked by FindingNameCollision nor removed, else the first one not blocked, else the first.
func Primary(stacks []StackStatus, i int) bool {
	rank := func(s StackStatus) int {
		switch {
		case slices.ContainsFunc(s.Findings, func(f FindingInfo) bool { return f.Kind == FindingNameCollision }):
			return 0
		case s.Removed:
			return 1
		}
		return 2
	}
	best := i
	for j, s := range stacks {
		if s.Name == stacks[i].Name && (rank(s) > rank(stacks[best]) || rank(s) == rank(stacks[best]) && j < best) {
			best = j
		}
	}
	return best == i
}

// StackStatus is the engine's view of one stack. Source is the id of the stack folder it comes from.
// ActiveRun is the id of the run in progress, LastRun the result of the last finished one. Paused is set
// while the stack gets no runs after a failed rollback.
type StackStatus struct {
	Name      string        `json:"name"`
	Source    string        `json:"source"`
	Deployed  string        `json:"deployed,omitempty"`
	Pending   string        `json:"pending,omitempty"`
	ActiveRun string        `json:"active_run,omitempty"`
	LastRun   string        `json:"last_run,omitempty"`
	Findings  []FindingInfo `json:"findings"`
	Removed   bool          `json:"removed,omitempty"`
	Paused    *PauseInfo    `json:"paused,omitempty"`
	// OperatorPause and Pinned are the operator's marks for the maintenance window, unrelated to Paused.
	OperatorPause *HoldInfo `json:"operator_pause,omitempty"`
	Pinned        *HoldInfo `json:"pinned,omitempty"`
	// Stopped is set while the stack was stopped through qdeploy and gets no runs until it is started.
	Stopped *HoldInfo `json:"stopped,omitempty"`
	// Failure is the container failure the monitoring found; it is cleared when the monitoring finds it recovered.
	Failure *FailureInfo `json:"failure,omitempty"`
	// Repair is the state of the automatic repair of a failure; nil when there is none.
	Repair *RepairInfo `json:"repair,omitempty"`
}

// RepairInfo is the state of the automatic repair: the attempts made of Max, when the last was made and the
// next is due (zero when none is due), and since when the repair is halted after Max failed attempts.
type RepairInfo struct {
	Attempts int       `json:"attempts"`
	Max      int       `json:"max"`
	Last     time.Time `json:"last,omitzero"`
	Next     time.Time `json:"next,omitzero"`
	Halted   *HoldInfo `json:"halted,omitempty"`
}

// FailureInfo is a failure of a stack's containers: exited, unhealthy, restart-loop or missing, the container
// (the service for a missing one) and since when.
type FailureInfo struct {
	Kind      string    `json:"kind"`
	Container string    `json:"container,omitempty"`
	Since     time.Time `json:"since"`
}

// HoldInfo says since when an operator mark is set.
type HoldInfo struct {
	Since time.Time `json:"since"`
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
	// Dirs lists the missing data directories of a finding of kind "data-dir-missing".
	Dirs []DirInfo `json:"dirs,omitempty"`
}

// DirInfo is a missing data directory: Path relative to the stack folder, Owner the proposed "UID:GID",
// empty when unknown.
type DirInfo struct {
	Path  string `json:"path"`
	Owner string `json:"owner,omitempty"`
}

// The commands that control runs. ResumeCommand clears the pause of a stack and starts no run.
const (
	DeployCommand = "deploy"
	RunsCommand   = "runs"
	LogCommand    = "log"
	ResumeCommand = "resume"
)

// The commands that mark a stack for the maintenance window. They are idempotent: repeating one answers with
// Changed false. PauseCommand keeps the window from starting runs of the stack, PinCommand keeps it from
// changing the stack's versions. Both are unrelated to the pause ResumeCommand clears.
const (
	PauseCommand   = "pause"
	UnpauseCommand = "unpause"
	PinCommand     = "pin"
	UnpinCommand   = "unpin"
)

// HoldArgs are the arguments of the commands above.
type HoldArgs struct {
	Stack string `json:"stack"`
}

// HoldResult answers them. Active is the mark's state afterwards, Changed whether the command changed it.
type HoldResult struct {
	Stack   string `json:"stack"`
	Active  bool   `json:"active"`
	Changed bool   `json:"changed"`
}

// StackStartCommand, StackStopCommand and StackRestartCommand start, stop or restart the containers of an own
// stack under its run slot. A stop sets the stopped mark, only a start clears it, and a restart of a stopped
// stack is refused with CodeStackStopped. A failure of Compose answers CodeStackAction.
const (
	StackStartCommand   = "stack.start"
	StackStopCommand    = "stack.stop"
	StackRestartCommand = "stack.restart"
)

// StackActionTimeout is how long a client should wait for a stack action: the Compose call and the check.
const StackActionTimeout = 10 * time.Minute

// StackActionArgs are the arguments of the stack actions.
type StackActionArgs struct {
	Stack string `json:"stack"`
}

// StackActionResult answers them. Stopped is whether the stack carries the stopped mark afterwards.
type StackActionResult struct {
	Stack   string `json:"stack"`
	Action  string `json:"action"`
	Stopped bool   `json:"stopped"`
}

// StackSettingsGetCommand answers the settings of a stack with every default written out. StackSettingsSetCommand
// replaces them as a whole: the service checks them as it checks them when it reads a stack, commits them to
// the stacks repository, pushes the commit and moves the working copy to it, after waiting for runs like an
// update's commit. Settings that do not fit answer CodeInvalid and change nothing; settings equal to the
// present ones commit nothing.
const (
	StackSettingsGetCommand = "stack.settings.get"
	StackSettingsSetCommand = "stack.settings.set"
)

// StackSettingsTimeout is how long a client should wait for StackSettingsSetCommand: the wait for the
// working copy, then a fetch and a push, twice when the branch moved meanwhile.
const StackSettingsTimeout = 3 * time.Minute

// StackSettingsArgs are the arguments of StackSettingsGetCommand.
type StackSettingsArgs struct {
	Stack string `json:"stack"`
}

// StackSettingsSetArgs are the arguments of StackSettingsSetCommand.
type StackSettingsSetArgs struct {
	Stack    string        `json:"stack"`
	Settings StackSettings `json:"settings"`
}

// StackSettingsResult answers StackSettingsGetCommand. Settings is absent for a stack without settings, which
// is deployed but never updated.
type StackSettingsResult struct {
	Stack    string         `json:"stack"`
	Settings *StackSettings `json:"settings,omitempty"`
}

// StackSettingsSetResult answers StackSettingsSetCommand with the settings as stored. Commit is the pushed
// commit; it is empty when Changed is false.
type StackSettingsSetResult struct {
	Stack    string        `json:"stack"`
	Settings StackSettings `json:"settings"`
	Changed  bool          `json:"changed"`
	Commit   string        `json:"commit,omitempty"`
}

// StackSettings are the settings of a stack. Version must be 1. A member left out takes its default: update
// mode manual, ceiling minor, major stepwise; no backup; containers checked, no HTTP probe; the compose files
// Compose finds itself.
type StackSettings struct {
	Version int                   `json:"version"`
	Compose *StackComposeSettings `json:"compose,omitempty"`
	Update  *StackUpdateSettings  `json:"update,omitempty"`
	Backup  *StackBackupSettings  `json:"backup,omitempty"`
	Check   *StackCheckSettings   `json:"check,omitempty"`
}

// StackComposeSettings lists the compose files in merge order, relative to the stack folder. They must not
// change the project name of the stack.
type StackComposeSettings struct {
	Files []string `json:"files"`
}

// StackUpdateSettings say whether and how far the stack is updated. Mode is automatic or manual, Ceiling
// patch, minor or major, Major direct or stepwise. Tags maps an image version variable to a regular
// expression its candidate tags must match; Changelog maps it to a GitHub repository (owner/name).
type StackUpdateSettings struct {
	Mode      string            `json:"mode,omitempty"`
	Ceiling   string            `json:"ceiling,omitempty"`
	Major     string            `json:"major,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
	Changelog map[string]string `json:"changelog,omitempty"`
}

// StackBackupSettings say what is saved before an update: data directories relative to the stack folder,
// named volumes and a command as an argument vector. At least one of them is needed.
type StackBackupSettings struct {
	Paths     []string                `json:"paths,omitempty"`
	Volumes   []string                `json:"volumes,omitempty"`
	Command   []string                `json:"command,omitempty"`
	Retention *StackRetentionSettings `json:"retention,omitempty"`
}

// StackRetentionSettings keep a backup while it is among the newest Keep and younger than MaxAgeDays, for
// each limit that is set; Keep is 7 when neither is set.
type StackRetentionSettings struct {
	Keep       int `json:"keep,omitempty"`
	MaxAgeDays int `json:"max_age_days,omitempty"`
}

// StackCheckSettings say when a deployed stack counts as healthy.
type StackCheckSettings struct {
	Containers *bool              `json:"containers,omitempty"`
	HTTP       *StackHTTPSettings `json:"http,omitempty"`
}

// StackHTTPSettings are an HTTP probe: the URL must answer Status (200 by default) within Timeout, a Go
// duration from 1s to 10m (10s by default).
type StackHTTPSettings struct {
	URL     string `json:"url"`
	Status  int    `json:"status,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

// ResumeArgs are the arguments of ResumeCommand.
type ResumeArgs struct {
	Stack string `json:"stack"`
}

// ResumeResult answers ResumeCommand with the pause that was cleared, nil when only the halt of the repair was
// cleared.
type ResumeResult struct {
	Stack string     `json:"stack"`
	Pause *PauseInfo `json:"pause,omitempty"`
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
// Dir names the stack folder the key is for; empty means the default key.
type KeyGenerateArgs struct {
	Replace bool   `json:"replace"`
	Dir     string `json:"dir,omitempty"`
}

// KeyShowArgs are the arguments of KeyShowCommand; Dir as in KeyGenerateArgs.
type KeyShowArgs struct {
	Dir string `json:"dir,omitempty"`
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
	// StacksCheckCommand reports per stack of the working copy which functions work with its files, and why not.
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

// RepoFetchResult answers RepoFetchCommand. Sources has one entry per stack folder, in the order they were
// bound; Dir, Head, RemoteHead and UpdatesAvailable repeat the first entry. The command fails only when no
// source could be fetched.
type RepoFetchResult struct {
	Dir              string            `json:"dir"`
	Head             string            `json:"head"`
	RemoteHead       string            `json:"remote_head"`
	UpdatesAvailable bool              `json:"updates_available"`
	Sources          []RepoFetchSource `json:"sources"`
}

// RepoFetchSource is the fetch of one stack folder. UpdatesAvailable says the fetched remote branch differs
// from the working copy's HEAD; the working copy itself is not touched. Error says why the fetch failed.
type RepoFetchSource struct {
	ID               string `json:"id"`
	Dir              string `json:"dir"`
	Head             string `json:"head,omitempty"`
	RemoteHead       string `json:"remote_head,omitempty"`
	UpdatesAvailable bool   `json:"updates_available"`
	Error            string `json:"error,omitempty"`
}

// StacksResult answers StacksListCommand. Dir is the stack folder bound first; Stacks lists the stacks of
// every stack folder by name, and for one name by source.
type StacksResult struct {
	Dir    string      `json:"dir"`
	Stacks []StackInfo `json:"stacks"`
}

// StackDir returns the folder of the listed stack name as a path below its stack folder (SourceDir, else Dir),
// the stack folder itself for a root stack (Folder "."); false when the list does not hold the stack, only
// holds it with a Collision, or its folder is not a single plain name.
func (r StacksResult) StackDir(name string) (string, bool) {
	for _, s := range r.Stacks {
		if s.Name != name || s.Collision != "" {
			continue
		}
		root := cmp.Or(s.SourceDir, r.Dir)
		f := s.Folder
		if root == "" || f == "" || f == ".." || strings.ContainsAny(f, "/\\\x00") {
			return "", false
		}
		return filepath.Join(root, f), true
	}
	return "", false
}

// StackInfo describes one stack of the working copy. Error is set when the stack cannot be read.
type StackInfo struct {
	Name string `json:"name"`
	// Folder is the directory of the stack below Dir, "." for a root stack that is the whole stack folder; it
	// differs from Name when the Compose file names the project.
	Folder   string      `json:"folder"`
	Update   string      `json:"update"`
	Images   []ImageInfo `json:"images"`
	Findings int         `json:"findings"`
	Error    string      `json:"error,omitempty"`
	// Stopped is set while the stack was stopped through qdeploy.
	Stopped *HoldInfo `json:"stopped,omitempty"`
	// Source and SourceDir are the id and the path of the stack folder the stack comes from.
	Source    string `json:"source"`
	SourceDir string `json:"source_dir"`
	// Collision says why the stack is not deployed when another stack folder holds its name.
	Collision string `json:"collision,omitempty"`
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

// StacksCheckResult answers StacksCheckCommand with one report per checked stack, sorted by name. Errors and
// Warnings count the findings of all reports.
type StacksCheckResult struct {
	Dir      string        `json:"dir"`
	Stacks   []StackReport `json:"stacks"`
	Errors   int           `json:"errors"`
	Warnings int           `json:"warnings"`
}

// StackReport is the check of one stack: the functions in the order deploy, update, rollback, backup, redact,
// and the findings sorted by severity and rule.
type StackReport struct {
	Stack string `json:"stack"`
	// Source is the id of the stack folder the stack comes from.
	Source    string          `json:"source"`
	Functions []StackFunction `json:"functions"`
	Findings  []CheckFinding  `json:"findings"`
}

// StackFunction says whether one function works with the stack's files. Function is deploy, update, rollback
// (with data), backup or redact (the secrets in logs and messages); Mode is the update mode (automatic, manual
// or none) and set for update only. Action is what a person would change, empty when nothing is to do.
type StackFunction struct {
	Function string `json:"function"`
	Works    bool   `json:"works"`
	Mode     string `json:"mode,omitempty"`
	Reason   string `json:"reason"`
	Action   string `json:"action,omitempty"`
}

// CheckFinding is one problem of a stack's files. Severity is "error" when the stack cannot be deployed or a
// secret would reach a remote, else "warning"; Rule is a stable word.
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

// The commands that manage the secrets of a stack in its secrets.env. None answers with a value.
const (
	SecretSetCommand    = "secret.set"
	SecretListCommand   = "secret.list"
	SecretDeleteCommand = "secret.delete"
)

// SecretSetArgs are the arguments of SecretSetCommand. The value travels only in this request.
type SecretSetArgs struct {
	Stack string `json:"stack"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// SecretKeyArgs are the arguments of SecretDeleteCommand.
type SecretKeyArgs struct {
	Stack string `json:"stack"`
	Key   string `json:"key"`
}

// SecretListArgs are the arguments of SecretListCommand.
type SecretListArgs struct {
	Stack string `json:"stack"`
}

// SecretResult answers SecretSetCommand and SecretDeleteCommand. Replaced is true when set overwrote a key.
// Neither the run nor a redeploy is started: the new value applies at the next deployment.
type SecretResult struct {
	Stack    string `json:"stack"`
	Key      string `json:"key"`
	Replaced bool   `json:"replaced,omitempty"`
}

// SecretListResult answers SecretListCommand: the sorted key names, never values. A stack without a
// secrets.env has no keys.
type SecretListResult struct {
	Stack string   `json:"stack"`
	Keys  []string `json:"keys"`
}

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

// UpdatesListCommand shows the update detection results of the stacks; with Refresh it asks the registries
// first. Nothing in any stack, repository or deployment changes.
const UpdatesListCommand = "updates.list"

// UpdatesArgs are the arguments of UpdatesListCommand. An empty Stack means every stack.
type UpdatesArgs struct {
	Stack   string `json:"stack,omitempty"`
	Refresh bool   `json:"refresh,omitempty"`
	// Changelog adds the release notes of every available update to the result.
	Changelog bool `json:"changelog,omitempty"`
}

// UpdatesResult answers UpdatesListCommand.
type UpdatesResult struct {
	Stacks []StackUpdates `json:"stacks"`
}

// StackUpdates is the detection result of one stack. Checked is zero for a stack never checked. Error is set
// when the stack cannot be read.
type StackUpdates struct {
	Name    string       `json:"name"`
	Update  string       `json:"update"`
	Checked time.Time    `json:"checked,omitzero"`
	Items   []UpdateInfo `json:"items"`
	Error   string       `json:"error,omitempty"`
	// Changelogs holds one entry per item with an available update; it is set only when asked for.
	Changelogs []ChangelogInfo `json:"changelogs,omitempty"`
}

// UpdateInfo is the outcome for one image variable. Status is current, update, not-comparable or error;
// Selected is the version within the stack's ceiling (the current one if nothing is newer) and MajorAvailable
// a newer major version that is not taken automatically.
type UpdateInfo struct {
	Variable       string `json:"variable"`
	Image          string `json:"image"`
	Current        string `json:"current,omitempty"`
	Selected       string `json:"selected,omitempty"`
	MajorAvailable string `json:"major_available,omitempty"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
}

// UpdatesRefreshTimeout bounds how long the service spends asking registries for one refresh.
const UpdatesRefreshTimeout = 5 * time.Minute

// UpdateRunCommand updates the image versions of stacks, one stack after another: each version step is a
// commit of the stack's .env pushed to the branch and a run with backup, check and rollback. Every stack is
// checked and planned before anything changes; the answer comes when the last stack is done.
const UpdateRunCommand = "update.run"

// MaxUpdateStacks bounds the stacks of one UpdateRunCommand.
const MaxUpdateStacks = 256

// UpdateRunArgs are the arguments of UpdateRunCommand. Without Version each stack moves to what its rules
// allow; with it, the named Variable (or the stack's only one) moves to Version, also above the ceiling.
// Version needs exactly one stack.
type UpdateRunArgs struct {
	Stacks  []string `json:"stacks"`
	Version string   `json:"version,omitempty"`
	// Variable names the image version variable Version applies to; it needs Version.
	Variable string `json:"variable,omitempty"`
}

// UpdateRunResult answers UpdateRunCommand with one entry per stack, in the order requested.
type UpdateRunResult struct {
	Stacks []StackUpdateRun `json:"stacks"`
}

// StackUpdateRun is what happened to one stack. Steps is empty when nothing was newer; Error says why the
// stack was refused before its first step.
type StackUpdateRun struct {
	Name  string           `json:"name"`
	Steps []UpdateStepInfo `json:"steps"`
	Error string           `json:"error,omitempty"`
}

// UpdateStepInfo is one version step. Result is a run result, empty for a step that was not attempted
// because an earlier one failed. Commit is the step's pushed commit, or the deployed one when the step
// failed before its push.
type UpdateStepInfo struct {
	Variable string `json:"variable"`
	From     string `json:"from"`
	To       string `json:"to"`
	Run      string `json:"run,omitempty"`
	Commit   string `json:"commit,omitempty"`
	Result   string `json:"result,omitempty"`
	Error    string `json:"error,omitempty"`
}

// ChangelogShowCommand shows the release notes between the current and the available version of every
// variable of one stack that has an update. It only reads the stored detection and asks GitHub; it never asks
// a registry for tags and changes nothing.
const ChangelogShowCommand = "changelog.show"

// ChangelogArgs are the arguments of ChangelogShowCommand.
type ChangelogArgs struct {
	Stack string `json:"stack"`
}

// ChangelogResult answers ChangelogShowCommand. Checked is zero for a stack never checked.
type ChangelogResult struct {
	Stack   string          `json:"stack"`
	Checked time.Time       `json:"checked,omitzero"`
	Items   []ChangelogInfo `json:"items"`
}

// ChangelogInfo is the release notes of one image variable with an available update. Source is the GitHub
// repository as owner/name, empty when none is known. Link is the page to read instead of or besides the
// releases. Reason is a fixed short text that says why Releases is empty.
type ChangelogInfo struct {
	Variable string             `json:"variable"`
	Current  string             `json:"current"`
	Target   string             `json:"target"`
	Source   string             `json:"source,omitempty"`
	Link     string             `json:"link,omitempty"`
	Reason   string             `json:"reason,omitempty"`
	Releases []ChangelogRelease `json:"releases"`
	// Omitted counts releases left out to keep the answer small.
	Omitted int `json:"omitted,omitempty"`
}

// ChangelogRelease is one release; the texts are untrusted plain text from GitHub.
type ChangelogRelease struct {
	Version   string    `json:"version"`
	Name      string    `json:"name,omitempty"`
	Published time.Time `json:"published,omitzero"`
	URL       string    `json:"url,omitempty"`
	Body      string    `json:"body,omitempty"`
}

// ChangelogTimeout bounds how long the service spends on the GitHub and label lookups of one request.
const ChangelogTimeout = 2 * time.Minute

// The commands of the maintenance window. WindowShowCommand answers the window and its openings;
// WindowSetCommand replaces the parts it names and answers the same. The running service follows a change at
// once.
const (
	WindowShowCommand = "window.show"
	WindowSetCommand  = "window.set"
)

// WindowSetArgs are the arguments of WindowSetCommand. A part left empty keeps its value; at least one must be
// set. Days are weekday names (mon or monday, in any case) or the single word daily; Start is HH:MM on the
// 24-hour clock; Duration is whole minutes such as 2h or 1h30m; Zone is local or an IANA time zone name.
type WindowSetArgs struct {
	Days     []string `json:"days,omitempty"`
	Start    string   `json:"start,omitempty"`
	Duration string   `json:"duration,omitempty"`
	Zone     string   `json:"zone,omitempty"`
}

// WindowResult is the maintenance window and its openings, with times in the window's zone. OpenUntil is set
// while the window is open; NextOpening is the opening after the current one then. LastRun is when the work of
// an opening last began, Running whether it is in progress.
type WindowResult struct {
	Days        []string  `json:"days"`
	Start       string    `json:"start"`
	Duration    string    `json:"duration"`
	Zone        string    `json:"zone"`
	Open        bool      `json:"open"`
	OpenUntil   time.Time `json:"open_until,omitzero"`
	NextOpening time.Time `json:"next_opening"`
	LastRun     time.Time `json:"last_run,omitzero"`
	Running     bool      `json:"running,omitempty"`
}

// The commands of the notification channels. A channel receives the events it selected; its secret parts live
// in the credential store and never travel back over the channel. NotifyTestCommand sends one test message
// and answers with the error code of the notification kit when the delivery fails.
const (
	NotifyListCommand   = "notify.list"
	NotifyAddCommand    = "notify.add"
	NotifyRemoveCommand = "notify.remove"
	NotifyTestCommand   = "notify.test"
)

// The types of a notification channel.
const (
	NotifyWebhook  = "webhook"
	NotifySlack    = "slack"
	NotifyTelegram = "telegram"
	NotifySMTP     = "smtp"
)

// The events a channel can select.
const (
	NotifyEventSucceeded       = "succeeded"
	NotifyEventFailed          = "failed"
	NotifyEventRolledBack      = "rolled-back"
	NotifyEventRollbackFailed  = "rollback-failed"
	NotifyEventContainerFailed = "container-failed"
	NotifyEventRecovered       = "recovered"
	NotifyEventRecoveryFailed  = "recovery-failed"
	NotifyEventUpdateAvailable = "update-available"
)

// NotifyTestTimeout bounds how long the service spends on one test message.
const NotifyTestTimeout = 30 * time.Second

// NotifyAddArgs are the arguments of NotifyAddCommand. Name is 1 to 32 characters: lowercase letters, digits
// and '-', starting with a letter. Events lists the events the channel receives; none means all. Secret is the
// part that grants the right to send: the URL of a webhook or of a Slack incoming webhook, the token of a
// Telegram bot, or the password of an SMTP account (required with User, otherwise absent). Key is the signing
// key of a webhook and required there. The other fields belong to one type each: Host, Port (0 for the default
// of the security), Security ("starttls", the default, or "tls"), User, From and To to smtp; ChatID and
// BaseURL (optional) to telegram. A field of another type is refused.
type NotifyAddArgs struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Events   []string `json:"events,omitempty"`
	Secret   string   `json:"secret,omitempty"`
	Key      string   `json:"key,omitempty"`
	Host     string   `json:"host,omitempty"`
	Port     int      `json:"port,omitempty"`
	Security string   `json:"security,omitempty"`
	User     string   `json:"user,omitempty"`
	From     string   `json:"from,omitempty"`
	To       []string `json:"to,omitempty"`
	ChatID   string   `json:"chat_id,omitempty"`
	BaseURL  string   `json:"base_url,omitempty"`
}

// NotifyNameArgs are the arguments of NotifyRemoveCommand and NotifyTestCommand.
type NotifyNameArgs struct {
	Name string `json:"name"`
}

// NotifyChannelInfo describes one channel without any secret part.
type NotifyChannelInfo struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Events   []string `json:"events"`
	Host     string   `json:"host,omitempty"`
	Port     int      `json:"port,omitempty"`
	Security string   `json:"security,omitempty"`
	User     string   `json:"user,omitempty"`
	From     string   `json:"from,omitempty"`
	To       []string `json:"to,omitempty"`
	ChatID   string   `json:"chat_id,omitempty"`
	BaseURL  string   `json:"base_url,omitempty"`
}

// NotifyListResult answers NotifyListCommand, the channels in name order. NotifyAddCommand answers with the
// channel it stored.
type NotifyListResult struct {
	Channels []NotifyChannelInfo `json:"channels"`
}

// NotifyTestResult answers a delivered test message.
type NotifyTestResult struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ContainerListCommand shows every container of the host, grouped into stacks of this host, foreign Compose
// projects and single containers. ContainerGetCommand shows the details of one container. Both only read.
const (
	ContainerListCommand = "container.list"
	ContainerGetCommand  = "container.get"
)

// ContainerStartCommand, ContainerStopCommand and ContainerRestartCommand act on one container by name or ID.
// A container of an own stack is acted on while the stack's run slot is held, so an active run of that stack is
// refused with CodeRunActive.
const (
	ContainerStartCommand   = "container.start"
	ContainerStopCommand    = "container.stop"
	ContainerRestartCommand = "container.restart"
)

// ContainerActionTimeout is how long a client should wait for a container action: the default grace period of
// a stop or restart plus the wait of a start or restart until the container is ready.
const ContainerActionTimeout = 3 * time.Minute

// ContainerActionArgs names the container. GraceSeconds is how long a stop or restart waits for the container
// to end before the engine kills it; zero means ContainerDefaultGraceSeconds, the largest value is one hour.
type ContainerActionArgs struct {
	Name         string `json:"name"`
	GraceSeconds int    `json:"grace_seconds,omitempty"`
}

// ContainerDefaultGraceSeconds is the grace period of a stop or restart that names none.
const ContainerDefaultGraceSeconds = 10

// ContainerActionResult answers a container action with the container as it is afterwards. Kind is the kind of
// its group. After a start or restart the service waits for the container to run and, if it has a health
// check, to be healthy; NotReady then says why it was not at the end of the wait (exited, restarting,
// not-running, unhealthy or starting) and is empty if it was ready. A stop never waits and has no NotReady.
type ContainerActionResult struct {
	ContainerSummary
	Kind     string `json:"kind"`
	NotReady string `json:"not_ready,omitempty"`
}

// ContainerLogsCommand reads the log of one container (Name, by name or ID) or of all containers of an own
// stack (Stack), merged by time; exactly one of the two is set. Only text, never a stream: a client that
// follows asks again with the Cursor of the last answer. A stack that is not an own stack of the working copy
// is CodeNoSuchStack.
const ContainerLogsCommand = "container.logs"

// ContainerLogsTimeout is how long a client should wait for ContainerLogsCommand.
const ContainerLogsTimeout = 30 * time.Second

// ContainerLogsMaxTail is the largest Tail.
const ContainerLogsMaxTail = 5000

// ContainerLogsArgs are the arguments of ContainerLogsCommand. Since and Until bound the lines by their time
// (inclusive; zero means unbounded). Tail keeps only the last n lines of the stack or container (zero means
// all) and cannot be combined with Cursor. Cursor is the Cursor of an earlier answer: only lines from there on
// are returned, which is how a client continues without gaps or repeats.
type ContainerLogsArgs struct {
	Name   string    `json:"name,omitempty"`
	Stack  string    `json:"stack,omitempty"`
	Since  time.Time `json:"since,omitzero"`
	Until  time.Time `json:"until,omitzero"`
	Tail   int       `json:"tail,omitempty"`
	Cursor time.Time `json:"cursor,omitzero"`
}

// ContainerLogsResult answers ContainerLogsCommand with the lines in time order. An answer carries lines up to
// a byte budget well below MaxResponseBytes; More is set when further lines were left out, and Cursor is then
// the time to continue from. Cursor is the time of the last line plus one nanosecond and is zero when no line
// was ever returned. The budget never cuts through lines with the same time: they all come in one answer or
// all in the next, except a single such group larger than the budget, whose remainder is lost.
type ContainerLogsResult struct {
	Lines  []ContainerLogLine `json:"lines"`
	More   bool               `json:"more,omitempty"`
	Cursor time.Time          `json:"cursor,omitzero"`
}

// ContainerLogLine is one line. Source is the container name; Stream is stdout or stderr. Text is redacted and
// has control characters masked; Truncated marks a line that was longer than the service reads.
type ContainerLogLine struct {
	Time      time.Time `json:"time"`
	Source    string    `json:"source"`
	Stream    string    `json:"stream"`
	Text      string    `json:"text"`
	Truncated bool      `json:"truncated,omitempty"`
}

// The codes of a container engine that cannot be used; a missing container is CodeNotFound and an answer
// beyond MaxResponseBytes is CodeTooLarge.
const (
	CodeDockerUnreachable = "docker-unreachable"
	CodeDockerPermission  = "docker-permission"
	CodeDockerTimeout     = "docker-timeout"
)

// The kinds of a container group. A group is own when its Compose project is a stack of the bound repository
// and was started from that stack's folder, foreign when it is another Compose project, single when its
// containers belong to no project.
const (
	ContainerKindOwn     = "own"
	ContainerKindForeign = "foreign"
	ContainerKindSingle  = "single"
)

// The states of a container or group, worst first: unhealthy, restarting, stopped, starting, ok.
const (
	ContainerUnhealthy  = "unhealthy"
	ContainerRestarting = "restarting"
	ContainerStopped    = "stopped"
	ContainerStarting   = "starting"
	ContainerOK         = "ok"
)

// ContainerListTimeout is how long a client should wait for ContainerListCommand with metrics.
const ContainerListTimeout = 2 * time.Minute

// ContainerListArgs are the arguments of ContainerListCommand. Metrics adds CPU and memory readings, which
// take about two seconds per running container.
type ContainerListArgs struct {
	Metrics bool `json:"metrics,omitempty"`
}

// ContainerListResult answers ContainerListCommand: own groups first, then foreign ones, then single
// containers, each by name. MetricsIncomplete is set when metrics were asked for and a running container has
// none, because its reading failed or the time for the readings ran out.
type ContainerListResult struct {
	Groups            []ContainerGroup `json:"groups"`
	MetricsIncomplete bool             `json:"metrics_incomplete,omitempty"`
}

// ContainerGroup is one stack, Compose project or single container. Conflict marks a foreign project that has
// the name of a stack of this host but was started from another folder. State is the worst state of its
// containers; UptimeSeconds is the shortest one among running containers; Restarts is the sum. WorkingDir and
// ConfigFiles come from the Compose labels of a foreign project. Metrics sums the readings of the containers
// that have one.
type ContainerGroup struct {
	Kind          string             `json:"kind"`
	Name          string             `json:"name"`
	Conflict      bool               `json:"conflict,omitempty"`
	State         string             `json:"state"`
	UptimeSeconds int64              `json:"uptime_seconds"`
	Restarts      int                `json:"restarts"`
	WorkingDir    string             `json:"working_dir,omitempty"`
	ConfigFiles   []string           `json:"config_files,omitempty"`
	Metrics       *ContainerMetrics  `json:"metrics,omitempty"`
	Containers    []ContainerSummary `json:"containers"`
}

// ContainerSummary is one container in a group. EngineState is the word of the container engine, such as
// running or exited; Health is empty without a health check.
type ContainerSummary struct {
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	State         string            `json:"state"`
	EngineState   string            `json:"engine_state"`
	Health        string            `json:"health,omitempty"`
	UptimeSeconds int64             `json:"uptime_seconds"`
	Restarts      int               `json:"restarts"`
	Metrics       *ContainerMetrics `json:"metrics,omitempty"`
}

// ContainerMetrics is one reading. CPUPercent is 100 per fully used CPU; MemoryLimit is zero in a group's sum.
type ContainerMetrics struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryUsed  uint64  `json:"memory_used"`
	MemoryLimit uint64  `json:"memory_limit,omitempty"`
}

// ContainerGetArgs names a container by name or ID.
type ContainerGetArgs struct {
	Name string `json:"name"`
}

// ContainerDetail answers ContainerGetCommand. Project, Kind and Conflict classify the container like its
// group in ContainerListResult. EnvNames are the names of the environment variables; their values never
// leave the service.
type ContainerDetail struct {
	ContainerSummary
	ID       string             `json:"id"`
	Kind     string             `json:"kind"`
	Project  string             `json:"project,omitempty"`
	Conflict bool               `json:"conflict,omitempty"`
	Ports    []ContainerPort    `json:"ports"`
	Mounts   []ContainerMount   `json:"mounts"`
	Networks []ContainerNetwork `json:"networks"`
	Labels   map[string]string  `json:"labels"`
	EnvNames []string           `json:"env_names"`
}

// ContainerPort is a port of a container; PublicPort and HostIP are zero or empty if it is not published.
type ContainerPort struct {
	PrivatePort int    `json:"private_port"`
	PublicPort  int    `json:"public_port,omitempty"`
	Protocol    string `json:"protocol"`
	HostIP      string `json:"host_ip,omitempty"`
}

// ContainerMount is a mount of a container; Type is volume, bind, tmpfs or another type of the engine.
type ContainerMount struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"read_only,omitempty"`
}

// ContainerNetwork is a network a container is attached to.
type ContainerNetwork struct {
	Name      string `json:"name"`
	IPAddress string `json:"ip_address,omitempty"`
}
