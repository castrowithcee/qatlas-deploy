package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
)

// runsAnswer maps an engine error to a fixed answer. Text from the engine's error is never passed on: it may
// carry names or paths from the working copy.
func runsAnswer(err error) error {
	switch {
	case errors.Is(err, engine.ErrRunActive):
		return control.NewError(control.CodeRunActive, "a run of this stack is already active")
	case errors.Is(err, engine.ErrLocalChanges):
		return control.NewError(control.CodeLocalChanges, "the stack has local changes in the working copy and is skipped")
	case errors.Is(err, engine.ErrUnknownStack):
		return control.NewError(control.CodeNoSuchStack, "no such stack in the working copy")
	case errors.Is(err, engine.ErrUnreadable):
		return control.NewError(control.CodeUnreadable, "the stack cannot be read")
	case errors.Is(err, engine.ErrNotUpdatable):
		return control.NewError(control.CodeUnreadable, "the stack is deploy-only: its .env cannot be read, so it is not updated; 'qdeploy stack check' names the line")
	case errors.Is(err, engine.ErrNoDeployer):
		return control.NewError(control.CodeNoDeployer, "deployments are not available in this version")
	case errors.Is(err, engine.ErrNoRepo):
		return control.NewError(control.CodeNoRepo, "no stacks repository is bound")
	case errors.Is(err, engine.ErrStopped):
		return control.NewError(control.CodeBusy, "the service is stopping")
	case errors.Is(err, engine.ErrPaused):
		return control.NewError(control.CodePaused, "the stack is paused after a failed rollback; resume it first")
	case errors.Is(err, engine.ErrNotPaused):
		return control.NewError(control.CodeNotPaused, "the stack is not paused")
	case errors.Is(err, engine.ErrPinned):
		return control.NewError(control.CodePinned, "the stack is pinned; unpin it first")
	case errors.Is(err, engine.ErrStackStopped):
		return control.NewError(control.CodeStackStopped, "the stack was stopped through qdeploy; start it with start first")
	case errors.Is(err, engine.ErrNoSuchRun):
		return control.NewError(control.CodeNoSuchRun, "no such run")
	}
	return err
}

func registerRunCommands(r *control.Registry, eng *engine.Engine, version string, started time.Time, window func() *control.WindowResult) error {
	commands := map[string]control.Handler{
		control.StatusCommand: func(context.Context, json.RawMessage) (any, error) {
			snap, err := eng.Snapshot()
			if err != nil {
				return nil, errors.New("the engine state cannot be read")
			}
			return control.StatusResult{Version: version, StartedAt: started, Engine: engineStatus(snap), Window: window()}, nil
		},
		control.DeployCommand: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a control.DeployArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			info, err := eng.Trigger(ctx, a.Stack)
			if err != nil {
				return nil, runsAnswer(err)
			}
			return control.DeployResult{ID: printable(info.ID), Stack: printable(info.Stack), Commit: printable(info.Commit)}, nil
		},
		control.RunsCommand: func(_ context.Context, raw json.RawMessage) (any, error) {
			var a control.RunsArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			runs, err := eng.Runs(a.Stack)
			if err != nil {
				return nil, errors.New("the engine state cannot be read")
			}
			res := control.RunsResult{Runs: make([]control.RunInfo, 0, len(runs))}
			for _, rec := range runs {
				res.Runs = append(res.Runs, control.RunInfo{
					ID: printable(rec.ID), Stack: printable(rec.Stack), Commit: printable(rec.Commit),
					Trigger: printable(rec.Trigger), Started: rec.Started, DurationMS: rec.Duration.Milliseconds(),
					Result: printable(rec.Result), Error: printable(rec.Error),
				})
			}
			return res, nil
		},
		control.ResumeCommand: func(_ context.Context, raw json.RawMessage) (any, error) {
			var a control.ResumeArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			p, err := eng.Resume(a.Stack)
			if err != nil {
				if errors.Is(err, engine.ErrNotPaused) {
					return nil, runsAnswer(err)
				}
				return nil, errors.New("the engine state cannot be written")
			}
			return control.ResumeResult{Stack: printable(a.Stack), Pause: pauseInfo(p)}, nil
		},
		control.PauseCommand:   holdHandler(eng.SetPause, true),
		control.UnpauseCommand: holdHandler(eng.SetPause, false),
		control.PinCommand:     holdHandler(eng.SetPin, true),
		control.UnpinCommand:   holdHandler(eng.SetPin, false),
		control.LogCommand: func(_ context.Context, raw json.RawMessage) (any, error) {
			var a control.LogArgs
			if err := decodeArgs(raw, &a); err != nil {
				return nil, err
			}
			text, err := eng.Log(a.ID)
			if err != nil {
				if errors.Is(err, engine.ErrNoSuchRun) {
					return nil, runsAnswer(err)
				}
				return nil, errors.New("the run log cannot be read")
			}
			// Run logs are sanitized line by line when written; the line breaks must survive.
			return control.LogResult{Text: text}, nil
		},
	}
	for name, h := range commands {
		if err := r.Register(name, h); err != nil {
			return err
		}
	}
	return nil
}

