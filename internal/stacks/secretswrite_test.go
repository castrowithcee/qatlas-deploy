package stacks

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const (
	fakeSecretValue  = "demo-value-not-real-1"
	otherSecretValue = "demo-other-not-real-2"
)

func secretStack(t *testing.T, env map[string]string) Stack {
	t.Helper()
	return Stack{Name: "demo", Dir: t.TempDir(), Env: env}
}

func readSecretsFile(t *testing.T, st Stack) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(st.Dir, SecretsFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetSecretCreatesFileWithMode0600AndKeepsOtherLines(t *testing.T) {
	st := secretStack(t, map[string]string{"VERSION": "1.0.0"})
	if replaced, err := SetSecret(st, "DB_PASSWORD", fakeSecretValue); err != nil || replaced {
		t.Fatalf("replaced %v, err %v", replaced, err)
	}
	info, err := os.Stat(filepath.Join(st.Dir, SecretsFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("info %v, err %v", info, err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir, SecretsFile), []byte("# keep\nA_KEY=abcd\nDB_PASSWORD=old-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if replaced, err := SetSecret(st, "DB_PASSWORD", otherSecretValue); err != nil || !replaced {
		t.Fatalf("replaced %v, err %v", replaced, err)
	}
	if got, want := readSecretsFile(t, st), "# keep\nA_KEY=abcd\nDB_PASSWORD='"+otherSecretValue+"'\n"; got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
	if err := DeleteSecret(st, "DB_PASSWORD"); err != nil {
		t.Fatal(err)
	}
	if got := readSecretsFile(t, st); got != "# keep\nA_KEY=abcd\n" {
		t.Fatalf("file = %q", got)
	}
	if err := DeleteSecret(st, "DB_PASSWORD"); !errors.Is(err, ErrNoSecret) {
		t.Fatalf("err = %v", err)
	}
	if info, _ := os.Stat(filepath.Join(st.Dir, SecretsFile)); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode())
	}
}

func TestSecretValuesReadBackExactly(t *testing.T) {
	st := secretStack(t, nil)
	values := map[string]string{
		"PLAIN":   "plain value # not a comment",
		"QUOTE":   `it's a "value" with \ backslash`,
		"DOLLAR":  "cost$ ${NOT_EXPANDED}",
		"NONASCI": "pässwörd-1234",
	}
	for k, v := range values {
		if _, err := SetSecret(st, k, v); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
	}
	got, err := SecretValues(st.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"PLAIN", "QUOTE", "DOLLAR", "NONASCI"} {
		if got[k] != values[k] {
			t.Fatalf("%s did not read back", k)
		}
	}
	if _, err := SetSecret(st, "BAD", "it's $HOME"); !errors.Is(err, ErrInvalidSecretValue) {
		t.Fatalf("err = %v", err)
	}
}

func TestSetSecretRefusals(t *testing.T) {
	st := secretStack(t, map[string]string{"IN_ENV": "x"})
	if _, err := SetSecret(st, "IN_ENV", fakeSecretValue); !errors.Is(err, ErrKeyInEnv) {
		t.Fatalf("err = %v", err)
	}
	for _, key := range []string{"", "1BAD", "A B", "A=B", "../x"} {
		if _, err := SetSecret(st, key, fakeSecretValue); !errors.Is(err, ErrInvalidSecretKey) {
			t.Fatalf("key %q: err = %v", key, err)
		}
	}
	for _, v := range []string{"", "abc", "line\nbreak-value", "tab\tvalue-1", "bad\xffutf8-value"} {
		if _, err := SetSecret(st, "OK", v); !errors.Is(err, ErrInvalidSecretValue) {
			t.Fatalf("value %q: err = %v", v, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(st.Dir, SecretsFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused call created the file: %v", err)
	}
}

func TestSetSecretRefusesLinks(t *testing.T) {
	st := secretStack(t, nil)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("X=keep-me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(st.Dir, SecretsFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := SetSecret(st, "KEY", fakeSecretValue); err == nil {
		t.Fatal("a symlinked secrets.env was written")
	}
	if err := DeleteSecret(st, "X"); err == nil {
		t.Fatal("a symlinked secrets.env was edited")
	}
	if b, _ := os.ReadFile(target); string(b) != "X=keep-me\n" {
		t.Fatalf("target changed: %q", b)
	}

	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := SetSecret(Stack{Name: "demo", Dir: link}, "KEY", fakeSecretValue); err == nil {
		t.Fatal("a symlinked stack folder was written")
	}
	if entries, _ := os.ReadDir(real); len(entries) != 0 {
		t.Fatalf("files below the link target: %v", entries)
	}

	dirSecrets := secretStack(t, nil)
	if err := os.Mkdir(filepath.Join(dirSecrets.Dir, SecretsFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := SetSecret(dirSecrets, "KEY", fakeSecretValue); err == nil {
		t.Fatal("a directory named secrets.env was replaced")
	}
}

func TestFailedWriteLeavesOldFileAndNoTemporaryFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a read-only folder does not stop root")
	}
	st := secretStack(t, nil)
	if _, err := SetSecret(st, "KEY_ONE", fakeSecretValue); err != nil {
		t.Fatal(err)
	}
	before := readSecretsFile(t, st)
	if err := os.Chmod(st.Dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(st.Dir, 0o700) })
	if _, err := SetSecret(st, "KEY_TWO", otherSecretValue); err == nil {
		t.Fatal("write into a read-only folder succeeded")
	}
	_ = os.Chmod(st.Dir, 0o700)
	if got := readSecretsFile(t, st); got != before {
		t.Fatalf("file changed: %q", got)
	}
	if entries, _ := os.ReadDir(st.Dir); len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestSetSecretRemovesLeftoverTemporaryFiles(t *testing.T) {
	st := secretStack(t, nil)
	leftover := filepath.Join(st.Dir, ".secrets.env.123456")
	if err := os.WriteFile(leftover, []byte("A_KEY="+otherSecretValue+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "kept")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(st.Dir, ".secrets.env.link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := SetSecret(st, "DB_PASSWORD", fakeSecretValue); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(leftover); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("leftover temporary file still there: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("a link was removed: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("the link target was touched: %v", err)
	}
}
