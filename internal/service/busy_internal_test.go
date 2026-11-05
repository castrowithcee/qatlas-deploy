package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
)

// TestBusyWorkingCopyAnswersBusy: a command that gave up waiting for the working copy gets the fixed busy
// answer from every handler path, not an internal error.
func TestBusyWorkingCopyAnswersBusy(t *testing.T) {
	busy := fmt.Errorf("wait: %w", engine.ErrBusy)
	wantBusy := func(what string, err error) {
		t.Helper()
		var ce *control.Error
		if !errors.As(err, &ce) || ce.Code != control.CodeBusy || ce.Message != "the working copy is busy with an update or a rollback; try again" {
			t.Fatalf("%s: %v", what, err)
		}
	}
	wantBusy("runsAnswer", runsAnswer(busy))
	wantBusy("updateAnswer", updateAnswer(context.Background(), busy))
	h := holdHandler(func(context.Context, string, bool) (bool, error) { return false, busy }, true)
	_, err := h(context.Background(), json.RawMessage(`{"stack":"demo-stack"}`))
	wantBusy("holdHandler", err)
}
