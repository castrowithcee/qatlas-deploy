package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/castrowithcee/qatlas-kit/docker"
	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/stackcheck"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const (
	// maxInspectPath is PATH_MAX; a longer path cannot exist.
	maxInspectPath = 4096
	// maxInspectEntries bounds the directory entries read while looking for unreadable folders.
	maxInspectEntries = 1 << 20
	// accessWriteSearch is W_OK|X_OK: what creating entries in a folder takes.
	accessWriteSearch = 0x3
	accessRead        = 0x4
)

// inspectService answers source.inspect. It reads the folder, the git repository in it and the container
// engine, and changes nothing: the only thing it writes is a temporary directory outside the folder.
type inspectService struct {
	repo   *repoService
	docker Docker
	log    *slog.Logger
}

func registerInspectCommands(r *control.Registry, repo *repoService, d Docker, log *slog.Logger) error {
	s := &inspectService{repo: repo, docker: d, log: log}
	if err := r.Register(control.SourceInspectCommand, s.handle); err != nil {
		return err
	}
	return registerSourceCommands(r, s)
}

// verdict is the outcome of looking at a folder: a case, or a rejection with its reason and fix.
type verdict struct {
	kase, url, reason, fix string
	needsURL               bool
	// view and remote are what the stack check asks git; set for the cases that bring stacks.
	view   stackcheck.Git
	remote bool
	// plan is set for a folder without a repository.
	plan *filePlan
	// list and loadErr are the stacks of the folder when the verdict had to read them already.
	list    []stacks.Stack
	loadErr error
	loaded  bool
	// repo is the folder's .git, set for an existing repository.
	repo bool
}

func rejected(reason, fix string) verdict {
	return verdict{kase: control.SourceCaseReject, reason: reason, fix: fix}
}

func (s *inspectService) handle(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.SourceInspectArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	res := control.SourceInspectResult{Path: maskControl(a.Path), Stacks: []control.SourceStack{}}
	v, access, err := s.classify(ctx, a)
	if err != nil {
		return nil, err
	}
	if v.plan != nil {
		defer v.plan.close()
	}
	res.Case, res.URL, res.Reason, res.Fix, res.NeedsURL, res.Access = v.kase, v.url, v.reason, v.fix, v.needsURL, access
	if v.plan != nil {
		res.PlannedFiles = v.plan.planned
		res.Excluded = v.plan.excluded
	}
	if v.view != nil {
		if !v.loaded {
			v.list, v.loadErr = stacks.Load(a.Path)
		}
		if err := s.addStacks(ctx, &res, v, a.Path); err != nil {
			return nil, err
		}
	}
	if err := fitAnswer(res, "the folder holds too many files to list in one answer; move data out of it or into a bind-mounted data folder"); err != nil {
		return nil, err
	}
	return res, nil
}

// classify decides the case of the folder and gathers its permissions. A rejection has no permissions.
func (s *inspectService) classify(ctx context.Context, a control.SourceInspectArgs) (verdict, *control.SourceAccess, error) {
	v, err := s.decide(ctx, a)
	if err != nil || v.kase == control.SourceCaseReject {
		return v, nil, err
	}
	target := a.Path
	if v.kase == control.SourceCaseClone {
		// A missing folder is created below its nearest existing parent.
		for {
			if _, err := os.Lstat(target); err == nil {
				break
			}
			target = filepath.Dir(target)
		}
	}
	acc, err := accessOf(target)
	if err != nil {
		return verdict{}, nil, err
	}
	if v.repo {
		w := syscall.Access(filepath.Join(a.Path, ".git"), accessWriteSearch) == nil
		acc.GitWritable = &w
	}
	return v, acc, nil
}

