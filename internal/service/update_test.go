package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/registry/registrytest"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// backupFails is a deployer whose backups of one stack fail.
type backupFails struct {
	fakeDeployer
	stack string
}

func (d *backupFails) Backup(_ context.Context, st stacks.Stack, _ string, _ *engine.Log) (string, error) {
	if st.Name == d.stack {
		return "", errors.New("the demo backup failed")
	}
	return "", engine.ErrNoBackup
}

const manualRules = "version: 1\nupdate:\n  mode: manual\n"

func updateHarness(t *testing.T, dep engine.Deployer) *harness {
	t.Helper()
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": {"0.9.1", "0.9.2", "1.2.3", "1.3.0", "2.0.0", "2.1.0-rc.1"}}})
	return boundWith(t, dep, map[string]string{"plain-stack/qdeploy.yaml": manualRules}, withRegistry(srv.URL))
}

func (h *harness) update(args control.UpdateRunArgs) (control.UpdateRunResult, error) {
	var res control.UpdateRunResult
	err := h.call(control.UpdateRunCommand, args, &res)
	return res, err
}

func TestUpdateRunsStacksOneAfterAnotherAndReportsEach(t *testing.T) {
	h := updateHarness(t, &backupFails{stack: "plain-stack"})
	res, err := h.update(control.UpdateRunArgs{Stacks: []string{"plain-stack", "demo-stack"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Stacks) != 2 || res.Stacks[0].Name != "plain-stack" || res.Stacks[1].Name != "demo-stack" {
		t.Fatalf("result = %+v", res)
	}
	plain, demo := res.Stacks[0].Steps, res.Stacks[1].Steps
	if len(plain) != 1 || plain[0].From != "0.9.1" || plain[0].To != "0.9.2" || plain[0].Result != engine.ResultRolledBack || plain[0].Error == "" {
		t.Fatalf("plain-stack = %+v", plain)
	}
	if len(demo) != 1 || demo[0].Variable != "DEMO_APP_VERSION" || demo[0].From != "1.2.3" || demo[0].To != "1.3.0" ||
		demo[0].Result != engine.ResultSucceeded || demo[0].Run == "" || demo[0].Commit == "" {
		t.Fatalf("demo-stack = %+v", demo)
	}
	if got := h.git.Pushed(); !slices.Equal(got, []string{"Update demo-stack DEMO_APP_VERSION from 1.2.3 to 1.3.0"}) {
		t.Fatalf("pushed = %q", got)
	}
	var st control.StatusResult
	if err := h.call(control.StatusCommand, nil, &st); err != nil {
		t.Fatal(err)
	}
	for _, s := range st.Engine.Stacks {
		if s.Name == "demo-stack" && s.Deployed != demo[0].Commit {
			t.Fatalf("demo-stack deployed at %s, want %s", s.Deployed, demo[0].Commit)
		}
	}
	var det control.UpdatesResult
	if err := h.call(control.UpdatesListCommand, control.UpdatesArgs{Stack: "demo-stack"}, &det); err != nil {
		t.Fatal(err)
	}
	if it := det.Stacks[0].Items; len(it) != 1 || it[0].Current != "1.3.0" {
		t.Fatalf("stored detection = %+v", det.Stacks[0])
	}
}

func TestUpdateExplicitVersionAboveTheCeilingGoesStepwise(t *testing.T) {
	h := updateHarness(t, &fakeDeployer{})
	res, err := h.update(control.UpdateRunArgs{Stacks: []string{"demo-stack"}, Version: "2.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	steps := res.Stacks[0].Steps
	if len(steps) != 2 || steps[0].To != "1.3.0" || steps[1].From != "1.3.0" || steps[1].To != "2.0.0" ||
		steps[0].Result != engine.ResultSucceeded || steps[1].Result != engine.ResultSucceeded {
		t.Fatalf("steps = %+v", steps)
	}
	if len(h.git.Pushed()) != 2 {
		t.Fatalf("pushed = %q", h.git.Pushed())
	}
}

func TestUpdateRefusesBeforeAnythingChanges(t *testing.T) {
	h := updateHarness(t, &fakeDeployer{})
	for _, tc := range []struct {
		args control.UpdateRunArgs
		code string
	}{
		{control.UpdateRunArgs{}, control.CodeInvalid},
		{control.UpdateRunArgs{Stacks: []string{"demo-stack", "demo-stack"}}, control.CodeInvalid},
		{control.UpdateRunArgs{Stacks: []string{"demo-stack", "broken-stack"}}, control.CodeUnreadable},
		{control.UpdateRunArgs{Stacks: []string{"demo-stack", "nope"}}, control.CodeNoSuchStack},
		{control.UpdateRunArgs{Stacks: []string{"demo-stack"}, Version: "1.2.3"}, control.CodeBadVersion},
		{control.UpdateRunArgs{Stacks: []string{"demo-stack"}, Version: "0.9.2"}, control.CodeBadVersion},
		{control.UpdateRunArgs{Stacks: []string{"demo-stack"}, Version: "9.9.9"}, control.CodeBadVersion},
		{control.UpdateRunArgs{Stacks: []string{"demo-stack"}, Version: "2.1.0-rc.1"}, control.CodeBadVersion},
	} {
		_, err := h.update(tc.args)
		wantCode(t, err, tc.code)
	}
	if len(h.git.Pushed()) != 0 {
		t.Fatalf("pushed = %q", h.git.Pushed())
	}
}

func TestUpdateRefusesAStackWithoutRules(t *testing.T) {
	h := bound(t, &fakeDeployer{})
	_, err := h.update(control.UpdateRunArgs{Stacks: []string{"plain-stack"}})
	wantCode(t, err, control.CodeNotUpdatable)
}
