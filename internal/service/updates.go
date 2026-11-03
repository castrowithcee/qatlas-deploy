package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/registry"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
	"github.com/castrowithcee/qatlas-deploy/internal/updates"
)

// registryCredential returns the login for a registry host. Registry logins are not configurable yet, so
// every registry is asked anonymously. This is the one place that decides which credential a host gets.
func registryCredential(string) (registry.Credential, error) { return registry.Credential{}, nil }

type updatesService struct {
	repo     *repoService
	eng      *engine.Engine
	detector *updates.Detector
}

func registerUpdateCommands(r *control.Registry, repo *repoService, eng *engine.Engine, opts registry.Options) error {
	s := &updatesService{repo: repo, eng: eng, detector: &updates.Detector{Credential: registryCredential, Options: opts}}
	return r.Register(control.UpdatesListCommand, s.handleList)
}

func (s *updatesService) handleList(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.UpdatesArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	s.repo.mu.Lock()
	c, err := s.repo.bound()
	var list []stacks.Stack
	if err == nil {
		list, err = stacks.Load(c.Dir)
		if err != nil {
			err = control.NewError(control.CodeNoRepo, "the working copy cannot be read; run 'qdeploy repo set' again")
		}
	}
	s.repo.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if a.Stack != "" {
		i := slices.IndexFunc(list, func(st stacks.Stack) bool { return st.Name == a.Stack })
		switch {
		case i < 0:
			return nil, runsAnswer(engine.ErrUnknownStack)
		case list[i].Err != nil:
			return nil, runsAnswer(engine.ErrUnreadable)
		}
		list = list[i : i+1]
	}

	var fresh map[string]engine.UpdateCheck
	if a.Refresh {
		rctx, cancel := context.WithTimeout(ctx, control.UpdatesRefreshTimeout)
		defer cancel()
		if fresh, err = s.detector.Check(rctx, list); err != nil {
			return nil, control.NewError(control.CodeBusy, "the update check did not finish in time")
		}
		for name, check := range fresh {
			if err := s.eng.RecordUpdates(name, check); err != nil {
				return nil, errors.New("the engine state cannot be written")
			}
		}
	}
	snap, err := s.eng.Snapshot()
	if err != nil {
		return nil, errors.New("the engine state cannot be read")
	}

	// Everything below comes from the working copy and the state and may reach a terminal.
	res := control.UpdatesResult{Stacks: make([]control.StackUpdates, 0, len(list))}
	for _, st := range list {
		info := control.StackUpdates{Name: printable(st.Name), Update: st.UpdateMode(), Items: []control.UpdateInfo{}}
		if st.Err != nil {
			info.Error = printable(st.Err.Error())
		}
		check, ok := fresh[st.Name]
		if !ok && snap.State.Stacks[st.Name].Updates != nil {
			check, ok = *snap.State.Stacks[st.Name].Updates, true
		}
		if ok {
			info.Checked = check.Checked
			for _, it := range check.Items {
				info.Items = append(info.Items, control.UpdateInfo{
					Variable: printable(it.Variable), Image: printable(it.Image), Current: printable(it.Current),
					Selected: printable(it.Selected), MajorAvailable: printable(it.MajorAvailable),
					Status: printable(it.Status), Error: printable(it.Error),
				})
			}
		}
		res.Stacks = append(res.Stacks, info)
	}
	return res, nil
}
