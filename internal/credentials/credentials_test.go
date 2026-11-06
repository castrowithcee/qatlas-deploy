package credentials

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

const demoToken = "demo-token-not-real"

func openStore(t *testing.T) (*Store, string, *[]string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	var seen []string
	s, err := Open(dir, func(v ...string) { seen = append(seen, v...) })
	if err != nil {
		t.Fatal(err)
	}
	return s, dir, &seen
}

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestPermissions(t *testing.T) {
	s, dir, _ := openStore(t)
	if _, err := s.GenerateKey(false); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("registry", demoToken); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("registry", demoToken+"-2"); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]fs.FileMode{
		dir: 0o700, filepath.Join(dir, "credentials"): 0o700,
		filepath.Join(dir, "deploy-key"): 0o600, filepath.Join(dir, "credentials", "registry"): 0o600,
	} {
		if got := mode(t, path); got != want {
			t.Errorf("%s has mode %o, want %o", path, got, want)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "credentials"))
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestGeneratedKeyMatchesItsPublicPart(t *testing.T) {
	s, dir, _ := openStore(t)
	pub, err := s.GenerateKey(false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "deploy-key"))
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		t.Fatal(err)
	}
	authorized, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pub.Authorized))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(authorized.Marshal(), signer.PublicKey().Marshal()) ||
		pub.Fingerprint != ssh.FingerprintSHA256(signer.PublicKey()) {
		t.Fatal("the public part does not belong to the private key")
	}
	shown, err := s.ShowKey()
	if err != nil || shown != pub {
		t.Fatalf("ShowKey = %v, %v", shown, err)
	}
	if strings.Contains(pub.Authorized, "PRIVATE") {
		t.Fatal("private material in the public part")
	}
}

func TestReplaceNeedsConfirmation(t *testing.T) {
	s, dir, _ := openStore(t)
	first, _ := s.GenerateKey(false)
	before, _ := os.ReadFile(filepath.Join(dir, "deploy-key"))
	if _, err := s.GenerateKey(false); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("err = %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "deploy-key")); !bytes.Equal(before, after) {
		t.Fatal("the key changed")
	}
	second, err := s.GenerateKey(true)
	if err != nil || second.Fingerprint == first.Fingerprint {
		t.Fatalf("replace = %v, %v", second, err)
	}
	if got := mode(t, filepath.Join(dir, "deploy-key")); got != 0o600 {
		t.Fatalf("mode %o", got)
	}
}

