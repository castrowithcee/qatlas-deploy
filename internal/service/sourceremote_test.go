package service_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/hostkey/hostkeytest"
	"github.com/castrowithcee/qatlas-deploy/internal/service/servicetest"
)

const (
	remoteTip   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	remoteNew   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	secretValue = "demo-value-that-must-never-be-printed"
)

// remoteTest is a service with a local source bound over the fake git layer.
type remoteTest struct {
	*harness
	dir string
	dep *fakeDeployer
}

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// newRemoteTest binds a folder with a .git folder as a local source and waits until its stack is deployed.
func newRemoteTest(t *testing.T, withKey bool) *remoteTest {
	t.Helper()
	needGit(t)
	dep := &fakeDeployer{}
	h := startWith(t, dep)
	if withKey {
		h.withKey()
	}
	dir := newFolder(t)
	writeTree(t, dir, map[string]string{"web/compose.yaml": webCompose, "web/.env": "WEB_VERSION=1.2.3\n"})
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sourceAdd(dir); err != nil {
		t.Fatal(err)
	}
	r := &remoteTest{harness: h, dir: dir, dep: dep}
	r.waitDeployed()
	return r
}

func (r *remoteTest) waitDeployed() control.StackStatus {
	r.t.Helper()
	st := r.waitStatus("web deployed", func(st control.StatusResult) bool {
		s := stacksNamed(st, "web")
		return len(s) == 1 && s[0].Deployed != "" && s[0].ActiveRun == ""
	})
	return stacksNamed(st, "web")[0]
}

func (r *remoteTest) fp() string { return r.srv.Key().Fingerprint }

// cmp sets the answer of the comparison and the secret check: of the comparison itself when the remote
// differs, of the scan of the history otherwise.
func (r *remoteTest) cmp(c gitrepo.Comparison, scan gitrepo.Scan) {
	if c.State == gitrepo.RemoteDifferent {
		c.Scan = scan
	}
	r.git.Configure(func(g *servicetest.Git) { g.Compare, g.Scan = c, scan })
}

func (r *remoteTest) head() string {
	var head string
	r.git.Configure(func(g *servicetest.Git) { head = g.Head })
	return head
}

func (r *remoteTest) compare(fp string) (control.SourceCompareResult, error) {
	var res control.SourceCompareResult
	err := r.call(control.SourceCompareCommand, control.SourceCompareArgs{Path: r.dir, URL: r.url, Fingerprint: fp}, &res)
	return res, err
}

func (r *remoteTest) connect(state, commit string) (control.SourceRemoteResult, error) {
	var res control.SourceRemoteResult
	err := r.call(control.SourceRemoteCommand, control.SourceRemoteArgs{
		Path: r.dir, URL: r.url, Fingerprint: r.fp(), ExpectState: state, ExpectCommit: commit}, &res)
	return res, err
}

func (r *remoteTest) kinds() []string {
	var out []string
	for _, s := range r.sourceList().Sources {
		out = append(out, s.Kind)
	}
	return out
}

func (r *remoteTest) wantLocal() {
	r.t.Helper()
	if got := r.kinds(); !slices.Equal(got, []string{control.SourceKindLocal}) {
		r.t.Fatalf("source kinds = %v, want a local source", got)
	}
}

func (r *remoteTest) opNames() []string {
	var out []string
	for _, op := range r.git.Ops() {
		out = append(out, strings.SplitN(op, " ", 2)[0])
	}
	return out
}

func leak(files ...string) gitrepo.Scan {
	var s gitrepo.Scan
	for _, f := range files {
		s.Findings = append(s.Findings, gitrepo.Finding{Commit: remoteNew, Path: f, Kind: gitrepo.KindSecretsFile})
	}
	s.Total = len(files)
	return s
}

func noValue(t *testing.T, what string, err error) {
	t.Helper()
	if strings.Contains(err.Error(), secretValue) {
		t.Fatalf("%s shows a secret value: %v", what, err)
	}
}

