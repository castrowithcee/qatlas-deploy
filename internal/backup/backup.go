// Package backup saves the data of a stack while it is stopped and restores it on request. A backup is an
// archive per data directory and per named volume (written by a short-lived helper container without network)
// and, optionally, the output of the stack's backup command. Backups live under the service's state directory,
// private to the service user.
//
// What may be touched is decided by the stack settings and nothing else: the listed data directories
// below data/ and the listed volumes. Every path is checked again right before it is mounted, a target is
// emptied only inside the helper container on the mounted path, and no value from the working copy reaches a
// shell: all calls are argv, the one helper script is a constant.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/compose"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// DefaultImage is the helper image: busybox 1.37.0, pinned by the digest of its multi-platform index.
const DefaultImage = "busybox@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec"

// DefaultCallTimeout limits one helper container call.
const DefaultCallTimeout = time.Hour

// restartTimeout bounds the start of the stack after a failure, independent of a canceled caller.
const restartTimeout = 10 * time.Minute

// dataRoot is the only folder below which data directories are backed up and restored: it is the host's own
// area in the stacks working copy, never tracked by git.
const dataRoot = "data"

// Errors the callers can tell apart.
var (
	ErrNoRules      = errors.New("the stack settings have no backup section")
	ErrNoSuchBackup = errors.New("no such backup")
)

