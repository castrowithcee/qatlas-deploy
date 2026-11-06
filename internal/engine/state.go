package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const (
	stateSchema = 1
	// keepRuns bounds the run history, and with it the run logs, per stack.
	keepRuns = 50
)

// Results of a run.
const (
	ResultSucceeded = "succeeded"
	// ResultFailed is a run that changed nothing it could go back from: it failed before the stack was
	// stopped, or the stack had never been deployed.
	ResultFailed = "failed"
	// ResultRolledBack is a failed run whose rollback brought the stack back to its previous state.
	ResultRolledBack = "rolled-back"
	// ResultRollbackFailed is a failed run whose rollback failed too; the stack is stopped and paused.
	ResultRollbackFailed = "rollback-failed"
)

// What started a run.
const (
	TriggerPoll   = "poll"
	TriggerManual = "manual"
	// TriggerUpdate is a step of an update, see Engine.Update.
	TriggerUpdate = "update"
)

// Kinds of findings. The poll replaces the findings of a stack as a whole in every cycle.
const (
	FindingLocalChanges = "local-changes" // the stack is skipped
	FindingUnreadable   = "unreadable"    // the stack cannot be read and is skipped
	FindingFailedCommit = "failed-commit" // the last run of this commit failed; only a new commit or a manual run retries
	FindingDiff         = "diff-failed"   // the changes since the deployed commit cannot be determined
	FindingRemoved      = "removed"       // the folder is gone or unreadable; nothing is stopped
	FindingPaused       = "paused"        // a rollback failed; no run starts until the stack is resumed
	FindingStopped      = "stopped"       // the operator stopped the stack; no run starts until it is started
	// FindingDataDirMissing means bind-mount source directories are missing and the image runs as a non-root
	// user, so Docker would create them owned by root; no run starts until they exist.
	FindingDataDirMissing = "data-dir-missing"
	// FindingProjectRenamed is a deployed stack whose folder now has another project name, on both names: the
	// new name gets no automatic run, since the old project is not stopped; a manual run decides.
	FindingProjectRenamed = "project-renamed"
	// FindingNameCollision is a stack whose name a stack of another source already uses: the one deployed
	// first, or else the one of the source bound first, keeps the name; this one is never deployed.
	FindingNameCollision = "name-collision"
	// FindingContainerFailed is not stored with the findings of a poll; status derives it from StackState.Failure.
	FindingContainerFailed = "container-failed"
)

// Kinds of a container failure found between runs, see Failure.
const (
	FailureExited      = "exited"       // a container exited or died without a stop through qdeploy
	FailureUnhealthy   = "unhealthy"    // a container stayed unhealthy
	FailureRestartLoop = "restart-loop" // a container's restart count keeps rising
	FailureMissing     = "missing"      // a container of the deployed stack is gone
)

var (
	runIDRe    = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)
	commitRe   = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	maxNameLen = 255
	ownerRe    = regexp.MustCompile(`^(?:[0-9]{1,10}:[0-9]{1,10})?$`)
)

// maxDataDirs bounds the directories one run reports.
const maxDataDirs = 64

// State is everything the engine remembers. It holds commit ids, names and redacted texts, never a secret.
type State struct {
	Schema int `json:"schema"`
	// LastPoll is when a poll last finished; PollError tells why it could not complete, empty when it did.
	LastPoll  time.Time `json:"last_poll,omitzero"`
	PollError string    `json:"poll_error,omitempty"`
	// Head and RemoteHead are the working copy and the fetched branch as of the last poll.
	Head       string `json:"head,omitempty"`
	RemoteHead string `json:"remote_head,omitempty"`
	// Note says why the working copy was not brought up to date, empty otherwise.
	Note   string                `json:"note,omitempty"`
	Stacks map[string]StackState `json:"stacks,omitempty"`
	// Runs is the history, oldest first.
	Runs []RunRecord `json:"runs,omitempty"`
}

