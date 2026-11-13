package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPreviewListsWhatGitAddWouldTakeAndWritesNothing(t *testing.T) {
	if _, err := lookIn(fixedPath, "git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "stacks")
	writeTree(t, dir, map[string]string{
		".gitignore":          "secrets.env\ncache/\n",
		"web/compose.yaml":    "a\n",
		"web/secrets.env":     "TOKEN=demo-value\n",
		"web/data/db.bin":     "x",
		"cache/blob":          "x",
		"odd [name]/file.txt": "x",
		"inner/readme.txt":    "x",
	})
	fixtureGit(t, filepath.Join(dir, "inner"), "init", "-q")
	// An ignore rule of the folder covers a path that does not exist, too.
	p, err := OpenPreview(ctx, Config{Local: true}, dir, []string{"web/data", "odd [name]"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	got, err := p.Untracked(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".gitignore", "inner/", "web/compose.yaml"}
	if !slices.Equal(got, want) {
		t.Fatalf("untracked = %q, want %q", got, want)
	}
	for path, ignored := range map[string]bool{"secrets.env": true, "web/secrets.env": true, "cache": true, "web/compose.yaml": false, "web/data/db.bin": true} {
		if got, err := p.IsIgnored(ctx, path); err != nil || got != ignored {
			t.Errorf("IsIgnored(%q) = %v, %v; want %v", path, got, err, ignored)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("the preview left a .git in the folder: %v", err)
	}
	tmp := p.tmp
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(tmp); !os.IsNotExist(err) {
		t.Fatalf("Close left %s", tmp)
	}
}

func TestRemotesNamesEveryRemote(t *testing.T) {
	r := localFixture(t)
	ctx := context.Background()
	if got, err := r.Remotes(ctx); err != nil || len(got) != 0 {
		t.Fatalf("remotes = %q, %v", got, err)
	}
	fixtureGit(t, r.dir, "remote", "add", "upstream", "ssh://git.example.test/demo/stacks.git")
	fixtureGit(t, r.dir, "remote", "add", "origin", "ssh://git.example.test/demo/stacks.git")
	if got, err := r.Remotes(ctx); err != nil || !slices.Equal(got, []string{"origin", "upstream"}) {
		t.Fatalf("remotes = %q, %v", got, err)
	}
}
