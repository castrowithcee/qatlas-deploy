package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestKeySetFromStdinFileAndReplace(t *testing.T) {
	var log syncBuffer
	dir := startServiceLogged(t, allow, &log)
	first := testKeyPEM(t, "ed25519", "")

	code, stdout, stderr := runWithStdin(t, first, "key", "set", "--output", "json")
	if code != 0 {
		t.Fatalf("set: code %d, stderr %q", code, stderr)
	}
	var res map[string]string
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatal(err)
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(res["public_key"]))
	if err != nil || ssh.FingerprintSHA256(pub) != res["fingerprint"] {
		t.Fatalf("public part %q: %v", res["public_key"], err)
	}
	all := stdout + stderr

	code, stdout, stderr = runWithStdin(t, first, "key", "set")
	if code != 2 || !strings.Contains(stderr, "key set --replace") || stdout != "" {
		t.Fatalf("set without --replace: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	all += stdout + stderr
	stored, _ := os.ReadFile(filepath.Join(dir, "deploy-key"))
	if strings.TrimSpace(string(stored)) != strings.TrimSpace(first) {
		t.Fatal("the key changed without --replace")
	}

	second := testKeyPEM(t, "rsa", "")
	path := filepath.Join(t.TempDir(), "id_test_only")
	if err := os.WriteFile(path, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runWithStdin(t, "", "key", "set", "--replace", "--file", path)
	if code != 0 || !strings.Contains(stdout, "ssh-rsa ") {
		t.Fatalf("replace from file: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	all += stdout + stderr
	for _, k := range []string{first, second} {
		for _, line := range strings.Split(strings.TrimSpace(k), "\n")[1:3] {
			if strings.Contains(all+log.String(), line) {
				t.Fatal("private key material appears in output or log")
			}
		}
	}
}

func TestKeySetRefusalsNameTheCauseWithoutKeyText(t *testing.T) {
	var log syncBuffer
	startServiceLogged(t, allow, &log)
	for want, key := range map[string]string{
		"passphrase":  testKeyPEM(t, "ed25519", "demo-passphrase"),
		"3072 bits":   testKeyPEM(t, "rsa2048", ""),
		"OpenSSH":     brokenKeyPEM(),
		"larger than": strings.Repeat("A", 17<<10),
		"no key was":  "  \n",
	} {
		code, stdout, stderr := runWithStdin(t, key, "key", "set")
		if code != 2 || stdout != "" || !strings.Contains(stderr, want) {
			t.Errorf("%s: code %d, stdout %q, stderr %q", want, code, stdout, stderr)
		}
		for _, line := range strings.Split(strings.TrimSpace(key), "\n") {
			if len(line) > 20 && strings.Contains(stderr+log.String(), line) {
				t.Errorf("%s: key text in stderr or log", want)
			}
		}
	}
	if code, _, stderr := runWithStdin(t, "", "key", "set", "--file", filepath.Join(t.TempDir(), "missing")); code != 2 || !strings.Contains(stderr, "cannot open the key file") {
		t.Errorf("missing file: code %d, stderr %q", code, stderr)
	}
}

func TestKeySetReadsHiddenLinesFromTerminal(t *testing.T) {
	dir := startServiceLogged(t, allow, io.Discard)
	tty, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	key := testKeyPEM(t, "ed25519", "")
	lines := strings.Split(strings.TrimSpace(key), "\n")
	next := 0
	oldTerm, oldRead := isTerminal, readHidden
	isTerminal = func(f *os.File) bool { return f == tty }
	readHidden = func(*os.File) ([]byte, error) {
		if next >= len(lines) {
			return nil, io.EOF
		}
		next++
		return []byte(lines[next-1]), nil
	}
	t.Cleanup(func() { isTerminal, readHidden = oldTerm, oldRead })
	useStdin(t, tty)

	code, stdout, stderr := run(t, "key", "set")
	if code != 0 || !strings.Contains(stderr, "input is hidden") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if strings.Contains(stdout+stderr, lines[1]) {
		t.Fatal("the key is echoed")
	}
	stored, _ := os.ReadFile(filepath.Join(dir, "deploy-key"))
	if strings.TrimSpace(string(stored)) != strings.TrimSpace(key) {
		t.Fatal("the pasted key was not stored intact")
	}
}
