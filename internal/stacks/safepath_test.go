package stacks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDirIsLexicalAndLinkFree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	abs, exists, err := ResolveDir(root, "data/a")
	if err != nil || !exists || abs != filepath.Join(root, "data", "a") {
		t.Fatalf("%q %v %v", abs, exists, err)
	}
	if abs, exists, err = ResolveDir(root, "data/new/deeper"); err != nil || exists || abs != filepath.Join(root, "data", "new", "deeper") {
		t.Fatalf("%q %v %v", abs, exists, err)
	}
	if _, _, err := ResolveDir("rel", "data"); err == nil {
		t.Fatal("relative root accepted")
	}
}

func TestResolveDirRefusesLinksAndFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "data", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"data/link", "data/link/x", "data/file", "data/file/x", "../x", "/etc", "", ".", "data//x"} {
		if _, _, err := ResolveDir(root, rel); err == nil {
			t.Errorf("ResolveDir(%q) succeeded", rel)
		}
	}
}
