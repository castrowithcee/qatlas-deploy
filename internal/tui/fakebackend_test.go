package tui

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// fakeBackend answers from fixed results. A set error makes that call fail.
type fakeBackend struct {
	mu        sync.Mutex
	status    control.StatusResult
	stacks    control.StacksResult
	check     control.StacksCheckResult
	updates   control.UpdatesResult
	runs      control.RunsResult
	statusErr error
	stacksErr error
	// logs, changelogs and backups answer by run ID or stack name; an error set for the key fails that call.
	logs       map[string]string
	changelogs map[string]control.ChangelogResult
	backups    map[string]control.BackupListResult
	errs       map[string]error
	calls      map[string]int
	// acts records the changing calls in order as "<call>:<stack>"; an error set in errs for that key fails
	// the call, and updateRuns and holds answer by stack.
	acts       []string
	updateRuns map[string]control.StackUpdateRun
	holds      map[string]control.HoldResult

	// The settings: what the service holds. A key name of errs such as "probe:" fails that call.
	probe   control.RepoProbeResult
	key     *control.KeyResult
	creds   []control.CredentialInfo
	window  control.WindowResult
	notify  []control.NotifyChannelInfo
	repoSet []control.RepoSetArgs
	// sent are the arguments of the credential and channel writes in order, kept to check what reached the
	// service and that nothing else did.
	credSets []control.CredentialSetArgs
	adds     []control.NotifyAddArgs
	windows  []control.WindowSetArgs

	// secrets holds the key names by stack; secretSets and restores record the writes in order.
	secrets    map[string][]string
	secretSets []control.SecretSetArgs
	restores   []control.BackupRestoreArgs

	// containers is the list the service reads from the container engine, and containerDetails the detail by
	// name; errs "containers:" and "container:<name>" fail those calls.
	containers       control.ContainerListResult
	containerDetails map[string]control.ContainerDetail
	// ctNotReady is the reason a start or restart answers with as NotReady; "" means ready.
	ctNotReady string
	// ctLogLines are all container log lines in time order, ctLogPage the most lines one answer holds (zero:
	// no limit) and ctLogArgs the arguments of the calls made.
	ctLogLines []control.ContainerLogLine
	ctLogPage  int
	ctLogArgs  []control.ContainerLogsArgs
}

func (f *fakeBackend) count(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[name]++
}

