package service

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/castrowithcee/qatlas-kit/configfile"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const (
	sourcesFile    = "sources.json"
	sourcesVersion = 2
	sourcesDir     = "sources"
	kindRemote     = control.SourceKindRemote
	kindLocal      = control.SourceKindLocal
	maxSourceID    = 200

	// legacyFile and legacyEngineDir are the single binding and its engine state of earlier versions.
	legacyFile      = "stacks-repo.json"
	legacyEngineDir = "deploy"
	legacyVersion   = 1
)

// sourceConfig is one stack folder the service deploys from. ID is derived from Path when the source is added
// and never changes: the source's engine state lives below sources/<id>/ and its deploy key is named after it.
// A local source is a repository without a remote: it has neither URL nor deploy key.
type sourceConfig struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
	Key  string `json:"key"`
}

type sourcesConfig struct {
	Version int            `json:"version"`
	Sources []sourceConfig `json:"sources"`
}

// sourceID derives the id of a stack folder: /srv/services/stacks becomes srv-services-stacks.
func sourceID(path string) string { return strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", "-") }

// overlaps reports whether one folder is or contains the other.
func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func validateSources(c sourcesConfig) error {
	if c.Version != sourcesVersion {
		return fmt.Errorf("unsupported version %d", c.Version)
	}
	for i, src := range c.Sources {
		if err := checkDirShape(src.Path); err != nil {
			return err
		}
		if src.ID != sourceID(src.Path) || len(src.ID) > maxSourceID {
			return errors.New("a source id does not match its folder")
		}
		switch src.Kind {
		case kindRemote:
			if src.Key != credentials.DefaultKey && src.Key != credentials.SourceKey(src.ID) {
				return errors.New("a source names a deploy key that is not its own")
			}
			if err := gitrepo.ValidateURL(src.URL); err != nil {
				return err
			}
		case kindLocal:
			if src.URL != "" || src.Key != "" {
				return errors.New("a local source has neither an address nor a deploy key")
			}
		default:
			return fmt.Errorf("unsupported source kind %q", src.Kind)
		}
		for _, other := range c.Sources[:i] {
			if other.ID == src.ID || src.Key != "" && other.Key == src.Key || overlaps(other.Path, src.Path) {
				return errors.New("two sources share an id, a deploy key or a folder")
			}
		}
	}
	return nil
}

func strictDecode[T any](b []byte) (T, error) {
	var c T
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	err := dec.Decode(&c)
	return c, err
}

var sourcesCodec = configfile.Codec[sourcesConfig]{
	Validate: validateSources,
	Encode:   func(c sourcesConfig) ([]byte, error) { return json.MarshalIndent(c, "", "  ") },
	Decode:   strictDecode[sourcesConfig],
}

// legacyConfig is the content of legacyFile.
type legacyConfig struct {
	Version int    `json:"version"`
	URL     string `json:"url"`
	Dir     string `json:"dir"`
}

func readLegacy(stateDir string) (legacyConfig, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, legacyFile))
	if err != nil {
		return legacyConfig{}, err
	}
	c, err := strictDecode[legacyConfig](data)
	switch {
	case err != nil:
		return c, err
	case c.Version != legacyVersion:
		return c, fmt.Errorf("unsupported version %d", c.Version)
	case gitrepo.ValidateURL(c.URL) != nil, checkDirShape(c.Dir) != nil:
		return c, errors.New("invalid binding")
	}
	return c, nil
}

