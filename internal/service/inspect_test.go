package service_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/castrowithcee/qatlas-kit/docker"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
)

const (
	bindCompose = "services:\n  app:\n    image: registry.example.test/demo/web:${WEB_VERSION}\n    volumes:\n      - ./data:/var/lib/demo\n"
	originURL   = "ssh://git.example.test/demo/stacks.git"
)

// realGitService runs the service on the real git, which the cases that look at a repository need.
func realGitService(t *testing.T, opts ...func(*service.Config)) *harness {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	return startWith(t, nil, append([]func(*service.Config){func(c *service.Config) { c.Git = nil }, withDocker(&fakeDocker{})}, opts...)...)
}

func notRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Demo", "GIT_AUTHOR_EMAIL=demo@example.test",
		"GIT_COMMITTER_NAME=Demo", "GIT_COMMITTER_EMAIL=demo@example.test",
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func (h *harness) inspect(path, url string) control.SourceInspectResult {
	h.t.Helper()
	var res control.SourceInspectResult
	if err := h.call(control.SourceInspectCommand, control.SourceInspectArgs{Path: path, URL: url}, &res); err != nil {
		h.t.Fatalf("inspect %s: %v", path, err)
	}
	return res
}

func wantReject(t *testing.T, res control.SourceInspectResult, what string) {
	t.Helper()
	if res.Case != control.SourceCaseReject || res.Reason == "" || res.Fix == "" || res.Access != nil ||
		!strings.Contains(res.Reason, what) {
		t.Fatalf("result = %+v, want a rejection about %q with a fix", res, what)
	}
}

func inspectedStack(t *testing.T, res control.SourceInspectResult, name string) control.SourceStack {
	t.Helper()
	for _, s := range res.Stacks {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no stack %q in %+v", name, res.Stacks)
	return control.SourceStack{}
}

func ruleOf(s control.SourceStack, rule string) (control.CheckFinding, bool) {
	for _, f := range s.Findings {
		if f.Rule == rule {
			return f, true
		}
	}
	return control.CheckFinding{}, false
}

// tempBase makes a temporary folder none of whose parents holds a .git, because the service refuses a
// folder inside a repository and a host may have one in its temporary folder.
func tempBase(t *testing.T) string {
	t.Helper()
	for _, base := range []string{os.TempDir(), "/var/tmp", "/dev/shm"} {
		inside := false
		for dir := base; ; dir = filepath.Dir(dir) {
			if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
				inside = true
			}
			if dir == "/" {
				break
			}
		}
		if inside {
			continue
		}
		if d, err := os.MkdirTemp(base, "qdeploy-inspect-*"); err == nil {
			t.Cleanup(func() { _ = os.RemoveAll(d) })
			return d
		}
	}
	t.Skip("every temporary location lies inside a repository")
	return ""
}

func newFolder(t *testing.T) string {
	t.Helper()
	return filepath.Join(tempBase(t), "stacks")
}

func TestInspectCloneOfAMissingOrEmptyFolder(t *testing.T) {
	h := realGitService(t)
	parent := tempBase(t)
	missing := filepath.Join(parent, "new", "stacks")

	res := h.inspect(missing, originURL)
	if res.Case != control.SourceCaseClone || res.URL != originURL || res.NeedsURL || len(res.Stacks) != 0 || res.Access == nil {
		t.Fatalf("clone with url = %+v", res)
	}
	if res.Access.ModePath != parent || res.Access.OwnerID != uint32(os.Getuid()) || !res.Access.ServiceCanWrite || res.Access.SetsPermissions {
		t.Fatalf("access of the nearest parent = %+v", res.Access)
	}
	if res.Access.GitWritable != nil {
		t.Fatal("a folder without a repository reports .git access")
	}

	if res := h.inspect(missing, ""); res.Case != control.SourceCaseClone || !res.NeedsURL || res.URL != "" {
		t.Fatalf("clone without url = %+v", res)
	}
	empty := newFolder(t)
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if res := h.inspect(empty, originURL); res.Case != control.SourceCaseClone || res.Access.ModePath != empty {
		t.Fatalf("empty folder = %+v", res)
	}
	for _, bad := range []string{"https://git.example.test/demo/stacks.git", "-oProxyCommand=id", "/srv/stacks.git"} {
		wantReject(t, h.inspect(missing, bad), "not a supported SSH address")
	}
}

