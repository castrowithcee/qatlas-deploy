package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"github.com/castrowithcee/qatlas-kit/selfupdate"
	"github.com/castrowithcee/qatlas-kit/sysservice"

	"github.com/castrowithcee/qatlas-deploy/internal/release"
)

const apiRepo = "/repos/castrowithcee/qatlas-deploy"

type fakeAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type fakeRelease struct {
	TagName string      `json:"tag_name"`
	Assets  []fakeAsset `json:"assets"`
}

// fakeReleases serves the GitHub API subset that selfupdate reads, with assets signed by signer.
type fakeReleases struct {
	server   *httptest.Server
	releases []fakeRelease
	blobs    map[string][]byte
}

func newFakeReleases(t *testing.T, signer ssh.Signer, tamper bool, tags ...string) *fakeReleases {
	t.Helper()
	f := &fakeReleases{blobs: map[string][]byte{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	id := 0
	for _, tag := range tags {
		name := fmt.Sprintf("qdeploy_%s_linux_%s.tar.gz", tag, runtime.GOARCH)
		archive := releasetest.Archive(t, name,
			releasetest.File{Name: "bin/qdeploy", Body: []byte("new program " + tag)},
			releasetest.File{Name: sysservice.UnitEntry("qdeploy"), Body: []byte("[Service]\n# unit " + tag + "\n")},
			releasetest.File{Name: manEntry, Body: []byte(".TH QDEPLOY 1 " + tag + "\n")})
		assets := releasetest.Release(t, signer, release.Namespace, map[string][]byte{name: archive})
		if tamper {
			assets["checksums.txt"] = append(assets["checksums.txt"], '\n')
		}
		rel := fakeRelease{TagName: tag}
		for _, n := range []string{name, "checksums.txt", "checksums.txt.sig"} {
			id++
			rel.Assets = append(rel.Assets, fakeAsset{Name: n, URL: f.server.URL + apiRepo + "/releases/assets/" + strconv.Itoa(id)})
			f.blobs[strconv.Itoa(id)] = assets[n]
		}
		f.releases = append(f.releases, rel)
	}
	return f
}

func (f *fakeReleases) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == apiRepo+"/releases":
		if r.URL.Query().Get("page") != "1" {
			_ = json.NewEncoder(w).Encode([]fakeRelease{})
			return
		}
		_ = json.NewEncoder(w).Encode(f.releases)
	case strings.HasPrefix(r.URL.Path, apiRepo+"/releases/tags/"):
		for _, rel := range f.releases {
			if rel.TagName == strings.TrimPrefix(r.URL.Path, apiRepo+"/releases/tags/") {
				_ = json.NewEncoder(w).Encode(rel)
				return
			}
		}
		http.NotFound(w, r)
	case strings.HasPrefix(r.URL.Path, apiRepo+"/releases/assets/"):
		if blob, ok := f.blobs[strings.TrimPrefix(r.URL.Path, apiRepo+"/releases/assets/")]; ok {
			_, _ = w.Write(blob)
			return
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

type fakeHost struct {
	dir      string
	exe      string
	unit     string
	restarts []selfupdate.Result
}

// useFakeHost points the self-update at the fake release server, trusts signer and records restarts.
func useFakeHost(t *testing.T, f *fakeReleases, signer ssh.Signer, running string) *fakeHost {
	t.Helper()
	h := &fakeHost{dir: t.TempDir()}
	h.exe = filepath.Join(h.dir, "qdeploy")
	h.unit = filepath.Join(h.dir, "qdeploy.service")
	if err := os.WriteFile(h.exe, []byte("old program"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldHost, oldVersion := selfUpdate, version
	t.Cleanup(func() { selfUpdate, version = oldHost, oldVersion })
	version = running
	selfUpdate = selfUpdateHost{
		apiURL:     f.server.URL + apiRepo,
		keys:       func() []ssh.PublicKey { return []ssh.PublicKey{signer.PublicKey()} },
		euid:       func() int { return 0 },
		executable: func() (string, error) { return h.exe, nil },
		companions: []selfupdate.Companion{
			{Entry: sysservice.UnitEntry("qdeploy"), Path: h.unit},
			{Entry: manEntry, Path: filepath.Join(h.dir, "qdeploy.1")},
		},
		restart: func(_ context.Context, result selfupdate.Result) error {
			h.restarts = append(h.restarts, result)
			return nil
		},
		shadowed: func() (bool, error) { return false, nil },
	}
	return h
}

func (h *fakeHost) program(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(h.exe)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestSelfUpdateStatusReportsNewerRelease(t *testing.T) {
	signer := releasetest.NewSigner(t)
	h := useFakeHost(t, newFakeReleases(t, signer, false, "v0.1.0", "v0.2.0"), signer, "v0.1.0")
	selfUpdate.euid = func() int { return 1000 }
	code, stdout, stderr := run(t, "update", "status", "--output", "json")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if got["installed"] != "v0.1.0" || got["latest"] != "v0.2.0" || got["update_available"] != true {
		t.Fatalf("status = %v", got)
	}
	if h.program(t) != "old program" || len(h.restarts) != 0 {
		t.Fatal("status changed the installation")
	}
}

func TestSelfUpdateInstallsLatestAndRestarts(t *testing.T) {
	signer := releasetest.NewSigner(t)
	h := useFakeHost(t, newFakeReleases(t, signer, false, "v0.1.0", "v0.2.0"), signer, "v0.1.0")
	code, stdout, stderr := run(t, "update", "--yes", "--confirm", "--output", "json")
	if code != 0 {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if h.program(t) != "new program v0.2.0" {
		t.Fatalf("program = %q", h.program(t))
	}
	if len(h.restarts) != 1 || h.restarts[0].Latest != "v0.2.0" || len(h.restarts[0].Companions) == 0 {
		t.Fatalf("restarts = %+v", h.restarts)
	}
	if !strings.Contains(stdout, `"updated":true`) && !strings.Contains(stdout, `"updated": true`) {
		t.Fatalf("stdout %q", stdout)
	}
}

func TestSelfUpdateToNamedVersion(t *testing.T) {
	signer := releasetest.NewSigner(t)
	h := useFakeHost(t, newFakeReleases(t, signer, false, "v0.1.0", "v0.2.0", "v0.3.0"), signer, "v0.1.0")
	if code, stdout, stderr := run(t, "update", "v0.2.0", "--yes", "--confirm"); code != 0 {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if h.program(t) != "new program v0.2.0" || len(h.restarts) != 1 {
		t.Fatalf("program %q, restarts %d", h.program(t), len(h.restarts))
	}
}

func TestSelfUpdateRefusesTamperedSignature(t *testing.T) {
	signer := releasetest.NewSigner(t)
	h := useFakeHost(t, newFakeReleases(t, signer, true, "v0.1.0", "v0.2.0"), signer, "v0.1.0")
	code, _, stderr := run(t, "update", "--yes", "--confirm")
	if code == 0 {
		t.Fatal("a release with a manipulated checksum file was installed")
	}
	if !strings.Contains(stderr, "update:") {
		t.Fatalf("stderr %q", stderr)
	}
	if h.program(t) != "old program" || len(h.restarts) != 0 {
		t.Fatal("the installation changed or the service restarted")
	}
}

func TestSelfUpdateRefusesUntrustedKey(t *testing.T) {
	signer := releasetest.NewSigner(t)
	h := useFakeHost(t, newFakeReleases(t, signer, false, "v0.1.0", "v0.2.0"), signer, "v0.1.0")
	other := releasetest.NewSigner(t)
	selfUpdate.keys = func() []ssh.PublicKey { return []ssh.PublicKey{other.PublicKey()} }
	if code, _, _ := run(t, "update", "--yes", "--confirm"); code == 0 {
		t.Fatal("a release signed by an untrusted key was installed")
	}
	if h.program(t) != "old program" || len(h.restarts) != 0 {
		t.Fatal("the installation changed or the service restarted")
	}
}

func TestSelfUpdateNeedsRootAndConfirm(t *testing.T) {
	signer := releasetest.NewSigner(t)
	h := useFakeHost(t, newFakeReleases(t, signer, false, "v0.1.0", "v0.2.0"), signer, "v0.1.0")
	selfUpdate.euid = func() int { return 1000 }
	if code, _, stderr := run(t, "update", "--yes", "--confirm"); code == 0 || !strings.Contains(stderr, "root") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	selfUpdate.euid = func() int { return 0 }
	if code, _, _ := run(t, "update", "--yes"); code == 0 {
		t.Fatal("update ran without --confirm")
	}
	if h.program(t) != "old program" || len(h.restarts) != 0 {
		t.Fatal("the installation changed")
	}
}

func TestSelfUpdateNothingToDo(t *testing.T) {
	signer := releasetest.NewSigner(t)
	h := useFakeHost(t, newFakeReleases(t, signer, false, "v0.1.0", "v0.2.0"), signer, "v0.2.0")
	if code, stdout, stderr := run(t, "update", "--yes", "--confirm"); code != 0 || !strings.Contains(stdout, "false") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if code, _, _ := run(t, "update", "v0.1.0", "--yes", "--confirm"); code != 2 {
		t.Fatalf("an older version gave code %d, want 2", code)
	}
	if h.program(t) != "old program" || len(h.restarts) != 0 {
		t.Fatal("the installation changed")
	}
}

func TestSelfUpdateDevelopmentBuild(t *testing.T) {
	signer := releasetest.NewSigner(t)
	useFakeHost(t, newFakeReleases(t, signer, false, "v0.1.0"), signer, "dev")
	for _, args := range [][]string{{"update", "status"}, {"update", "--yes", "--confirm"}} {
		if code, _, stderr := run(t, args...); code == 0 || !strings.Contains(stderr, "development build") {
			t.Fatalf("%v: code %d, stderr %q", args, code, stderr)
		}
	}
}

func TestStackUpdateStaysSeparate(t *testing.T) {
	code, stdout, _ := run(t, "stack", "update", "--help")
	if code != 0 || !strings.Contains(stdout, "<stack>") {
		t.Fatalf("code %d, stdout %q", code, stdout)
	}
}