func (s *inspectService) decide(ctx context.Context, a control.SourceInspectArgs) (verdict, error) {
	if a.URL != "" {
		if err := gitrepo.ValidateURL(a.URL); err != nil {
			return rejected("the repository address is not a supported SSH address",
				"Use user@host:path or ssh://[user@]host[:port]/path."), nil
		}
	}
	if err := checkDirShape(a.Path); err != nil || len(a.Path) > maxInspectPath {
		return rejected("the path must be absolute, normalized and made of letters, digits and ._/@+-",
			"Give the folder as an absolute path without '..', '.', '//' or a trailing slash."), nil
	}
	if bad, isRejected := lstatChain(a.Path); isRejected {
		return bad, nil
	}
	if s.repo.checkDirOutsideState(a.Path) != nil {
		return rejected("the folder contains or lies inside the service's own state directory",
			"Choose a folder elsewhere."), nil
	}
	for _, src := range s.repo.set.sources() {
		if overlaps(a.Path, src.conf.Path) {
			return rejected(fmt.Sprintf("the folder is, contains or lies inside the stack folder %s, which is already a source", printable(src.conf.Path)),
				"Choose a folder that does not overlap a stack folder that is in use."), nil
		}
	}
	for dir := filepath.Dir(a.Path); ; dir = filepath.Dir(dir) {
		_, err := os.Lstat(filepath.Join(dir, ".git"))
		switch {
		case err == nil:
			return rejected(fmt.Sprintf("the folder is part of another repository, whose root is %s", printable(dir)),
				"Choose a folder outside every repository, or use the root of that repository."), nil
		case !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR):
			return rejected(fmt.Sprintf("the service cannot look for a repository in %s", printable(dir)),
				"Give the service search permission on every folder of the path, or choose another folder."), nil
		}
		if dir == "/" {
			break
		}
	}

	entries, err := os.ReadDir(a.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return verdict{kase: control.SourceCaseClone, url: a.URL, needsURL: a.URL == ""}, nil
	case err != nil:
		return rejected("the service cannot read the folder",
			"Give the service read permission on the folder, or choose another folder."), nil
	case len(entries) == 0:
		return verdict{kase: control.SourceCaseClone, url: a.URL, needsURL: a.URL == ""}, nil
	}

	gi, err := os.Lstat(filepath.Join(a.Path, ".git"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s.decideFiles(ctx, a)
	case err != nil:
		return rejected("the service cannot inspect the .git entry of the folder",
			"Give the service read permission on the folder, or choose another folder."), nil
	case !gi.IsDir():
		return rejected(".git in the folder is not a folder (a linked worktree, a submodule or a link)",
			"Use the main working copy of the repository, or a folder with a repository of its own."), nil
	}
	return s.decideRepo(ctx, a)
}

// lstatChain walks the path from the root without following links. It returns a rejection (and true) when a
// component is a link, not a folder or cannot be inspected; a missing component ends the walk without one.
func lstatChain(p string) (verdict, bool) {
	cur := "/"
	for _, part := range strings.Split(p[1:], "/") {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return verdict{}, false
		case err != nil:
			return rejected(fmt.Sprintf("the service cannot inspect %s", printable(cur)),
				"Give the service search permission on every folder of the path, or choose another folder."), true
		case info.Mode()&fs.ModeSymlink != 0:
			where := "the folder"
			if cur != p {
				where = "a folder above it"
			}
			return rejected(fmt.Sprintf("%s is a symbolic link (%s); a link is never followed", where, printable(cur)),
				"Give the real path of the folder."), true
		case !info.IsDir() && cur == p:
			return rejected("the path is not a folder", "Choose a folder, or a path that does not exist yet."), true
		case !info.IsDir():
			return rejected(fmt.Sprintf("%s is not a folder", printable(cur)), "Choose a path below existing folders."), true
		}
	}
	return verdict{}, false
}

func (s *inspectService) decideRepo(ctx context.Context, a control.SourceInspectArgs) (verdict, error) {
	unreadable := func(err error) verdict {
		return rejected("git cannot read the repository in the folder: "+printable(err.Error()),
			"Repair the repository, or choose another folder.")
	}
	// The key is never used: nothing here reaches the remote.
	wc, err := s.repo.set.git.Open(s.repo.set.gitConfig(sourceConfig{Key: credentials.DefaultKey}), a.Path)
	if err != nil {
		return unreadable(err), nil
	}
	names, err := wc.Remotes(ctx)
	if err != nil {
		return unreadable(err), nil
	}
	switch {
	case slices.Contains(names, "origin"):
		url, err := wc.RemoteURL(ctx)
		if err != nil {
			return unreadable(err), nil
		}
		if err := gitrepo.ValidateURL(url); err != nil {
			return rejected("the origin remote of the repository is not a supported SSH address",
				"Set origin to user@host:path or ssh://[user@]host[:port]/path, or remove the remote to run the folder locally."), nil
		}
		if a.URL != "" && a.URL != url {
			return rejected("the given address differs from the origin remote of the repository",
				"Give the address the repository already has as origin, or leave the address out."), nil
		}
		return verdict{kase: control.SourceCaseAdopt, url: url, view: wc, remote: true, repo: true}, nil
	case len(names) > 0:
		return rejected("the repository has remotes but none is called origin",
			"Rename the remote to origin, or remove the remotes to run the folder locally."), nil
	case a.URL != "":
		return rejected("the folder is a repository without a remote, and source add cannot add one",
			"Leave the address out to run the folder locally, or add the origin remote yourself."), nil
	}
	return verdict{kase: control.SourceCaseLocal, view: wc, repo: true}, nil
}