// migrateLegacy turns the single binding of an earlier version into the first source. Every step can be
// repeated: as long as legacyFile exists the migration is unfinished and the next start resumes it. The
// sources file is written first, then the engine state is moved in one rename, and only then is legacyFile
// removed. Whatever does not fit is left in place with a warning, so no state is ever lost.
func migrateLegacy(stateDir string, store *configfile.Store[sourcesConfig], log *slog.Logger) error {
	old, err := readLegacy(stateDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		log.Warn("the old stacks repository binding cannot be read and is left in place", "file", legacyFile)
		return nil
	}
	conf, rev, err := store.LoadVersioned()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		conf = sourcesConfig{Version: sourcesVersion, Sources: []sourceConfig{{
			ID: sourceID(old.Dir), Path: old.Dir, Kind: kindRemote, URL: old.URL, Key: credentials.DefaultKey,
		}}}
		if _, err := store.SaveIfUnchanged(conf, rev); err != nil {
			return fmt.Errorf("write %s: %w", sourcesFile, err)
		}
	case err != nil:
		return fmt.Errorf("read %s: %w", sourcesFile, err)
	}
	i := slices.IndexFunc(conf.Sources, func(c sourceConfig) bool { return c.Path == old.Dir && c.URL == old.URL })
	if i < 0 {
		log.Warn("the old stacks repository binding is not among the sources and is left in place", "file", legacyFile)
		return nil
	}
	from := filepath.Join(stateDir, legacyEngineDir)
	to := filepath.Join(stateDir, sourcesDir, conf.Sources[i].ID, legacyEngineDir)
	if _, err := os.Lstat(from); err == nil {
		if _, err := os.Lstat(to); !errors.Is(err, fs.ErrNotExist) {
			log.Warn("the old engine state is left in place: the source already has a state", "dir", from)
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			return fmt.Errorf("prepare the state of source %s: %w", conf.Sources[i].ID, err)
		}
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("move the engine state to source %s: %w", conf.Sources[i].ID, err)
		}
		syncDir(filepath.Dir(to))
		syncDir(stateDir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect %s: %w", from, err)
	}
	if _, err := store.BackupCopy(filepath.Join(stateDir, legacyFile)); err != nil {
		log.Warn("no backup copy of the old binding was kept", "error", err)
	}
	if err := os.Remove(filepath.Join(stateDir, legacyFile)); err != nil {
		return fmt.Errorf("remove %s: %w", legacyFile, err)
	}
	syncDir(stateDir)
	log.Info("the stacks repository binding became the first source", "source", conf.Sources[i].ID)
	return nil
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// BoundDirs returns the stack folders bound in stateDir, also the one of a binding not migrated yet; nil when
// none is bound or the configuration cannot be read.
func BoundDirs(stateDir string) []string {
	var dirs []string
	if data, err := os.ReadFile(filepath.Join(stateDir, sourcesFile)); err == nil {
		if c, err := sourcesCodec.Decode(data); err == nil && validateSources(c) == nil {
			for _, src := range c.Sources {
				dirs = append(dirs, src.Path)
			}
		}
	}
	if old, err := readLegacy(stateDir); err == nil && !slices.Contains(dirs, old.Dir) {
		dirs = append(dirs, old.Dir)
	}
	return dirs
}

// source is one stack folder with its own engine, state, locks and deploy key.
type source struct {
	conf sourceConfig
	// mu is the working-copy lock, shared with the engine (engine.Config.Lock): it serializes moving and
	// evaluating the working copy. refs is the ref lock (engine.Config.Refs): it serializes fetches and
	// updates of the remote-tracking ref. Whoever needs both takes mu first.
	mu   sync.Mutex
	refs sync.Mutex
	eng  *engine.Engine
	// cancel stops the engine's loop and done closes when it has ended; both are set by start, under the
	// source set's lock, and nil until then.
	cancel context.CancelFunc
	done   chan struct{}
}

// sourceSet holds the sources in the order they were bound and maps stack names to them.
type sourceSet struct {
	stateDir string
	keys     *credentials.Store
	git      Git
	store    *configfile.Store[sourcesConfig]
	// engine is what every source's engine starts from; StateDir, Source, Lock, Refs and Logger are its own.
	engine engine.Config
	log    *slog.Logger

	mu     sync.Mutex // guards list, ctx, closed and wg.Add
	list   []*source
	ctx    context.Context
	closed bool
	wg     sync.WaitGroup
}