func TestShowWithoutKey(t *testing.T) {
	s, _, _ := openStore(t)
	if _, err := s.ShowKey(); !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestNamesAreValidatedStrictly(t *testing.T) {
	s, dir, _ := openStore(t)
	for _, name := range []string{"", "..", ".", "../x", "a/b", `a\b`, ".hidden", ".tmp-1", "Upper", "9a", "a b", "a\x00", "é", strings.Repeat("a", 65)} {
		if err := s.Set(name, demoToken); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Set(%q) = %v", name, err)
		}
		if err := s.Delete(name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Delete(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"a", "registry.example", "notify_mail-1", strings.Repeat("a", 64)} {
		if err := s.Set(name, demoToken); err != nil {
			t.Errorf("Set(%q) = %v", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "..", "x")); err == nil {
		t.Fatal("escaped the store")
	}
}

func TestValuesAreBounded(t *testing.T) {
	s, _, _ := openStore(t)
	for _, v := range []string{"", "abc", strings.Repeat("x", MaxValueBytes+1), "a\nb-not-real", "tab\there", "bad\xffutf8", "nul\x00byte"} {
		err := s.Set("name", v)
		if !errors.Is(err, ErrInvalidValue) {
			t.Errorf("Set(%q) = %v", v, err)
		} else if strings.Contains(err.Error(), "bad") || strings.Contains(err.Error(), "nul") {
			t.Errorf("error repeats the value: %v", err)
		}
	}
	if err := s.Set("name", strings.Repeat("x", MaxValueBytes)); err != nil {
		t.Fatal(err)
	}
}

func TestListAndDelete(t *testing.T) {
	s, _, _ := openStore(t)
	_ = s.Set("beta", demoToken)
	_ = s.Set("alpha", demoToken)
	list, err := s.List()
	if err != nil || len(list) != 2 || list[0].Name != "alpha" || list[1].Name != "beta" || list[0].Modified.IsZero() {
		t.Fatalf("List = %v, %v", list, err)
	}
	if err := s.Delete("alpha"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("alpha"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}
	if list, _ := s.List(); len(list) != 1 || list[0].Name != "beta" {
		t.Fatalf("List = %v", list)
	}
}

func TestSecretsReachTheRedactorBeforeAndAfterRestart(t *testing.T) {
	s, dir, seen := openStore(t)
	_, _ = s.GenerateKey(false)
	_ = s.Set("registry", demoToken)
	if !contains(*seen, demoToken) || len(*seen) < 3 {
		t.Fatalf("registered %d secrets", len(*seen))
	}
	var again []string
	if _, err := Open(dir, func(v ...string) { again = append(again, v...) }); err != nil {
		t.Fatal(err)
	}
	if !contains(again, demoToken) || len(again) != 3 {
		t.Fatalf("restart registered %v", again)
	}
	private, _ := os.ReadFile(filepath.Join(dir, "deploy-key"))
	if !contains(again, strings.TrimSpace(string(private))) {
		t.Fatal("the private key text is not registered")
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestOpenFailsClosedOnUnsafeStore(t *testing.T) {
	t.Run("directory mode", func(t *testing.T) {
		_, dir, _ := openStore(t)
		if err := os.Chmod(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(dir, nil); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("credentials directory mode", func(t *testing.T) {
		_, dir, _ := openStore(t)
		_ = os.Chmod(filepath.Join(dir, "credentials"), 0o755)
		if _, err := Open(dir, nil); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("directory is a link", func(t *testing.T) {
		_, dir, _ := openStore(t)
		link := dir + "-link"
		if err := os.Symlink(dir, link); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(link, nil); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("file mode", func(t *testing.T) {
		s, dir, _ := openStore(t)
		_ = s.Set("registry", demoToken)
		_ = os.Chmod(filepath.Join(dir, "credentials", "registry"), 0o644)
		if _, err := Open(dir, nil); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Open = %v", err)
		}
		if _, err := s.List(); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("List = %v", err)
		}
		if err := s.Set("registry", demoToken); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Set = %v", err)
		}
	})
	t.Run("file is a link", func(t *testing.T) {
		s, dir, _ := openStore(t)
		target := filepath.Join(t.TempDir(), "other")
		_ = os.WriteFile(target, []byte(demoToken), 0o600)
		if err := os.Symlink(target, filepath.Join(dir, "deploy-key")); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ShowKey(); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("ShowKey = %v", err)
		}
		if _, err := s.GenerateKey(true); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("GenerateKey = %v", err)
		}
		if got, _ := os.ReadFile(target); string(got) != demoToken {
			t.Fatal("the link target was written")
		}
	})
	t.Run("extra link", func(t *testing.T) {
		s, dir, _ := openStore(t)
		_ = s.Set("registry", demoToken)
		if err := os.Link(filepath.Join(dir, "credentials", "registry"), filepath.Join(t.TempDir(), "hard")); err != nil {
			t.Skip(err)
		}
		if _, err := s.List(); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("List = %v", err)
		}
	})
	t.Run("unexpected entry", func(t *testing.T) {
		_, dir, _ := openStore(t)
		_ = os.WriteFile(filepath.Join(dir, "credentials", "Bad Name"), []byte("x"), 0o600)
		if _, err := Open(dir, nil); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestOpenRemovesInterruptedWrites(t *testing.T) {
	_, dir, _ := openStore(t)
	leftover := filepath.Join(dir, "credentials", tmpPrefix+"deadbeef")
	_ = os.WriteFile(leftover, []byte(demoToken), 0o600)
	if _, err := Open(dir, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(leftover); err == nil {
		t.Fatal("leftover remains")
	}
}

func TestOpenNeedsAbsoluteDirectory(t *testing.T) {
	for _, dir := range []string{"", "state", "./state"} {
		if _, err := Open(dir, nil); err == nil {
			t.Errorf("Open(%q) succeeded", dir)
		}
	}
}

func TestValue(t *testing.T) {
	s, _, _ := openStore(t)
	if err := s.Set("found", demoToken); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Value("found"); err != nil || v != demoToken {
		t.Fatalf("Value(found) = %v", err)
	}
	if _, err := s.Value("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Value(missing) = %v", err)
	}
	if _, err := s.Value("../x"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("Value(../x) = %v", err)
	}
}

func TestEachSourceKeyIsItsOwn(t *testing.T) {
	s, dir, _ := openStore(t)
	name := SourceKey("srv-more-stacks")
	first, err := s.GenerateKey(false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.GenerateNamedKey(name, false)
	if err != nil || second.Fingerprint == first.Fingerprint {
		t.Fatalf("second key: %+v, %v", second, err)
	}
	if got := mode(t, filepath.Join(dir, name)); got != 0o600 {
		t.Fatalf("mode = %v", got)
	}
	if shown, err := s.ShowNamedKey(name); err != nil || shown != second || s.NamedKeyPath(name) != filepath.Join(dir, name) {
		t.Fatalf("show: %+v, %v", shown, err)
	}
	if _, err := s.GenerateNamedKey(name, false); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("second generate: %v", err)
	}
	for _, bad := range []string{"deploy-key-", "deploy-key-a/b", "../deploy-key", "credentials", "deploy-keyx"} {
		if _, err := s.GenerateNamedKey(bad, false); !errors.Is(err, ErrInvalidName) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	var again []string
	if _, err := Open(dir, func(v ...string) { again = append(again, v...) }); err != nil {
		t.Fatal(err)
	}
	private, _ := os.ReadFile(filepath.Join(dir, name))
	if !contains(again, strings.TrimSpace(string(private))) {
		t.Fatal("the private text of the source key is not registered")
	}
}