func TestSourceCompareOfALocalSourceReportsStateAndSecretCheck(t *testing.T) {
	r := newRemoteTest(t, true)
	head := r.head()

	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteEmpty, Branch: "main"}, leak("web/secrets.env"))
	res, err := r.compare(r.fp())
	if err != nil {
		t.Fatal(err)
	}
	if res.Case != control.SourceCaseLocal || res.State != "empty" || res.Branch != "main" || res.Commit != "" ||
		res.Secrets == nil || res.Secrets.Total != 1 || res.Secrets.Findings[0].Path != "web/secrets.env" ||
		res.Secrets.Findings[0].Kind != control.SourceSecretFile {
		t.Fatalf("empty = %+v", res)
	}
	// The push of an empty remote would send the whole history from HEAD.
	if got := r.git.Ops(); !slices.Equal(got, []string{"compare-remote " + r.url, "scan " + head + " "}) {
		t.Fatalf("ops = %q", got)
	}
	if want := r.srv.Pattern() + " " + r.srv.Key().Authorized + "\n"; r.knownHosts() != want {
		t.Fatalf("known_hosts = %q", r.knownHosts())
	}

	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteEqual, Branch: "main", Commit: remoteTip}, gitrepo.Scan{})
	if res, err = r.compare(r.fp()); err != nil || res.State != "equal" || res.Commit != remoteTip || res.Secrets != nil {
		t.Fatalf("equal = %+v, %v", res, err)
	}

	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteDifferent, Branch: "main", Commit: remoteTip,
		Paths: []string{"db/compose.yaml", "web/compose.yaml"}, PathCount: 7}, leak("a.env", "b.env"))
	if res, err = r.compare(r.fp()); err != nil {
		t.Fatal(err)
	}
	if res.State != "different" || !slices.Equal(res.Paths, []string{"db/compose.yaml", "web/compose.yaml"}) || res.PathCount != 7 ||
		res.Secrets == nil || res.Secrets.Total != 2 {
		t.Fatalf("different = %+v", res)
	}

	// Nothing but the pinned host key changed.
	for _, op := range r.opNames() {
		if op != "compare-remote" && op != "scan" {
			t.Fatalf("a compare took the step %s", op)
		}
	}
	r.wantLocal()
	if r.head() != head {
		t.Fatal("the branch moved")
	}
}

func TestSourceCompareChecksTheKeysBeforeAnyGitStep(t *testing.T) {
	r := newRemoteTest(t, true)
	other := ssh.FingerprintSHA256(hostkeytest.NewSigner(t).PublicKey())
	_, err := r.compare(other)
	wantCode(t, err, control.CodeHostKey)
	for _, bad := range []string{"", "MD5:aa"} {
		_, err = r.compare(bad)
		wantCode(t, err, control.CodeInvalid)
	}
	var res control.SourceCompareResult
	args := control.SourceCompareArgs{Path: r.dir, URL: "https://git.example.test/demo/stacks.git", Fingerprint: r.fp()}
	wantCode(t, r.call(control.SourceCompareCommand, args, &res), control.CodeInvalid)
	if r.knownHosts() != "" || len(r.git.Ops()) != 0 {
		t.Fatalf("a refused compare pinned %q or took steps %q", r.knownHosts(), r.git.Ops())
	}

	noKey := newRemoteTest(t, false)
	_, err = noKey.compare(noKey.fp())
	wantCode(t, err, control.CodeNotFound)
	if !strings.Contains(err.Error(), "key generate") || noKey.knownHosts() != "" || len(noKey.git.Ops()) != 0 {
		t.Fatalf("missing key: %v, known hosts %q, ops %q", err, noKey.knownHosts(), noKey.git.Ops())
	}
}

