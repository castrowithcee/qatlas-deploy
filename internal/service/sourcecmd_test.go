package service_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
	"github.com/castrowithcee/qatlas-deploy/internal/service"
)

// localRepo is a committed repository without a remote.
func localRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := repoWith(t, "", files)
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "first")
	return dir
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func (h *harness) sourceAdd(path string) (control.SourceAddResult, error) {
	var res control.SourceAddResult
	err := h.call(control.SourceAddCommand, control.SourceAddArgs{Path: path}, &res)
	return res, err
}

func (h *harness) sourceList() control.SourceListResult {
	h.t.Helper()
	var res control.SourceListResult
	if err := h.call(control.SourceListCommand, nil, &res); err != nil {
		h.t.Fatal(err)
	}
	return res
}

func (h *harness) sourceRemove(path string) error {
	return h.call(control.SourceRemoveCommand, control.SourceRemoveArgs{Path: path}, &control.SourceRemoveResult{})
}

// tree maps every path below root, except .git, to its mode and content.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" && d.IsDir() {
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		body := ""
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			body = string(b)
		}
		out[p] = info.Mode().String() + "|" + body
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func localFiles() map[string]string {
	return map[string]string{"web/compose.yaml": bindCompose, "web/.env": "WEB_VERSION=1.2.3\n", "web/data/db.bin": "x"}
}