// StackState is what the engine knows about one stack.
type StackState struct {
	// Folder is the stack's folder in the repository, empty when it equals the name, as in every state written
	// before a stack could have a name of its own.
	Folder string `json:"folder,omitempty"`
	// Deployed is the commit of the last successful run; empty for a stack that was never deployed.
	Deployed string `json:"deployed,omitempty"`
	// Pending is the commit the stack waits to be deployed at; empty when it is up to date.
	Pending string `json:"pending,omitempty"`
	// FailedCommit is the commit the last run failed at, until a run succeeds.
	FailedCommit string       `json:"failed_commit,omitempty"`
	LastRun      *RunRecord   `json:"last_run,omitempty"`
	Findings     []Finding    `json:"findings,omitempty"`
	Dropped      *DroppedTrig `json:"dropped,omitempty"`
	// Removed is set while the stack's folder is missing from the repository.
	Removed bool `json:"removed,omitempty"`
	// Paused is set when a rollback failed; it is cleared only by Resume.
	Paused *Pause `json:"paused,omitempty"`
	// OperatorPause and Pin are the operator's marks, see Hold. They are independent of Paused and survive
	// runs, polls and restarts; only SetPause and SetPin change them.
	OperatorPause *Hold `json:"operator_pause,omitempty"`
	Pin           *Hold `json:"pin,omitempty"`
	// Stopped is set by a stop through qdeploy and cleared only by a start: polls, the window and manual
	// restarts leave the stack as it is.
	Stopped *Hold `json:"stopped,omitempty"`
	// DataDirs is set while the stack waits for missing data directories.
	DataDirs *DataDirWait `json:"data_dirs,omitempty"`
	// Updates is the result of the last update detection; nil for a stack that was never checked.
	Updates *UpdateCheck `json:"updates,omitempty"`
	// Failure is the open container failure the monitor found between runs; nil while the stack runs.
	Failure *Failure `json:"failure,omitempty"`
	// Services are the Compose services seen running healthy at the monitor's last good observation; a
	// service missing from the containers later is a failure. Empty until the first good observation after a run.
	Services []string `json:"services,omitempty"`
	// StoppedContainers are the IDs of containers stopped through qdeploy; the monitor does not report them
	// until they run again or are gone.
	StoppedContainers []string `json:"stopped_containers,omitempty"`
}

// MaxMonitorServices bounds Services.
const MaxMonitorServices = 256

// Failure is one failure of a stack's containers: what kind, which container (or service for a missing one)
// and since when. It lasts until the stack runs again.
type Failure struct {
	Kind      string    `json:"kind"`
	Container string    `json:"container,omitempty"`
	Since     time.Time `json:"since"`
}

// folder returns the folder of the stack stored under name.
func (s StackState) folder(name string) string {
	if s.Folder == "" {
		return name
	}
	return s.Folder
}

// folderField is what StackState.Folder holds for st.
func folderField(st stacks.Stack) string {
	if st.Folder == st.Name {
		return ""
	}
	return st.Folder
}

// Limits of a stored update check. The detector clips to them, so a valid result is never refused.
const (
	MaxUpdateItems = 256
	// MaxUpdateText bounds every text of an item in bytes.
	MaxUpdateText = 1024
)

// Statuses of an update item.
const (
	UpdateCurrent       = "current"
	UpdateAvailable     = "update"
	UpdateNotComparable = "not-comparable"
	UpdateError         = "error"
)

// UpdateCheck is the result of one update detection of a stack: when the registries were asked and what they
// said for each image variable. It holds versions and fixed texts, never registry answers or credentials.
type UpdateCheck struct {
	Checked time.Time    `json:"checked"`
	Items   []UpdateItem `json:"items,omitempty"`
}