func repoWith(t *testing.T, remote string, files map[string]string) string {
	t.Helper()
	dir := newFolder(t)
	writeTree(t, dir, files)
	gitIn(t, dir, "init", "-q", "-b", "main")
	if remote != "" {
		gitIn(t, dir, "remote", "add", "origin", remote)
	}
	return dir
}

func TestInspectAdoptsACloneAndRefusesAMismatch(t *testing.T) {
	h := realGitService(t)
	dir := repoWith(t, originURL, map[string]string{"web/compose.yaml": webCompose, "web/.env": "WEB_VERSION=1.2.3\n", "web/secrets.env": "TOKEN=demo\n"})
	gitIn(t, dir, "add", "-A")

	for _, url := range []string{"", originURL} {
		res := h.inspect(dir, url)
		if res.Case != control.SourceCaseAdopt || res.URL != originURL || res.NeedsURL {
			t.Fatalf("adopt with %q = %+v", url, res)
		}
		if res.Access == nil || res.Access.GitWritable == nil || !*res.Access.GitWritable || res.Access.ModePath != dir {
			t.Fatalf("access = %+v", res.Access)
		}
		// A clone has a remote, so a versioned secret reaches it.
		f, ok := ruleOf(inspectedStack(t, res, "web"), "tracked-secrets-env")
		if !ok || f.Severity != "error" {
			t.Fatalf("tracked secrets finding = %+v, %v", f, ok)
		}
		if !res.ContainersChecked {
			t.Fatalf("containers were not checked: %+v", res)
		}
	}
	wantReject(t, h.inspect(dir, "ssh://git.example.test/demo/other.git"), "differs from the origin")

	https := repoWith(t, "https://git.example.test/demo/stacks.git", map[string]string{"a.txt": "x"})
	wantReject(t, h.inspect(https, ""), "origin remote")
	odd := newFolder(t)
	writeTree(t, odd, map[string]string{"a.txt": "x"})
	gitIn(t, odd, "init", "-q", "-b", "main")
	gitIn(t, odd, "remote", "add", "upstream", originURL)
	wantReject(t, h.inspect(odd, ""), "none is called origin")
}

func TestInspectRunsARepositoryWithoutRemoteLocally(t *testing.T) {
	h := realGitService(t)
	dir := repoWith(t, "", map[string]string{"web/compose.yaml": webCompose, "web/.env": "WEB_VERSION=1.2.3\n", "web/secrets.env": "TOKEN=demo\n"})
	gitIn(t, dir, "add", "-A")
	res := h.inspect(dir, "")
	if res.Case != control.SourceCaseLocal || res.URL != "" || res.NeedsURL || res.Access.GitWritable == nil || !*res.Access.GitWritable {
		t.Fatalf("local = %+v", res)
	}
	// Without a remote the versioned secret stays on the host.
	if f, ok := ruleOf(inspectedStack(t, res, "web"), "tracked-secrets-env"); !ok || f.Severity != "warning" {
		t.Fatalf("tracked secrets finding = %+v, %v", f, ok)
	}
	wantReject(t, h.inspect(dir, originURL), "without a remote")

	if err := os.Chmod(filepath.Join(dir, ".git"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, ".git"), 0o755) })
	if os.Geteuid() != 0 {
		if res := h.inspect(dir, ""); res.Access.GitWritable == nil || *res.Access.GitWritable {
			t.Fatalf("read-only .git reported writable: %+v", res.Access)
		}
	}
}

func filesTree(t *testing.T) string {
	t.Helper()
	dir := newFolder(t)
	writeTree(t, dir, map[string]string{
		".gitignore":          "cache/\nsecrets.env\n",
		"web/compose.yaml":    bindCompose,
		"web/.env":            "WEB_VERSION=1.2.3\n",
		"web/secrets.env":     "TOKEN=demo\n",
		"web/data/db.bin":     "x",
		"cache/blob":          "x",
		"cache/private/x":     "x",
		"big.bin":             strings.Repeat("x", 1<<20+1),
		"exact.bin":           strings.Repeat("x", 1<<20),
		"locked/inside.txt":   "x",
		"noread.txt":          "x",
		"inner/readme.txt":    "x",
		"web/data/nested/log": "x",
	})
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("web/compose.yaml", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	gitIn(t, filepath.Join(dir, "inner"), "init", "-q")
	if os.Geteuid() != 0 {
		for _, p := range []string{"noread.txt", "locked", "cache/private"} {
			p := filepath.Join(dir, p)
			if err := os.Chmod(p, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(p, 0o755) })
		}
	}
	return dir
}