func TestSourceCompareRefusesWhatIsNotALocalSourceOrFiles(t *testing.T) {
	r := newRemoteTest(t, true)
	if err := r.call(control.SourceCompareCommand, control.SourceCompareArgs{
		Path: filepath.Join(tempBase(t), "none"), URL: r.url, Fingerprint: r.fp()}, &control.SourceCompareResult{}); err == nil {
		t.Fatal("a missing folder was compared")
	}
	if len(r.git.Ops()) != 0 {
		t.Fatalf("ops = %q", r.git.Ops())
	}
}

func TestSourceRemoteRefusesAChangedRemoteAndWhatIsNotLocal(t *testing.T) {
	r := newRemoteTest(t, true)
	head := r.head()
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteEqual, Branch: "main", Commit: remoteTip}, gitrepo.Scan{})

	for name, c := range map[string]struct{ state, commit string }{
		"state moved":  {control.SourceRemoteDifferent, remoteTip},
		"commit moved": {control.SourceRemoteEqual, remoteNew},
		"was empty":    {control.SourceRemoteEmpty, ""},
	} {
		_, err := r.connect(c.state, c.commit)
		wantCode(t, err, control.CodeInvalid)
		if !strings.Contains(err.Error(), "nothing was changed") {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, bad := range []struct{ state, commit string }{
		{"", ""}, {"equal", ""}, {"empty", remoteTip}, {"different", "not-a-commit"}, {"other", remoteTip},
	} {
		_, err := r.connect(bad.state, bad.commit)
		wantCode(t, err, control.CodeInvalid)
	}
	// Only comparisons were made: the repository has no remote, the branch stayed, the source stays local.
	for _, op := range r.opNames() {
		if op != "compare-remote" {
			t.Fatalf("a refused command took the step %s", op)
		}
	}
	r.wantLocal()
	if r.head() != head {
		t.Fatal("the branch moved")
	}

	other := filepath.Join(tempBase(t), "other")
	if err := r.call(control.SourceRemoteCommand, control.SourceRemoteArgs{
		Path: other, URL: r.url, Fingerprint: r.fp(), ExpectState: "empty"}, &control.SourceRemoteResult{}); err == nil {
		t.Fatal("a folder that is no source was connected")
	} else {
		wantCode(t, err, control.CodeNotFound)
	}
	wantCode(t, r.call(control.SourceRemoteCommand, control.SourceRemoteArgs{
		Path: r.dir, URL: r.url, Fingerprint: "SHA256:wrong", ExpectState: "equal", ExpectCommit: remoteTip}, &control.SourceRemoteResult{}),
		control.CodeHostKey)
}

func TestSourceRemoteRefusesAWrongFingerprintAndAMissingKeyWithoutEffect(t *testing.T) {
	r := newRemoteTest(t, true)
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteEmpty, Branch: "main"}, gitrepo.Scan{})
	wrong := ssh.FingerprintSHA256(hostkeytest.NewSigner(t).PublicKey())
	_, err := r.call2(control.SourceRemoteArgs{Path: r.dir, URL: r.url, Fingerprint: wrong, ExpectState: "empty"})
	wantCode(t, err, control.CodeHostKey)
	if r.knownHosts() != "" || len(r.git.Ops()) != 0 {
		t.Fatalf("pinned %q, steps %q", r.knownHosts(), r.git.Ops())
	}
	r.wantLocal()

	noKey := newRemoteTest(t, false)
	noKey.cmp(gitrepo.Comparison{State: gitrepo.RemoteEmpty, Branch: "main"}, gitrepo.Scan{})
	_, err = noKey.connect("empty", "")
	wantCode(t, err, control.CodeNotFound)
	if noKey.knownHosts() != "" || len(noKey.git.Ops()) != 0 {
		t.Fatalf("pinned %q, steps %q", noKey.knownHosts(), noKey.git.Ops())
	}
	noKey.wantLocal()
}

func (r *remoteTest) call2(a control.SourceRemoteArgs) (control.SourceRemoteResult, error) {
	var res control.SourceRemoteResult
	err := r.call(control.SourceRemoteCommand, a, &res)
	return res, err
}

