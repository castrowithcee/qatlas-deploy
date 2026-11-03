package tui

import (
	"context"
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
		f.count("refresh")
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

func (f *fakeBackend) Window(context.Context) (control.WindowResult, error) {
	f.count("window")
	return control.WindowResult{}, nil
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
		check: control.StacksCheckResult{Findings: []control.CheckFinding{
			{Stack: "demo-stack", Rule: "env-example", Severity: "warning"},
			{Stack: "photo-album", Rule: "healthcheck", Severity: "error"},
			{Stack: "wiki", Rule: "env-example", Severity: "warning"},
			{Stack: "wiki", Rule: "fixed-tag", Severity: "warning"},
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
		errs: map[string]error{},
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