func TestInspectPlansTheFirstCommitOfAFolderWithoutGit(t *testing.T) {
	h := realGitService(t)
	dir := filesTree(t)
	res := h.inspect(dir, "")
	if res.Case != control.SourceCaseFiles || res.URL != "" || res.Access == nil || res.Access.GitWritable != nil {
		t.Fatalf("files = %+v", res)
	}
	wantPlanned := []string{".gitignore", "exact.bin", "noread.txt", "web/.env", "web/compose.yaml"}
	wantExcluded := []control.SourceExcluded{
		{Path: "big.bin", Reason: control.SourceExcludedTooLarge},
		{Path: "inner", Reason: control.SourceExcludedNestedRepo},
		{Path: "link", Reason: control.SourceExcludedNotRegular},
		{Path: "locked", Reason: control.SourceExcludedUnreadable},
		{Path: "pipe", Reason: control.SourceExcludedNotRegular},
		{Path: "web/data", Reason: control.SourceExcludedBindMount, Stack: "web"},
	}
	if os.Geteuid() != 0 {
		wantPlanned = slices.DeleteFunc(wantPlanned, func(p string) bool { return p == "noread.txt" })
		wantExcluded = slices.Insert(wantExcluded, 4, control.SourceExcluded{Path: "noread.txt", Reason: control.SourceExcludedUnreadable})
	} else {
		wantExcluded = slices.DeleteFunc(wantExcluded, func(e control.SourceExcluded) bool { return e.Path == "locked" })
	}
	if !slices.Equal(res.PlannedFiles, wantPlanned) {
		t.Fatalf("planned = %q, want %q", res.PlannedFiles, wantPlanned)
	}
	if !slices.Equal(res.Excluded, wantExcluded) {
		t.Fatalf("excluded = %+v, want %+v", res.Excluded, wantExcluded)
	}
	// The ignored secrets.env and the left-out data folder are not versioned as far as the check can see.
	web := inspectedStack(t, res, "web")
	for _, rule := range []string{"tracked-secrets-env", "secrets-env-not-ignored", "tracked-data"} {
		if f, ok := ruleOf(web, rule); ok {
			t.Errorf("unexpected finding %+v", f)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, ".git")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("inspect left a .git: %v", err)
	}
}

func TestInspectChecksTheFilesAWouldBeFirstCommitTakes(t *testing.T) {
	h := realGitService(t)
	dir := newFolder(t)
	writeTree(t, dir, map[string]string{
		"web/compose.yaml": bindCompose, "web/.env": "WEB_VERSION=1.2.3\n", "web/secrets.env": "TOKEN=demo\n", "web/data/db.bin": "x",
	})
	res := h.inspect(dir, "")
	if res.Case != control.SourceCaseFiles || !slices.Contains(res.PlannedFiles, "web/secrets.env") {
		t.Fatalf("files = %+v", res)
	}
	web := inspectedStack(t, res, "web")
	if f, ok := ruleOf(web, "tracked-secrets-env"); !ok || f.Severity != "warning" {
		t.Fatalf("tracked secrets = %+v, %v", f, ok)
	}
	if _, ok := ruleOf(web, "secrets-env-not-ignored"); !ok {
		t.Fatalf("findings = %+v", web.Findings)
	}
	if _, ok := ruleOf(web, "tracked-data"); ok {
		t.Fatalf("the bind-mounted data folder counts as versioned: %+v", web.Findings)
	}
}

