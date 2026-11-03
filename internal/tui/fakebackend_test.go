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
	calls     map[string]int
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

func (f *fakeBackend) Log(context.Context, string) (control.LogResult, error) {
	f.count("log")
	return control.LogResult{}, nil
}

func (f *fakeBackend) Changelog(context.Context, string) (control.ChangelogResult, error) {
	f.count("changelog")
	return control.ChangelogResult{}, nil
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
				{Variable: "APP_VERSION", Current: "1.4.2", Selected: "1.5.0", Status: "update"}}},
			{Name: "mail-relay", Update: "manual", Checked: at(30, 3, 5), Items: []control.UpdateInfo{
				{Variable: "RELAY_VERSION", Current: "3.1", Selected: "3.1", Status: "current"}}},
			{Name: "photo-album", Update: "automatic", Checked: at(30, 3, 5), Items: []control.UpdateInfo{
				{Variable: "ALBUM_VERSION", Current: "2.0.1", Status: "error", Error: "registry unreachable"}}},
			{Name: "web-shop", Update: "automatic", Checked: at(30, 3, 5), Items: []control.UpdateInfo{
				{Variable: "SHOP_VERSION", Current: "5.2.0", Selected: "5.2.0", MajorAvailable: "6.0.0", Status: "current"},
				{Variable: "DB_VERSION", Current: "16.4", Selected: "16.4", Status: "current"}}},
			{Name: "wiki", Update: "none"},
		}},
		runs: control.RunsResult{Runs: []control.RunInfo{
			{ID: "20261030T150000Z-0000000b", Stack: "web-shop", Started: at(30, 15, 0), Trigger: "manual"},
			{ID: "20261030T140200Z-0000000c", Stack: "demo-stack", Started: at(30, 14, 2), Result: "succeeded"},
			{ID: "20261030T120000Z-0000000d", Stack: "web-shop", Started: at(30, 12, 0), Result: "rolled-back"},
			{ID: "20261029T031000Z-0000000a", Stack: "photo-album", Started: at(29, 3, 10), Result: "rollback-failed"},
			{ID: "20261028T031000Z-0000000e", Stack: "mail-relay", Started: at(28, 3, 10), Result: "succeeded"},
			{ID: "20261027T031000Z-0000000f", Stack: "wiki", Started: at(27, 3, 10), Result: "succeeded"},
		}},
	}
}
