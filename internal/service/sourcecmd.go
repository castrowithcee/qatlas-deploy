package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// sourceAddFix is what the answer to a folder the service cannot write tells the operator.
const sourceAddFix = "run 'sudo qdeploy source add' to grant the service access"

func registerSourceCommands(r *control.Registry, s *inspectService) error {
	commands := map[string]control.Handler{
		control.SourceAddCommand:    s.handleAdd,
		control.SourceListCommand:   s.handleList,
		control.SourceRemoveCommand: s.handleRemove,
	}
	for name, h := range commands {
		if err := r.Register(name, h); err != nil {
			return err
		}
	}
	return nil
}

// handleAdd binds a repository without a remote as a local source. The folder is classified like source.inspect
// does it; any other case is refused, and so is a folder the service cannot write, because it never changes
// permissions itself.
func (s *inspectService) handleAdd(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.SourceAddArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	set := s.repo.set
	s.repo.setMu.Lock()
	defer s.repo.setMu.Unlock()
	v, access, err := s.classify(ctx, control.SourceInspectArgs{Path: a.Path})
	if err != nil {
		return nil, err
	}
	switch v.kase {
	case control.SourceCaseLocal:
	case control.SourceCaseReject:
		return nil, control.NewError(control.CodeInvalid, fmt.Sprintf("%s. %s", v.reason, v.fix))
	case control.SourceCaseClone:
		return nil, control.NewError(control.CodeInvalid,
			"the folder is missing or empty, so there is nothing to bind; clone a repository into it with 'qdeploy repo set'")
	case control.SourceCaseAdopt:
		return nil, control.NewError(control.CodeInvalid,
			"the folder is a clone with a remote, which runs against that remote; bind it with 'qdeploy repo set'")
	default:
		return nil, control.NewError(control.CodeInvalid,
			"the folder holds files and no repository; source add does not start a repository")
	}
	if !access.ServiceCanWrite || access.GitWritable == nil || !*access.GitWritable {
		return nil, control.NewError(control.CodeInvalid, fmt.Sprintf(
			"the service cannot write the folder and its .git folder; %s", sourceAddFix))
	}
	conf, rev, err := set.load()
	if err != nil {
		return nil, err
	}
	c, err := set.newSourceConfig(conf, "", a.Path)
	if err != nil {
		return nil, err
	}
	wc, err := set.git.Open(set.gitConfig(c), a.Path)
	if err != nil {
		return nil, gitFailure(err)
	}
	head, err := wc.Head(ctx)
	if err == nil {
		_, err = wc.RemoteHead(ctx)
	}
	if err != nil {
		return nil, control.NewError(control.CodeInvalid,
			"the repository has no commit on a checked-out branch; make a first commit on a branch and try again")
	}
	if _, err := set.add(c, conf, rev); err != nil {
		return nil, err
	}
	return control.SourceAddResult{ID: c.ID, Path: c.Path, Kind: c.Kind, Head: head}, nil
}

func (s *inspectService) handleList(_ context.Context, raw json.RawMessage) (any, error) {
	var none struct{}
	if err := decodeArgs(raw, &none); err != nil {
		return nil, err
	}
	snaps, err := s.repo.set.snapshots()
	if err != nil {
		return nil, err
	}
	return control.SourceListResult{Sources: engineStatus(snaps).Sources}, nil
}

// handleRemove releases a source; it changes nothing in the folder.
func (s *inspectService) handleRemove(_ context.Context, raw json.RawMessage) (any, error) {
	var a control.SourceRemoveArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if err := checkDirShape(a.Path); err != nil {
		return nil, control.NewError(control.CodeInvalid, err.Error())
	}
	set := s.repo.set
	s.repo.setMu.Lock()
	defer s.repo.setMu.Unlock()
	src := set.byPath(a.Path)
	if src == nil {
		return nil, control.NewError(control.CodeNotFound, "no source is bound to this folder")
	}
	conf, rev, err := set.load()
	if err != nil {
		return nil, err
	}
	if err := set.remove(src, conf, rev); err != nil {
		return nil, err
	}
	return control.SourceRemoveResult{ID: src.conf.ID, Path: src.conf.Path}, nil
}