func TestSourceRemoteRefusesWhileARunIsActive(t *testing.T) {
	r := newRemoteTest(t, true)
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteEmpty, Branch: "main"}, gitrepo.Scan{})
	entered, release := r.dep.hold()
	var dep control.DeployResult
	if err := r.call(control.DeployCommand, control.DeployArgs{Stack: "web"}, &dep); err != nil {
		t.Fatal(err)
	}
	<-entered
	_, err := r.connect("empty", "")
	wantCode(t, err, control.CodeRunActive)
	if len(r.git.Ops()) != 0 {
		t.Fatalf("steps %q", r.git.Ops())
	}
	r.wantLocal()
	release()
	waitRun(t, r.harness, dep.ID)
}

func TestSourceRemoteRefusesSecretsBeforeChangingAnything(t *testing.T) {
	r := newRemoteTest(t, true)
	head := r.head()

	// An empty remote: the history is scanned before the remote is added.
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteEmpty, Branch: "main"}, leak("web/secrets.env"))
	_, err := r.connect("empty", "")
	wantCode(t, err, control.CodeSecrets)
	for _, want := range []string{"web/secrets.env", "secrets-file", remoteNew[:12], "nothing was changed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message lacks %q: %v", want, err)
		}
	}
	noValue(t, "message", err)
	if got := r.opNames(); !slices.Equal(got, []string{"compare-remote", "scan"}) {
		t.Fatalf("steps = %q", got)
	}
	r.wantLocal()

	// A different remote: the remote entry may stay, the branch does not move and nothing is pushed.
	r.git.Configure(func(g *servicetest.Git) { g.Remote, g.HostCommit, g.NotAncestor = remoteTip, remoteNew, true })
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteDifferent, Branch: "main", Commit: remoteTip, PathCount: 1}, leak("web/secrets.env"))
	_, err = r.connect("different", remoteTip)
	wantCode(t, err, control.CodeSecrets)
	if !strings.Contains(err.Error(), "the branch was not moved") {
		t.Fatalf("message = %v", err)
	}
	for _, op := range r.opNames() {
		if slices.Contains([]string{"save-history", "reset-branch", "push-branch"}, op) {
			t.Fatalf("a refused command took the step %s: %q", op, r.git.Ops())
		}
	}
	r.wantLocal()
	if r.head() != head {
		t.Fatal("the branch moved")
	}

	// The same command goes through once the secret is gone, with the remote entry of the first attempt.
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteDifferent, Branch: "main", Commit: remoteTip, PathCount: 1}, gitrepo.Scan{})
	res, err := r.connect("different", remoteTip)
	if err != nil || !res.Pushed {
		t.Fatalf("retry = %+v, %v", res, err)
	}
	if n := strings.Count(strings.Join(r.git.Ops(), "\n"), "add-remote"); n != 1 {
		t.Fatalf("the remote was added %d times", n)
	}
}