// UpdateItem is the outcome for one image variable. Selected is the version within the stack's ceiling and
// equals Current when there is nothing newer; MajorAvailable is a newer major version that is never taken
// automatically. Error is a short fixed text for a status of UpdateNotComparable or UpdateError.
type UpdateItem struct {
	Variable       string `json:"variable"`
	Image          string `json:"image"`
	Current        string `json:"current,omitempty"`
	Selected       string `json:"selected,omitempty"`
	MajorAvailable string `json:"major_available,omitempty"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
}

// Pause records why a stack gets no runs: the run whose rollback failed and its redacted reason.
type Pause struct {
	Time   time.Time `json:"time"`
	Run    string    `json:"run"`
	Reason string    `json:"reason,omitempty"`
}

// Hold records an operator's mark on a stack and since when it is set.
type Hold struct {
	Since time.Time `json:"since"`
}

// Finding is a fact about a stack that needs a human or explains why the stack is skipped.
type Finding struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
	// Dirs is set for FindingDataDirMissing.
	Dirs []MissingDir `json:"dirs,omitempty"`
}

// MissingDir is a bind-mount source directory below the stack folder that does not exist. Path is clean,
// slash-separated and relative to the stack folder. Owner is the proposed "UID:GID" the image's user needs,
// empty when it could not be determined.
type MissingDir struct {
	Path  string `json:"path"`
	Owner string `json:"owner,omitempty"`
}

// DataDirWait records the commit a run found missing data directories at and which ones. The stack gets no
// run for that commit until they all exist; a different commit runs and checks again.
type DataDirWait struct {
	Commit string       `json:"commit"`
	Dirs   []MissingDir `json:"dirs"`
}

// DroppedTrig records the latest trigger that found a run of the stack active and was dropped.
type DroppedTrig struct {
	Time    time.Time `json:"time"`
	Trigger string    `json:"trigger"`
}

// RunRecord is one finished run.
type RunRecord struct {
	ID       string        `json:"id"`
	Stack    string        `json:"stack"`
	Commit   string        `json:"commit"`
	Trigger  string        `json:"trigger"`
	Started  time.Time     `json:"started"`
	Duration time.Duration `json:"duration"`
	Result   string        `json:"result"`
	// Error is the redacted reason of a run that did not succeed.
	Error string `json:"error,omitempty"`
	// Backup is the id of the backup the run made, empty when it made none.
	Backup string `json:"backup,omitempty"`
	// RolledBackTo is the commit a successful rollback left the stack at.
	RolledBackTo string `json:"rolled_back_to,omitempty"`
}

func validateState(s State) error {
	if s.Schema != stateSchema {
		return fmt.Errorf("unsupported schema %d", s.Schema)
	}
	for name, st := range s.Stacks {
		if name == "" || len(name) > maxNameLen {
			return errors.New("invalid stack name")
		}
		if len(st.Folder) > maxNameLen {
			return errors.New("invalid stack folder")
		}
		for _, c := range []string{st.Deployed, st.Pending, st.FailedCommit} {
			if c != "" && !commitRe.MatchString(c) {
				return errors.New("invalid commit id")
			}
		}
		if st.Paused != nil && !runIDRe.MatchString(st.Paused.Run) {
			return errors.New("invalid run id")
		}
		for _, h := range []*Hold{st.OperatorPause, st.Pin, st.Stopped} {
			if h != nil && h.Since.IsZero() {
				return errors.New("invalid hold")
			}
		}
		if w := st.DataDirs; w != nil {
			if err := validateDataDirs(*w); err != nil {
				return err
			}
		}
		if st.Updates != nil {
			if err := validateUpdates(*st.Updates); err != nil {
				return err
			}
		}
		if err := validateMonitor(st); err != nil {
			return err
		}
	}
	for _, r := range s.Runs {
		if !runIDRe.MatchString(r.ID) {
			return errors.New("invalid run id")
		}
		if r.RolledBackTo != "" && !commitRe.MatchString(r.RolledBackTo) {
			return errors.New("invalid commit id")
		}
	}
	return nil
}

func validateDataDirs(w DataDirWait) error {
	if !commitRe.MatchString(w.Commit) || len(w.Dirs) == 0 || len(w.Dirs) > maxDataDirs {
		return errors.New("invalid data directory wait")
	}
	for _, d := range w.Dirs {
		if !ValidDirPath(d.Path) || !ownerRe.MatchString(d.Owner) {
			return errors.New("invalid data directory")
		}
	}
	return nil
}

func validateMonitor(st StackState) error {
	if f := st.Failure; f != nil {
		switch f.Kind {
		case FailureExited, FailureUnhealthy, FailureRestartLoop, FailureMissing:
		default:
			return errors.New("invalid failure kind")
		}
		if f.Since.IsZero() || len(f.Container) > maxNameLen {
			return errors.New("invalid failure")
		}
	}
	if len(st.Services) > MaxMonitorServices {
		return errors.New("too many services")
	}
	for _, n := range st.Services {
		if n == "" || len(n) > maxNameLen {
			return errors.New("invalid service name")
		}
	}
	if len(st.StoppedContainers) > MaxMonitorServices {
		return errors.New("too many stopped containers")
	}
	for _, id := range st.StoppedContainers {
		if id == "" || len(id) > maxNameLen {
			return errors.New("invalid container id")
		}
	}
	return nil
}

// ValidDirPath accepts only a clean, slash-separated path below the stack folder.
func ValidDirPath(p string) bool {
	return p != "" && p != "." && len(p) <= 1024 && !strings.ContainsAny(p, "\\\x00\n") && path.Clean(p) == p &&
		!path.IsAbs(p) && p != ".." && !strings.HasPrefix(p, "../")
}

func validateUpdates(u UpdateCheck) error {
	if u.Checked.IsZero() || len(u.Items) > MaxUpdateItems {
		return errors.New("invalid update check")
	}
	for _, it := range u.Items {
		switch it.Status {
		case UpdateCurrent, UpdateAvailable, UpdateNotComparable, UpdateError:
		default:
			return errors.New("invalid update status")
		}
		if it.Variable == "" || it.Image == "" {
			return errors.New("invalid update item")
		}
		for _, t := range []string{it.Variable, it.Image, it.Current, it.Selected, it.MajorAvailable, it.Error} {
			if len(t) > MaxUpdateText {
				return errors.New("update text too long")
			}
		}
	}
	return nil
}

func decodeState(b []byte) (State, error) {
	var s State
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	err := dec.Decode(&s)
	return s, err
}

func encodeState(s State) ([]byte, error) { return json.MarshalIndent(s, "", "  ") }

// clone copies the maps and slices, so a returned snapshot never shares memory with engine state.
func (s State) clone() State {
	out := s
	out.Stacks = make(map[string]StackState, len(s.Stacks))
	for k, v := range s.Stacks {
		v.Findings = append([]Finding(nil), v.Findings...)
		if v.LastRun != nil {
			r := *v.LastRun
			v.LastRun = &r
		}
		if v.Dropped != nil {
			d := *v.Dropped
			v.Dropped = &d
		}
		if v.Paused != nil {
			p := *v.Paused
			v.Paused = &p
		}
		if v.DataDirs != nil {
			w := *v.DataDirs
			w.Dirs = append([]MissingDir(nil), w.Dirs...)
			v.DataDirs = &w
		}
		if v.OperatorPause != nil {
			h := *v.OperatorPause
			v.OperatorPause = &h
		}
		if v.Pin != nil {
			h := *v.Pin
			v.Pin = &h
		}
		if v.Stopped != nil {
			h := *v.Stopped
			v.Stopped = &h
		}
		if v.Updates != nil {
			u := *v.Updates
			u.Items = append([]UpdateItem(nil), u.Items...)
			v.Updates = &u
		}
		if v.Failure != nil {
			f := *v.Failure
			v.Failure = &f
		}
		v.Services = append([]string(nil), v.Services...)
		v.StoppedContainers = append([]string(nil), v.StoppedContainers...)
		out.Stacks[k] = v
	}
	out.Runs = append([]RunRecord(nil), s.Runs...)
	return out
}

// addRun appends the record and trims the history of its stack; the records dropped are returned.
func (s *State) addRun(r RunRecord) (dropped []RunRecord) {
	s.Runs = append(s.Runs, r)
	n := 0
	for _, x := range s.Runs {
		if x.Stack == r.Stack {
			n++
		}
	}
	kept := s.Runs[:0:0]
	for _, x := range s.Runs {
		if x.Stack == r.Stack && n > keepRuns {
			n--
			dropped = append(dropped, x)
			continue
		}
		kept = append(kept, x)
	}
	s.Runs = kept
	return dropped
}