func TestSourceAddBindsAndRunsALocalFolder(t *testing.T) {
	clock := &svcClock{now: noon}
	h := realGitService(t, withClock(clock, nil))
	dir := localRepo(t, localFiles())

	res, err := h.sourceAdd(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != dir || res.ID != strings.ReplaceAll(strings.TrimPrefix(dir, "/"), "/", "-") || res.Kind != "local" || res.Head != headOf(t, dir) {
		t.Fatalf("add = %+v", res)
	}
	list := h.sourceList()
	if len(list.Sources) != 1 || list.Sources[0].Kind != control.SourceKindLocal || list.Sources[0].URL != "" || list.Sources[0].ID != res.ID {
		t.Fatalf("list = %+v", list)
	}
	// No deploy key and no pinned host key are involved.
	if entries, _ := filepath.Glob(filepath.Join(h.state, "deploy-key*")); len(entries) != 0 {
		t.Fatalf("key files = %v", entries)
	}
	if h.knownHosts() != "" {
		t.Fatalf("known hosts = %q", h.knownHosts())
	}
	wantCode(t, h.call(control.KeyShowCommand, control.KeyShowArgs{Dir: dir}, nil), control.CodeInvalid)

	st := h.waitStatus("local source polled", func(st control.StatusResult) bool {
		return len(st.Engine.Sources) == 1 && st.Engine.Sources[0].Head == res.Head && len(stacksNamed(st, "web")) == 1
	})
	if st.Engine.Sources[0].Kind != "local" || st.Engine.Sources[0].PollError != "" {
		t.Fatalf("status = %+v", st.Engine.Sources[0])
	}

	// A commit made in the folder is seen by the engine.
	writeTree(t, dir, map[string]string{"web/.env": "WEB_VERSION=1.2.4\n"})
	gitIn(t, dir, "commit", "-q", "-am", "second")
	next := headOf(t, dir)
	// The poll loop runs on a manual clock: let time pass until it has polled again.
	for deadline := time.Now().Add(5 * time.Second); h.sourceList().Sources[0].Head != next; time.Sleep(10 * time.Millisecond) {
		clock.Advance(time.Minute)
		if time.Now().After(deadline) {
			t.Fatalf("the engine did not see the new commit: %+v", h.sourceList())
		}
	}

	// Fetching a local source needs no remote.
	var fetched control.RepoFetchResult
	if err := h.call(control.RepoFetchCommand, nil, &fetched); err != nil || len(fetched.Sources) != 1 || fetched.Sources[0].Error != "" {
		t.Fatalf("fetch = %+v, %v", fetched, err)
	}

	// Binding it again is refused: it is a source already.
	_, err = h.sourceAdd(dir)
	wantCode(t, err, control.CodeInvalid)
}

func TestSourceAddRefusesWhatIsNotALocalRepository(t *testing.T) {
	h := realGitService(t)
	missing := filepath.Join(tempBase(t), "new", "stacks")
	clone := repoWith(t, originURL, localFiles())
	gitIn(t, clone, "add", "-A")
	gitIn(t, clone, "commit", "-q", "-m", "first")
	files := newFolder(t)
	writeTree(t, files, localFiles())
	noCommit := repoWith(t, "", localFiles())

	for name, c := range map[string]struct{ path, want string }{
		"missing":   {missing, "nothing to bind"},
		"clone":     {clone, "repo set"},
		"files":     {files, "no repository"},
		"no commit": {noCommit, "first commit"},
		"relative":  {"stacks", "absolute"},
	} {
		_, err := h.sourceAdd(c.path)
		wantCode(t, err, control.CodeInvalid)
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	if len(h.sourceList().Sources) != 0 {
		t.Fatal("a refused folder was bound")
	}
	if _, err := os.Lstat(filepath.Join(files, ".git")); err == nil {
		t.Fatal("source add started a repository")
	}
}

func TestSourceAddRefusesAFolderTheServiceCannotWrite(t *testing.T) {
	notRoot(t)
	h := realGitService(t)
	dir := localRepo(t, localFiles())
	git := filepath.Join(dir, ".git")
	if err := os.Chmod(git, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(git, 0o755) })
	_, err := h.sourceAdd(dir)
	wantCode(t, err, control.CodeInvalid)
	if !strings.Contains(err.Error(), "sudo qdeploy source add") {
		t.Fatalf("answer = %v", err)
	}
	if len(h.sourceList().Sources) != 0 {
		t.Fatal("a folder the service cannot write was bound")
	}
}

func TestSourceRemoveReleasesOnlyTheBinding(t *testing.T) {
	h := realGitService(t)
	dir := localRepo(t, localFiles())
	other := localRepo(t, map[string]string{"api/compose.yaml": webCompose, "api/.env": "WEB_VERSION=1.0.0\n"})
	first, err := h.sourceAdd(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.sourceAdd(other); err != nil {
		t.Fatal(err)
	}
	h.waitStatus("both polled", func(st control.StatusResult) bool {
		return len(stacksNamed(st, "web")) == 1 && len(stacksNamed(st, "api")) == 1
	})
	state := filepath.Join(h.state, "sources", first.ID)
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("no engine state: %v", err)
	}
	before, head := tree(t, dir), headOf(t, dir)

	wantCode(t, h.sourceRemove(filepath.Join(dir, "web")), control.CodeNotFound)
	wantCode(t, h.sourceRemove(filepath.Join(tempBase(t), "unknown")), control.CodeNotFound)
	wantCode(t, h.sourceRemove("relative"), control.CodeInvalid)
	if err := h.sourceRemove(dir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Lstat(state); err == nil {
		t.Fatal("the engine state stays")
	}
	if after := tree(t, dir); len(after) != len(before) || !slices.Equal(mapKeys(after), mapKeys(before)) {
		t.Fatalf("the folder changed:\n%v\n%v", before, after)
	} else {
		for k, v := range before {
			if after[k] != v {
				t.Fatalf("%s changed", k)
			}
		}
	}
	if headOf(t, dir) != head {
		t.Fatal("the repository moved")
	}
	list := h.sourceList()
	if len(list.Sources) != 1 || list.Sources[0].Path != other {
		t.Fatalf("list = %+v", list)
	}
	st := h.waitStatus("other source remains", func(st control.StatusResult) bool { return len(st.Engine.Sources) == 1 })
	if len(stacksNamed(st, "web")) != 0 || len(stacksNamed(st, "api")) != 1 {
		t.Fatalf("stacks = %+v", st.Engine.Stacks)
	}
	// Forgetting is permanent across a restart: the file no longer lists it.
	if dirs := service.BoundDirs(h.state); len(dirs) != 1 || dirs[0] != other {
		t.Fatalf("BoundDirs = %v", dirs)
	}

	// The same folder can be bound again.
	again, err := h.sourceAdd(dir)
	if err != nil || again.ID != first.ID {
		t.Fatalf("bind again = %+v, %v", again, err)
	}
	h.waitStatus("rebound", func(st control.StatusResult) bool { return len(stacksNamed(st, "web")) == 1 })
}

func mapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestSourceRemoveKeepsKeysAndAllowsBindingAgain(t *testing.T) {
	h := start(t)
	h.withKey()
	if _, err := h.set(h.srv.Key().Fingerprint); err != nil {
		t.Fatal(err)
	}
	dir := h.addSource(moreStacks(false))
	var key control.KeyResult
	if err := h.call(control.KeyShowCommand, control.KeyShowArgs{Dir: dir}, &key); err != nil {
		t.Fatal(err)
	}
	var first control.KeyResult
	if err := h.call(control.KeyShowCommand, nil, &first); err != nil {
		t.Fatal(err)
	}
	hosts, files := h.knownHosts(), tree(t, dir)

	if err := h.sourceRemove(dir); err != nil {
		t.Fatal(err)
	}
	if h.knownHosts() != hosts || hosts == "" || !slices.Equal(mapKeys(tree(t, dir)), mapKeys(files)) {
		t.Fatal("known hosts or the folder changed")
	}
	if _, err := os.Stat(filepath.Join(h.state, credentials.SourceKey(strings.ReplaceAll(strings.TrimPrefix(dir, "/"), "/", "-")))); err != nil {
		t.Fatalf("the deploy key is gone: %v", err)
	}
	if list := h.sourceList(); len(list.Sources) != 1 || list.Sources[0].Path != h.dir || list.Sources[0].URL != h.url {
		t.Fatalf("list = %+v", list)
	}

	// The key of the folder is found again without generating one.
	args := control.RepoSetArgs{URL: strings.Replace(h.url, "/stacks.git", "/more-stacks.git", 1), Dir: dir, Fingerprint: h.srv.Key().Fingerprint}
	if err := h.call(control.RepoSetCommand, args, &control.RepoSetResult{}); err != nil {
		t.Fatalf("bind again: %v", err)
	}
	var second, firstAgain control.KeyResult
	if err := h.call(control.KeyShowCommand, control.KeyShowArgs{Dir: dir}, &second); err != nil || second.Fingerprint != key.Fingerprint {
		t.Fatalf("key after binding again = %+v, %v", second, err)
	}
	if err := h.call(control.KeyShowCommand, nil, &firstAgain); err != nil || firstAgain.Fingerprint != first.Fingerprint {
		t.Fatalf("default key = %+v, %v", firstAgain, err)
	}
	if got := h.git.ConfigFor(h.dir).KeyFile; got != filepath.Join(h.state, "deploy-key") {
		t.Fatalf("first folder uses key %s", got)
	}
}

func TestSourceRemoveRefusesWhileARunIsActive(t *testing.T) {
	d := &fakeDeployer{}
	h := realGitService(t, func(c *service.Config) { c.Deployer = d })
	dir := localRepo(t, localFiles())
	res, err := h.sourceAdd(dir)
	if err != nil {
		t.Fatal(err)
	}
	h.waitStatus("polled", func(st control.StatusResult) bool { return len(stacksNamed(st, "web")) == 1 })
	// The first poll may deploy the stack; the run to hold starts after that finished.
	h.waitStatus("idle", func(st control.StatusResult) bool {
		for _, s := range st.Engine.Stacks {
			if s.ActiveRun != "" {
				return false
			}
		}
		return true
	})
	entered, release := d.hold()
	var dep control.DeployResult
	if err := h.call(control.DeployCommand, control.DeployArgs{Stack: "web"}, &dep); err != nil {
		t.Fatal(err)
	}
	<-entered
	wantCode(t, h.sourceRemove(dir), control.CodeRunActive)
	if list := h.sourceList(); len(list.Sources) != 1 || list.Sources[0].ID != res.ID {
		t.Fatalf("list = %+v", list)
	}
	release()
	waitRun(t, h, dep.ID)
	if err := h.sourceRemove(dir); err != nil {
		t.Fatal(err)
	}
	if len(h.sourceList().Sources) != 0 {
		t.Fatal("the source stays")
	}
}

func TestInspectListsTheBindMountsOfEachStack(t *testing.T) {
	h := realGitService(t)
	files := map[string]string{
		"web/compose.yaml": bindCompose, "web/.env": "WEB_VERSION=1.2.3\n", "web/data/db.bin": "x",
		"plain/compose.yaml": webCompose, "plain/.env": "WEB_VERSION=1.0.0\n",
	}
	local := localRepo(t, files)
	adopt := repoWith(t, originURL, files)
	gitIn(t, adopt, "add", "-A")
	plain := newFolder(t)
	writeTree(t, plain, files)

	for name, dir := range map[string]string{"local": local, "adopt-clone": adopt, "files": plain} {
		res := h.inspect(dir, "")
		if res.Case != name {
			t.Fatalf("%s: case %s", name, res.Case)
		}
		if got := inspectedStack(t, res, "web").BindMounts; !slices.Equal(got, []string{"web/data"}) {
			t.Errorf("%s: bind mounts of web = %v", name, got)
		}
		if got := inspectedStack(t, res, "plain").BindMounts; len(got) != 0 {
			t.Errorf("%s: bind mounts of plain = %v", name, got)
		}
	}
	// A mount whose source does not exist yet is not listed.
	if err := os.RemoveAll(filepath.Join(local, "web", "data")); err != nil {
		t.Fatal(err)
	}
	if got := inspectedStack(t, h.inspect(local, ""), "web").BindMounts; len(got) != 0 {
		t.Errorf("a missing source is listed: %v", got)
	}
}