func (s *inspectService) decideFiles(ctx context.Context, a control.SourceInspectArgs) (verdict, error) {
	if a.URL != "" {
		return rejected("the folder holds files and no repository, and source add cannot connect it to an address",
			"Leave the address out to start a local repository from the files."), nil
	}
	// The plan needs the stacks for their bind mounts; a folder whose stacks cannot be read has none.
	list, loadErr := stacks.Load(a.Path)
	plan, err := planFiles(ctx, a.Path, list)
	if err != nil {
		return verdict{}, err
	}
	return verdict{kase: control.SourceCaseFiles, view: plan, plan: plan, list: list, loadErr: loadErr, loaded: true}, nil
}

// filePlan is what a first commit of a folder without a repository would hold, and the git view of it: the
// planned files count as tracked, what is left out or ignored counts as ignored.
type filePlan struct {
	planned  []string
	excluded []control.SourceExcluded
	// rawPlanned and rawExcluded are planned and the paths of excluded as they are on disk, in the same order;
	// the lists above show them with unprintable characters replaced.
	rawPlanned, rawExcluded []string
	preview                 *gitrepo.Preview
}

func (p *filePlan) close() { _ = p.preview.Close() }

func (p *filePlan) TrackedFiles(_ context.Context, dir string) ([]string, error) {
	if dir == stacks.RootFolder {
		return p.planned, nil
	}
	var out []string
	for _, f := range p.planned {
		if strings.HasPrefix(f, dir+"/") {
			out = append(out, f)
		}
	}
	return out, nil
}

func (p *filePlan) IsIgnored(ctx context.Context, rel string) (bool, error) {
	for _, e := range p.excluded {
		if underPath(e.Path, rel) {
			return true, nil
		}
	}
	return p.preview.IsIgnored(ctx, rel)
}

// underPath reports whether rel is root or lies below it; both are clean paths relative to the folder.
func underPath(root, rel string) bool { return rel == root || strings.HasPrefix(rel, root+"/") }