func TestSourceRemoteGivesAnEmptyRemoteTheBranchAndKeepsTheEngineState(t *testing.T) {
	r := newRemoteTest(t, true)
	before := r.waitDeployed()
	head := r.head()
	calls := len(r.dep.callsOf("web"))
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteEmpty, Branch: "main"}, gitrepo.Scan{})

	res, err := r.connect("empty", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "empty" || res.Branch != "main" || res.Head != head || !res.Pushed || res.HistoryRef != "" ||
		res.URL != r.url || res.Path != r.dir {
		t.Fatalf("result = %+v", res)
	}
	want := []string{"compare-remote " + r.url, "scan " + head + " ", "add-remote " + r.url, "set-upstream", "push-branch"}
	if got := r.git.Ops(); !slices.Equal(got, want) {
		t.Fatalf("steps = %q, want %q", got, want)
	}

	list := r.sourceList()
	if len(list.Sources) != 1 || list.Sources[0].Kind != control.SourceKindRemote || list.Sources[0].URL != r.url || list.Sources[0].ID != res.ID {
		t.Fatalf("list = %+v", list)
	}
	cfg := r.git.ConfigFor(r.dir)
	if cfg.Local || cfg.KeyFile != filepath.Join(r.state, "deploy-key") || cfg.KnownHosts != filepath.Join(r.state, "known_hosts") {
		t.Fatalf("git configuration of the source = %+v", cfg)
	}
	if data, err := os.ReadFile(filepath.Join(r.state, "sources.json")); err != nil || !strings.Contains(string(data), `"kind": "remote"`) ||
		!strings.Contains(string(data), r.url) {
		t.Fatalf("sources.json = %s, %v", data, err)
	}

	// The engine runs on the state it had: the deployed stack stays deployed and is not deployed again.
	st := r.waitStatus("polled as a remote source", func(st control.StatusResult) bool {
		return len(st.Engine.Sources) == 1 && st.Engine.Sources[0].Kind == control.SourceKindRemote && st.Engine.Sources[0].Head == head &&
			st.Engine.Sources[0].PollError == ""
	})
	after := stacksNamed(st, "web")
	if len(after) != 1 || after[0].Deployed != before.Deployed || len(r.dep.callsOf("web")) != calls {
		t.Fatalf("stack after = %+v, before %+v, deployer calls %d -> %d", after, before, calls, len(r.dep.callsOf("web")))
	}
	if !strings.Contains(readState(t, r.state, r.dir), "web") {
		t.Fatalf("engine state = %s", readState(t, r.state, r.dir))
	}
	// A second connection is refused: the source has a remote now.
	_, err = r.connect("empty", "")
	wantCode(t, err, control.CodeInvalid)
}

func TestSourceRemoteFollowsAnEqualRemoteWithoutAPush(t *testing.T) {
	r := newRemoteTest(t, true)
	head := r.head()
	r.git.Configure(func(g *servicetest.Git) { g.Remote = remoteTip })
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteEqual, Branch: "main", Commit: remoteTip}, gitrepo.Scan{})

	res, err := r.connect("equal", remoteTip)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "equal" || res.Head != remoteTip || res.Pushed || res.HistoryRef != gitrepo.LocalHistoryRef {
		t.Fatalf("result = %+v", res)
	}
	want := []string{"compare-remote " + r.url, "add-remote " + r.url, "fetch-branch main", "save-history " + head,
		"reset-branch " + remoteTip, "set-upstream"}
	if got := r.git.Ops(); !slices.Equal(got, want) {
		t.Fatalf("steps = %q, want %q", got, want)
	}
	if got := r.kinds(); !slices.Equal(got, []string{control.SourceKindRemote}) {
		t.Fatalf("kinds = %v", got)
	}
}

func TestSourceRemotePushesOneCommitToADifferentRemote(t *testing.T) {
	r := newRemoteTest(t, true)
	head := r.head()
	r.git.Configure(func(g *servicetest.Git) { g.Remote, g.HostCommit, g.NotAncestor = remoteTip, remoteNew, true })
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteDifferent, Branch: "main", Commit: remoteTip, PathCount: 2}, gitrepo.Scan{})

	res, err := r.connect("different", remoteTip)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "different" || res.Head != remoteNew || !res.Pushed || res.HistoryRef != gitrepo.LocalHistoryRef {
		t.Fatalf("result = %+v", res)
	}
	want := []string{"compare-remote " + r.url, "add-remote " + r.url, "fetch-branch main", "commit-host-tree " + remoteTip,
		"scan " + remoteNew + " " + remoteTip, "save-history " + head, "reset-branch " + remoteNew, "set-upstream", "push-branch"}
	if got := r.git.Ops(); !slices.Equal(got, want) {
		t.Fatalf("steps = %q, want %q", got, want)
	}
	if got := r.kinds(); !slices.Equal(got, []string{control.SourceKindRemote}) {
		t.Fatalf("kinds = %v", got)
	}
}

