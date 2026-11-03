package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

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
	repo     *repoService
	eng      *engine.Engine
	detector *updates.Detector
}

func registerUpdateCommands(r *control.Registry, repo *repoService, eng *engine.Engine, store *credentials.Store, opts registry.Options) error {
	s := &updatesService{repo: repo, eng: eng, detector: &updates.Detector{Credential: registryCredentialFrom(store), Options: opts}}
	if err := r.Register(control.UpdatesListCommand, s.handleList); err != nil {
		return err
	}
	return r.Register(control.UpdateRunCommand, s.handleRun)
}

func (s *updatesService) handleList(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.UpdatesArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	list, err := s.stacks()
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

// stacks reads the stacks of the bound working copy.
func (s *updatesService) stacks() ([]stacks.Stack, error) {
	s.repo.mu.Lock()
	defer s.repo.mu.Unlock()
	c, err := s.repo.bound()
	if err != nil {
		return nil, err
	}
	list, err := stacks.Load(c.Dir)
	if err != nil {
		return nil, control.NewError(control.CodeNoRepo, "the working copy cannot be read; run 'qdeploy repo set' again")
	}
	return list, nil
}

func (s *updatesService) handleRun(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.UpdateRunArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if len(a.Stacks) == 0 || len(a.Stacks) > control.MaxUpdateStacks {
		return nil, control.NewError(control.CodeInvalid, "name at least one stack")
	}
	for i, name := range a.Stacks {
		if slices.Contains(a.Stacks[:i], name) {
			return nil, control.NewError(control.CodeInvalid, "a stack is named more than once")
		}
	}

	// Every stack is checked and planned before the first one changes.
	plans := make([][]engine.VersionStep, len(a.Stacks))
	pctx, cancel := context.WithTimeout(ctx, control.UpdatesRefreshTimeout)
	defer cancel()
	for i, name := range a.Stacks {
		st, err := s.eng.CheckUpdate(pctx, name)
		if err == nil {
			plans[i], err = s.detector.Plan(pctx, st, a.Version)
		}
		if err != nil {
			return nil, forStack(name, updateAnswer(pctx, err))
		}
	}

	res := control.UpdateRunResult{Stacks: make([]control.StackUpdateRun, 0, len(a.Stacks))}
	var done []string
	for i, name := range a.Stacks {
		item := control.StackUpdateRun{Name: printable(name), Steps: []control.UpdateStepInfo{}}
		if len(plans[i]) > 0 {
			steps, err := s.eng.Update(ctx, name, plans[i])
			if err != nil {
				var ce *control.Error
				if !errors.As(updateAnswer(ctx, err), &ce) {
					ce = control.NewError(control.CodeInternal, "the update could not be started")
				}
				item.Error = ce.Message
			} else {
				done = append(done, name)
			}
			for _, st := range steps {
				item.Steps = append(item.Steps, control.UpdateStepInfo{
					Variable: printable(st.Variable), From: printable(st.From), To: printable(st.To), Run: printable(st.Run),
					Commit: printable(st.Commit), Result: printable(st.Result), Error: printable(st.Error),
				})
			}
		}
		res.Stacks = append(res.Stacks, item)
	}
	s.recheck(ctx, done)
	return res, nil
}

// recheck replaces the stored detection of the updated stacks, so it shows their new versions. A failure
// leaves the old detection; the next check replaces it.
func (s *updatesService) recheck(ctx context.Context, names []string) {
	if len(names) == 0 {
		return
	}
	list, err := s.stacks()
	if err != nil {
		return
	}
	list = slices.DeleteFunc(list, func(st stacks.Stack) bool { return !slices.Contains(names, st.Name) })
	ctx, cancel := context.WithTimeout(ctx, control.UpdatesRefreshTimeout)
	defer cancel()
	fresh, err := s.detector.Check(ctx, list)
	if err != nil {
		return
	}
	for name, check := range fresh {
		_ = s.eng.RecordUpdates(name, check)
	}
}

// updateAnswer maps the refusals of an update to fixed answers; the texts of the engine's and the planner's
// errors are fixed apart from variable names, which pass printable.
func updateAnswer(ctx context.Context, err error) error {
	var pe *updates.PlanError
	switch {
	case errors.As(err, &pe):
		code := control.CodeNotUpdatable
		switch pe.Kind {
		case updates.PlanVersion:
			code = control.CodeBadVersion
		case updates.PlanRegistry:
			code = control.CodeRegistry
		}
		return control.NewError(code, printable(pe.Error()))
	case errors.Is(err, engine.ErrNotDeployed), errors.Is(err, engine.ErrUndeployed), errors.Is(err, engine.ErrStalePlan):
		return control.NewError(control.CodeNotUpdatable, err.Error())
	case errors.Is(err, engine.ErrCopyChanges):
		return control.NewError(control.CodeLocalChanges, err.Error())
	case ctx.Err() != nil:
		return control.NewError(control.CodeBusy, "the update check did not finish in time")
	}
	return runsAnswer(err)
}

// forStack names the stack in an answer; other errors stay as they are.
func forStack(name string, err error) error {
	var ce *control.Error
	if !errors.As(err, &ce) {
		return err
	}
	return control.NewError(ce.Code, printable(name)+": "+ce.Message)
}