func planFiles(ctx context.Context, root string, list []stacks.Stack) (*filePlan, error) {
	var excluded []control.SourceExcluded
	roots := bindRoots(root, list)
	slices.SortFunc(roots, func(a, b control.SourceExcluded) int { return strings.Compare(a.Path, b.Path) })
	for _, ex := range roots {
		if !slices.ContainsFunc(excluded, func(o control.SourceExcluded) bool { return underPath(o.Path, ex.Path) }) {
			excluded = append(excluded, ex)
		}
	}
	hints := make([]string, len(excluded))
	for i, e := range excluded {
		hints[i] = e.Path
	}
	pv, err := gitrepo.OpenPreview(ctx, gitrepo.Config{Local: true}, root, hints)
	if err != nil {
		return nil, gitFailure(err)
	}
	plan := &filePlan{preview: pv, planned: []string{}}
	ok := false
	defer func() {
		if !ok {
			plan.close()
		}
	}()
	listed, err := pv.Untracked(ctx)
	if errors.Is(err, gitrepo.ErrOutputLimit) {
		return nil, control.NewError(control.CodeTooLarge, "the folder holds too many files to list; move data out of it or into a bind-mounted data folder")
	}
	if err != nil {
		return nil, gitFailure(err)
	}
	bind := slices.Clone(excluded)
	for _, f := range listed {
		if slices.ContainsFunc(bind, func(o control.SourceExcluded) bool { return underPath(o.Path, strings.TrimSuffix(f, "/")) }) {
			continue
		}
		if nested, isDir := strings.CutSuffix(f, "/"); isDir {
			excluded = append(excluded, control.SourceExcluded{Path: nested, Reason: control.SourceExcludedNestedRepo})
			continue
		}
		full := filepath.Join(root, f)
		info, err := os.Lstat(full)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			excluded = append(excluded, control.SourceExcluded{Path: f, Reason: control.SourceExcludedUnreadable})
		case !info.Mode().IsRegular():
			excluded = append(excluded, control.SourceExcluded{Path: f, Reason: control.SourceExcludedNotRegular})
		case info.Size() > stacks.MaxFileSize:
			excluded = append(excluded, control.SourceExcluded{Path: f, Reason: control.SourceExcludedTooLarge})
		case syscall.Access(full, accessRead) != nil:
			excluded = append(excluded, control.SourceExcluded{Path: f, Reason: control.SourceExcludedUnreadable})
		default:
			plan.planned = append(plan.planned, maskControl(f))
			plan.rawPlanned = append(plan.rawPlanned, f)
		}
	}

	// Git passes over a folder it cannot open and over a pipe, socket or device without a word, so they are
	// looked for here.
	odd, err := oddEntries(ctx, root, excluded)
	if err != nil {
		return nil, err
	}
	for _, o := range odd {
		ignored, err := pv.IsIgnored(ctx, o.Path)
		if err != nil {
			return nil, gitFailure(err)
		}
		if !ignored {
			excluded = append(excluded, o)
		}
	}
	slices.SortFunc(excluded, func(a, b control.SourceExcluded) int { return strings.Compare(a.Path, b.Path) })
	for i := range excluded {
		plan.rawExcluded = append(plan.rawExcluded, excluded[i].Path)
		excluded[i].Path = maskControl(excluded[i].Path)
	}
	plan.excluded = excluded
	ok = true
	return plan, nil
}

// bindRoots lists the sources of the stacks' bind mounts that exist in the folder, as paths below it.
func bindRoots(root string, list []stacks.Stack) []control.SourceExcluded {
	var out []control.SourceExcluded
	for _, st := range list {
		for _, bm := range st.BindMounts {
			rel := st.RepoPath(bm)
			if existsNoFollow(root, rel) && !slices.ContainsFunc(out, func(o control.SourceExcluded) bool { return o.Path == rel }) {
				out = append(out, control.SourceExcluded{Path: rel, Reason: control.SourceExcludedBindMount, Stack: printable(st.Name)})
			}
		}
	}
	return out
}

// existsNoFollow tells whether rel exists below root, without following a link on the way.
func existsNoFollow(root, rel string) bool {
	cur := root
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return false
		}
		if i < len(parts)-1 && !info.IsDir() {
			return false
		}
	}
	return true
}

var errTooManyEntries = control.NewError(control.CodeTooLarge, "the folder holds too many entries to inspect")

// oddEntries returns what git does not list and the first commit cannot take, as paths below root: folders the
// service cannot list and entries that are neither files, folders nor links. It does not look into folders that
// are excluded already or hold a repository of their own, and never follows a link.
func oddEntries(ctx context.Context, root string, skip []control.SourceExcluded) ([]control.SourceExcluded, error) {
	var out []control.SourceExcluded
	count := 0
	var walk func(rel string) error
	walk = func(rel string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := os.ReadDir(filepath.Join(root, rel))
		if err != nil {
			if rel != "" {
				out = append(out, control.SourceExcluded{Path: rel, Reason: control.SourceExcludedUnreadable})
			}
			return nil
		}
		if count += len(entries); count > maxInspectEntries {
			return errTooManyEntries
		}
		for _, e := range entries {
			child := path.Join(rel, e.Name())
			if slices.ContainsFunc(skip, func(o control.SourceExcluded) bool { return underPath(o.Path, child) }) {
				continue
			}
			switch typ := e.Type(); {
			case typ.IsDir():
				if _, err := os.Lstat(filepath.Join(root, child, ".git")); err == nil {
					continue
				}
				if err := walk(child); err != nil {
					return err
				}
			case !typ.IsRegular() && typ&fs.ModeSymlink == 0:
				out = append(out, control.SourceExcluded{Path: child, Reason: control.SourceExcludedNotRegular})
			}
		}
		return nil
	}
	if err := walk(""); err != nil {
		return nil, err
	}
	return out, nil
}

