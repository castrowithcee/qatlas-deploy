package tui

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// snapshot is what one refresh read from the service.
type snapshot struct {
	status control.StatusResult
	stacks control.StacksResult
	// noRepo is set when no stacks repository is bound; check and updates are not asked then.
	noRepo  bool
	check   control.StacksCheckResult
	updates control.UpdatesResult
	runs    control.RunsResult
}

// refreshMsg carries a refresh: err is set when the service could not be asked at all, problem when a part
// of the snapshot is missing.
type refreshMsg struct {
	snap    snapshot
	err     error
	problem error
}

// fetch reads a snapshot. Only the status is required; a failure of a later part keeps the parts read.
func fetch(ctx context.Context, b Backend) refreshMsg {
	var msg refreshMsg
	var err error
	if msg.snap.status, err = b.Status(ctx); err != nil {
		return refreshMsg{err: err}
	}
	keep := func(err error) {
		if err != nil && msg.problem == nil {
			msg.problem = err
		}
	}
	var remote *control.Error
	msg.snap.stacks, err = b.Stacks(ctx)
	if errors.As(err, &remote) && remote.Code == control.CodeNoRepo {
		msg.snap.noRepo = true
	} else {
		keep(err)
	}
	if !msg.snap.noRepo {
		msg.snap.check, err = b.Check(ctx, "")
		keep(err)
		msg.snap.updates, err = b.Updates(ctx, control.UpdatesArgs{})
		keep(err)
	}
	msg.snap.runs, err = b.Runs(ctx, "")
	keep(err)
	return msg
}

// stackState is the one state a row shows, the most pressing first.
type stackState int

const (
	stateNotDeployed stackState = iota
	stateDeployed
	statePending
	stateFailed
	stateNeedsDirs
	statePinned
	stateContainerFailed
	stateRepairHalted
	statePaused
	stateStopped
	stateRemoved
	stateUnreadable
	stateRollbackPause
	stateRunning
)

// availability says what the update detection found for a stack.
type availability int

const (
	availNone availability = iota // no image version variable
	availNotChecked
	availUpToDate
	availUpdate
	availMajor
	availError
	availNotComparable
)

// stackRow is one stack as the Stacks area shows it, before translation.
type stackRow struct {
	name      string
	current   []string
	avail     availability
	available []string
	mode      string
	state     stackState
	// lastResult is the result of the last finished run, lastAt its start; zero when only the status knew it.
	lastResult string
	lastAt     time.Time
	errors     int
	warnings   int
}

// stateFindings are the engine findings the state column already shows; they are not counted again.
var stateFindings = []string{"removed", "paused", findingDataDirMissing, findingContainerFailed}

// failedResults are the run results after which a stack counts as failed.
var failedResults = []string{"failed", "rolled-back", "rollback-failed"}

// buildRows joins the parts of a snapshot into one row per stack, sorted by name. A stack is listed when the
// status or the working copy names it.
func buildRows(s snapshot) []stackRow {
	byName := map[string]*stackRow{}
	row := func(name string) *stackRow {
		if r, ok := byName[name]; ok {
			return r
		}
		r := &stackRow{name: name, avail: availNotChecked}
		byName[name] = r
		return r
	}
	unreadable := map[string]bool{}
	for i, st := range s.status.Engine.Stacks {
		r := row(st.Name)
		// Another source's entry of the name, such as one blocked by name-collision, adds only its findings.
		if control.Primary(s.status.Engine.Stacks, i) {
			r.state = stateOf(st)
			r.lastResult = st.LastRun
		}
		for _, f := range st.Findings {
			if !slices.Contains(stateFindings, f.Kind) {
				r.errors++
			}
		}
	}
	for _, si := range s.stacks.Stacks {
		if si.Collision != "" {
			continue
		}
		r := row(si.Name)
		r.mode = si.Update
		if si.Error != "" {
			unreadable[si.Name] = true
		}
		for _, img := range si.Images {
			r.current = appendDistinct(r.current, img.Version)
		}
		if len(si.Images) == 0 {
			r.avail = availNone
		}
	}
	for _, rep := range s.check.Stacks {
		if len(rep.Findings) == 0 {
			continue
		}
		r := row(rep.Stack)
		for _, f := range rep.Findings {
			if f.Severity == "error" {
				r.errors++
			} else {
				r.warnings++
			}
		}
	}
	for _, su := range s.updates.Stacks {
		r := row(su.Name)
		if r.mode == "" {
			r.mode = su.Update
		}
		if su.Error != "" {
			unreadable[su.Name] = true
		}
		applyUpdates(r, su)
	}
	seen := map[string]bool{}
	for _, run := range s.runs.Runs {
		if run.Result == "" || seen[run.Stack] {
			continue
		}
		seen[run.Stack] = true
		if r, ok := byName[run.Stack]; ok {
			r.lastResult, r.lastAt = run.Result, run.Started
		}
	}
	rows := make([]stackRow, 0, len(byName))
	for name, r := range byName {
		if unreadable[name] && r.state < stateUnreadable {
			r.state = stateUnreadable
		}
		rows = append(rows, *r)
	}
	slices.SortFunc(rows, func(a, b stackRow) int { return strings.Compare(a.name, b.name) })
	return rows
}