func TestSourceRemoteKeepsTheSourceLocalWhenThePushFailsAndCanBeRepeated(t *testing.T) {
	r := newRemoteTest(t, true)
	r.git.Configure(func(g *servicetest.Git) {
		g.Remote, g.HostCommit, g.NotAncestor, g.PushErr = remoteTip, remoteNew, true, errors.New("remote rejected the update")
	})
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteDifferent, Branch: "main", Commit: remoteTip, PathCount: 2}, gitrepo.Scan{})

	_, err := r.connect("different", remoteTip)
	wantCode(t, err, control.CodeRemote)
	for _, want := range []string{"branch main", gitrepo.LocalHistoryRef, "the source stays local", "remote rejected"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message lacks %q: %v", want, err)
		}
	}
	r.wantLocal()
	if r.head() != remoteNew {
		t.Fatalf("head = %s", r.head())
	}

	// The branch follows the remote commit already, so the next attempt pushes it and creates no second commit.
	r.git.Configure(func(g *servicetest.Git) { g.PushErr, g.NotAncestor = nil, false })
	res, err := r.connect("different", remoteTip)
	if err != nil || !res.Pushed || res.Head != remoteNew {
		t.Fatalf("second attempt = %+v, %v", res, err)
	}
	if n := strings.Count(strings.Join(r.git.Ops(), "\n"), "commit-host-tree"); n != 1 {
		t.Fatalf("commit-host-tree ran %d times", n)
	}
	if got := r.kinds(); !slices.Equal(got, []string{control.SourceKindRemote}) {
		t.Fatalf("kinds = %v", got)
	}
}

func TestSourceRemoteRefusesAMovedRemoteAfterTheFetch(t *testing.T) {
	r := newRemoteTest(t, true)
	head := r.head()
	// The branch the fetch brings is not the compared commit.
	r.git.Configure(func(g *servicetest.Git) { g.Remote = remoteNew })
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteDifferent, Branch: "main", Commit: remoteTip, PathCount: 1}, gitrepo.Scan{})
	_, err := r.connect("different", remoteTip)
	wantCode(t, err, control.CodeInvalid)
	for _, op := range r.opNames() {
		if slices.Contains([]string{"commit-host-tree", "save-history", "reset-branch", "push-branch"}, op) {
			t.Fatalf("a moved remote led to %s", op)
		}
	}
	r.wantLocal()
	if r.head() != head {
		t.Fatal("the branch moved")
	}
}

// filesFolder is a folder of files without a repository and the file list inspect shows for it.
func (r *remoteTest) filesFolder(t *testing.T) (string, []string) {
	t.Helper()
	dir := filepath.Join(tempBase(t), "files")
	writeTree(t, dir, map[string]string{
		"web/compose.yaml": webCompose, "web/.env": "WEB_VERSION=1.2.3\n", "web/secrets.env": "DEMO_TOKEN=" + secretValue + "\n"})
	shown := r.inspect(dir, r.url)
	if shown.Case != control.SourceCaseFiles || shown.URL != r.url || len(shown.PlannedFiles) != 3 {
		t.Fatalf("inspect = %+v", shown)
	}
	return dir, shown.PlannedFiles
}

func (r *remoteTest) addRemote(dir string, files []string, fp, state, commit string) (control.SourceAddResult, error) {
	var res control.SourceAddResult
	err := r.call(control.SourceAddCommand, control.SourceAddArgs{
		Path: dir, Files: files, URL: r.url, Fingerprint: fp, ExpectState: state, ExpectCommit: commit}, &res)
	return res, err
}

func (r *remoteTest) noGit(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		t.Fatal("a .git was created")
	}
	if len(r.sourceList().Sources) != 1 {
		t.Fatalf("a folder was bound: %+v", r.sourceList())
	}
}