func openSources(stateDir string, keys *credentials.Store, git Git, tmpl engine.Config, log *slog.Logger) (*sourceSet, error) {
	store, err := configfile.NewStore(filepath.Join(stateDir, sourcesFile), sourcesCodec)
	if err != nil {
		return nil, err
	}
	if git == nil {
		git = realGit{}
	}
	s := &sourceSet{stateDir: stateDir, keys: keys, git: git, store: store, engine: tmpl, log: log}
	if err := migrateLegacy(stateDir, store, log); err != nil {
		return nil, err
	}
	conf, _, err := s.load()
	if err != nil {
		return nil, err
	}
	for _, c := range conf.Sources {
		src, err := s.newSource(c)
		if err != nil {
			return nil, err
		}
		s.list = append(s.list, src)
	}
	return s, nil
}

// load returns the stored sources; none when the file does not exist yet.
func (s *sourceSet) load() (sourcesConfig, configfile.Revision, error) {
	c, rev, err := s.store.LoadVersioned()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return sourcesConfig{Version: sourcesVersion}, rev, nil
	case err != nil:
		return sourcesConfig{}, rev, errors.New("the source configuration cannot be read")
	}
	return c, rev, nil
}

func (s *sourceSet) newSource(c sourceConfig) (*source, error) {
	src := &source{conf: c}
	cfg := s.engine
	cfg.StateDir = filepath.Join(s.stateDir, sourcesDir, c.ID)
	cfg.Source = func() (engine.Binding, error) { return s.binding(src) }
	cfg.Lock, cfg.Refs = &src.mu, &src.refs
	cfg.Logger = s.log.With("source", c.ID)
	eng, err := engine.New(cfg)
	if err != nil {
		return nil, err
	}
	src.eng = eng
	return src, nil
}

func (s *sourceSet) gitConfig(c sourceConfig) gitrepo.Config {
	if c.Kind == kindLocal {
		return gitrepo.Config{Local: true}
	}
	return gitrepo.Config{KeyFile: s.keys.NamedKeyPath(c.Key), KnownHosts: filepath.Join(s.stateDir, knownHostsFile)}
}

// binding is the engine's view of its source. Blocked is read after the engine has read its stack list, so
// a stack a fast-forward just brought is decided too.
func (s *sourceSet) binding(src *source) (engine.Binding, error) {
	wc, err := s.git.Open(s.gitConfig(src.conf), src.conf.Path)
	if err != nil {
		return engine.Binding{}, err
	}
	return engine.Binding{Dir: src.conf.Path, Copy: wc, Blocked: func() map[string]string { return s.resolve().blocked[src] }}, nil
}

func (s *sourceSet) sources() []*source {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.list)
}

func (s *sourceSet) byPath(dir string) *source {
	for _, src := range s.sources() {
		if src.conf.Path == dir {
			return src
		}
	}
	return nil
}

// keyFor names the deploy key of the stack folder dir: the key of the source bound there, else the key the
// folder had before (its own, when the store holds it), else the default key while no source uses it, else a
// key of the folder's own. exists tells whether the store holds a key; it keeps a folder that was released and is
// bound again from taking the default key, which belongs to another folder by then.
func keyFor(conf sourcesConfig, dir string, exists func(name string) bool) string {
	if i := slices.IndexFunc(conf.Sources, func(c sourceConfig) bool { return c.Path == dir }); i >= 0 {
		return conf.Sources[i].Key
	}
	own := credentials.SourceKey(sourceID(dir))
	if exists(own) {
		return own
	}
	if !slices.ContainsFunc(conf.Sources, func(c sourceConfig) bool { return c.Key == credentials.DefaultKey }) {
		return credentials.DefaultKey
	}
	return own
}

// keyExists tells whether the credential store holds the deploy key name.
func (s *sourceSet) keyExists(name string) bool {
	_, err := s.keys.ShowNamedKey(name)
	return err == nil
}

