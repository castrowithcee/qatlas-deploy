package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-deploy/internal/changelog"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/registry"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
	"github.com/castrowithcee/qatlas-deploy/internal/updates"
)

// dockerHubAPIHost is the host registry.ParseRepo gives Docker Hub images.
const dockerHubAPIHost = "registry-1.docker.io"

// registryCredentialName derives the credential name for a registry host: "registry." plus the lowercase
// host with the port separator written as '-'. Docker Hub uses "registry.docker.io". ok is false when the
// result is no valid credential name.
func registryCredentialName(host string) (string, bool) {
	host = strings.ToLower(host)
	if host == dockerHubAPIHost {
		host = "docker.io"
	}
	name := "registry." + strings.ReplaceAll(host, ":", "-")
	return name, credentials.ValidName(name)
}

var errRegistryLogin = errors.New("the stored registry login is malformed")

// parseRegistryLogin splits "<user>:<token>" at the first ':'; both parts must be non-empty. The error
// never contains the value.
func parseRegistryLogin(value string) (registry.Credential, error) {
	user, token, ok := strings.Cut(value, ":")
	if !ok || user == "" || token == "" {
		return registry.Credential{}, errRegistryLogin
	}
	return registry.Credential{User: user, Token: token}, nil
}

// registryCredentialFrom returns the lookup that decides which credential a registry host gets: none stored
// or no valid name means anonymous; an unreadable or malformed credential is an error.
func registryCredentialFrom(store *credentials.Store) func(string) (registry.Credential, error) {
	return func(host string) (registry.Credential, error) {
		name, ok := registryCredentialName(host)
		if !ok {
			return registry.Credential{}, nil
		}
		value, err := store.Value(name)
		if errors.Is(err, credentials.ErrNotFound) {
			return registry.Credential{}, nil
		}
		if err != nil {
			return registry.Credential{}, err
		}
		return parseRegistryLogin(value)
	}
}

type updatesService struct {
	repo      *repoService
	eng       *engine.Engine
	detector  *updates.Detector
	changelog *changelogSource
}

func registerUpdateCommands(r *control.Registry, repo *repoService, eng *engine.Engine, store *credentials.Store, opts registry.Options, clog changelog.Options) error {
	cs := newChangelogSource(store, opts, clog, filepath.Join(repo.stateDir, changelogCacheDir))
	cs.clean()
	s := &updatesService{repo: repo, eng: eng, detector: &updates.Detector{Credential: registryCredentialFrom(store), Options: opts}, changelog: cs}
	if err := r.Register(control.UpdatesListCommand, s.handleList); err != nil {
		return err
	}
	return r.Register(control.ChangelogShowCommand, s.handleChangelog)
}

// loadStacks reads the working copy and narrows it to the named stack; an empty name keeps all of them.
func (s *updatesService) loadStacks(name string) ([]stacks.Stack, error) {
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
	if name != "" {
		i := slices.IndexFunc(list, func(st stacks.Stack) bool { return st.Name == name })
		switch {
		case i < 0:
			return nil, runsAnswer(engine.ErrUnknownStack)
		case list[i].Err != nil:
			return nil, runsAnswer(engine.ErrUnreadable)
		}
		list = list[i : i+1]
	}
	return list, nil
}

func (s *updatesService) handleChangelog(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.ChangelogArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Stack == "" {
		return nil, control.NewError(control.CodeBadRequest, "a stack name is required")
	}
	list, err := s.loadStacks(a.Stack)
	if err != nil {
		return nil, err
	}
	snap, err := s.eng.Snapshot()
	if err != nil {
		return nil, errors.New("the engine state cannot be read")
	}
	res := control.ChangelogResult{Stack: printable(list[0].Name), Items: []control.ChangelogInfo{}}
	if check := snap.State.Stacks[list[0].Name].Updates; check != nil {
		res.Checked = check.Checked
		cctx, cancel := context.WithTimeout(ctx, control.ChangelogTimeout)
		defer cancel()
		res.Items = s.changelog.forStack(cctx, list[0], *check, &changelogBudget{left: maxChangelogBytes})
	}
	return res, nil
}

func (s *updatesService) handleList(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.UpdatesArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	list, err := s.loadStacks(a.Stack)
	if err != nil {
		return nil, err
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
	var budget *changelogBudget
	cctx := ctx
	if a.Changelog {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, control.ChangelogTimeout)
		defer cancel()
		budget = &changelogBudget{left: maxChangelogBytes}
	}
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
			if a.Changelog {
				info.Changelogs = s.changelog.forStack(cctx, st, check, budget)
			}
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