func TestSourceCompareOfAFolderOfFilesAndTheInspectOfAnAddress(t *testing.T) {
	r := newRemoteTest(t, true)
	dir, files := r.filesFolder(t)

	// An address is no reason to refuse a folder of files, and still one for a repository without a remote.
	local := localRepo(t, localFiles())
	if res := r.inspect(local, r.url); res.Case != control.SourceCaseReject || !strings.Contains(res.Fix, "qdeploy source remote") {
		t.Fatalf("local with an address = %+v", res)
	}

	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteDifferent, Branch: "trunk", Commit: remoteTip, Paths: []string{"web/secrets.env"}, PathCount: 1},
		leak("web/secrets.env"))
	var res control.SourceCompareResult
	if err := r.call(control.SourceCompareCommand, control.SourceCompareArgs{Path: dir, URL: r.url, Fingerprint: r.fp()}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Case != control.SourceCaseFiles || res.State != "different" || res.Branch != "trunk" || res.Commit != remoteTip ||
		!slices.Equal(res.PlannedFiles, files) || res.Secrets == nil || res.Secrets.Total != 1 {
		t.Fatalf("compare = %+v", res)
	}
	if got := r.git.Ops(); !slices.Equal(got, []string{"compare-folder " + r.url + " 3"}) {
		t.Fatalf("steps = %q", got)
	}
	r.noGit(t, dir)

	// An empty remote has nothing to compare and no push to scan.
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteEmpty, Branch: "main"}, gitrepo.Scan{})
	var empty control.SourceCompareResult
	if err := r.call(control.SourceCompareCommand, control.SourceCompareArgs{Path: dir, URL: r.url, Fingerprint: r.fp()}, &empty); err != nil ||
		empty.State != "empty" || empty.Secrets != nil {
		t.Fatalf("empty = %+v, %v", empty, err)
	}
	// A repository that is no source yet is compared after it is bound.
	err := r.call(control.SourceCompareCommand, control.SourceCompareArgs{Path: local, URL: r.url, Fingerprint: r.fp()}, &res)
	wantCode(t, err, control.CodeInvalid)
}

func TestSourceAddWithAnAddressCreatesNothingWhenTheAnswerChangedOrIsRefused(t *testing.T) {
	r := newRemoteTest(t, true)
	dir, files := r.filesFolder(t)
	r.git.Configure(func(g *servicetest.Git) { g.HostCommit = remoteNew })

	// The remote has no commit: the folder is taken over without an address first.
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteEmpty, Branch: "main"}, gitrepo.Scan{})
	_, err := r.addRemote(dir, files, r.fp(), "empty", "")
	wantCode(t, err, control.CodeInvalid)
	if !strings.Contains(err.Error(), "source remote") {
		t.Fatalf("message = %v", err)
	}
	r.noGit(t, dir)

	// The answer is not the one that was shown.
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteEqual, Branch: "main", Commit: remoteTip}, gitrepo.Scan{})
	for _, c := range []struct{ state, commit string }{{"different", remoteTip}, {"equal", remoteNew}} {
		_, err = r.addRemote(dir, files, r.fp(), c.state, c.commit)
		wantCode(t, err, control.CodeInvalid)
		if !strings.Contains(err.Error(), "nothing was changed") {
			t.Fatalf("message = %v", err)
		}
	}
	r.noGit(t, dir)

	// A secret in what would be pushed stops it before .git exists.
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteDifferent, Branch: "main", Commit: remoteTip, PathCount: 1}, leak("web/secrets.env"))
	_, err = r.addRemote(dir, files, r.fp(), "different", remoteTip)
	wantCode(t, err, control.CodeSecrets)
	if !strings.Contains(err.Error(), "web/secrets.env") || !strings.Contains(err.Error(), "nothing was created") {
		t.Fatalf("message = %v", err)
	}
	noValue(t, "message", err)
	r.noGit(t, dir)

	// A wrong host key, a changed list and an invalid state.
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteEqual, Branch: "main", Commit: remoteTip}, gitrepo.Scan{})
	wrong := ssh.FingerprintSHA256(hostkeytest.NewSigner(t).PublicKey())
	_, err = r.addRemote(dir, files, wrong, "equal", remoteTip)
	wantCode(t, err, control.CodeHostKey)
	_, err = r.addRemote(dir, files[1:], r.fp(), "equal", remoteTip)
	wantCode(t, err, control.CodeInvalid)
	_, err = r.addRemote(dir, files, r.fp(), "equal", "")
	wantCode(t, err, control.CodeInvalid)
	r.noGit(t, dir)
	for _, op := range r.opNames() {
		if op == "init-from-remote" || op == "push-branch" {
			t.Fatalf("a refused add took the step %s", op)
		}
	}
}