// newSourceConfig describes a further source at dir, refusing one that clashes with a source bound earlier.
// A local source has no URL and no key.
func (s *sourceSet) newSourceConfig(conf sourcesConfig, url, dir string) (sourceConfig, error) {
	c := sourceConfig{ID: sourceID(dir), Path: dir, Kind: kindLocal}
	if url != "" {
		c = sourceConfig{ID: c.ID, Path: dir, Kind: kindRemote, URL: url, Key: keyFor(conf, dir, s.keyExists)}
	}
	if len(c.ID) > maxSourceID {
		return sourceConfig{}, control.NewError(control.CodeInvalid, "the stack folder path is too long")
	}
	for _, other := range conf.Sources {
		if overlaps(other.Path, dir) {
			return sourceConfig{}, control.NewError(control.CodeInvalid, fmt.Sprintf(
				"the stack folder must not contain or lie inside the stack folder %s bound earlier", other.Path))
		}
		if other.ID == c.ID {
			return sourceConfig{}, control.NewError(control.CodeInvalid, fmt.Sprintf(
				"the stack folder gets the id %s, which the stack folder %s already has", c.ID, other.Path))
		}
	}
	return c, nil
}

// add stores a further source and starts its engine.
func (s *sourceSet) add(c sourceConfig, conf sourcesConfig, rev configfile.Revision) (*source, error) {
	src, err := s.newSource(c)
	if err != nil {
		return nil, err
	}
	conf.Sources = append(slices.Clone(conf.Sources), c)
	if _, err := s.store.SaveIfUnchanged(conf, rev); err != nil {
		return nil, errors.New("the source configuration cannot be written")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list = append(s.list, src)
	if s.ctx != nil && !s.closed {
		s.start(src)
	}
	return src, nil
}

// convert replaces the stored source that has the id of c with c and runs a new engine on the state the old one
// leaves below sources/<id>/, so nothing it has deployed is deployed again. The caller holds neither lock of the
// old source: stopping its engine waits for work that needs them.
func (s *sourceSet) convert(old *source, c sourceConfig, conf sourcesConfig, rev configfile.Revision) (*source, error) {
	next, err := s.newSource(c)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(conf.Sources, func(o sourceConfig) bool { return o.ID == c.ID })
	if i < 0 {
		next.eng.Close()
		return nil, control.NewError(control.CodeNotFound, "no source is bound to this folder")
	}
	conf.Sources = slices.Clone(conf.Sources)
	conf.Sources[i] = c
	if _, err := s.store.SaveIfUnchanged(conf, rev); err != nil {
		next.eng.Close()
		return nil, errors.New("the source configuration cannot be written")
	}
	s.mu.Lock()
	if j := slices.Index(s.list, old); j >= 0 {
		s.list[j] = next
	}
	cancel, done := old.cancel, old.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	old.eng.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx != nil && !s.closed {
		s.start(next)
	}
	return next, nil
}

// remove releases src: it is dropped from the stored sources and the routing, its engine stops, and its state
// below sources/<id>/ is removed. The folder, the containers and the keys are not touched. It refuses while a run
// of one of the source's stacks is active. A run that starts between that check and the stop is cancelled
// with the engine.
func (s *sourceSet) remove(src *source, conf sourcesConfig, rev configfile.Revision) error {
	if snap, err := src.eng.Snapshot(); err != nil {
		return errors.New("the engine state cannot be read")
	} else if len(snap.Active) > 0 {
		return control.NewError(control.CodeRunActive, "a run of a stack of this source is active; remove the source when it has finished")
	}
	conf.Sources = slices.DeleteFunc(slices.Clone(conf.Sources), func(c sourceConfig) bool { return c.ID == src.conf.ID })
	if _, err := s.store.SaveIfUnchanged(conf, rev); err != nil {
		return errors.New("the source configuration cannot be written")
	}
	s.mu.Lock()
	s.list = slices.DeleteFunc(s.list, func(o *source) bool { return o == src })
	cancel, done := src.cancel, src.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	src.eng.Close()
	if err := os.RemoveAll(filepath.Join(s.stateDir, sourcesDir, src.conf.ID)); err != nil {
		return errors.New("the engine state of the source cannot be removed")
	}
	syncDir(filepath.Join(s.stateDir, sourcesDir))
	return nil
}

// run runs the engine of every source, also of those added meanwhile, until ctx ends.
func (s *sourceSet) run(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	for _, src := range s.list {
		s.start(src)
	}
	s.mu.Unlock()
	<-ctx.Done()
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.wg.Wait()
}

// start runs the engine of src until ctx ends or src is removed; the caller holds mu.
func (s *sourceSet) start(src *source) {
	ctx, cancel := context.WithCancel(s.ctx)
	src.cancel, src.done = cancel, make(chan struct{})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(src.done)
		if err := src.eng.Run(ctx); err != nil {
			s.log.Error("engine stopped", "source", src.conf.ID, "error", err)
		}
	}()
}