// holdHandler serves a command that sets or clears an operator mark.
func holdHandler(set func(context.Context, string, bool) (bool, error), on bool) control.Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var a control.HoldArgs
		if err := decodeArgs(raw, &a); err != nil {
			return nil, err
		}
		changed, err := set(ctx, a.Stack, on)
		switch {
		case err == nil:
			return control.HoldResult{Stack: printable(a.Stack), Active: on, Changed: changed}, nil
		case errors.Is(err, engine.ErrUnknownStack), errors.Is(err, engine.ErrNoRepo):
			return nil, runsAnswer(err)
		}
		return nil, errors.New("the engine state cannot be written")
	}
}

func engineStatus(snap engine.Snapshot) control.EngineStatus {
	st := snap.State
	res := control.EngineStatus{
		LastPoll: st.LastPoll, PollError: printable(st.PollError), Note: printable(st.Note),
		Head: printable(st.Head), RemoteHead: printable(st.RemoteHead), Stacks: []control.StackStatus{},
	}
	names := make([]string, 0, len(st.Stacks))
	for n := range st.Stacks {
		names = append(names, n)
	}
	for n := range snap.Active {
		if _, ok := st.Stacks[n]; !ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	names = slices.Compact(names)
	for _, n := range names {
		s := st.Stacks[n]
		item := control.StackStatus{
			Name: printable(n), Deployed: printable(s.Deployed), Pending: printable(s.Pending),
			ActiveRun: printable(snap.Active[n]), Findings: []control.FindingInfo{}, Removed: s.Removed,
		}
		if s.LastRun != nil {
			item.LastRun = printable(s.LastRun.Result)
		}
		if s.Paused != nil {
			p := pauseInfo(*s.Paused)
			item.Paused = &p
		}
		if s.OperatorPause != nil {
			item.OperatorPause = &control.HoldInfo{Since: s.OperatorPause.Since}
		}
		if s.Pin != nil {
			item.Pinned = &control.HoldInfo{Since: s.Pin.Since}
		}
		if s.Stopped != nil {
			item.Stopped = &control.HoldInfo{Since: s.Stopped.Since}
		}
		for _, f := range s.Findings {
			info := control.FindingInfo{Kind: printable(f.Kind), Detail: printable(f.Detail)}
			for _, d := range f.Dirs {
				// Validated paths pass unchanged: a client acts on them, so they are never shortened.
				if engine.ValidDirPath(d.Path) {
					info.Dirs = append(info.Dirs, control.DirInfo{Path: d.Path, Owner: d.Owner})
				}
			}
			item.Findings = append(item.Findings, info)
		}
		if f := s.Failure; f != nil {
			detail := "since " + f.Since.UTC().Format(time.RFC3339) + ": " + f.Kind
			if f.Container != "" {
				detail += " " + f.Container
			}
			item.Findings = append(item.Findings, control.FindingInfo{Kind: engine.FindingContainerFailed, Detail: printable(detail)})
		}
		res.Stacks = append(res.Stacks, item)
	}
	return res
}

func pauseInfo(p engine.Pause) control.PauseInfo {
	return control.PauseInfo{Since: p.Time, Run: printable(p.Run), Reason: printable(p.Reason)}
}