var (
	imageShape  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:/@-]*$`)
	volumeShape = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	// mountPath is what may be a bind mount source: it must not hold a comma, equals sign, quote or space,
	// which would change the meaning of the --mount option.
	mountPath = regexp.MustCompile(`^/[A-Za-z0-9_./@+-]+$`)
)

// Logger is the run log the manager writes to.
type Logger = compose.Logger

// Manager creates, lists, restores and prunes the backups of stacks. Exec and Root are required. A Manager
// does not lock; the caller serializes all calls for one stack, see engine.WithStack.
type Manager struct {
	Exec *compose.Executor
	// Root is the absolute backup store, normally <state dir>/backups.
	Root string
	// Image is the helper image; DefaultImage when empty.
	Image string
	// CallTimeout limits one helper call; DefaultCallTimeout when zero.
	CallTimeout time.Duration
	// Clock is replaced by tests.
	Clock func() time.Time
}

// NewManager returns the production manager: docker through os/exec.
func NewManager(root string) *Manager {
	return &Manager{
		Exec: &compose.Executor{Runner: &compose.ExecRunner{}, Env: compose.ProductionEnv(os.Getenv)},
		Root: root,
	}
}

func (m *Manager) now() time.Time {
	if m.Clock != nil {
		return m.Clock().UTC()
	}
	return time.Now().UTC()
}

func (m *Manager) image() (string, error) {
	img := m.Image
	if img == "" {
		img = DefaultImage
	}
	if !imageShape.MatchString(img) {
		return "", fmt.Errorf("helper image %q is not a valid image reference", img)
	}
	return img, nil
}

func (m *Manager) timeout() time.Duration {
	if m.CallTimeout > 0 {
		return m.CallTimeout
	}
	return DefaultCallTimeout
}

// Result is what Create returns besides the metadata: the ids of the backups retention removed.
type Result struct {
	Meta    Meta
	Removed []string
}

// Create backs up the stack: stop it if it runs, archive each data directory and volume, run the backup
// command, write the metadata, prune old backups and start the stack again if it was running. commit is the
// commit last deployed. On any failure the stack is started again, if it was running, and the partial backup
// is removed. When only the start after a successful backup fails, the backup stays and is returned with the
// error.
func (m *Manager) Create(ctx context.Context, st stacks.Stack, commit string, log Logger) (res Result, err error) {
	b, stackAbs, err := m.prepare(st)
	if err != nil {
		return Result{}, err
	}
	log.Step("plan")
	wants := make([]Item, 0, len(b.Paths)+len(b.Volumes))
	for _, p := range b.Paths {
		wants = append(wants, Item{Kind: KindPath, Name: p})
	}
	for _, v := range b.Volumes {
		wants = append(wants, Item{Kind: KindVolume, Name: v})
	}
	targets, err := m.resolve(ctx, st, stackAbs, wants, true, log)
	if err != nil {
		return Result{}, err
	}
	if len(b.Command) > 0 {
		if err := checkProgram(b.Command[0]); err != nil {
			return Result{}, err
		}
	}

	running, err := m.Exec.Running(ctx, st, log)
	if err != nil {
		return Result{}, err
	}
	id, dir, err := m.create(st.Name, m.now())
	if err != nil {
		return Result{}, err
	}
	meta := Meta{ID: id, Stack: st.Name, Created: m.now(), Commit: validCommit(commit), Images: imagesOf(st)}

	defer func() {
		if err != nil {
			if rerr := m.remove(st.Name, id); rerr != nil {
				log.Printf("the partial backup %s could not be removed: %v", id, rerr)
			}
			res = Result{}
		}
		if running {
			if uerr := m.start(ctx, st, log); uerr != nil {
				if err == nil {
					err = fmt.Errorf("backup %s is complete but the stack could not be started: %w", id, uerr)
					res = Result{Meta: meta, Removed: res.Removed}
				} else {
					err = errors.Join(err, fmt.Errorf("the stack could not be started again: %w", uerr))
				}
			}
		}
	}()

	if running {
		log.Step("stop")
		if err = m.Exec.Stop(ctx, st, log); err != nil {
			return Result{}, err
		}
	}
	for _, t := range targets {
		log.Step("archive " + t.Kind + " " + t.Name)
		if err = m.archive(ctx, log, stackAbs, dir, t); err != nil {
			return Result{}, err
		}
		meta.Items = append(meta.Items, t.Item)
	}
	if len(b.Command) > 0 {
		log.Step("command")
		if err = m.runCommand(ctx, log, stackAbs, dir, b.Command); err != nil {
			return Result{}, err
		}
		meta.Items = append(meta.Items, Item{Kind: KindCommand, File: commandFile})
	}
	for i := range meta.Items {
		path := filepath.Join(dir, meta.Items[i].File)
		info, serr := os.Lstat(path)
		if serr != nil || !info.Mode().IsRegular() {
			err = fmt.Errorf("the %s of the backup was not written", meta.Items[i].File)
			return Result{}, err
		}
		meta.Items[i].Size = info.Size()
	}
	if err = writeMeta(dir, meta); err != nil {
		return Result{}, err
	}
	log.Step("retention")
	removed, rerr := m.retain(st.Name, b.Keep, b.MaxAgeDays, id)
	if rerr != nil {
		// The backup itself succeeded; a leftover old backup must not fail it.
		log.Printf("an old backup could not be removed: %v", rerr)
	}
	return Result{Meta: meta, Removed: removed}, nil
}

// Restore replaces the backed-up data of the stack with the content of backup id: it stops the stack if it
// runs, empties each listed data directory and volume and unpacks the archive into it, then starts the stack
// again if it was running. Only items that are in the backup and still listed in the stack's rules are
// restored; a backup that holds an item the rules no longer list is refused as a whole. The command output
// is not restored: the stack settings define no restore command.
//
// Before anything is emptied every archive is read through once. A failure up to there starts the stack again.
// A failure after the first target was emptied leaves the stack stopped, because it would run on half-restored
// data; the error says so.
func (m *Manager) Restore(ctx context.Context, st stacks.Stack, id string, log Logger) (meta Meta, err error) {
	b, stackAbs, err := m.prepare(st)
	if err != nil {
		return Meta{}, err
	}
	dir, err := m.backupDir(st.Name, id)
	if err != nil {
		return Meta{}, err
	}
	meta, err = readMeta(dir, st.Name, id)
	if err != nil {
		return Meta{}, ErrNoSuchBackup
	}
	var wants []Item
	for _, it := range meta.Items {
		switch {
		case it.Kind == KindPath && contains(b.Paths, it.Name), it.Kind == KindVolume && contains(b.Volumes, it.Name):
			wants = append(wants, it)
		case it.Kind == KindCommand:
		default:
			return Meta{}, fmt.Errorf("the backup holds %s %q, which the stack settings do not list now; nothing was changed", it.Kind, it.Name)
		}
	}
	if len(wants) == 0 {
		return Meta{}, errors.New("the backup holds no data directory or volume to restore")
	}
	log.Step("plan")
	targets, err := m.resolve(ctx, st, stackAbs, wants, false, log)
	if err != nil {
		return Meta{}, err
	}
	for _, t := range targets {
		info, serr := os.Lstat(filepath.Join(dir, t.File))
		if serr != nil || !info.Mode().IsRegular() {
			return Meta{}, fmt.Errorf("the archive %s is missing from the backup", t.File)
		}
	}

	running, err := m.Exec.Running(ctx, st, log)
	if err != nil {
		return Meta{}, err
	}
	destructive := false
	defer func() {
		if !running {
			return
		}
		if destructive && err != nil {
			err = fmt.Errorf("%w; the stack is left stopped because its data may be incomplete", err)
			return
		}
		if uerr := m.start(ctx, st, log); uerr != nil {
			if err == nil {
				err = fmt.Errorf("the backup was restored but the stack could not be started: %w", uerr)
			} else {
				err = errors.Join(err, fmt.Errorf("the stack could not be started again: %w", uerr))
			}
		}
	}()
	if running {
		log.Step("stop")
		if err = m.Exec.Stop(ctx, st, log); err != nil {
			return Meta{}, err
		}
	}
	for _, t := range targets {
		log.Step("verify " + t.Kind + " " + t.Name)
		if err = m.verify(ctx, log, stackAbs, dir, t); err != nil {
			return Meta{}, err
		}
	}
	destructive = true
	for _, t := range targets {
		log.Step("restore " + t.Kind + " " + t.Name)
		if err = m.extract(ctx, log, stackAbs, dir, t); err != nil {
			return Meta{}, err
		}
	}
	return meta, nil
}

func (m *Manager) prepare(st stacks.Stack) (*stacks.Backup, string, error) {
	if m.Exec == nil {
		return nil, "", errors.New("backup manager has no executor")
	}
	if st.Err != nil {
		return nil, "", fmt.Errorf("stack %s cannot be backed up: %w", st.Name, st.Err)
	}
	if !ValidStack(st.Name) {
		return nil, "", fmt.Errorf("stack name %q cannot be used for backups", st.Name)
	}
	if st.Rules == nil || st.Rules.Backup == nil {
		return nil, "", ErrNoRules
	}
	abs, err := filepath.Abs(st.Dir)
	if err != nil || st.Dir == "" {
		return nil, "", errors.New("stack has no usable directory")
	}
	return st.Rules.Backup, abs, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func validCommit(c string) string {
	if commitForm.MatchString(c) {
		return c
	}
	return ""
}

func imagesOf(st stacks.Stack) []ImageVersion {
	var out []ImageVersion
	for _, img := range st.Images {
		out = append(out, ImageVersion{Var: img.Var, Image: img.Image, Version: img.Value})
	}
	return out
}

// target is an item with what it resolved to.
type target struct {
	Item
	// abs is the data directory on the host; empty for a volume.
	abs    string
	exists bool
}

// resolve checks every item against the rules' boundaries and resolves it. For a backup (strict) the target
// must exist; for a restore a missing data directory is allowed and a missing volume is not.
func (m *Manager) resolve(ctx context.Context, st stacks.Stack, stackAbs string, wants []Item, strict bool, log Logger) ([]target, error) {
	if !mountPath.MatchString(stackAbs) {
		return nil, fmt.Errorf("stack directory %q cannot be mounted", stackAbs)
	}
	var vols map[string]string
	out := make([]target, 0, len(wants))
	seen := map[string]int{}
	for _, w := range wants {
		t := target{Item: w}
		if t.File == "" {
			t.File = fmt.Sprintf("%s-%d.tar", w.Kind, seen[w.Kind])
			seen[w.Kind]++
		}
		if !fileShape.MatchString(t.File) || !strings.HasPrefix(t.File, w.Kind+"-") {
			return nil, errors.New("unusable backup archive name")
		}
		switch w.Kind {
		case KindPath:
			if w.Name != dataRoot && !strings.HasPrefix(w.Name, dataRoot+"/") {
				return nil, fmt.Errorf("backup path %q is not below %s/; only data directories are backed up and restored", w.Name, dataRoot)
			}
			abs, exists, err := stacks.ResolveDir(stackAbs, w.Name)
			if err != nil {
				return nil, fmt.Errorf("backup path %q: %w", w.Name, err)
			}
			if !mountPath.MatchString(abs) {
				return nil, fmt.Errorf("backup path %q cannot be mounted (letters, digits and ._/@+- only)", w.Name)
			}
			if !exists && strict {
				return nil, fmt.Errorf("backup path %q does not exist", w.Name)
			}
			t.abs, t.exists = abs, exists
		case KindVolume:
			if vols == nil {
				var err error
				if vols, err = m.Exec.VolumeNames(ctx, st, log); err != nil {
					return nil, err
				}
			}
			name, ok := vols[w.Name]
			if !ok {
				return nil, fmt.Errorf("backup volume %q is not a volume of the Compose configuration", w.Name)
			}
			if !volumeShape.MatchString(name) {
				return nil, fmt.Errorf("volume %q resolves to an unusable name", w.Name)
			}
			if err := m.volumeExists(ctx, log, stackAbs, name); err != nil {
				return nil, fmt.Errorf("backup volume %q: %w", w.Name, err)
			}
			t.Volume = name
		default:
			return nil, fmt.Errorf("unknown backup item kind %q", w.Kind)
		}
		out = append(out, t)
	}
	return out, nil
}

func (m *Manager) volumeExists(ctx context.Context, log Logger, stackAbs, name string) error {
	c := compose.Command{Name: "docker", Args: []string{"volume", "inspect", name}, Dir: stackAbs, Env: m.Exec.Env, Stdout: io.Discard}
	if _, err := m.Exec.Do(ctx, log, c, compose.DefaultPSTimeout, "docker volume inspect"); err != nil {
		return fmt.Errorf("the Docker volume %s does not exist or cannot be inspected; deploy the stack first", name)
	}
	return nil
}

// helper builds one `docker run` of the helper image: no network, read-only root filesystem, no privilege
// gain, and exactly the mounts given.
func (m *Manager) helper(mounts []string, argv ...string) ([]string, error) {
	img, err := m.image()
	if err != nil {
		return nil, err
	}
	args := []string{"run", "--rm", "--network", "none", "--read-only", "--security-opt", "no-new-privileges"}
	for _, mnt := range mounts {
		args = append(args, "--mount", mnt)
	}
	args = append(args, img)
	return append(args, argv...), nil
}

func bindMount(source, target string, readOnly bool) (string, error) {
	if !mountPath.MatchString(source) {
		return "", fmt.Errorf("%q cannot be mounted", source)
	}
	return mountSpec("bind", source, target, readOnly), nil
}

func volumeMount(name, target string, readOnly bool) (string, error) {
	if !volumeShape.MatchString(name) {
		return "", fmt.Errorf("volume name %q cannot be mounted", name)
	}
	return mountSpec("volume", name, target, readOnly), nil
}

func mountSpec(kind, source, target string, readOnly bool) string {
	spec := "type=" + kind + ",source=" + source + ",target=" + target
	if readOnly {
		spec += ",readonly"
	}
	return spec
}

// dataMount mounts the target's data: a host directory or a volume.
func dataMount(t target, readOnly bool) (string, error) {
	if t.Kind == KindVolume {
		return volumeMount(t.Volume, "/data", readOnly)
	}
	return bindMount(t.abs, "/data", readOnly)
}

func (m *Manager) runHelper(ctx context.Context, log Logger, stackAbs, what string, mounts []string, argv ...string) error {
	args, err := m.helper(mounts, argv...)
	if err != nil {
		return err
	}
	c := compose.Command{Name: "docker", Args: args, Dir: stackAbs, Env: m.Exec.Env, Stdout: io.Discard}
	res, err := m.Exec.Do(ctx, log, c, m.timeout(), what)
	if err != nil {
		if text := strings.TrimSpace(string(res.Output)); text != "" {
			log.Printf("output:\n%s", text)
		}
		return err
	}
	return nil
}

// archive streams the tar of the target out of the helper container into a file this process creates, so the
// store never holds a file written by the container and no container gets the backup directory mounted
// writable.
func (m *Manager) archive(ctx context.Context, log Logger, stackAbs, dir string, t target) error {
	src, err := dataMount(t, true)
	if err != nil {
		return err
	}
	args, err := m.helper([]string{src}, "tar", "--numeric-owner", "-C", "/data", "-cf", "-", ".")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, t.File)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return err
	}
	c := compose.Command{Name: "docker", Args: args, Dir: stackAbs, Env: m.Exec.Env, Stdout: f}
	res, err := m.Exec.Do(ctx, log, c, m.timeout(), "backup archive")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		if text := strings.TrimSpace(string(res.Output)); text != "" {
			log.Printf("output:\n%s", text)
		}
		_ = os.Remove(path)
	}
	return err
}

func (m *Manager) verify(ctx context.Context, log Logger, stackAbs, dir string, t target) error {
	in, err := bindMount(dir, "/backup", true)
	if err != nil {
		return err
	}
	return m.runHelper(ctx, log, stackAbs, "backup verify", []string{in}, "tar", "-tf", "/backup/"+t.File)
}

// emptyAndUnpack is the only script a helper container runs. The archive name is its argument, never part of
// the script text. It empties /data, the mounted target and nothing else, and unpacks the archive there.
const emptyAndUnpack = `set -e
find /data -mindepth 1 -maxdepth 1 -exec rm -rf {} +
exec tar --numeric-owner -xpf "/backup/$1" -C /data`

func (m *Manager) extract(ctx context.Context, log Logger, stackAbs, dir string, t target) error {
	if t.Kind == KindPath {
		// Check again right before use: the first check was before the stack stopped.
		abs, exists, err := stacks.ResolveDir(stackAbs, t.Name)
		if err != nil {
			return fmt.Errorf("backup path %q: %w", t.Name, err)
		}
		if abs != t.abs {
			return fmt.Errorf("backup path %q changed while it was checked", t.Name)
		}
		if !exists {
			if err := mkdirBelow(stackAbs, t.Name); err != nil {
				return fmt.Errorf("backup path %q: %w", t.Name, err)
			}
		}
	}
	dst, err := dataMount(t, false)
	if err != nil {
		return err
	}
	in, err := bindMount(dir, "/backup", true)
	if err != nil {
		return err
	}
	return m.runHelper(ctx, log, stackAbs, "backup restore", []string{dst, in}, "sh", "-c", emptyAndUnpack, "sh", t.File)
}

// mkdirBelow creates the missing tail of rel below root, one level at a time, never through a link.
func mkdirBelow(root, rel string) error {
	cur := root
	for _, part := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		switch {
		case err == nil && info.IsDir():
			continue
		case err == nil:
			return errors.New("not a directory")
		case !errors.Is(err, os.ErrNotExist):
			return err
		}
		if err := os.Mkdir(cur, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// checkProgram accepts a program name resolved in the fixed search path or an absolute path, never a path
// relative to the stack folder, which would run a file of the working copy.
func checkProgram(p string) error {
	switch {
	case p == "" || strings.ContainsAny(p, "\x00\n") || strings.HasPrefix(p, "-"):
		return errors.New("backup.command: unusable program")
	case strings.Contains(p, "/") && !filepath.IsAbs(p):
		return errors.New("backup.command: the program must be a name in the search path or an absolute path")
	}
	return nil
}

// runCommand runs the backup command on the host with the stack folder as working directory and the fixed
// environment of docker calls. Its standard output is the file command.out of the backup, its standard error
// is not kept.
func (m *Manager) runCommand(ctx context.Context, log Logger, stackAbs, dir string, argv []string) error {
	if err := checkProgram(argv[0]); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, commandFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return err
	}
	c := compose.Command{Name: argv[0], Args: argv[1:], Dir: stackAbs, Env: m.Exec.Env, Stdout: f}
	_, err = m.Exec.Do(ctx, log, c, m.timeout(), "backup command")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// start brings the stack up again. It does not depend on the caller's context, which may have ended.
func (m *Manager) start(ctx context.Context, st stacks.Stack, log Logger) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), restartTimeout)
	defer cancel()
	log.Step("up")
	return m.Exec.Up(ctx, st, log)
}

// ForRuns returns the manager as the backups of deployment runs. A run calls it for a stack it stopped
// itself, so Create and Restore leave the stack stopped.
func (m *Manager) ForRuns() compose.Backups { return runBackups{m} }

type runBackups struct{ m *Manager }

func (b runBackups) Create(ctx context.Context, st stacks.Stack, commit string, log compose.Logger) (string, error) {
	res, err := b.m.Create(ctx, st, commit, log)
	return res.Meta.ID, err
}

func (b runBackups) Restore(ctx context.Context, st stacks.Stack, id string, log compose.Logger) error {
	_, err := b.m.Restore(ctx, st, id, log)
	return err
}