// stackRef is a stack of the working copy of a source.
type stackRef struct {
	src *source
	st  stacks.Stack
}

// resolution is who owns which stack name across the sources.
type resolution struct {
	// all are the stacks of every readable working copy in source order, owned or not; owned are those that
	// own their name, by name.
	all, owned []stackRef
	// owner maps a name to its source, also a name only a source's state still deploys.
	owner map[string]*source
	// blocked maps a source to its stacks whose name another source owns, with the reason.
	blocked map[*source]map[string]string
	// readable counts the sources whose working copy could be read.
	readable int
}

// resolve decides the owner of every stack name. Compose separates containers, networks and volumes by project
// name only, so a name is deployed from one source alone. A source competes for a name it lists, and for one
// its state still has deployed and not removed, so a working copy that cannot be read for a while loses
// nothing. The deployed stack wins, then one that has run, and among equals the source bound first. The
// decision rests on the persisted state and the source order alone, so it is the same after a restart.
func (s *sourceSet) resolve() resolution {
	r := resolution{owner: map[string]*source{}, blocked: map[*source]map[string]string{}}
	type candidate struct {
		src  *source
		rank int
		st   *stacks.Stack
	}
	cands := map[string][]candidate{}
	for _, src := range s.sources() {
		snap, _ := src.eng.Snapshot()
		rank := func(name string) int {
			st := snap.State.Stacks[name]
			switch {
			case st.Deployed != "":
				return 2
			case st.LastRun != nil || snap.Active[name] != "":
				return 1
			}
			return 0
		}
		list, err := stacks.Load(src.conf.Path)
		if err == nil {
			r.readable++
		}
		listed := map[string]bool{}
		for i := range list {
			st := &list[i]
			listed[st.Name] = true
			r.all = append(r.all, stackRef{src, *st})
			cands[st.Name] = append(cands[st.Name], candidate{src, rank(st.Name), st})
		}
		for name, st := range snap.State.Stacks {
			if !listed[name] && st.Deployed != "" && !st.Removed {
				cands[name] = append(cands[name], candidate{src, 2, nil})
			}
		}
	}
	for name, cs := range cands {
		best := cs[0]
		for _, c := range cs[1:] {
			if c.rank > best.rank {
				best = c
			}
		}
		r.owner[name] = best.src
		if best.st != nil {
			r.owned = append(r.owned, stackRef{best.src, *best.st})
		}
		for _, c := range cs {
			if c.src == best.src || c.st == nil {
				continue
			}
			if r.blocked[c.src] == nil {
				r.blocked[c.src] = map[string]string{}
			}
			r.blocked[c.src][name] = fmt.Sprintf("the stack folder %s already uses the name %s; this stack is not "+
				"deployed while that name is taken: give one of them another Compose project name", best.src.conf.Path, name)
		}
	}
	slices.SortStableFunc(r.owned, func(a, b stackRef) int { return cmp.Compare(a.st.Name, b.st.Name) })
	return r
}

// ownedStacks returns the stacks that own their name, by name; CodeNoRepo without a source or when no working
// copy can be read.
func (s *sourceSet) ownedStacks() ([]stackRef, resolution, error) {
	if len(s.sources()) == 0 {
		return nil, resolution{}, control.NewError(control.CodeNoRepo, "no stacks repository is bound")
	}
	r := s.resolve()
	if r.readable == 0 {
		return nil, r, control.NewError(control.CodeNoRepo, "the working copy cannot be read; run 'qdeploy repo set' again")
	}
	return r.owned, r, nil
}