func (f *fakeBackend) called(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

func (f *fakeBackend) Status(context.Context) (control.StatusResult, error) {
	f.count("status")
	return f.status, f.statusErr
}

func (f *fakeBackend) Stacks(context.Context) (control.StacksResult, error) {
	f.count("stacks")
	return f.stacks, f.stacksErr
}

func (f *fakeBackend) Check(context.Context, string) (control.StacksCheckResult, error) {
	f.count("check")
	return f.check, nil
}

func (f *fakeBackend) Updates(_ context.Context, args control.UpdatesArgs) (control.UpdatesResult, error) {
	f.count("updates")
	if args.Refresh {
		if err := f.act("refresh", "*"); err != nil {
			return control.UpdatesResult{}, err
		}
	}
	return f.updates, nil
}

func (f *fakeBackend) Runs(context.Context, string) (control.RunsResult, error) {
	f.count("runs")
	return f.runs, nil
}

// fail returns the error set for a call and key, "log:<run>", "changelog:<stack>" or "backups:<stack>".
func (f *fakeBackend) fail(call, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.errs[call+":"+key]
}

// setLog replaces the log of a run, as the service does while the run writes.
func (f *fakeBackend) setLog(run, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.logs == nil {
		f.logs = map[string]string{}
	}
	f.logs[run] = text
}

func (f *fakeBackend) Log(_ context.Context, run string) (control.LogResult, error) {
	f.count("log")
	if err := f.fail("log", run); err != nil {
		return control.LogResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	text, ok := f.logs[run]
	if !ok {
		return control.LogResult{}, control.NewError(control.CodeNoSuchRun, "no such run")
	}
	return control.LogResult{Text: text}, nil
}

func (f *fakeBackend) Changelog(_ context.Context, stack string) (control.ChangelogResult, error) {
	f.count("changelog")
	if err := f.fail("changelog", stack); err != nil {
		return control.ChangelogResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if res, ok := f.changelogs[stack]; ok {
		return res, nil
	}
	return control.ChangelogResult{Stack: stack}, nil
}

func (f *fakeBackend) Backups(_ context.Context, stack string) (control.BackupListResult, error) {
	f.count("backups")
	if err := f.fail("backups", stack); err != nil {
		return control.BackupListResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return control.BackupListResult{Stack: stack, Backups: f.backups[stack].Backups}, nil
}

// act records a changing call and returns the error set for it.
func (f *fakeBackend) act(call, stack string) error {
	f.count(call)
	f.mu.Lock()
	f.acts = append(f.acts, call+":"+stack)
	f.mu.Unlock()
	return f.fail(call, stack)
}

func (f *fakeBackend) actions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.acts...)
}

func (f *fakeBackend) Deploy(_ context.Context, stack string) (control.DeployResult, error) {
	if err := f.act("deploy", stack); err != nil {
		return control.DeployResult{}, err
	}
	return control.DeployResult{ID: "20261031T100000Z-" + stack, Stack: stack, Commit: commitB}, nil
}

func (f *fakeBackend) Update(_ context.Context, stacks []string) (control.UpdateRunResult, error) {
	var res control.UpdateRunResult
	f.count("update.call")
	for _, stack := range stacks {
		if err := f.act("update", stack); err != nil {
			return control.UpdateRunResult{}, err
		}
		f.mu.Lock()
		run, ok := f.updateRuns[stack]
		f.mu.Unlock()
		if !ok {
			run = control.StackUpdateRun{Name: stack, Steps: []control.UpdateStepInfo{{Variable: "APP_VERSION", From: "1.4.2",
				To: "1.5.0", Run: "20261031T100000Z-" + stack, Result: "succeeded"}}}
		}
		res.Stacks = append(res.Stacks, run)
	}
	return res, nil
}

func (f *fakeBackend) hold(call, stack string, active bool) (control.HoldResult, error) {
	if err := f.act(call, stack); err != nil {
		return control.HoldResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if res, ok := f.holds[call+":"+stack]; ok {
		return res, nil
	}
	return control.HoldResult{Stack: stack, Active: active, Changed: true}, nil
}

func (f *fakeBackend) Pause(_ context.Context, stack string) (control.HoldResult, error) {
	return f.hold("pause", stack, true)
}

func (f *fakeBackend) Unpause(_ context.Context, stack string) (control.HoldResult, error) {
	return f.hold("unpause", stack, false)
}

func (f *fakeBackend) Pin(_ context.Context, stack string) (control.HoldResult, error) {
	return f.hold("pin", stack, true)
}

func (f *fakeBackend) Unpin(_ context.Context, stack string) (control.HoldResult, error) {
	return f.hold("unpin", stack, false)
}

func (f *fakeBackend) Resume(_ context.Context, stack string) (control.ResumeResult, error) {
	if err := f.act("resume", stack); err != nil {
		return control.ResumeResult{}, err
	}
	return control.ResumeResult{Stack: stack, Pause: control.PauseInfo{Since: at(29, 3, 12), Run: "20261029T031000Z-0000000a"}}, nil
}

func (f *fakeBackend) stackAction(call, stack string, stopped bool) (control.StackActionResult, error) {
	if err := f.act(call, stack); err != nil {
		return control.StackActionResult{}, err
	}
	return control.StackActionResult{Stack: stack, Action: call, Stopped: stopped}, nil
}

func (f *fakeBackend) StackStart(_ context.Context, stack string) (control.StackActionResult, error) {
	return f.stackAction("start", stack, false)
}

func (f *fakeBackend) StackStop(_ context.Context, stack string) (control.StackActionResult, error) {
	return f.stackAction("stop", stack, true)
}

func (f *fakeBackend) StackRestart(_ context.Context, stack string) (control.StackActionResult, error) {
	return f.stackAction("restart", stack, false)
}

// containerAction records "<call>:<id>", where id is what the interface passed (the container ID), and answers
// with the container as it is afterwards: running after a start or restart, exited after a stop.
func (f *fakeBackend) containerAction(call, id string) (control.ContainerActionResult, error) {
	if err := f.act(call, id); err != nil {
		return control.ContainerActionResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	res := control.ContainerActionResult{}
	for _, d := range f.containerDetails {
		if d.ID == id || d.Name == id {
			res.ContainerSummary, res.Kind = d.ContainerSummary, d.Kind
		}
	}
	if call == "ctstop" {
		res.State, res.EngineState, res.UptimeSeconds, res.Health = control.ContainerStopped, "exited", 0, ""
		return res, nil
	}
	res.State, res.EngineState, res.UptimeSeconds = control.ContainerOK, "running", 2
	res.NotReady = f.ctNotReady
	if res.NotReady != "" {
		res.State, res.EngineState = control.ContainerStopped, "exited"
	}
	return res, nil
}

func (f *fakeBackend) ContainerStart(_ context.Context, id string) (control.ContainerActionResult, error) {
	return f.containerAction("ctstart", id)
}

func (f *fakeBackend) ContainerStop(_ context.Context, id string) (control.ContainerActionResult, error) {
	return f.containerAction("ctstop", id)
}

func (f *fakeBackend) ContainerRestart(_ context.Context, id string) (control.ContainerActionResult, error) {
	return f.containerAction("ctrestart", id)
}

func (f *fakeBackend) Window(context.Context) (control.WindowResult, error) {
	f.count("window")
	if err := f.fail("window", ""); err != nil {
		return control.WindowResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.window, nil
}

// at is a fixed point in time of the fixtures.
func at(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC)
}

const (
	commitA = "1111111111111111111111111111111111111111"
	commitB = "2222222222222222222222222222222222222222"
)

// demoBackend is a host with stacks in every state the overview shows.
func demoBackend() *fakeBackend {
	since := &control.HoldInfo{Since: at(28, 9, 0)}
	return &fakeBackend{
		status: control.StatusResult{Version: "9.9.9", StartedAt: at(27, 8, 0),
			Window: &control.WindowResult{Days: []string{"sun"}, Start: "03:00", Duration: "2h", Zone: "UTC", NextOpening: at(31, 3, 0)},
			Engine: control.EngineStatus{LastPoll: at(30, 14, 30), Stacks: []control.StackStatus{
				{Name: "demo-stack", Deployed: commitA, LastRun: "succeeded", Findings: []control.FindingInfo{}},
				{Name: "mail-relay", Deployed: commitA, Pending: commitB, LastRun: "succeeded", Findings: []control.FindingInfo{}},
				{Name: "old-tool", Deployed: commitA, Removed: true, LastRun: "succeeded",
					Findings: []control.FindingInfo{{Kind: "removed", Detail: "the folder is gone from the repository"}}},
				{Name: "photo-album", Deployed: commitA, LastRun: "rollback-failed",
					Paused:   &control.PauseInfo{Since: at(29, 3, 12), Run: "20261029T031000Z-0000000a"},
					Findings: []control.FindingInfo{{Kind: "paused", Detail: "a rollback failed"}}},
				{Name: "web-shop", Deployed: commitA, ActiveRun: "20261030T150000Z-0000000b", LastRun: "rolled-back", Findings: []control.FindingInfo{}},
				{Name: "wiki", Deployed: commitA, Pinned: since, LastRun: "succeeded", Findings: []control.FindingInfo{}},
			}}},
		stacks: control.StacksResult{Stacks: []control.StackInfo{
			{Name: "demo-stack", Update: "automatic", Images: []control.ImageInfo{{Service: "app", Image: "example/app", Var: "APP_VERSION", Version: "1.4.2"}}},
			{Name: "mail-relay", Update: "manual", Images: []control.ImageInfo{{Service: "relay", Image: "example/relay", Var: "RELAY_VERSION", Version: "3.1"}}},
			{Name: "photo-album", Update: "automatic", Images: []control.ImageInfo{{Service: "web", Image: "example/album", Var: "ALBUM_VERSION", Version: "2.0.1"}}},
			{Name: "web-shop", Update: "automatic", Images: []control.ImageInfo{
				{Service: "shop", Image: "example/shop", Var: "SHOP_VERSION", Version: "5.2.0"},
				{Service: "db", Image: "example/db", Var: "DB_VERSION", Version: "16.4"}}},
			{Name: "wiki", Update: "none", Images: []control.ImageInfo{{Service: "wiki", Image: "example/wiki", Var: "WIKI_VERSION", Version: "1.41.0"}}},
		}},
		check: control.StacksCheckResult{Stacks: []control.StackReport{
			{Stack: "demo-stack", Functions: []control.StackFunction{
				{Function: "deploy", Works: true, Reason: "Compose starts the stack from compose.yaml"},
				{Function: "update", Works: true, Mode: "automatic", Reason: "the maintenance window updates APP_VERSION by itself"},
				{Function: "rollback", Works: true, Reason: "a failed run goes back to the previous commit and restores the backup it made before the change"},
				{Function: "backup", Works: true, Reason: "a backup saves the data directories data/db"},
				{Function: "redact", Works: true, Reason: "the values of secrets.env (2 key(s)) are redacted in logs and messages"},
			}, Findings: []control.CheckFinding{
				{Stack: "demo-stack", Rule: "secrets-env-not-ignored", Severity: "warning", Cause: "secrets.env is not listed in .gitignore", Fix: "Add the line 'secrets.env' to the .gitignore."},
			}},
			{Stack: "photo-album", Findings: []control.CheckFinding{{Stack: "photo-album", Rule: "unset-image-variable", Severity: "error"}}},
			{Stack: "wiki", Functions: []control.StackFunction{
				{Function: "deploy", Works: true, Reason: "Compose starts the stack from compose.yaml"},
				{Function: "update", Mode: "none", Reason: "the image versions (WIKI_VERSION) can be updated, but updates are not switched on",
					Action: "Switch on updates in the stack's settings in the qdeploy TUI."},
			}, Findings: []control.CheckFinding{
				{Stack: "wiki", Rule: "duplicate-key", Severity: "warning"},
				{Stack: "wiki", Rule: "tracked-data", Severity: "warning"},
			}},
		}, Errors: 1, Warnings: 3},
		updates: control.UpdatesResult{Stacks: []control.StackUpdates{
			{Name: "demo-stack", Update: "automatic", Checked: at(30, 3, 5), Items: []control.UpdateInfo{
				{Variable: "APP_VERSION", Image: "example/app", Current: "1.4.2", Selected: "1.5.0", Status: "update"}}},
			{Name: "mail-relay", Update: "manual", Checked: at(30, 3, 5), Items: []control.UpdateInfo{
				{Variable: "RELAY_VERSION", Image: "example/relay", Current: "3.1", Selected: "3.1", Status: "current"}}},
			{Name: "photo-album", Update: "automatic", Checked: at(30, 3, 5), Items: []control.UpdateInfo{
				{Variable: "ALBUM_VERSION", Image: "example/album", Current: "2.0.1", Status: "error", Error: "registry unreachable"}}},
			{Name: "web-shop", Update: "automatic", Checked: at(30, 3, 5), Items: []control.UpdateInfo{
				{Variable: "SHOP_VERSION", Image: "example/shop", Current: "5.2.0", Selected: "5.2.0", MajorAvailable: "6.0.0", Status: "current"},
				{Variable: "DB_VERSION", Image: "example/db", Current: "16.4", Selected: "16.4", Status: "current"}}},
			{Name: "wiki", Update: "none"},
		}},
		runs: control.RunsResult{Runs: []control.RunInfo{
			{ID: "20261030T150000Z-0000000b", Stack: "web-shop", Started: at(30, 15, 0), Trigger: "manual"},
			{ID: "20261030T140200Z-0000000c", Stack: "demo-stack", Started: at(30, 14, 2), Trigger: "update", DurationMS: 48000, Result: "succeeded"},
			{ID: "20261030T120000Z-0000000d", Stack: "web-shop", Started: at(30, 12, 0), Trigger: "poll", DurationMS: 215000, Result: "rolled-back",
				Error: "health check of shop failed"},
			{ID: "20261029T031000Z-0000000a", Stack: "photo-album", Started: at(29, 3, 10), Trigger: "poll", DurationMS: 9000, Result: "rollback-failed"},
			{ID: "20261029T020000Z-00000010", Stack: "demo-stack", Started: at(29, 2, 0), Trigger: "poll", DurationMS: 850, Result: "failed",
				Error: "compose pull failed: registry unreachable"},
			{ID: "20261028T031000Z-0000000e", Stack: "mail-relay", Started: at(28, 3, 10), Trigger: "poll", DurationMS: 31000, Result: "succeeded"},
			{ID: "20261027T031000Z-0000000f", Stack: "wiki", Started: at(27, 3, 10), Trigger: "manual", DurationMS: 12000, Result: "succeeded"},
		}},
		logs: map[string]string{
			"20261030T140200Z-0000000c": demoLog,
			"20261030T150000Z-0000000b": demoActiveLog,
		},
		changelogs: map[string]control.ChangelogResult{
			"demo-stack": {Stack: "demo-stack", Checked: at(30, 3, 5), Items: []control.ChangelogInfo{{
				Variable: "APP_VERSION", Current: "1.4.2", Target: "1.5.0", Source: "example/app",
				Link: "https://example.invalid/app/releases",
				Releases: []control.ChangelogRelease{
					{Version: "1.5.0", Name: "Spring release", Published: at(29, 9, 0),
						Body: "Adds a dark theme.\n\n- faster start\n- fixes the upload limit"},
					{Version: "1.4.3", Published: at(20, 9, 0), Body: "Fixes a crash on empty input."},
				}}}},
		},
		backups: map[string]control.BackupListResult{
			"demo-stack": {Stack: "demo-stack", Backups: []control.BackupInfo{
				{ID: "20261030T140100Z-0123abcd", Stack: "demo-stack", Created: at(30, 14, 1), Size: 12582912},
				{ID: "20261023T031000Z-4567cdef", Stack: "demo-stack", Created: at(23, 3, 10), Size: 11534336},
				{ID: "20261016T031000Z-89abcdef", Stack: "demo-stack", Incomplete: true},
			}},
		},
		secrets: map[string][]string{"demo-stack": {"API_SIGNING_KEY", "DB_PASSWORD"}},
		errs:    map[string]error{},
	}
}

// demoLog is the log of a finished run in the format of the service.
const demoLog = `2026-10-30T14:02:00Z step check
2026-10-30T14:02:01Z info compose file is valid
2026-10-30T14:02:02Z step backup
2026-10-30T14:02:09Z info backup 20261030T140100Z-0123abcd, 12.0 MiB
2026-10-30T14:02:10Z step deploy
2026-10-30T14:02:11Z info pull example/app:1.4.2
2026-10-30T14:02:40Z info up -d finished
2026-10-30T14:02:45Z step verify
2026-10-30T14:02:50Z info all services healthy
`

// demoActiveLog is the log of the run that is still active.
const demoActiveLog = `2026-10-30T15:00:00Z step check
2026-10-30T15:00:01Z info compose file is valid
2026-10-30T15:00:02Z step backup
2026-10-30T15:00:12Z info backup 20261030T150001Z-0123abcd, 48.0 MiB
2026-10-30T15:00:13Z step deploy
2026-10-30T15:00:14Z info pull example/shop:5.2.0
`

func (f *fakeBackend) RepoProbe(_ context.Context, url string) (control.RepoProbeResult, error) {
	if err := f.act("probe", url); err != nil {
		return control.RepoProbeResult{}, err
	}
	return f.probe, nil
}

func (f *fakeBackend) RepoSet(_ context.Context, args control.RepoSetArgs) (control.RepoSetResult, error) {
	if err := f.act("reposet", args.URL); err != nil {
		return control.RepoSetResult{}, err
	}
	f.mu.Lock()
	f.repoSet = append(f.repoSet, args)
	f.mu.Unlock()
	return control.RepoSetResult{URL: args.URL, Dir: args.Dir, Fingerprint: args.Fingerprint, Head: commitA, Cloned: true}, nil
}

func (f *fakeBackend) RepoFetch(context.Context) (control.RepoFetchResult, error) {
	if err := f.act("fetch", ""); err != nil {
		return control.RepoFetchResult{}, err
	}
	return control.RepoFetchResult{Dir: stackDir, Head: commitA, RemoteHead: commitB, UpdatesAvailable: true}, nil
}

func (f *fakeBackend) KeyShow(context.Context) (control.KeyResult, error) {
	f.count("keyshow")
	if err := f.fail("keyshow", ""); err != nil {
		return control.KeyResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.key == nil {
		return control.KeyResult{}, control.NewError(control.CodeNotFound, "no deploy key exists")
	}
	return *f.key, nil
}

func (f *fakeBackend) KeyGenerate(context.Context) (control.KeyResult, error) {
	if err := f.act("keygen", ""); err != nil {
		return control.KeyResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.key != nil {
		return control.KeyResult{}, control.NewError(control.CodeKeyExists, "a deploy key already exists")
	}
	f.key = &control.KeyResult{PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyForTestsOnly qdeploy",
		Fingerprint: "SHA256:ExampleKeyFingerprintForTestsOnly000000000"}
	return *f.key, nil
}

func (f *fakeBackend) Credentials(context.Context) (control.CredentialListResult, error) {
	f.count("credentials")
	if err := f.fail("credentials", ""); err != nil {
		return control.CredentialListResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return control.CredentialListResult{Credentials: append([]control.CredentialInfo(nil), f.creds...)}, nil
}

func (f *fakeBackend) CredentialSet(_ context.Context, name, value string) error {
	if err := f.act("credset", name); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.credSets = append(f.credSets, control.CredentialSetArgs{Name: name, Value: value})
	f.creds = append(f.creds, control.CredentialInfo{Name: name, Modified: at(31, 9, 0)})
	return nil
}

func (f *fakeBackend) CredentialDelete(_ context.Context, name string) error {
	if err := f.act("creddel", name); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creds = slices.DeleteFunc(f.creds, func(c control.CredentialInfo) bool { return c.Name == name })
	return nil
}

func (f *fakeBackend) WindowSet(_ context.Context, args control.WindowSetArgs) (control.WindowResult, error) {
	if err := f.act("windowset", ""); err != nil {
		return control.WindowResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.windows = append(f.windows, args)
	if args.Days != nil {
		f.window.Days = args.Days
	}
	if args.Start != "" {
		f.window.Start = args.Start
	}
	if args.Duration != "" {
		f.window.Duration = args.Duration
	}
	if args.Zone != "" {
		f.window.Zone = args.Zone
	}
	return f.window, nil
}

func (f *fakeBackend) Notifications(context.Context) (control.NotifyListResult, error) {
	f.count("notifications")
	if err := f.fail("notifications", ""); err != nil {
		return control.NotifyListResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return control.NotifyListResult{Channels: append([]control.NotifyChannelInfo(nil), f.notify...)}, nil
}

func (f *fakeBackend) NotifyAdd(_ context.Context, args control.NotifyAddArgs) (control.NotifyChannelInfo, error) {
	if err := f.act("notifyadd", args.Name); err != nil {
		return control.NotifyChannelInfo{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.adds = append(f.adds, args)
	info := control.NotifyChannelInfo{Name: args.Name, Type: args.Type, Events: args.Events, Host: args.Host, Port: args.Port,
		Security: args.Security, User: args.User, From: args.From, To: args.To, ChatID: args.ChatID, BaseURL: args.BaseURL}
	f.notify = append(f.notify, info)
	return info, nil
}

func (f *fakeBackend) NotifyRemove(_ context.Context, name string) error {
	if err := f.act("notifyremove", name); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notify = slices.DeleteFunc(f.notify, func(c control.NotifyChannelInfo) bool { return c.Name == name })
	return nil
}

func (f *fakeBackend) NotifyTest(_ context.Context, name string) (control.NotifyTestResult, error) {
	if err := f.act("notifytest", name); err != nil {
		return control.NotifyTestResult{}, err
	}
	return control.NotifyTestResult{Name: name, Type: control.NotifyWebhook}, nil
}

func (f *fakeBackend) Secrets(_ context.Context, stack string) (control.SecretListResult, error) {
	f.count("secrets")
	if err := f.fail("secrets", stack); err != nil {
		return control.SecretListResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return control.SecretListResult{Stack: stack, Keys: slices.Clone(f.secrets[stack])}, nil
}

func (f *fakeBackend) SecretSet(_ context.Context, stack, key, value string) (control.SecretResult, error) {
	if err := f.act("secretset", stack+"/"+key); err != nil {
		return control.SecretResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.secretSets = append(f.secretSets, control.SecretSetArgs{Stack: stack, Key: key, Value: value})
	replaced := slices.Contains(f.secrets[stack], key)
	if !replaced {
		if f.secrets == nil {
			f.secrets = map[string][]string{}
		}
		f.secrets[stack] = append(f.secrets[stack], key)
		slices.Sort(f.secrets[stack])
	}
	return control.SecretResult{Stack: stack, Key: key, Replaced: replaced}, nil
}

func (f *fakeBackend) SecretDelete(_ context.Context, stack, key string) (control.SecretResult, error) {
	if err := f.act("secretdelete", stack+"/"+key); err != nil {
		return control.SecretResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.secrets[stack] = slices.DeleteFunc(f.secrets[stack], func(k string) bool { return k == key })
	return control.SecretResult{Stack: stack, Key: key}, nil
}

func (f *fakeBackend) BackupRestore(_ context.Context, stack, id string) (control.BackupRestoreResult, error) {
	if err := f.act("restore", stack+"/"+id); err != nil {
		return control.BackupRestoreResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restores = append(f.restores, control.BackupRestoreArgs{Stack: stack, ID: id, Confirm: true})
	return control.BackupRestoreResult{Backup: control.BackupInfo{ID: id, Stack: stack}}, nil
}

func (f *fakeBackend) Containers(context.Context) (control.ContainerListResult, error) {
	f.count("containers")
	if err := f.fail("containers", ""); err != nil {
		return control.ContainerListResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.containers, nil
}

func (f *fakeBackend) Container(_ context.Context, name string) (control.ContainerDetail, error) {
	f.count("container")
	if err := f.fail("container", name); err != nil {
		return control.ContainerDetail{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.containerDetails[name]
	if !ok {
		return control.ContainerDetail{}, control.NewError(control.CodeNotFound, "no such container")
	}
	return d, nil
}

// ContainerLogs answers like the service from ctLogLines: it filters by container or stack (a source that
// starts with "<stack>-"), by time and cursor, keeps the last Tail lines and cuts a page at ctLogPage lines.
// The arguments of every call are kept in ctLogArgs; an error set in errs under "containerlogs:<name or stack>"
// fails the call.
func (f *fakeBackend) ContainerLogs(_ context.Context, a control.ContainerLogsArgs) (control.ContainerLogsResult, error) {
	f.count("containerlogs")
	if err := f.fail("containerlogs", a.Name+a.Stack); err != nil {
		return control.ContainerLogsResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ctLogArgs = append(f.ctLogArgs, a)
	var sel []control.ContainerLogLine
	for _, l := range f.ctLogLines {
		switch {
		case a.Name != "" && l.Source != a.Name,
			a.Stack != "" && !strings.HasPrefix(l.Source, a.Stack+"-"),
			!a.Since.IsZero() && l.Time.Before(a.Since),
			!a.Until.IsZero() && l.Time.After(a.Until),
			!a.Cursor.IsZero() && l.Time.Before(a.Cursor):
			continue
		}
		sel = append(sel, l)
	}
	if a.Tail > 0 && len(sel) > a.Tail {
		sel = sel[len(sel)-a.Tail:]
	}
	res := control.ContainerLogsResult{Lines: []control.ContainerLogLine{}}
	if f.ctLogPage > 0 && len(sel) > f.ctLogPage {
		sel, res.More = sel[:f.ctLogPage], true
	}
	res.Lines = append(res.Lines, sel...)
	if len(sel) > 0 {
		res.Cursor = sel[len(sel)-1].Time.Add(time.Nanosecond)
	}
	return res, nil
}
