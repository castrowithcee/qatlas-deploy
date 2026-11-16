package tui

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// testKeyPEM is a freshly generated Ed25519 key in OpenSSH format; it exists only in memory and in test files.
func testKeyPEM(t *testing.T) string {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(k, "test")
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block))
}

func paste(m *Model, text string) {
	_, cmd := m.Update(tea.PasteMsg{Content: text})
	drain(m, cmd)
	m.render()
}

// noKeyText fails when a line of the key is on the screen.
func noKeyText(t *testing.T, screen, key string) {
	t.Helper()
	for _, line := range strings.Split(key, "\n") {
		if len(strings.TrimSpace(line)) >= 8 && strings.Contains(screen, strings.TrimSpace(line)) {
			t.Fatalf("a line of the private key is on the screen:\n%s", screen)
		}
	}
}

// sendKeyChecked sends keys and checks every frame for the key.
func sendKeyChecked(t *testing.T, m *Model, key string, keys ...string) {
	t.Helper()
	for _, k := range keys {
		send(m, k)
		noKeyText(t, m.render(), key)
	}
}

// keyBackend is the demo host with the stack folder of its stacks and the given others, which have no stack.
func keyBackend(sources ...string) *fakeBackend {
	b := settingsBackend()
	b.status.Engine.Sources = []control.SourceStatus{{ID: "srv-stacks-example", Path: stackDir, Kind: control.SourceKindRemote, URL: repoURL,
		LastPoll: at(30, 14, 30), Head: commitA}}
	for i := range b.status.Engine.Stacks {
		b.status.Engine.Stacks[i].Source = "srv-stacks-example"
	}
	for i := range b.stacks.Stacks {
		b.stacks.Stacks[i].Source, b.stacks.Stacks[i].SourceDir = "srv-stacks-example", stackDir
	}
	for _, s := range sources {
		b.status.Engine.Sources = append(b.status.Engine.Sources, control.SourceStatus{ID: filepath.Base(s), Path: s, Kind: control.SourceKindRemote})
	}
	return b
}