func TestInspectRejects(t *testing.T) {
	h := realGitService(t)
	base := tempBase(t)
	for name, tc := range map[string]struct{ path, what string }{
		"relative": {"relative/folder", "absolute, normalized"},
		"dots":     {base + "/a/../b", "absolute, normalized"},
		"slashes":  {base + "//b", "absolute, normalized"},
		"trailing": {base + "/b/", "absolute, normalized"},
		"root":     {"/", "absolute, normalized"},
		"odd":      {base + "/a b", "absolute, normalized"},
		"empty":    {"", "absolute, normalized"},
		"nul":      {base + "/a\x00b", "absolute, normalized"},
		"toolong":  {base + "/" + strings.Repeat("a", 5000), "absolute, normalized"},
	} {
		t.Run(name, func(t *testing.T) { wantReject(t, h.inspect(tc.path, ""), tc.what) })
	}

	file := filepath.Join(base, "file")
	real := filepath.Join(base, "real")
	for _, err := range []error{
		os.WriteFile(file, []byte("x"), 0o644), os.Mkdir(real, 0o755),
		os.Symlink(real, filepath.Join(base, "link")), os.Symlink(base+"/nowhere", filepath.Join(base, "dangling")),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	wantReject(t, h.inspect(file, ""), "not a folder")
	wantReject(t, h.inspect(file+"/below", ""), "is not a folder")
	wantReject(t, h.inspect(filepath.Join(base, "link"), ""), "symbolic link")
	wantReject(t, h.inspect(filepath.Join(base, "dangling"), ""), "symbolic link")
	wantReject(t, h.inspect(filepath.Join(base, "link", "below"), ""), "a folder above it")

	repo := repoWith(t, originURL, map[string]string{"a.txt": "x"})
	if err := os.MkdirAll(filepath.Join(repo, "sub", "stacks"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTree(t, repo, map[string]string{"sub/stacks/a.txt": "x"})
	for _, p := range []string{filepath.Join(repo, "sub", "stacks"), filepath.Join(repo, "sub", "missing", "stacks")} {
		res := h.inspect(p, "")
		wantReject(t, res, "part of another repository")
		if !strings.Contains(res.Reason, repo) {
			t.Fatalf("the reason does not name the repository root: %q", res.Reason)
		}
	}
	// A worktree or submodule has a .git file instead of a folder.
	linked := newFolder(t)
	writeTree(t, linked, map[string]string{".git": "gitdir: /nowhere\n", "a.txt": "x"})
	wantReject(t, h.inspect(linked, ""), ".git in the folder is not a folder")
}

func TestInspectRejectsOverlapsWithStackFoldersAndTheState(t *testing.T) {
	h := boundWith(t, nil, nil)
	for _, p := range []string{h.dir, filepath.Join(h.dir, "demo-stack"), filepath.Join(h.dir, "new"), filepath.Dir(h.dir)} {
		wantReject(t, h.inspect(p, ""), "already a source")
	}
	for _, p := range []string{h.state, filepath.Join(h.state, "x"), filepath.Dir(h.state)} {
		wantReject(t, h.inspect(p, ""), "state directory")
	}
}

func TestInspectWithoutWriteAccessIsNotARejection(t *testing.T) {
	notRoot(t)
	h := realGitService(t)
	dir := newFolder(t)
	writeTree(t, dir, map[string]string{"web/compose.yaml": webCompose, "web/.env": "WEB_VERSION=1.2.3\n"})
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	res := h.inspect(dir, "")
	if res.Case != control.SourceCaseFiles || res.Access == nil || res.Access.ServiceCanWrite || !res.Access.SetsPermissions || res.Access.Mode != "0555" {
		t.Fatalf("read-only folder = %+v", res)
	}
	if cur, err := user.Current(); err == nil && res.Access.Owner != "" && res.Access.Owner != cur.Username {
		t.Fatalf("owner = %q, want %q", res.Access.Owner, cur.Username)
	}
	if res.Access.GroupID != uint32(os.Getgid()) {
		t.Fatalf("group id = %d", res.Access.GroupID)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if res := h.inspect(dir, ""); !res.Access.ServiceCanWrite || res.Access.SetsPermissions || res.Access.Mode != "0755" {
		t.Fatalf("writable folder = %+v", res.Access)
	}
}

func TestInspectReportsContainersOfOtherProjectsInTheStackFolders(t *testing.T) {
	fd := &fakeDocker{}
	h := realGitService(t, withDocker(fd))
	dir := newFolder(t)
	writeTree(t, dir, map[string]string{
		"web/compose.yaml": webCompose, "web/.env": "WEB_VERSION=1.2.3\n",
		"db/compose.yaml": webCompose, "db/.env": "WEB_VERSION=1.2.3\n",
	})
	web, db := filepath.Join(dir, "web"), filepath.Join(dir, "db")
	fd.list = []docker.Container{
		running("c1", "web-app-1", map[string]string{lblProject: "web", lblDir: web}),
		running("c2", "old-web-1", map[string]string{lblProject: "oldweb", lblDir: web}),
		running("c3", "old-web-2", map[string]string{lblProject: "oldweb", lblDir: web}),
		running("c4", "db-elsewhere-1", map[string]string{lblProject: "db", lblDir: "/srv/other/db"}),
		running("c5", "lonely", nil),
		running("c6", "other-db-1", map[string]string{lblProject: "other", lblDir: db + "/"}),
	}
	res := h.inspect(dir, "")
	if !res.ContainersChecked || res.ContainersError != "" {
		t.Fatalf("containers = %+v", res)
	}
	want := []control.SourceConflict{{Container: "old-web-1", Project: "oldweb"}, {Container: "old-web-2", Project: "oldweb"}}
	if got := inspectedStack(t, res, "web").Conflicts; !slices.Equal(got, want) {
		t.Fatalf("web conflicts = %+v, want %+v", got, want)
	}
	if got := inspectedStack(t, res, "db").Conflicts; !slices.Equal(got, []control.SourceConflict{{Container: "other-db-1", Project: "other"}}) {
		t.Fatalf("db conflicts = %+v", got)
	}

	// An engine that cannot be asked leaves the rest of the answer valid.
	fd.listErr = &docker.Error{Code: output.CodeUnreachable, Message: "x"}
	res = h.inspect(dir, "")
	if res.ContainersChecked || !strings.Contains(res.ContainersError, "not reachable") || res.Case != control.SourceCaseFiles || len(res.Stacks) != 2 {
		t.Fatalf("unreachable engine = %+v", res)
	}
	for _, s := range res.Stacks {
		if len(s.Conflicts) != 0 || len(s.Functions) == 0 {
			t.Fatalf("stack = %+v", s)
		}
	}
}

func TestInspectShowsNoValueOfAnEnvFile(t *testing.T) {
	h := realGitService(t)
	dir := newFolder(t)
	writeTree(t, dir, map[string]string{
		"web/compose.yaml": webCompose,
		"web/.env":         "WEB_VERSION=sentinelfloat\nAPI_NOTE=sentinel-env-value-1234\n",
		"web/secrets.env":  "DB_PASSWORD=sentinel-secret-value-5678\n",
	})
	res := h.inspect(dir, "")
	if res.Case != control.SourceCaseFiles || len(inspectedStack(t, res, "web").Functions) == 0 {
		t.Fatalf("files = %+v", res)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"sentinelfloat", "sentinel-env-value-1234", "sentinel-secret-value-5678"} {
		if strings.Contains(string(raw), v) {
			t.Errorf("the answer carries %q: %s", v, raw)
		}
	}
	if !strings.Contains(string(raw), "[redacted]") {
		t.Errorf("the quoted .env value was not redacted: %s", raw)
	}
}

type entry struct {
	mode       fs.FileMode
	size       int64
	mtime      int64
	uid, gid   uint32
	linkTarget string
}

// snapshot records everything about a tree that a read could change, including permissions of folders.
func snapshot(t *testing.T, root string) map[string]entry {
	t.Helper()
	out := map[string]entry{}
	var walk func(dir string)
	walk = func(dir string) {
		names, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, n := range names {
			p := filepath.Join(dir, n.Name())
			info, err := os.Lstat(p)
			if err != nil {
				t.Fatal(err)
			}
			st := info.Sys().(*syscall.Stat_t)
			e := entry{mode: info.Mode(), size: info.Size(), mtime: info.ModTime().UnixNano(), uid: st.Uid, gid: st.Gid}
			if info.Mode()&fs.ModeSymlink != 0 {
				e.linkTarget, _ = os.Readlink(p)
			}
			out[p] = e
			if info.IsDir() {
				walk(p)
			}
		}
	}
	walk(root)
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	out[root] = entry{mode: info.Mode(), mtime: info.ModTime().UnixNano()}
	return out
}

func TestInspectChangesNothing(t *testing.T) {
	fd := &fakeDocker{}
	h := realGitService(t, withDocker(fd))
	files := filesTree(t)
	clone := repoWith(t, originURL, map[string]string{"web/compose.yaml": webCompose, "web/.env": "WEB_VERSION=1.2.3\n"})
	gitIn(t, clone, "add", "-A")
	local := repoWith(t, "", map[string]string{"web/compose.yaml": webCompose, "web/.env": "WEB_VERSION=1.2.3\n"})
	empty := newFolder(t)
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	roots := []string{files, clone, local, empty}
	before := make([]map[string]entry, len(roots))
	for i, r := range roots {
		before[i] = snapshot(t, r)
	}
	parent := filepath.Dir(files)
	parentBefore := snapshot(t, parent)
	for _, r := range roots {
		h.inspect(r, "")
		h.inspect(filepath.Join(r, "missing", "below"), "")
	}
	h.inspect(filepath.Join(parent, "missing"), originURL)
	for i, r := range roots {
		after := snapshot(t, r)
		for p, b := range before[i] {
			if a, ok := after[p]; !ok || a != b {
				t.Errorf("%s changed: before %+v, after %+v", p, b, a)
			}
		}
		for p := range after {
			if _, ok := before[i][p]; !ok {
				t.Errorf("%s appeared", p)
			}
		}
	}
	// Only the own folders of the roots are compared above; the parent of the first one gained nothing either.
	parentAfter := snapshot(t, parent)
	for p, b := range parentBefore {
		if a, ok := parentAfter[p]; !ok || a != b {
			t.Errorf("%s changed: before %+v, after %+v", p, b, a)
		}
	}
	if len(fd.calls()) != 0 {
		t.Fatalf("inspect acted on containers: %v", fd.calls())
	}
}

func TestInspectNamesWhatADeployWouldPass(t *testing.T) {
	h := realGitService(t)

	// A one-stack folder: the compose file lies in its root; .env and secrets.env are passed when present.
	one := newFolder(t)
	writeTree(t, one, map[string]string{"compose.yaml": webCompose, ".env": "WEB_VERSION=1.2.3\n", "secrets.env": "DEMO_TOKEN=invented\n"})
	res := h.inspect(one, "")
	if len(res.Stacks) != 1 {
		t.Fatalf("stacks = %+v", res.Stacks)
	}
	st := res.Stacks[0]
	if st.Project != st.Name || st.Project == "" ||
		!slices.Equal(st.ComposeFiles, []string{filepath.Join(one, "compose.yaml")}) ||
		!slices.Equal(st.EnvFiles, []string{filepath.Join(one, ".env"), filepath.Join(one, "secrets.env")}) {
		t.Fatalf("one-stack folder = %+v", st)
	}
	for _, f := range st.EnvFiles {
		if strings.Contains(f, "invented") {
			t.Fatalf("a value reached the answer: %q", f)
		}
	}

	// Without env files none is named.
	bare := newFolder(t)
	writeTree(t, bare, map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/web:1\n"})
	if st := h.inspect(bare, "").Stacks[0]; len(st.EnvFiles) != 0 || len(st.ComposeFiles) != 1 {
		t.Fatalf("bare folder = %+v", st)
	}

	// A folder with subfolders: each stack has its own folder, files and env files.
	multi := newFolder(t)
	writeTree(t, multi, map[string]string{
		"web/compose.yaml": webCompose, "web/.env": "WEB_VERSION=1.2.3\n",
		"db/compose.yaml": webCompose, "db/secrets.env": "DEMO_TOKEN=invented\n",
		"cache/compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/cache:1\n",
	})
	res = h.inspect(multi, "")
	for name, env := range map[string][]string{"web": {".env"}, "db": {"secrets.env"}, "cache": nil} {
		s := inspectedStack(t, res, name)
		var want []string
		for _, e := range env {
			want = append(want, filepath.Join(multi, name, e))
		}
		if s.Project != name || !slices.Equal(s.ComposeFiles, []string{filepath.Join(multi, name, "compose.yaml")}) || !slices.Equal(s.EnvFiles, want) {
			t.Fatalf("stack %s = %+v, want env %v", name, s, want)
		}
	}
}
