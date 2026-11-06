package tui

import (
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// A stack blocked by name-collision adds its finding to the row of its name but never its state.
func TestBlockedEntryLeavesTheRowOfItsName(t *testing.T) {
	head := "0123456789abcdef0123456789abcdef01234567"
	s := snapshot{status: control.StatusResult{Engine: control.EngineStatus{Stacks: []control.StackStatus{
		{Name: "demo-stack", Source: "srv-first", Deployed: head},
		{Name: "demo-stack", Source: "srv-second", Findings: []control.FindingInfo{{Kind: control.FindingNameCollision, Detail: "taken"}}},
	}}}, stacks: control.StacksResult{Stacks: []control.StackInfo{
		{Name: "demo-stack", Source: "srv-first", Update: "automatic"},
		{Name: "demo-stack", Source: "srv-second", Update: "none", Collision: "taken"},
	}}}
	rows := buildRows(s)
	if len(rows) != 1 || rows[0].state != stateDeployed || rows[0].mode != "automatic" || rows[0].errors != 1 {
		t.Fatalf("rows = %+v", rows)
	}
}