func stateOf(st control.StackStatus) stackState {
	switch {
	case st.ActiveRun != "":
		return stateRunning
	case st.Paused != nil:
		return stateRollbackPause
	case st.Removed:
		return stateRemoved
	case st.Stopped != nil:
		return stateStopped
	case st.OperatorPause != nil:
		return statePaused
	case st.Pinned != nil:
		return statePinned
	case st.Repair != nil && st.Repair.Halted != nil:
		return stateRepairHalted
	case st.Failure != nil:
		return stateContainerFailed
	case slices.ContainsFunc(st.Findings, func(f control.FindingInfo) bool { return f.Kind == findingDataDirMissing }):
		return stateNeedsDirs
	case slices.Contains(failedResults, st.LastRun):
		return stateFailed
	case st.Pending != "":
		return statePending
	case st.Deployed != "":
		return stateDeployed
	}
	return stateNotDeployed
}

// applyUpdates takes the versions of the update detection, which are newer than the working copy's when the
// stack was checked.
func applyUpdates(r *stackRow, su control.StackUpdates) {
	switch {
	case su.Error != "":
		return
	case su.Checked.IsZero():
		r.avail = availNotChecked
		return
	case len(su.Items) == 0:
		r.avail = availNone
		return
	}
	r.current = nil
	var updates, majors []string
	var failed, uncomparable bool
	for _, it := range su.Items {
		r.current = appendDistinct(r.current, it.Current)
		switch it.Status {
		case "update":
			updates = appendDistinct(updates, it.Selected)
		case "error":
			failed = true
		case "not-comparable":
			uncomparable = true
		}
		majors = appendDistinct(majors, it.MajorAvailable)
	}
	switch {
	case len(updates) > 0:
		r.avail, r.available = availUpdate, updates
	case len(majors) > 0:
		r.avail, r.available = availMajor, majors
	case failed:
		r.avail = availError
	case uncomparable:
		r.avail = availNotComparable
	default:
		r.avail = availUpToDate
	}
}

func appendDistinct(list []string, v string) []string {
	if v == "" || slices.Contains(list, v) {
		return list
	}
	return append(list, v)
}

// stackColumns are the columns of the Stacks area; tones colours a cell by its text.
func stackColumns(t kittui.Translator, tones map[string]kittui.TextStyle) []kittui.Column {
	short := func(pairs ...kittui.TextID) map[string]string {
		m := map[string]string{}
		for i := 0; i+1 < len(pairs); i += 2 {
			if long, s := t.Lookup(pairs[i]), t.Lookup(pairs[i+1]); long != s {
				m[long] = s
			}
		}
		return m
	}
	tone := func(cell string) kittui.TextStyle {
		if t, ok := tones[cell]; ok {
			return t
		}
		return kittui.StyleText
	}
	return []kittui.Column{
		{Title: textColStack},
		{Title: textColCurrent},
		{Title: textColAvailable, Short: short(textAvailError, textAvailErrorS, textAvailNotComparable, textAvailNotComparableS)},
		{Title: textColMode, Optional: true},
		{Title: textColState, Tone: tone, Short: short(textStateRollbackPause, textStateRollbackPauseS,
			textStateCtrFailed, textStateCtrFailedS, textStateRepairHalted, textStateRepairHaltedS, textStatePending, textStatePendingS, textStateNotDeployed, textStateNotDeployedS,
			textStateUnreadable, textStateUnreadableS, textStateNeedsDirs, textStateNeedsDirsS, textStateFailed, textStateFailedS)},
		{Title: textColLastRun, Optional: true, Tone: tone},
		{Title: textColFindings, Flex: true, Tone: tone},
	}
}

var stateTexts = map[stackState]kittui.TextID{
	stateRunning: textStateRunning, stateRollbackPause: textStateRollbackPause, stateUnreadable: textStateUnreadable,
	stateRemoved: textStateRemoved, stateStopped: textStateStopped, statePaused: textStatePaused, statePinned: textStatePinned,
	stateContainerFailed: textStateCtrFailed, stateRepairHalted: textStateRepairHalted,
	stateFailed: textStateFailed, statePending: textStatePending, stateDeployed: textStateDeployed,
	stateNotDeployed: textStateNotDeployed, stateNeedsDirs: textStateNeedsDirs,
}

