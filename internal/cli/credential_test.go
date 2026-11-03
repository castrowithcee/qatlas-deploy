package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

const demoToken = "demo-token-not-real"

func allow(net.Conn) error { return nil }

func useStdin(t *testing.T, r io.Reader) {
	t.Helper()
	old := stdinStream
	stdinStream = func() io.Reader { return r }
	t.Cleanup(func() { stdinStream = old })
}

func runWithStdin(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	useStdin(t, strings.NewReader(stdin))
	return run(t, args...)
}

func TestCredentialSetFromStdinNeverShowsValue(t *testing.T) {
	var log syncBuffer
	dir := startServiceLogged(t, allow, &log)

	code, stdout, stderr := runWithStdin(t, demoToken+"\n", "credential", "set", "registry.example")
	if code != 0 {
		t.Fatalf("set: code %d, stderr %q", code, stderr)
	}
	all := []string{stdout, stderr}
	code, stdout, stderr = run(t, "credential", "list")
	if code != 0 || !strings.Contains(stdout, "registry.example") {
		t.Fatalf("list: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	all = append(all, stdout, stderr)
	code, stdout, stderr = run(t, "credential", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("list json: code %d", code)
	}
	all = append(all, stdout, stderr, log.String())
	for _, text := range all {
		if strings.Contains(text, demoToken) {
			t.Fatalf("the value appears in output or log: %q", text)
		}
	}
	stored, err := os.ReadFile(filepath.Join(dir, "credentials", "registry.example"))
	if err != nil || string(stored) != demoToken {
		t.Fatalf("stored value %q, %v", stored, err)
	}
}

func TestCredentialListShowsNamesAndTimes(t *testing.T) {
	startService(t, allow)
	for _, name := range []string{"zeta", "alpha"} {
		if code, _, stderr := runWithStdin(t, demoToken, "credential", "set", name); code != 0 {
			t.Fatalf("set %s: %s", name, stderr)
		}
	}
	code, stdout, _ := run(t, "credential", "list", "--output", "json")
	var rows []map[string]any
	if code != 0 || json.Unmarshal([]byte(stdout), &rows) != nil || len(rows) != 2 ||
		rows[0]["name"] != "alpha" || rows[1]["name"] != "zeta" || rows[0]["modified"] == nil {
		t.Fatalf("code %d, rows %v, stdout %q", code, rows, stdout)
	}
	for _, row := range rows {
		if len(row) != 2 {
			t.Fatalf("row has more than name and modified: %v", row)
		}
	}
}

func TestCredentialSetRejectsValueArgument(t *testing.T) {
	dir := startServiceLogged(t, allow, io.Discard)
	code, stdout, stderr := runWithStdin(t, "", "credential", "set", "registry", demoToken)
	if code != 2 || !strings.Contains(stderr, "never accepted as an argument") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if strings.Contains(stdout+stderr, demoToken) {
		t.Fatalf("the argument is echoed: %q %q", stdout, stderr)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "credentials")); len(entries) != 0 {
		t.Fatalf("something was stored: %v", entries)
	}
	if code, _, stderr := run(t, "credential", "set"); code != 2 || !strings.Contains(stderr, "exactly one argument") {
		t.Fatalf("no argument: code %d, stderr %q", code, stderr)
	}
}

func TestCredentialInvalidNamesRejected(t *testing.T) {
	dir := startServiceLogged(t, allow, io.Discard)
	for _, name := range []string{"../escape", "a/b", ".hidden", "Upper", "1abc", "with space", "", strings.Repeat("a", 65), "a\x00b", "_x"} {
		code, _, stderr := runWithStdin(t, demoToken, "credential", "set", name)
		if code != 2 || !strings.Contains(stderr, "invalid name") {
			t.Fatalf("set %q: code %d, stderr %q", name, code, stderr)
		}
		if code, _, _ := run(t, "credential", "delete", name); code != 2 {
			t.Fatalf("delete %q: code %d", name, code)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "credentials")); len(entries) != 0 {
		t.Fatalf("something was stored: %v", entries)
	}
	if _, err := os.Lstat(filepath.Join(dir, "..", "escape")); err == nil {
		t.Fatal("a file escaped the store")
	}
}

func TestCredentialInvalidValuesRejected(t *testing.T) {
	startService(t, allow)
	for name, value := range map[string]string{
		"empty": "", "short": "abc", "control": "demo\x01token", "long": strings.Repeat("x", 8193), "inner": "demo\ntoken-not-real",
	} {
		code, _, stderr := runWithStdin(t, value, "credential", "set", "registry")
		if code != 2 {
			t.Fatalf("%s: code %d, stderr %q", name, code, stderr)
		}
	}
}

func TestCredentialDelete(t *testing.T) {
	startService(t, allow)
	if code, _, _ := runWithStdin(t, demoToken, "credential", "set", "registry"); code != 0 {
		t.Fatal("set failed")
	}
	if code, _, stderr := run(t, "credential", "delete", "registry"); code != 0 {
		t.Fatalf("delete: code %d, stderr %q", code, stderr)
	}
	if _, stdout, _ := run(t, "credential", "list"); strings.Contains(stdout, "registry") {
		t.Fatalf("still listed: %q", stdout)
	}
	if code, _, stderr := run(t, "credential", "delete", "registry"); code != 1 || !strings.Contains(stderr, "no such credential") {
		t.Fatalf("second delete: code %d, stderr %q", code, stderr)
	}
}

func TestCredentialSetReadsHiddenFromTerminal(t *testing.T) {
	startService(t, allow)
	tty, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	oldTerm, oldRead := isTerminal, readHidden
	isTerminal = func(f *os.File) bool { return f == tty }
	readHidden = func(f *os.File) ([]byte, error) { return []byte(demoToken), nil }
	t.Cleanup(func() { isTerminal, readHidden = oldTerm, oldRead })
	useStdin(t, tty)

	code, stdout, stderr := run(t, "credential", "set", "registry")
	if code != 0 || !strings.Contains(stderr, "input is hidden") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if strings.Contains(stdout+stderr, demoToken) {
		t.Fatalf("the value is echoed: %q %q", stdout, stderr)
	}
}

func TestCredentialSetErrorTextsNeverContainValue(t *testing.T) {
	startService(t, func(net.Conn) error { return nil })
	// Too long for the service; the client refuses first, and its text must not repeat the value.
	value := demoToken + strings.Repeat("x", 9000)
	code, stdout, stderr := runWithStdin(t, value, "credential", "set", "registry")
	if code != 2 || strings.Contains(stdout+stderr, demoToken) {
		t.Fatalf("code %d, out %q, err %q", code, stdout, stderr)
	}
}

func TestKeyGenerateShowAndReplace(t *testing.T) {
	var log syncBuffer
	dir := startServiceLogged(t, allow, &log)

	code, stdout, stderr := run(t, "key", "show")
	if code != 1 || !strings.Contains(stderr, "no deploy key") {
		t.Fatalf("show without key: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	code, stdout, stderr = run(t, "key", "generate", "--output", "json")
	if code != 0 {
		t.Fatalf("generate: code %d, stderr %q", code, stderr)
	}
	var first map[string]string
	if err := json.Unmarshal([]byte(stdout), &first); err != nil {
		t.Fatal(err)
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(first["public_key"]))
	if err != nil || pub.Type() != ssh.KeyAlgoED25519 || first["fingerprint"] != ssh.FingerprintSHA256(pub) {
		t.Fatalf("public key %q does not parse or match: %v", first["public_key"], err)
	}
	if !strings.HasPrefix(first["fingerprint"], "SHA256:") {
		t.Fatalf("fingerprint %q", first["fingerprint"])
	}

	code, stdout, _ = run(t, "key", "show", "--output", "json")
	var shown map[string]string
	if code != 0 || json.Unmarshal([]byte(stdout), &shown) != nil || shown["fingerprint"] != first["fingerprint"] {
		t.Fatalf("show: code %d, %q", code, stdout)
	}

	private, err := os.ReadFile(filepath.Join(dir, "deploy-key"))
	if err != nil {
		t.Fatal(err)
	}
	secretLine := strings.Split(strings.TrimSpace(string(private)), "\n")[1]
	if strings.Contains(stdout+stderr+log.String(), secretLine) {
		t.Fatal("private key material appears in output or log")
	}

	code, stdout, stderr = run(t, "key", "generate")
	if code != 2 || !strings.Contains(stderr, "--replace") || stdout != "" {
		t.Fatalf("generate without --replace: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if again, _ := os.ReadFile(filepath.Join(dir, "deploy-key")); !bytes.Equal(again, private) {
		t.Fatal("the key changed without confirmation")
	}

	code, stdout, _ = run(t, "key", "generate", "--replace", "--output", "json")
	var second map[string]string
	if code != 0 || json.Unmarshal([]byte(stdout), &second) != nil || second["fingerprint"] == first["fingerprint"] {
		t.Fatalf("replace: code %d, %q", code, stdout)
	}
}

func TestCredentialCommandsWithoutServiceExitThree(t *testing.T) {
	useEndpoint(t, channeltest.Endpoint(t))
	for _, args := range [][]string{{"key", "show"}, {"key", "generate"}, {"credential", "list"}} {
		if code, _, stderr := run(t, args...); code != ExitServiceNotRunning || !strings.Contains(stderr, "not running") {
			t.Fatalf("%v: code %d, stderr %q", args, code, stderr)
		}
	}
	if code, _, stderr := runWithStdin(t, demoToken, "credential", "set", "registry"); code != ExitServiceNotRunning ||
		strings.Contains(stderr, demoToken) {
		t.Fatalf("set: code %d, stderr %q", code, stderr)
	}
}

func TestCredentialCommandsNeedRoot(t *testing.T) {
	startService(t, func(net.Conn) error { return control.ErrNotRoot() })
	code, _, stderr := run(t, "credential", "list")
	if code != 1 || !strings.Contains(stderr, "sudo") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
}
