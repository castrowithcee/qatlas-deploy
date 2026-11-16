package tui

import (
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
