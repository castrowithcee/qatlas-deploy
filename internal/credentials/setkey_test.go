package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestSetNamedKeyAcceptsEd25519AndRSA(t *testing.T) {
	for _, kind := range []string{"ed25519", "rsa"} {
		s, dir, seen := openStore(t)
		key := testKeyPEM(t, kind, "")
		pub, err := s.SetNamedKey(DefaultKey, key, false)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pub.Authorized))
		if err != nil || ssh.FingerprintSHA256(parsed) != pub.Fingerprint || !strings.HasSuffix(pub.Authorized, " qdeploy") {
			t.Fatalf("%s: public part %q: %v", kind, pub.Authorized, err)
		}
		if got := mode(t, filepath.Join(dir, DefaultKey)); got != 0o600 {
			t.Errorf("%s: mode %o", kind, got)
		}
		shown, err := s.ShowNamedKey(DefaultKey)
		if err != nil || shown != pub {
			t.Fatalf("%s: show = %+v, %v", kind, shown, err)
		}
		if !strings.Contains(strings.Join(*seen, "\n"), strings.TrimSpace(key)) {
			t.Errorf("%s: the key text was not handed to the redactor", kind)
		}
	}
}

func TestSetNamedKeyRSAIsRedactedAfterRestart(t *testing.T) {
	s, dir, _ := openStore(t)
	key := testKeyPEM(t, "rsa", "")
	if _, err := s.SetNamedKey(DefaultKey, key, false); err != nil {
		t.Fatal(err)
	}
	var seen []string
	if _, err := Open(dir, func(v ...string) { seen = append(seen, v...) }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(seen, "\n"), strings.TrimSpace(key)) {
		t.Fatal("the stored RSA key is not registered at start")
	}
}

func TestSetNamedKeyRefusals(t *testing.T) {
	long := testKeyPEM(t, "ed25519", "") + strings.Repeat("\n# padding", MaxKeyBytes/8)
	cases := []struct {
		name string
		key  string
		want error
	}{
		{"passphrase", testKeyPEM(t, "ed25519", "demo-passphrase"), ErrKeyPassphrase},
		{"small rsa", testKeyPEM(t, "rsa2048", ""), ErrKeyTooSmall},
		{"too large", long, ErrKeyTooLarge},
		{"not a key", "hello world", ErrKeyFormat},
		{"empty", "", ErrKeyFormat},
		{"public key", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDemoDemoDemoDemoDemoDemoDemoDemoDemoDemoDemo1 x", ErrKeyFormat},
		{"truncated", strings.TrimSpace(testKeyPEM(t, "ed25519", ""))[:120], ErrKeyFormat},
		{"trailing data", testKeyPEM(t, "ed25519", "") + "extra", ErrKeyFormat},
	}
	for _, c := range cases {
		s, dir, _ := openStore(t)
		_, err := s.SetNamedKey(DefaultKey, c.key, false)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
			continue
		}
		if _, statErr := os.Lstat(filepath.Join(dir, DefaultKey)); statErr == nil {
			t.Errorf("%s: a refused key was stored", c.name)
		}
		for _, word := range strings.Fields(c.key) {
			if len(word) > 30 && strings.Contains(err.Error(), word) {
				t.Errorf("%s: the error echoes key material", c.name)
			}
		}
	}
}

func TestSetNamedKeyNeedsReplace(t *testing.T) {
	s, dir, _ := openStore(t)
	first := testKeyPEM(t, "ed25519", "")
	if _, err := s.SetNamedKey(DefaultKey, first, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetNamedKey(DefaultKey, testKeyPEM(t, "rsa", ""), false); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.GenerateNamedKey(DefaultKey, false); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("generate: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, DefaultKey))
	if strings.TrimSpace(string(got)) != strings.TrimSpace(first) {
		t.Fatal("the key changed without replace")
	}
	second := testKeyPEM(t, "rsa", "")
	if _, err := s.SetNamedKey(DefaultKey, second, true); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(filepath.Join(dir, DefaultKey))
	if strings.TrimSpace(string(got)) != strings.TrimSpace(second) {
		t.Fatal("replace did not store the new key")
	}
}

func TestSetNamedKeyNormalizesLineEnds(t *testing.T) {
	s, _, _ := openStore(t)
	key := strings.ReplaceAll(testKeyPEM(t, "ed25519", ""), "\n", "\r\n")
	if _, err := s.SetNamedKey(DefaultKey, "\n  "+key+"\n\n", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShowNamedKey(DefaultKey); err != nil {
		t.Fatal(err)
	}
}
