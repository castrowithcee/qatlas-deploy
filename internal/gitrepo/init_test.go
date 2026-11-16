package gitrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type fileState struct {
	mode  os.FileMode
	mtime time.Time
	body  string
}

// treeState records mode, mtime and content of everything below dir except .git.
func treeState(t *testing.T, dir string) map[string]fileState {
	t.Helper()
	out := map[string]fileState{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
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
		out[p] = fileState{info.Mode(), info.ModTime(), body}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func initFolder(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := lookIn(fixedPath, "git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := filepath.Join(t.TempDir(), "stacks")
	writeTree(t, dir, files)
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	for p := range treeState(t, dir) {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestInitStartsARepositoryWithExactlyTheGivenFiles(t *testing.T) {
	ctx := context.Background()
	odd := "odd [a]*?\\#!name  "
	dir := initFolder(t, map[string]string{
		"web/compose.yaml": "a\r\nb\n",
		"web/.env":         "APP_VERSION=1.0.0\n",
		"web/data/db.bin":  "x",
		"top.txt":          "t\n",
		odd:                "y",
		"web/secrets.env":  "TOKEN=demo\n",
		".gitattributes":   "* text=auto eol=lf\n",
	})
	if err := os.Chmod(filepath.Join(dir, "top.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	// An ignored-looking neighbour of a listed file stays out because it is not listed.
	files := []string{".gitattributes", "top.txt", "web/.env", "web/compose.yaml"}
	excluded := []string{"web/data", odd, "web/secrets.env"}
	before := treeState(t, dir)

	id, err := Init(ctx, Config{Local: true}, dir, files, excluded)
	if err != nil {
		t.Fatal(err)
	}
	if after := treeState(t, dir); len(after) != len(before) {
		t.Fatalf("entries before %d, after %d", len(before), len(after))
	} else {
		for p, b := range before {
			// The folder itself gains its .git entry.
			if a := after[p]; p != dir && (a.mode != b.mode || !a.mtime.Equal(b.mtime) || a.body != b.body) {
				t.Errorf("%s changed: %+v -> %+v", p, b, a)
			}
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf(".gitignore: %v", err)
	}
	if got := fixtureGit(t, dir, "rev-parse", "HEAD"); got != id {
		t.Fatalf("HEAD = %s, init returned %s", got, id)
	}
	if got := fixtureGit(t, dir, "symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Fatalf("branch = %q", got)
	}
	if got := fixtureGit(t, dir, "config", "--local", "core.sharedRepository"); got != "group" {
		t.Fatalf("core.sharedRepository = %q", got)
	}
	if got := strings.Fields(fixtureGit(t, dir, "ls-tree", "-r", "--name-only", "-z", "HEAD")); len(got) == 0 {
		t.Fatal("empty commit")
	}
	listed := strings.Split(strings.TrimSuffix(fixtureGit(t, dir, "ls-tree", "-r", "--name-only", "-z", "HEAD"), "\x00"), "\x00")
	if !slices.Equal(listed, files) {
		t.Fatalf("committed = %q, want %q", listed, files)
	}
	if got := fixtureGit(t, dir, "show", "-s", "--format=%an|%ae|%cn|%ce|%s|%P", id); got !=
		commitName+"|"+commitEmail+"|"+commitName+"|"+commitEmail+"|"+firstCommitMessage+"|" {
		t.Fatalf("commit = %q", got)
	}
	// The stored bytes are the bytes on disk, whatever the attributes of the folder say.
	if got := fixtureGit(t, dir, "show", "HEAD:web/compose.yaml"); got != "a\r\nb" {
		t.Fatalf("blob = %q", got)
	}
	if got := fixtureGit(t, dir, "ls-tree", "HEAD", "top.txt"); !strings.HasPrefix(got, "100755 ") {
		t.Fatalf("mode = %q", got)
	}
	// Nothing is left to commit or to report: the excluded paths are ignored, the rest is tracked.
	if got := fixtureGit(t, dir, "status", "--porcelain", "--untracked-files=all"); got != "" {
		t.Fatalf("status = %q", got)
	}
	exclude, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exclude), "/web/data\n") || !strings.Contains(string(exclude), `/odd \[a]\*\?\\#!name \ `+"\n") {
		t.Fatalf("exclude = %q", exclude)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".git", "info", "attributes")); !os.IsNotExist(err) {
		t.Fatalf("attributes left behind: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".git", "hooks")); !os.IsNotExist(err) {
		t.Fatalf("hooks: %v", err)
	}
	r, err := Open(Config{Local: true}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if head, err := r.RemoteHead(ctx); err != nil || head != id {
		t.Fatalf("base = %s, %v", head, err)
	}
}

func TestInitRefusesAnExistingGitAndCleansUpAfterAFailure(t *testing.T) {
	ctx := context.Background()
	dir := initFolder(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	mark := filepath.Join(dir, ".git")
	if err := os.Mkdir(mark, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mark, "keep"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(ctx, Config{Local: true}, dir, []string{"a.txt"}, nil); !errors.Is(err, ErrHasGit) {
		t.Fatalf("err = %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(mark, "keep")); err != nil || string(b) != "k" {
		t.Fatalf("an existing .git was touched: %q, %v", b, err)
	}
	if err := os.RemoveAll(mark); err != nil {
		t.Fatal(err)
	}

	// git add fails for a file that is not there, after .git was created; Init removes its own .git.
	if err := os.Remove(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal(err)
	}
	before := treeState(t, dir)
	for name, c := range map[string]struct {
		files, excluded []string
	}{
		"vanished":  {files: []string{"a.txt", "b.txt"}},
		"ignored":   {files: []string{"a.txt"}, excluded: []string{"a.txt"}},
		"bad":       {files: []string{"../a.txt"}},
		"directory": {files: []string{"."}},
		"none":      {},
	} {
		if _, err := Init(ctx, Config{Local: true}, dir, c.files, c.excluded); err == nil {
			t.Errorf("%s: no error", name)
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
			t.Errorf("%s: .git left behind: %v", name, err)
		}
	}
	after := treeState(t, dir)
	for p, b := range before {
		if a, ok := after[p]; !ok || a.body != b.body || (p != dir && !a.mtime.Equal(b.mtime)) {
			t.Errorf("%s changed", p)
		}
	}
}

func TestExcludePatternMasksWhatGitReadsAsSyntax(t *testing.T) {
	for in, want := range map[string]string{
		"a/b":     "/a/b",
		"#x":      "/#x",
		"!x":      "/!x",
		"a b ":    `/a b\ `,
		"a*?[\\":  `/a\*\?\[\\`,
		"two  ":   `/two \ `,
		"web/x y": "/web/x y",
	} {
		if got, ok := excludePattern(in); !ok || got != want {
			t.Errorf("excludePattern(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "a\nb", "a\x00b", "a\rb"} {
		if _, ok := excludePattern(in); ok {
			t.Errorf("excludePattern(%q) accepted", in)
		}
	}
}
