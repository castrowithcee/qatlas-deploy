package credentials

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

var (
	rsaOnce sync.Once
	rsaKey  *rsa.PrivateKey
)

// testKeyPEM returns a freshly generated test key in OpenSSH format: "ed25519", "rsa" (3072 bits) or
// "rsa2048"; a non-empty passphrase encrypts it.
func testKeyPEM(t *testing.T, kind, passphrase string) string {
	t.Helper()
	var priv any
	switch kind {
	case "ed25519":
		_, k, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		priv = k
	case "rsa":
		rsaOnce.Do(func() {
			var err error
			if rsaKey, err = rsa.GenerateKey(rand.Reader, 3072); err != nil {
				panic(err)
			}
		})
		priv = rsaKey
	case "rsa2048":
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		priv = k
	default:
		t.Fatalf("kind %q", kind)
	}
	var block *pem.Block
	var err error
	if passphrase != "" {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "test", []byte(passphrase))
	} else {
		block, err = ssh.MarshalPrivateKey(priv, "test")
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block))
}