// route returns the engine responsible for a stack: that of the source owning its name, else the first that
// remembers it, else the first source, which refuses it as unknown.
func (s *sourceSet) route(name string) (*engine.Engine, error) {
	srcs := s.sources()
	if len(srcs) == 0 {
		return nil, engine.ErrNoRepo
	}
	if src := s.resolve().owner[name]; src != nil {
		return src.eng, nil
	}
	for _, src := range srcs {
		if _, ok, err := src.eng.Stack(name); err == nil && ok {
			return src.eng, nil
		}
	}
	return srcs[0].eng, nil
}

func routed[T any](s *sourceSet, name string, fn func(e *engine.Engine) (T, error)) (T, error) {
	e, err := s.route(name)
	if err != nil {
		var zero T
		return zero, err
	}
	return fn(e)
}

func routedErr(s *sourceSet, name string, fn func(e *engine.Engine) error) error {
	_, err := routed(s, name, func(e *engine.Engine) (struct{}, error) { return struct{}{}, fn(e) })
	return err
}

// sourceSnapshot is the state of one source.
type sourceSnapshot struct {
	conf sourceConfig
	snap engine.Snapshot
}

// snapshots returns the state of every source in source order.
func (s *sourceSet) snapshots() ([]sourceSnapshot, error) {
	var out []sourceSnapshot
	for _, src := range s.sources() {
		snap, err := src.eng.Snapshot()
		if err != nil {
			return nil, err
		}
		out = append(out, sourceSnapshot{conf: src.conf, snap: snap})
	}
	return out, nil
}

// Snapshot merges the state of the sources by stack name: each name has the state of the source that owns it.
func (s *sourceSet) Snapshot() (engine.Snapshot, error) {
	snaps, err := s.snapshots()
	if err != nil {
		return engine.Snapshot{}, err
	}
	owner := s.resolve().owner
	out := engine.Snapshot{State: engine.State{Stacks: map[string]engine.StackState{}}, Active: map[string]string{}}
	if len(snaps) > 0 {
		first := snaps[0].snap.State
		out.State.LastPoll, out.State.PollError, out.State.Head, out.State.RemoteHead, out.State.Note =
			first.LastPoll, first.PollError, first.Head, first.RemoteHead, first.Note
	}
	ownedBy := func(name string, c sourceConfig) bool { o := owner[name]; return o == nil || o.conf.ID == c.ID }
	for _, ss := range snaps {
		for name, st := range ss.snap.State.Stacks {
			if _, seen := out.State.Stacks[name]; !seen && ownedBy(name, ss.conf) {
				out.State.Stacks[name] = st
			}
		}
		for name, id := range ss.snap.Active {
			if _, seen := out.Active[name]; !seen && ownedBy(name, ss.conf) {
				out.Active[name] = id
			}
		}
	}
	return out, nil
}

// Stack returns the state of a stack at the source responsible for it.
func (s *sourceSet) Stack(name string) (engine.StackState, bool, error) {
	e, err := s.route(name)
	if err != nil {
		return engine.StackState{}, false, nil
	}
	return e.Stack(name)
}

// Runs lists the runs of every source, newest first; an empty stack means all stacks.
func (s *sourceSet) Runs(stack string) ([]engine.RunRecord, error) {
	var out []engine.RunRecord
	for _, src := range s.sources() {
		runs, err := src.eng.Runs(stack)
		if err != nil {
			return nil, err
		}
		out = append(out, runs...)
	}
	slices.SortStableFunc(out, func(a, b engine.RunRecord) int { return b.Started.Compare(a.Started) })
	return out, nil
}

// Log returns the run log of a run of any source.
func (s *sourceSet) Log(id string) (string, error) {
	for _, src := range s.sources() {
		text, err := src.eng.Log(id)
		if !errors.Is(err, engine.ErrNoSuchRun) {
			return text, err
		}
	}
	return "", engine.ErrNoSuchRun
}

// AppendLog adds an entry to the log of a run of any source.
func (s *sourceSet) AppendLog(id, text string) error {
	for _, src := range s.sources() {
		if err := src.eng.AppendLog(id, text); !errors.Is(err, engine.ErrNoSuchRun) {
			return err
		}
	}
	return engine.ErrNoSuchRun
}

