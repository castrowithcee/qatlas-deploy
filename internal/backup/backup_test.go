package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/compose"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

type fakeLog struct{ lines []string }

func (l *fakeLog) Step(name string) { l.lines = append(l.lines, "step "+name) }
func (l *fakeLog) Printf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

// fakeDocker answers docker calls: it records them, writes the archives a helper container would write and
// lets a test make single calls fail.
type fakeDocker struct {
	calls   []compose.Command
	running bool
	// volumes is the answer of `compose config`: volume key to Docker name.
	volumes map[string]string
	// missing lists Docker volumes `docker volume inspect` does not know.
	missing map[string]bool
	// fail returns an error for a call, matched on its summary.
	fail func(summary string) error
	// commandOut is what the backup command prints.
	commandOut string
}

func (f *fakeDocker) Run(_ context.Context, c compose.Command) (compose.Result, error) {
	f.calls = append(f.calls, c)
	sum := summary(c)
	if f.fail != nil {
		if err := f.fail(sum); err != nil {
			return compose.Result{Output: []byte("boom")}, err
		}
	}
	switch {
	case strings.HasPrefix(sum, "compose ps"):
		state := "exited"
		if f.running {
			state = "running"
		}
		return compose.Result{Output: []byte(`{"Name":"demo-app-1","Service":"app","State":"` + state + `"}`)}, nil
	case strings.HasPrefix(sum, "compose config"):
		var parts []string
		for k, v := range f.volumes {
			parts = append(parts, fmt.Sprintf("%q:{\"name\":%q}", k, v))
		}
		_, _ = fmt.Fprintf(c.Stdout, `{"name":"demo","volumes":{%s}}`, strings.Join(parts, ","))
	case strings.HasPrefix(sum, "docker volume inspect"):
		if f.missing[c.Args[2]] {
			return compose.Result{}, &compose.ExitError{Code: 1}
		}
	case c.Name == "docker" && c.Args[0] == "run":
		return compose.Result{}, f.helper(c)
	case c.Name == "pg-dump-fake":
		_, _ = c.Stdout.Write([]byte(f.commandOut))
	}
	return compose.Result{}, nil
}

// helper plays the container: tar -c writes the archive to its standard output.
func (f *fakeDocker) helper(c compose.Command) error {
	for i, a := range c.Args {
		if a == "-cf" && c.Args[i+1] == "-" {
			_, err := c.Stdout.Write([]byte("archive of " + strings.Join(c.Args, " ")))
			return err
		}
	}
	return nil
}

// summary reduces a call to what a test cares about: `compose <verb ...>` for Compose, else the full line.
func summary(c compose.Command) string {
	if c.Name == "docker" && len(c.Args) > 0 && c.Args[0] == "compose" {
		last := 0
		for i, a := range c.Args {
			if a == "-f" {
				last = i + 1
			}
		}
		return "compose " + strings.Join(c.Args[last+1:], " ")
	}
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

func (f *fakeDocker) summaries() []string {
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = summary(c)
	}
	return out
}

type env struct {
	t      *testing.T
	dir    string // stack folder
	root   string // backup store
	docker *fakeDocker
	m      *Manager
	log    *fakeLog
	now    time.Time
}

const rulesYAML = `version: 1
backup:
  paths: [data/db, data/files]
  volumes: [pgdata]
  command: [pg-dump-fake, --all]
  retention: {keep: 3}
`