func TestSourceAddWithAnAddressFollowsAnEqualRemote(t *testing.T) {
	r := newRemoteTest(t, true)
	dir, files := r.filesFolder(t)
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteEqual, Branch: "main", Commit: remoteTip}, gitrepo.Scan{})

	res, err := r.addRemote(dir, files, r.fp(), "equal", remoteTip)
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != control.SourceKindRemote || res.Head != remoteTip || res.Pushed || res.Files != 3 || res.Path != dir {
		t.Fatalf("add = %+v", res)
	}
	want := []string{"compare-folder " + r.url + " 3", "init-from-remote " + r.url + " main " + remoteTip + " 3"}
	if got := r.git.Ops(); !slices.Equal(got, want) {
		t.Fatalf("steps = %q, want %q", got, want)
	}
	kinds := map[string]string{}
	for _, s := range r.sourceList().Sources {
		kinds[s.Path] = s.Kind
	}
	if kinds[dir] != control.SourceKindRemote || kinds[r.dir] != control.SourceKindLocal {
		t.Fatalf("sources = %v", kinds)
	}
	if cfg := r.git.ConfigFor(dir); cfg.Local || cfg.KeyFile == "" {
		t.Fatalf("git configuration = %+v", cfg)
	}
}

func TestSourceAddWithAnAddressPushesOneCommitToADifferentRemote(t *testing.T) {
	r := newRemoteTest(t, true)
	dir, files := r.filesFolder(t)
	r.git.Configure(func(g *servicetest.Git) { g.HostCommit = remoteNew })
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteDifferent, Branch: "main", Commit: remoteTip, PathCount: 1}, gitrepo.Scan{})

	res, err := r.addRemote(dir, files, r.fp(), "different", remoteTip)
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != control.SourceKindRemote || res.Head != remoteNew || !res.Pushed {
		t.Fatalf("add = %+v", res)
	}
	if got := r.opNames(); !slices.Equal(got, []string{"compare-folder", "init-from-remote", "push-branch"}) {
		t.Fatalf("steps = %q", got)
	}
}

func TestSourceAddWithAnAddressLeavesTheRepositoryUnboundWhenThePushFails(t *testing.T) {
	r := newRemoteTest(t, true)
	dir, files := r.filesFolder(t)
	r.git.Configure(func(g *servicetest.Git) {
		g.HostCommit, g.PushErr = remoteNew, errors.New("remote rejected the update")
	})
	r.cmp(gitrepo.Comparison{State: gitrepo.RemoteDifferent, Branch: "main", Commit: remoteTip, PathCount: 1}, gitrepo.Scan{})

	_, err := r.addRemote(dir, files, r.fp(), "different", remoteTip)
	wantCode(t, err, control.CodeRemote)
	for _, want := range []string{"branch main", remoteNew[:12], "the source is not bound", "remote rejected"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message lacks %q: %v", want, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf("the repository was removed: %v", err)
	}
	if len(r.sourceList().Sources) != 1 {
		t.Fatalf("the folder was bound: %+v", r.sourceList())
	}
}
