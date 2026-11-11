package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// twoFoldersBackend is the demo host with a second, local stack folder that brings one stack and a stack
// named like one of the first folder, and whose first folder failed its last poll.
func twoFoldersBackend() *fakeBackend {
	b := demoBackend()
	b.status.Engine.Sources = []control.SourceStatus{
		{ID: "srv-stacks-example", Path: stackDir, Kind: control.SourceKindRemote, URL: repoURL, LastPoll: at(30, 14, 30),
			PollError: "cannot reach git.example.test: connection refused"},
		{ID: "srv-local-example", Path: localDir, Kind: control.SourceKindLocal, LastPoll: at(30, 14, 25)},
	}
	for i := range b.status.Engine.Stacks {
		b.status.Engine.Stacks[i].Source = "srv-stacks-example"
	}
	for i := range b.stacks.Stacks {
		b.stacks.Stacks[i].Source, b.stacks.Stacks[i].SourceDir = "srv-stacks-example", stackDir
	}
	b.status.Engine.Stacks = append(b.status.Engine.Stacks,
		control.StackStatus{Name: "local-app", Source: "srv-local-example", Deployed: commitB, LastRun: "succeeded", Findings: []control.FindingInfo{}},
		control.StackStatus{Name: "wiki", Source: "srv-local-example", Findings: []control.FindingInfo{{Kind: control.FindingNameCollision, Detail: "taken"}}})
	b.stacks.Stacks = append(b.stacks.Stacks,
		control.StackInfo{Name: "local-app", Source: "srv-local-example", SourceDir: localDir, Update: "none"},
		control.StackInfo{Name: "wiki", Source: "srv-local-example", SourceDir: localDir, Update: "none", Collision: "taken"})
	return b
}

func TestGoldenStacksOfTwoFolders(t *testing.T) {
	goldenScreens(t, "stacks.folders", 18, twoFoldersBackend)
}

// A stack shows the folder of the entry that stands for its name, and a blocked entry never takes it over.
func TestRowsNameTheirFolder(t *testing.T) {
	folders := map[string]string{}
	for _, r := range buildRows(twoFoldersBackend().snapshot()) {
		folders[r.name] = r.folder
	}
	want := map[string]string{"demo-stack": stackDir, "wiki": stackDir, "local-app": localDir}
	for name, folder := range want {
		if folders[name] != folder {
			t.Errorf("%s: folder %q, want %q", name, folders[name], folder)
		}
	}
}

// The folder column and the line for a failed poll appear with the second folder; one folder keeps the list
// as it was.
func TestFolderColumnFollowsTheNumberOfFolders(t *testing.T) {
	m := newTestModel(t, goldenLangs["en"], demoBackend(), wideWidth, goldenRows)
	if s := m.render(); strings.Contains(s, "FOLDER") || strings.Contains(s, "poll failed:") {
		t.Fatalf("one folder shows the column or an error:\n%s", s)
	}
	send(m, "down", "down")
	b := twoFoldersBackend()
	m.backend = b
	load(t, m)
	s := m.render()
	for _, want := range []string{stackDir, localDir, "local-app", "Stack folder " + stackDir + ": poll failed: cannot reach"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if id, _ := m.list.Selected(); id != "old-tool" {
		t.Errorf("the cursor moved to %q when the column came", id)
	}
	m.backend = demoBackend()
	load(t, m)
	if s := m.render(); strings.Contains(s, "FOLDER") {
		t.Errorf("the column stays after the second folder is gone:\n%s", s)
	}
}
