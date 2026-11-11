package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
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
// does it. A folder with files and no repository is started as one only with the confirmed file list and only
// while that list is what the folder holds; any other case is refused, and so is a folder the service cannot
// write, because it never changes permissions itself.
func (s *inspectService) handleAdd(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.SourceAddArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	s.repo.setMu.Lock()
	defer s.repo.setMu.Unlock()
	v, access, err := s.classify(ctx, control.SourceInspectArgs{Path: a.Path})
	if v.plan != nil {
		defer v.plan.close()
	}
	if err != nil {
		return nil, err
	}
	switch v.kase {
	case control.SourceCaseLocal:
	case control.SourceCaseFiles:
		return s.addFiles(ctx, a, v.plan, access)
	case control.SourceCaseReject:
		return nil, control.NewError(control.CodeInvalid, fmt.Sprintf("%s. %s", v.reason, v.fix))
	case control.SourceCaseClone:
		return nil, control.NewError(control.CodeInvalid,
			"the folder is missing or empty, so there is nothing to bind; clone a repository into it with 'qdeploy repo set'")
	default:
		return nil, control.NewError(control.CodeInvalid,
			"the folder is a clone with a remote, which runs against that remote; bind it with 'qdeploy repo set'")
	}
	if !access.ServiceCanWrite || access.GitWritable == nil || !*access.GitWritable {
		return nil, control.NewError(control.CodeInvalid, fmt.Sprintf(
			"the service cannot write the folder and its .git folder; %s", sourceAddFix))
	}
	id, head, err := s.bindLocal(ctx, a.Path)
	if err != nil {
		return nil, err
	}
	return control.SourceAddResult{ID: id, Path: a.Path, Kind: control.SourceKindLocal, Head: head}, nil
}

// maxListedChanges bounds the paths a refusal names per direction.
const maxListedChanges = 20

// addFiles starts a repository in a folder without one, with exactly the confirmed files, and binds it. It
// plans the files again and refuses, creating nothing, when the folder no longer holds the confirmed list. The
// caller holds setMu and closes plan.
func (s *inspectService) addFiles(ctx context.Context, a control.SourceAddArgs, plan *filePlan, access *control.SourceAccess) (any, error) {
	if len(a.Files) == 0 {
		return nil, control.NewError(control.CodeInvalid,
			"the folder holds files and no repository; confirm the file list first, then source add starts a repository with exactly these files")
	}
	if !access.ServiceCanWrite {
		return nil, control.NewError(control.CodeInvalid, fmt.Sprintf("the service cannot write the folder; %s", sourceAddFix))
	}
	added, removed := listDiff(plan.planned, a.Files)
	if len(added) > 0 || len(removed) > 0 {
		return nil, control.NewError(control.CodeInvalid, fmt.Sprintf(
			"the files of the folder changed since the list was shown (%s); nothing was created, inspect the folder again",
			describeChanges(added, removed)))
	}
	if len(plan.planned) == 0 {
		return nil, control.NewError(control.CodeInvalid, "the folder holds no file the first commit could take")
	}
	// A name with unprintable characters cannot be confirmed by what was shown.
	for i, f := range plan.rawPlanned {
		if f != plan.planned[i] {
			return nil, control.NewError(control.CodeInvalid, fmt.Sprintf(
				"the file name %s contains unprintable characters; rename the file and inspect the folder again", plan.planned[i]))
		}
	}
	for i, f := range plan.rawExcluded {
		if f != plan.excluded[i].Path {
			return nil, control.NewError(control.CodeInvalid, fmt.Sprintf(
				"the name %s contains unprintable characters; rename it and inspect the folder again", plan.excluded[i].Path))
		}
	}
	// Fail before .git exists when the folder cannot be bound.
	conf, _, err := s.repo.set.load()
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.set.newSourceConfig(conf, "", a.Path); err != nil {
		return nil, err
	}
	excluded := make([]string, len(plan.excluded))
	for i, e := range plan.excluded {
		excluded[i] = e.Path
	}
	if _, err := gitrepo.Init(ctx, gitrepo.Config{Local: true}, a.Path, plan.rawPlanned, excluded); err != nil {
		return nil, control.NewError(control.CodeInvalid, fmt.Sprintf("the repository could not be started: %s", printable(err.Error())))
	}
	id, head, err := s.bindLocal(ctx, a.Path)
	if err != nil {
		return nil, err
	}
	return control.SourceAddResult{ID: id, Path: a.Path, Kind: control.SourceKindLocal, Head: head, Files: len(plan.planned)}, nil
}

// bindLocal binds the repository in dir as a local source and returns its id and head.
func (s *inspectService) bindLocal(ctx context.Context, dir string) (id, head string, err error) {
	set := s.repo.set
	conf, rev, err := set.load()
	if err != nil {
		return "", "", err
	}
	c, err := set.newSourceConfig(conf, "", dir)
	if err != nil {
		return "", "", err
	}
	wc, err := set.git.Open(set.gitConfig(c), dir)
	if err != nil {
		return "", "", gitFailure(err)
	}
	head, err = wc.Head(ctx)
	if err == nil {
		_, err = wc.RemoteHead(ctx)
	}
	if err != nil {
		return "", "", control.NewError(control.CodeInvalid,
			"the repository has no commit on a checked-out branch; make a first commit on a branch and try again")
	}
	if _, err := set.add(c, conf, rev); err != nil {
		return "", "", err
	}
	return c.ID, head, nil
}

// listDiff returns the sorted paths of got that are not in want (added) and of want that are not in got (removed).
func listDiff(got, want []string) (added, removed []string) {
	in := func(list []string) map[string]bool {
		m := make(map[string]bool, len(list))
		for _, p := range list {
			m[p] = true
		}
		return m
	}
	gotSet, wantSet := in(got), in(want)
	for p := range gotSet {
		if !wantSet[p] {
			added = append(added, p)
		}
	}
	for p := range wantSet {
		if !gotSet[p] {
			removed = append(removed, p)
		}
	}
	slices.Sort(added)
	slices.Sort(removed)
	return added, removed
}

func describeChanges(added, removed []string) string {
	part := func(n int, word string, list []string) string {
		shown := list[:min(len(list), maxListedChanges)]
		names := make([]string, len(shown))
		for i, p := range shown {
			names[i] = strconv.Quote(printable(p))
		}
		more := ""
		if n > len(shown) {
			more = ", ..."
		}
		return fmt.Sprintf("%d %s: %s%s", n, word, strings.Join(names, ", "), more)
	}
	var parts []string
	if len(added) > 0 {
		parts = append(parts, part(len(added), "added", added))
	}
	if len(removed) > 0 {
		parts = append(parts, part(len(removed), "removed", removed))
	}
	return strings.Join(parts, "; ")
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