// The commands on one stack go to the source responsible for it.

func (s *sourceSet) Trigger(ctx context.Context, name string) (engine.RunInfo, error) {
	return routed(s, name, func(e *engine.Engine) (engine.RunInfo, error) { return e.Trigger(ctx, name) })
}

func (s *sourceSet) Resume(name string) (engine.Pause, error) {
	return routed(s, name, func(e *engine.Engine) (engine.Pause, error) { return e.Resume(name) })
}

func (s *sourceSet) SetPause(ctx context.Context, name string, on bool) (bool, error) {
	return routed(s, name, func(e *engine.Engine) (bool, error) { return e.SetPause(ctx, name, on) })
}

func (s *sourceSet) SetPin(ctx context.Context, name string, on bool) (bool, error) {
	return routed(s, name, func(e *engine.Engine) (bool, error) { return e.SetPin(ctx, name, on) })
}

func (s *sourceSet) WithStack(ctx context.Context, name, label string, fn func(ctx context.Context, st stacks.Stack, deployed string) error) error {
	return routedErr(s, name, func(e *engine.Engine) error { return e.WithStack(ctx, name, label, fn) })
}

func (s *sourceSet) StackAction(ctx context.Context, name, action string) error {
	return routedErr(s, name, func(e *engine.Engine) error { return e.StackAction(ctx, name, action) })
}

func (s *sourceSet) StackSettings(ctx context.Context, name string) (*stacks.Rules, error) {
	return routed(s, name, func(e *engine.Engine) (*stacks.Rules, error) { return e.StackSettings(ctx, name) })
}

func (s *sourceSet) SetSettings(ctx context.Context, name string, rules *stacks.Rules) (string, error) {
	return routed(s, name, func(e *engine.Engine) (string, error) { return e.SetSettings(ctx, name, rules) })
}

func (s *sourceSet) UpdateStoppedContainers(name string, add, remove []string) error {
	return routedErr(s, name, func(e *engine.Engine) error { return e.UpdateStoppedContainers(name, add, remove) })
}

func (s *sourceSet) RecordUpdates(name string, check engine.UpdateCheck) error {
	return routedErr(s, name, func(e *engine.Engine) error { return e.RecordUpdates(name, check) })
}

func (s *sourceSet) RecordMonitor(name string, failure *engine.Failure, services []string) error {
	return routedErr(s, name, func(e *engine.Engine) error { return e.RecordMonitor(name, failure, services) })
}

func (s *sourceSet) Repair(ctx context.Context, name string, rec engine.Repair, restart func(ctx context.Context) error) (bool, error) {
	return routed(s, name, func(e *engine.Engine) (bool, error) { return e.Repair(ctx, name, rec, restart) })
}

func (s *sourceSet) RecordRepair(name string, rec *engine.Repair) (bool, error) {
	return routed(s, name, func(e *engine.Engine) (bool, error) { return e.RecordRepair(name, rec) })
}

func (s *sourceSet) WindowEligibility(name string) (engine.WindowVerdict, error) {
	return routed(s, name, func(e *engine.Engine) (engine.WindowVerdict, error) { return e.WindowEligibility(name) })
}

func (s *sourceSet) CheckUpdate(ctx context.Context, name string) (stacks.Stack, error) {
	return routed(s, name, func(e *engine.Engine) (stacks.Stack, error) { return e.CheckUpdate(ctx, name) })
}

func (s *sourceSet) Update(ctx context.Context, name string, steps []engine.VersionStep) ([]engine.StepResult, error) {
	return s.UpdateWhile(ctx, name, steps, nil)
}

func (s *sourceSet) UpdateWhile(ctx context.Context, name string, steps []engine.VersionStep, proceed func() bool) ([]engine.StepResult, error) {
	return routed(s, name, func(e *engine.Engine) ([]engine.StepResult, error) { return e.UpdateWhile(ctx, name, steps, proceed) })
}