var stateTones = map[stackState]kittui.TextStyle{
	stateRunning: kittui.StyleActive, stateRollbackPause: kittui.StyleFail, stateUnreadable: kittui.StyleFail,
	stateRemoved: kittui.StyleHint, stateStopped: kittui.StyleWarn, statePaused: kittui.StyleWarn, statePinned: kittui.StyleWarn,
	stateContainerFailed: kittui.StyleFail, stateRepairHalted: kittui.StyleFail,
	stateFailed: kittui.StyleFail, statePending: kittui.StyleWarn, stateDeployed: kittui.StyleOK,
	stateNotDeployed: kittui.StyleHint, stateNeedsDirs: kittui.StyleWarn,
}

var failureKinds = map[string]kittui.TextID{
	"exited": textFailureExited, "unhealthy": textFailureUnhealthy, "restart-loop": textFailureRestartLoop,
	"missing": textFailureMissing,
}

// lastRunLayout is the time of the last run; the year is left out to keep the column narrow.
const lastRunLayout = "01-02 15:04"

var resultTexts = map[string]kittui.TextID{
	"succeeded": textResultSucceeded, "failed": textResultFailed, "rolled-back": textResultRolledBack,
	"rollback-failed": textResultRollbackFailed,
}

var modeTexts = map[string]kittui.TextID{
	"automatic": textModeAutomatic, "manual": textModeManual, "none": textModeNone,
}

// cells translates a row into the cells of stackColumns and records the tone of the cells that carry one.
func cells(r stackRow, t kittui.Translator, loc *time.Location, tones map[string]kittui.TextStyle) []string {
	none := t.Lookup(textNoValue)
	current := none
	if len(r.current) > 0 {
		current = strings.Join(r.current, ", ")
	}
	var available string
	switch r.avail {
	case availNone:
		available = none
	case availNotChecked:
		available = t.Lookup(textAvailNotChecked)
	case availUpToDate:
		available = t.Lookup(textAvailUpToDate)
	case availUpdate:
		available = strings.Join(r.available, ", ")
	case availMajor:
		available = t.Lookup(textAvailMajor, strings.Join(r.available, ", "))
	case availError:
		available = t.Lookup(textAvailError)
	case availNotComparable:
		available = t.Lookup(textAvailNotComparable)
	}
	mode := r.mode
	if id, ok := modeTexts[mode]; ok {
		mode = t.Lookup(id)
	} else if mode == "" {
		mode = none
	}
	state := t.Lookup(stateTexts[r.state])
	tones[state] = stateTones[r.state]

	last := none
	if r.lastResult != "" {
		last = r.lastResult
		if id, ok := resultTexts[last]; ok {
			last = t.Lookup(id)
		}
		if !r.lastAt.IsZero() {
			last += " " + r.lastAt.In(loc).Format(lastRunLayout)
		}
		if slices.Contains(failedResults, r.lastResult) {
			tones[last] = kittui.StyleFail
		}
	}

	var parts []string
	if r.errors > 0 {
		parts = append(parts, plural(t, r.errors, textFindingsErrorsOne, textFindingsErrorsMany))
	}
	if r.warnings > 0 {
		parts = append(parts, plural(t, r.warnings, textFindingsWarningsOne, textFindingsWarningsMany))
	}
	findings := t.Lookup(textFindingsNone)
	if len(parts) > 0 {
		findings = strings.Join(parts, ", ")
		tones[findings] = kittui.StyleWarn
		if r.errors > 0 {
			tones[findings] = kittui.StyleFail
		}
	}
	return []string{r.name, current, available, mode, state, last, findings}
}

func plural(t kittui.Translator, n int, one, many kittui.TextID) string {
	if n == 1 {
		return t.Lookup(one, n)
	}
	return t.Lookup(many, n)
}

// itemIDs gives every row a list ID that holds no control character. Names that escape to the same text get a
// counter, so no row is lost.
func itemIDs(rows []stackRow) []kittui.ItemID {
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.name
	}
	return uniqueIDs(names)
}

// uniqueIDs is itemIDs for any names.
func uniqueIDs(names []string) []kittui.ItemID {
	ids := make([]kittui.ItemID, len(names))
	used := map[kittui.ItemID]bool{}
	for i, name := range names {
		base := kittui.Escape(name)
		id := kittui.ItemID(base)
		for n := 2; used[id] || id == ""; n++ {
			id = kittui.ItemID(base + "#" + strconv.Itoa(n))
		}
		used[id] = true
		ids[i] = id
	}
	return ids
}