// writeSettings writes the settings of the stack folder dir, which is the stack demo.
func writeSettings(t *testing.T, dir, rules string) {
	t.Helper()
	p := filepath.Join(filepath.Dir(dir), filepath.FromSlash(stacks.SettingsPath("demo")))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newEnv(t *testing.T, rules string) *env {
	t.Helper()
	base := t.TempDir()
	stack := filepath.Join(base, "stacks", "demo")
	files := map[string]string{
		"compose.yaml":    "services:\n  app:\n    image: registry.example.test/demo/app:${DEMO_APP_VERSION}\n",
		".env":            "DEMO_APP_VERSION=1.2.3\n",
		"secrets.env":     "DEMO_TOKEN=invented\n",
		"data/db/a.txt":   "a",
		"data/files/b.go": "b",
	}
	writeSettings(t, stack, rules)
	for rel, body := range files {
		p := filepath.Join(stack, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	d := &fakeDocker{running: true, volumes: map[string]string{"pgdata": "demo_pgdata"}, commandOut: "dump"}
	e := &env{t: t, dir: stack, root: filepath.Join(base, "state", "backups"), docker: d, log: &fakeLog{},
		now: time.Date(2026, 5, 4, 3, 2, 1, 0, time.UTC)}
	if err := os.MkdirAll(filepath.Dir(e.root), 0o700); err != nil {
		t.Fatal(err)
	}
	e.m = &Manager{
		Exec:  &compose.Executor{Runner: d, Env: []string{"PATH=/bin"}},
		Root:  e.root,
		Clock: func() time.Time { return e.now },
	}
	return e
}

func (e *env) stack() stacks.Stack {
	e.t.Helper()
	list, err := stacks.Load(filepath.Dir(e.dir))
	if err != nil || len(list) != 1 || list[0].Err != nil {
		e.t.Fatalf("stack = %+v, %v", list, err)
	}
	return list[0]
}

func (e *env) create() (Result, error) {
	e.t.Helper()
	return e.m.Create(context.Background(), e.stack(), testCommit, e.log)
}

func failOn(substr string) func(string) error {
	return func(s string) error {
		if strings.Contains(s, substr) {
			return &compose.ExitError{Code: 1}
		}
		return nil
	}
}

func (e *env) backupDirs() []string {
	e.t.Helper()
	entries, err := os.ReadDir(filepath.Join(e.root, "demo"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, ent := range entries {
		out = append(out, ent.Name())
	}
	return out
}

func equal(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func helperLine(mounts []string, argv string) string {
	line := "docker run --rm --network none --read-only --security-opt no-new-privileges"
	for _, m := range mounts {
		line += " --mount " + m
	}
	return line + " " + DefaultImage + " " + argv
}

func TestCreateRunsTheExactSequence(t *testing.T) {
	e := newEnv(t, rulesYAML)
	res, err := e.create()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(e.root, "demo", res.Meta.ID)
	data := func(rel string) string {
		return "type=bind,source=" + filepath.Join(e.dir, rel) + ",target=/data,readonly"
	}
	equal(t, e.docker.summaries(), []string{
		"compose config --format json",
		"docker volume inspect demo_pgdata",
		"compose ps --all --format json",
		"compose stop",
		helperLine([]string{data("data/db")}, "tar --numeric-owner -C /data -cf - ."),
		helperLine([]string{data("data/files")}, "tar --numeric-owner -C /data -cf - ."),
		helperLine([]string{"type=volume,source=demo_pgdata,target=/data,readonly"}, "tar --numeric-owner -C /data -cf - ."),
		"pg-dump-fake --all",
		"compose up -d --remove-orphans",
	})
	for _, c := range e.docker.calls {
		if c.Dir != e.dir || len(c.Env) != 1 || c.Env[0] != "PATH=/bin" {
			t.Fatalf("call %q: dir %q env %v", summary(c), c.Dir, c.Env)
		}
	}

	if !idShape.MatchString(res.Meta.ID) || !strings.HasPrefix(res.Meta.ID, "20260504T030201Z-") {
		t.Fatalf("id = %q", res.Meta.ID)
	}
	m := res.Meta
	if m.Stack != "demo" || m.Commit != testCommit || !m.Created.Equal(e.now) || len(m.Items) != 4 {
		t.Fatalf("meta = %+v", m)
	}
	if len(m.Images) != 1 || m.Images[0].Var != "DEMO_APP_VERSION" || m.Images[0].Version != "1.2.3" {
		t.Fatalf("images = %+v", m.Images)
	}
	if m.Items[3].Kind != KindCommand || m.Items[3].File != "command.out" || m.Items[2].Volume != "demo_pgdata" || m.Items[0].Size == 0 {
		t.Fatalf("items = %+v", m.Items)
	}
	if out, _ := os.ReadFile(filepath.Join(dir, "command.out")); string(out) != "dump" {
		t.Fatalf("command.out = %q", out)
	}
	for _, p := range []string{e.root, filepath.Dir(dir), dir} {
		if info, err := os.Stat(p); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s: %v %v", p, info, err)
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, ent := range entries {
		if info, _ := ent.Info(); info.Mode().Perm() != 0o600 {
			t.Fatalf("%s has mode %v", ent.Name(), info.Mode())
		}
	}
	list, err := e.m.List("demo")
	if err != nil || len(list) != 1 || list[0].ID != m.ID || list[0].Size() != m.Size() {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestCreateOfAStoppedStackNeitherStopsNorStarts(t *testing.T) {
	e := newEnv(t, "version: 1\nbackup:\n  paths: [data/db]\n")
	e.docker.running = false
	if _, err := e.create(); err != nil {
		t.Fatal(err)
	}
	for _, s := range e.docker.summaries() {
		if s == "compose stop" || strings.HasPrefix(s, "compose up") {
			t.Fatalf("unexpected call %q", s)
		}
	}
}

func TestCreateFailuresStartTheStackAgainAndRemoveThePartialBackup(t *testing.T) {
	for name, fail := range map[string]string{
		"stop":    "compose stop",
		"archive": "source=" + "STACKDIR/data/files",
		"volume":  "type=volume",
		"command": "pg-dump-fake",
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, rulesYAML)
			e.docker.fail = failOn(strings.ReplaceAll(fail, "STACKDIR", e.dir))
			if _, err := e.create(); err == nil {
				t.Fatal("create succeeded")
			}
			calls := e.docker.summaries()
			if last := calls[len(calls)-1]; last != "compose up -d --remove-orphans" {
				t.Fatalf("last call = %q", last)
			}
			if dirs := e.backupDirs(); len(dirs) != 0 {
				t.Fatalf("partial backup left: %v", dirs)
			}
		})
	}
}

func TestCreateReportsAFailedStartButKeepsTheBackup(t *testing.T) {
	e := newEnv(t, rulesYAML)
	e.docker.fail = failOn("compose up")
	res, err := e.create()
	if err == nil || !strings.Contains(err.Error(), "complete but the stack could not be started") || res.Meta.ID == "" {
		t.Fatalf("res %+v, err %v", res, err)
	}
	if dirs := e.backupDirs(); len(dirs) != 1 {
		t.Fatalf("dirs = %v", dirs)
	}
}

func TestCreateStillStartsAfterACanceledContext(t *testing.T) {
	e := newEnv(t, rulesYAML)
	ctx, cancel := context.WithCancel(context.Background())
	e.docker.fail = func(s string) error {
		if strings.Contains(s, "data/db") {
			cancel()
			return context.Canceled
		}
		return nil
	}
	if _, err := e.m.Create(ctx, e.stack(), testCommit, e.log); err == nil {
		t.Fatal("create succeeded")
	}
	calls := e.docker.summaries()
	if calls[len(calls)-1] != "compose up -d --remove-orphans" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestCreateRefusesUnsafeTargetsBeforeAnythingRuns(t *testing.T) {
	outside := t.TempDir()
	cases := map[string]func(e *env, b *stacks.Backup){
		"outside data/":    func(e *env, b *stacks.Backup) { b.Paths = []string{"compose.yaml"} },
		"other folder":     func(e *env, b *stacks.Backup) { b.Paths = []string{"storage"} },
		"parent reference": func(e *env, b *stacks.Backup) { b.Paths = []string{"data/../.."} },
		"absolute":         func(e *env, b *stacks.Backup) { b.Paths = []string{outside} },
		"dot":              func(e *env, b *stacks.Backup) { b.Paths = []string{"."} },
		"not clean":        func(e *env, b *stacks.Backup) { b.Paths = []string{"data//db"} },
		"missing":          func(e *env, b *stacks.Backup) { b.Paths = []string{"data/gone"} },
		"a file":           func(e *env, b *stacks.Backup) { b.Paths = []string{"data/db/a.txt"} },
		"mount option":     func(e *env, b *stacks.Backup) { b.Paths = []string{"data/db,target=/etc"} },
		"space":            func(e *env, b *stacks.Backup) { b.Paths = []string{"data/my db"} },
		"unknown volume":   func(e *env, b *stacks.Backup) { b.Paths, b.Volumes = nil, []string{"other"} },
		"volume is missing": func(e *env, b *stacks.Backup) {
			b.Paths, b.Volumes = nil, []string{"pgdata"}
			e.docker.missing = map[string]bool{"demo_pgdata": true}
		},
		"symlinked data": func(e *env, b *stacks.Backup) {
			if err := os.Symlink(outside, filepath.Join(e.dir, "data", "link")); err != nil {
				t.Fatal(err)
			}
			b.Paths = []string{"data/link"}
		},
		"relative command": func(e *env, b *stacks.Backup) { b.Command = []string{"./dump.sh"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, rulesYAML)
			if err := os.MkdirAll(filepath.Join(e.dir, "data", "my db"), 0o755); err != nil {
				t.Fatal(err)
			}
			st := e.stack()
			b := *st.Rules.Backup
			b.Command = nil
			mutate(e, &b)
			st.Rules = &stacks.Rules{Backup: &b}
			if _, err := e.m.Create(context.Background(), st, testCommit, e.log); err == nil {
				t.Fatal("create succeeded")
			}
			for _, s := range e.docker.summaries() {
				if s == "compose stop" || strings.HasPrefix(s, "docker run") || strings.HasPrefix(s, "pg-dump") {
					t.Fatalf("unexpected call %q", s)
				}
			}
			if dirs := e.backupDirs(); len(dirs) != 0 {
				t.Fatalf("dirs = %v", dirs)
			}
		})
	}
}

func TestCreateUsesTheVolumeNameComposeResolves(t *testing.T) {
	e := newEnv(t, "version: 1\nbackup:\n  volumes: [pgdata]\n")
	e.docker.volumes = map[string]string{"pgdata": "shared-db-volume"}
	if _, err := e.create(); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range e.docker.summaries() {
		found = found || strings.Contains(s, "type=volume,source=shared-db-volume,target=/data,readonly")
	}
	if !found {
		t.Fatalf("calls = %v", e.docker.summaries())
	}
}

func TestUnusableNamesAndRules(t *testing.T) {
	e := newEnv(t, rulesYAML)
	st := e.stack()
	st.Name = "../evil"
	if _, err := e.m.Create(context.Background(), st, testCommit, e.log); err == nil {
		t.Fatal("bad stack name accepted")
	}
	st = e.stack()
	st.Rules = nil
	if _, err := e.m.Create(context.Background(), st, testCommit, e.log); !errors.Is(err, ErrNoRules) {
		t.Fatalf("err = %v", err)
	}
	if len(e.docker.calls) != 0 {
		t.Fatalf("calls = %v", e.docker.summaries())
	}
	for _, id := range []string{"", "../x", "20260504T030201Z-zzzzzzzz", "20260504T030201Z-0123456"} {
		if _, err := e.m.Restore(context.Background(), e.stack(), id, e.log); !errors.Is(err, ErrNoSuchBackup) {
			t.Fatalf("restore %q: %v", id, err)
		}
	}
	if _, err := (&Manager{Exec: e.m.Exec, Root: "relative"}).List("demo"); err == nil {
		t.Fatal("relative root accepted")
	}
	bad := *e.m
	bad.Image = "-v /:/host"
	if _, err := bad.Create(context.Background(), e.stack(), testCommit, e.log); err == nil {
		t.Fatal("bad image accepted")
	}
}

func TestRetention(t *testing.T) {
	e := newEnv(t, "version: 1\nbackup:\n  paths: [data/db]\n  retention: {keep: 2}\n")
	var ids []string
	for i := range 4 {
		e.now = e.now.Add(time.Hour)
		res, err := e.create()
		if err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
		ids = append(ids, res.Meta.ID)
		if i == 3 && (len(res.Removed) != 1 || res.Removed[0] != ids[1]) {
			t.Fatalf("removed = %v", res.Removed)
		}
	}
	equal(t, e.backupDirs(), []string{ids[2], ids[3]})
}

func TestRetentionByAgeNeverRemovesTheNewestAndSweepsIncomplete(t *testing.T) {
	e := newEnv(t, "version: 1\nbackup:\n  paths: [data/db]\n  retention: {keep: 10, max_age_days: 2}\n")
	old, err := e.create()
	if err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(24 * time.Hour)
	mid, err := e.create()
	if err != nil {
		t.Fatal(err)
	}
	incomplete := filepath.Join(e.root, "demo", "20260101T000000Z-00000000")
	if err := os.Mkdir(incomplete, 0o700); err != nil {
		t.Fatal(err)
	}
	// A link that looks like a backup is never followed or removed.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(e.root, "demo", "20250101T000000Z-11111111")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	e.now = e.now.Add(49 * time.Hour) // old is 73h, mid is 49h old: both beyond 2 days
	res, err := e.create()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) != 3 {
		t.Fatalf("removed = %v", res.Removed)
	}
	equal(t, e.backupDirs(), []string{"20250101T000000Z-11111111", res.Meta.ID})
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Fatal("retention followed a link out of the store")
	}
	_ = old
	_ = mid

	// Even when every backup is older than the limit, the newest successful one stays.
	e.now = e.now.Add(30 * 24 * time.Hour)
	if removed, err := e.m.retain("demo", 10, 2, ""); err != nil || len(removed) != 0 {
		t.Fatalf("removed %v, %v", removed, err)
	}
	if list, _ := e.m.List("demo"); len(list) != 1 || list[0].ID != res.Meta.ID {
		t.Fatalf("list = %+v", list)
	}
}

func TestRetentionRequiresEveryLimit(t *testing.T) {
	e := newEnv(t, "version: 1\nbackup:\n  paths: [data/db]\n  retention: {keep: 5, max_age_days: 1}\n")
	first, _ := e.create()
	e.now = e.now.Add(2 * 24 * time.Hour)
	second, err := e.create()
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Removed) != 1 || second.Removed[0] != first.Meta.ID {
		t.Fatalf("removed = %v", second.Removed)
	}
}

func TestListIgnoresForeignEntriesAndOrdersNewestFirst(t *testing.T) {
	e := newEnv(t, "version: 1\nbackup:\n  paths: [data/db]\n")
	a, _ := e.create()
	e.now = e.now.Add(time.Minute)
	b, _ := e.create()
	sd := filepath.Join(e.root, "demo")
	if err := os.WriteFile(filepath.Join(sd, "notes.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(sd, "not-an-id"), 0o700); err != nil {
		t.Fatal(err)
	}
	list, err := e.m.List("demo")
	if err != nil || len(list) != 2 || list[0].ID != b.Meta.ID || list[1].ID != a.Meta.ID {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list, err := e.m.List("other"); err != nil || len(list) != 0 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := e.m.List("../x"); err == nil {
		t.Fatal("bad stack name listed")
	}
}

const unpack = `sh -c set -e
find /data -mindepth 1 -maxdepth 1 -exec rm -rf {} +
exec tar --numeric-owner -xpf "/backup/$1" -C /data sh `

func TestRestoreRunsTheExactSequence(t *testing.T) {
	e := newEnv(t, rulesYAML)
	res, err := e.create()
	if err != nil {
		t.Fatal(err)
	}
	e.docker.calls = nil
	got, err := e.m.Restore(context.Background(), e.stack(), res.Meta.ID, e.log)
	if err != nil || got.ID != res.Meta.ID {
		t.Fatalf("restore: %+v, %v", got, err)
	}
	dir := filepath.Join(e.root, "demo", res.Meta.ID)
	ro := "type=bind,source=" + dir + ",target=/backup,readonly"
	rw := func(rel string) string { return "type=bind,source=" + filepath.Join(e.dir, rel) + ",target=/data" }
	vol := "type=volume,source=demo_pgdata,target=/data"
	equal(t, e.docker.summaries(), []string{
		"compose config --format json",
		"docker volume inspect demo_pgdata",
		"compose ps --all --format json",
		"compose stop",
		helperLine([]string{ro}, "tar -tf /backup/path-0.tar"),
		helperLine([]string{ro}, "tar -tf /backup/path-1.tar"),
		helperLine([]string{ro}, "tar -tf /backup/volume-0.tar"),
		helperLine([]string{rw("data/db"), ro}, unpack+"path-0.tar"),
		helperLine([]string{rw("data/files"), ro}, unpack+"path-1.tar"),
		helperLine([]string{vol, ro}, unpack+"volume-0.tar"),
		"compose up -d --remove-orphans",
	})
}

func TestRestoreOfAStoppedStackLeavesItStopped(t *testing.T) {
	e := newEnv(t, "version: 1\nbackup:\n  paths: [data/db]\n")
	res, _ := e.create()
	e.docker.running, e.docker.calls = false, nil
	if _, err := e.m.Restore(context.Background(), e.stack(), res.Meta.ID, e.log); err != nil {
		t.Fatal(err)
	}
	for _, s := range e.docker.summaries() {
		if s == "compose stop" || strings.HasPrefix(s, "compose up") {
			t.Fatalf("unexpected call %q", s)
		}
	}
}

func TestRestoreCreatesAMissingDataDirectory(t *testing.T) {
	e := newEnv(t, "version: 1\nbackup:\n  paths: [data/db]\n")
	res, _ := e.create()
	if err := os.RemoveAll(filepath.Join(e.dir, "data", "db")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Restore(context.Background(), e.stack(), res.Meta.ID, e.log); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(e.dir, "data", "db")); err != nil || !info.IsDir() {
		t.Fatalf("target not created: %v", err)
	}
}

func TestRestoreFailureBeforeTheFirstWipeStartsTheStack(t *testing.T) {
	e := newEnv(t, rulesYAML)
	res, _ := e.create()
	e.docker.calls = nil
	e.docker.fail = failOn("tar -tf /backup/volume-0.tar")
	if _, err := e.m.Restore(context.Background(), e.stack(), res.Meta.ID, e.log); err == nil {
		t.Fatal("restore succeeded")
	}
	calls := e.docker.summaries()
	if calls[len(calls)-1] != "compose up -d --remove-orphans" {
		t.Fatalf("calls = %v", calls)
	}
	for _, s := range calls {
		if strings.Contains(s, "rm -rf") {
			t.Fatalf("a target was emptied before every archive was verified: %q", s)
		}
	}
}

func TestRestoreFailureAfterTheWipeLeavesTheStackStopped(t *testing.T) {
	e := newEnv(t, rulesYAML)
	res, _ := e.create()
	e.docker.calls = nil
	e.docker.fail = func(s string) error {
		if strings.Contains(s, "rm -rf") && strings.HasSuffix(s, "path-1.tar") {
			return &compose.ExitError{Code: 1}
		}
		return nil
	}
	_, err := e.m.Restore(context.Background(), e.stack(), res.Meta.ID, e.log)
	if err == nil || !strings.Contains(err.Error(), "left stopped") {
		t.Fatalf("err = %v", err)
	}
	for _, s := range e.docker.summaries() {
		if strings.HasPrefix(s, "compose up") || strings.Contains(s, "volume-0.tar") && strings.Contains(s, "rm -rf") {
			t.Fatalf("unexpected call %q", s)
		}
	}
}

func TestRestoreRefusesWhatTheRulesNoLongerList(t *testing.T) {
	e := newEnv(t, rulesYAML)
	res, _ := e.create()
	e.docker.calls = nil
	writeSettings(t, e.dir, "version: 1\nbackup:\n  paths: [data/db]\n")
	_, err := e.m.Restore(context.Background(), e.stack(), res.Meta.ID, e.log)
	if err == nil || !strings.Contains(err.Error(), "do not list now") {
		t.Fatalf("err = %v", err)
	}
	if len(e.docker.calls) != 0 {
		t.Fatalf("calls = %v", e.docker.summaries())
	}
}

func TestRestoreRefusesUnknownIncompleteAndTamperedBackups(t *testing.T) {
	e := newEnv(t, "version: 1\nbackup:\n  paths: [data/db]\n")
	res, _ := e.create()
	e.docker.calls = nil
	unknown := "20200101T000000Z-00000000"
	incomplete := "20200102T000000Z-00000000"
	if err := os.Mkdir(filepath.Join(e.root, "demo", incomplete), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{unknown, incomplete} {
		if _, err := e.m.Restore(context.Background(), e.stack(), id, e.log); !errors.Is(err, ErrNoSuchBackup) {
			t.Fatalf("restore %s: %v", id, err)
		}
	}
	// Metadata that names a file outside the archive naming scheme is treated as no backup.
	meta := filepath.Join(e.root, "demo", res.Meta.ID, metaFile)
	data, _ := os.ReadFile(meta)
	if err := os.WriteFile(meta, []byte(strings.ReplaceAll(string(data), "path-0.tar", "../../x")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Restore(context.Background(), e.stack(), res.Meta.ID, e.log); !errors.Is(err, ErrNoSuchBackup) {
		t.Fatalf("tampered: %v", err)
	}
	if len(e.docker.calls) != 0 {
		t.Fatalf("calls = %v", e.docker.summaries())
	}
}

func TestRestoreRefusesALinkedTarget(t *testing.T) {
	e := newEnv(t, "version: 1\nbackup:\n  paths: [data/db]\n")
	res, _ := e.create()
	e.docker.calls = nil
	outside := t.TempDir()
	if err := os.RemoveAll(filepath.Join(e.dir, "data", "db")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(e.dir, "data", "db")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Restore(context.Background(), e.stack(), res.Meta.ID, e.log); err == nil {
		t.Fatal("restore followed a link")
	}
	if len(e.docker.calls) != 0 {
		t.Fatalf("calls = %v", e.docker.summaries())
	}
}

func TestCreateNeverMountsTheBackupDirectoryAndLeavesNoContainerFiles(t *testing.T) {
	e := newEnv(t, rulesYAML)
	res, err := e.create()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range e.docker.calls {
		for _, a := range c.Args {
			if strings.Contains(a, "target=/backup") || strings.Contains(a, e.root) {
				t.Fatalf("create mounted the store: %q", summary(c))
			}
		}
	}
	dir := filepath.Join(e.root, "demo", res.Meta.ID)
	if data, err := os.ReadFile(filepath.Join(dir, "path-0.tar")); err != nil || !strings.HasPrefix(string(data), "archive of ") {
		t.Fatalf("archive = %q, %v", data, err)
	}
	// A failed archive leaves no file behind.
	e.docker.fail = failOn("source=" + filepath.Join(e.dir, "data/files"))
	if _, err := e.create(); err == nil {
		t.Fatal("create succeeded")
	}
	if dirs := e.backupDirs(); len(dirs) != 1 {
		t.Fatalf("dirs = %v", dirs)
	}
}

func TestForRunsWithoutRules(t *testing.T) {
	m := &Manager{Exec: &compose.Executor{}, Root: t.TempDir()}
	st := stacks.Stack{Name: "demo-stack", Dir: t.TempDir()}
	if _, err := m.ForRuns().Create(context.Background(), st, "", nil); !errors.Is(err, ErrNoRules) {
		t.Fatalf("create err = %v", err)
	}
	if err := m.ForRuns().Restore(context.Background(), st, "x", nil); !errors.Is(err, ErrNoRules) {
		t.Fatalf("restore err = %v", err)
	}
}