// accessOf reads the owner and mode of a folder, and what the service may do there.
func accessOf(target string) (*control.SourceAccess, error) {
	info, err := os.Lstat(target)
	if err != nil {
		return nil, control.NewError(control.CodeInvalid, "the permissions of the folder cannot be read")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, control.NewError(control.CodeInvalid, "the permissions of the folder cannot be read")
	}
	acc := &control.SourceAccess{
		ModePath: target, OwnerID: st.Uid, GroupID: st.Gid, Mode: fmt.Sprintf("%04o", st.Mode&0o7777),
		ServiceCanWrite: syscall.Access(target, accessWriteSearch) == nil,
	}
	acc.SetsPermissions = !acc.ServiceCanWrite
	if u, err := user.LookupId(strconv.FormatUint(uint64(st.Uid), 10)); err == nil {
		acc.Owner = printable(u.Username)
	}
	if g, err := user.LookupGroupId(strconv.FormatUint(uint64(st.Gid), 10)); err == nil {
		acc.Group = printable(g.Name)
	}
	return acc, nil
}

// addStacks reads the stacks of the folder, checks each against the git view and looks for containers of
// other Compose projects in their folders.
func (s *inspectService) addStacks(ctx context.Context, res *control.SourceInspectResult, v verdict, root string) error {
	list := v.list
	if v.loadErr != nil {
		res.StacksError = "the stacks of the folder cannot be read: " + printable(v.loadErr.Error())
		return nil
	}
	reports, err := stackcheck.Check(ctx, list, v.view, v.remote)
	if err != nil {
		return gitFailure(err)
	}
	// Values of the env files must not reach the answer, however a rule's text came to quote them.
	red := &redact.Redactor{}
	for _, st := range list {
		registerStackSecrets(red, st)
		for _, val := range st.Env {
			red.Add(val)
		}
	}
	text := func(t string) string { return printable(red.Apply(t)) }
	for i, rep := range reports {
		out := control.SourceStack{
			Name: printable(rep.Stack), Folder: printable(list[i].Folder),
			Functions: make([]control.StackFunction, 0, len(rep.Functions)), Findings: make([]control.CheckFinding, 0, len(rep.Findings)),
		}
		for _, bm := range bindRoots(root, list[i:i+1]) {
			out.BindMounts = append(out.BindMounts, maskControl(bm.Path))
		}
		for _, f := range rep.Functions {
			out.Functions = append(out.Functions, control.StackFunction{
				Function: f.Name, Works: f.Works, Mode: f.Mode, Reason: text(f.Reason), Action: text(f.Action),
			})
		}
		for _, f := range rep.Findings {
			out.Findings = append(out.Findings, control.CheckFinding{
				Stack: printable(f.Stack), Rule: f.Rule, Severity: f.Severity, Cause: text(f.Cause), Fix: text(f.Fix),
			})
		}
		res.Stacks = append(res.Stacks, out)
	}
	if len(list) == 0 {
		return nil
	}
	conflicts, err := s.conflicts(ctx, list)
	if err != nil {
		res.ContainersError = err.Error()
		return nil
	}
	res.ContainersChecked = true
	for i := range res.Stacks {
		res.Stacks[i].Conflicts = conflicts[i]
	}
	return nil
}

// conflicts finds, per stack of list, the containers that run in the stack's folder under another Compose
// project name than the stack's.
func (s *inspectService) conflicts(ctx context.Context, list []stacks.Stack) (map[int][]control.SourceConflict, error) {
	if s.docker == nil {
		return nil, errors.New("the container engine is not available")
	}
	all, err := s.docker.List(ctx)
	if err != nil {
		var ce *control.Error
		if errors.As((&containerService{log: s.log}).dockerAnswer(err), &ce) {
			return nil, errors.New(ce.Message + "; containers were not checked")
		}
		return nil, errors.New("the container engine could not be asked; containers were not checked")
	}
	projects, _ := docker.Group(all)
	out := map[int][]control.SourceConflict{}
	for _, p := range projects {
		if p.WorkingDir == "" {
			continue
		}
		for i, st := range list {
			if st.Dir == "" || p.Name == st.Name || filepath.Clean(p.WorkingDir) != filepath.Clean(st.Dir) {
				continue
			}
			for _, c := range p.Containers {
				out[i] = append(out[i], control.SourceConflict{Container: maskControl(c.Name), Project: maskControl(p.Name)})
			}
		}
	}
	return out, nil
}