func TestPastedKeyIsStoredAndNeverShown(t *testing.T) {
	key := testKeyPEM(t)
	b := keyBackend()
	m := newSettingsModel(t, b)
	sendKeyChecked(t, m, key, toKey...)
	sendKeyChecked(t, m, key, "e", "f2")
	paste(m, key)
	noKeyText(t, m.render(), key)
	contains(t, m, "(hidden; lines: ", "Private key")
	sendKeyChecked(t, m, key, "f2")
	if got := b.actions(); !slices.Equal(got, []string{"keyset:" + stackDir}) {
		t.Fatalf("calls = %v", got)
	}
	if len(b.keyArgs) != 1 || b.keyArgs[0].Key != key || b.keyArgs[0].Replace || b.keyArgs[0].Dir != stackDir {
		t.Fatalf("the service got %+v", b.keyArgs)
	}
	contains(t, m, "The deploy key is stored.", "SHA256:StoredKey")
	sendKeyChecked(t, m, key, "enter")
	contains(t, m, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIStoredKey")
}

func TestKeyIsStoredFromAFile(t *testing.T) {
	key := testKeyPEM(t)
	path := filepath.Join(t.TempDir(), "id_test_only")
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	b := keyBackend()
	m := newSettingsModel(t, b)
	send(m, toKey...)
	sendKeyChecked(t, m, key, "e", "f2", "down")
	sendKeyChecked(t, m, key, typed(path)...)
	sendKeyChecked(t, m, key, "f2")
	if len(b.keyArgs) != 1 || b.keyArgs[0].Key != key {
		t.Fatalf("the service got %+v", b.keyArgs)
	}
}

func TestKeyFormRefusesWhatIsNoSingleKeySource(t *testing.T) {
	key := testKeyPEM(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "k")
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(file, []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(big, []byte(strings.Repeat("A", 16<<10+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		paste, path, want string
	}{
		"none":    {"", "", "Paste the private key or give the path"},
		"both":    {key, file, "either the pasted key or a key file"},
		"missing": {"", filepath.Join(dir, "absent"), "key file cannot be read"},
		"large":   {"", big, "larger than 16 KiB"},
		"pasted":  {strings.Repeat("A", 16<<10+1), "", "larger than 16 KiB"},
	} {
		t.Run(name, func(t *testing.T) {
			b := keyBackend()
			m := newSettingsModel(t, b)
			send(m, toKey...)
			shown := b.called("keyshow")
			send(m, "e", "f2")
			if c.paste != "" {
				paste(m, c.paste)
			}
			if c.path != "" {
				send(m, "down")
				send(m, typed(c.path)...)
			}
			send(m, "f2")
			contains(t, m, c.want)
			noKeyText(t, m.render(), key)
			if len(b.actions()) != 0 || b.called("keyshow") != shown {
				t.Fatalf("a refused form reached the service: %v", b.actions())
			}
			if m.set.form == nil {
				t.Fatal("the form was dropped")
			}
		})
	}
}

func TestKeyViewIsForTheMarkedFolderAlone(t *testing.T) {
	first, second := "/srv/first-example", "/srv/second-example"
	b := keyBackend(first, second)
	m := newSettingsModel(t, b)
	send(m, "1")
	onFolder(t, m, second)
	send(m, "e")
	contains(t, m, "stack folder: "+second, "No deploy key yet")
	send(m, "e")
	if _, ok := m.set.form.form.Values()["folder"]; ok {
		t.Fatal("the form offers a folder choice")
	}
	send(m, "right", "f2")
	if got := b.actions(); !slices.Equal(got, []string{"keygen:" + second}) {
		t.Fatalf("calls = %v", got)
	}
	if len(b.keyArgs) != 1 || b.keyArgs[0].Dir != second {
		t.Fatalf("the service got %+v", b.keyArgs)
	}
	send(m, "enter")
	contains(t, m, "SHA256:ExampleKey")
	// Esc leaves the view with the marked row still on the folder.
	send(m, "esc")
	if key, ok := m.list.SelectedGroup(); m.set.keyOpen || !ok || key != second {
		t.Fatalf("key view %v, marked folder %q (%v)", m.set.keyOpen, key, ok)
	}
}

func TestReplacingAKeyAsksFirstAndOnlyYesReplaces(t *testing.T) {
	key := testKeyPEM(t)
	old := control.KeyResult{PublicKey: "ssh-ed25519 AAAAoldexample old", Fingerprint: fingerprint}
	b := keyBackend()
	b.key = &old
	m := newSettingsModel(t, b)
	send(m, toKey...)
	sendKeyChecked(t, m, key, "e", "f2")
	paste(m, key)
	sendKeyChecked(t, m, key, "f2")
	contains(t, m, "Replace the existing deploy key?", "stops working at once")
	if len(b.actions()) != 0 {
		t.Fatalf("something was sent before the answer: %v", b.actions())
	}
	sendKeyChecked(t, m, key, "n")
	if len(b.actions()) != 0 || *b.key != old || m.set.form == nil {
		t.Fatalf("cancel changed something: %v %+v", b.actions(), b.key)
	}
	sendKeyChecked(t, m, key, "f2", "y")
	if len(b.keyArgs) != 1 || !b.keyArgs[0].Replace || b.keyArgs[0].Key != key {
		t.Fatalf("the service got %+v", b.keyArgs)
	}
	if b.key.Fingerprint == old.Fingerprint {
		t.Fatal("the key was not replaced")
	}
}

func TestReplacingAGeneratedKeyAsksToo(t *testing.T) {
	b := keyBackend()
	b.key = &control.KeyResult{PublicKey: "ssh-ed25519 AAAAoldexample old", Fingerprint: fingerprint}
	m := newSettingsModel(t, b)
	send(m, toKey...)
	send(m, "e", "right", "f2")
	contains(t, m, "Replace the existing deploy key?")
	send(m, "y")
	if len(b.keyArgs) != 1 || !b.keyArgs[0].Replace {
		t.Fatalf("the service got %+v", b.keyArgs)
	}
}

func TestRefusedKeyIsShownAtTheKeyField(t *testing.T) {
	key := testKeyPEM(t)
	for name, c := range map[string]struct {
		err  error
		want string
	}{
		"invalid": {control.NewError(control.CodeInvalid, "the key is protected by a passphrase"), "protected by a passphrase"},
		"other":   {errors.New("channel lost"), "channel lost"},
	} {
		t.Run(name, func(t *testing.T) {
			b := keyBackend()
			b.errs["keyset:"+stackDir] = c.err
			m := newSettingsModel(t, b)
			send(m, toKey...)
			send(m, "e", "f2")
			paste(m, key)
			sendKeyChecked(t, m, key, "f2")
			contains(t, m, c.want)
			if m.set.form == nil || m.set.form.kind != formKeyStore {
				t.Fatal("the form was dropped")
			}
			if name == "invalid" {
				if p := m.set.form.form.Focused(); p != "key" {
					t.Fatalf("focus %q", p)
				}
			}
		})
	}
}

func TestKeyFormGoldenFramesHideTheKey(t *testing.T) {
	key := testKeyPEM(t)
	b := keyBackend()
	m := newSettingsModel(t, b)
	send(m, toKey...)
	send(m, "e", "f2")
	paste(m, key)
	for _, l := range strings.Split(m.render(), "\n") {
		noKeyText(t, l, key)
	}
}
