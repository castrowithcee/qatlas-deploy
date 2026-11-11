package gitrepo

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"testing"
)

// fakePrivateKey is a PEM private key made on the spot from random bytes; it opens nothing.
func fakePrivateKey(t *testing.T) string {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func TestScanPushFindsEachKindInAnOlderCommitAndPrintsNoValue(t *testing.T) {
	ctx := context.Background()
	r := localFixture(t)
	r.cfg = testConfig(t)
	key := fakePrivateKey(t)
	secrets := localCommit(t, r.dir, map[string]string{"web/secrets.env": "# nothing but a comment\n"})
	fixtureGit(t, r.dir, "rm", "-q", "web/secrets.env")
	fixtureGit(t, r.dir, "commit", "-q", "-m", "drop it")
	env := localCommit(t, r.dir, map[string]string{
		"db/app.env":  "export DEMO_TOKEN=not-a-real-secret\n",
		"db/ok.env":   "APP_VERSION=1.0.0\nDEMO_KEY=\nDEMO_PASSWORD=''\nDEMO_SECRET= # later\n",
		"notes.txt":   "DEMO_TOKEN=not-a-real-secret\n",
		".env":        "DEMO_SECRET=\"not-a-real-secret\"\n",
		"deep/a/.env": "APP_VERSION=1\n",
	})
	pemCommit := localCommit(t, r.dir, map[string]string{"keys/id.txt": "header\n" + key})
	big := localCommit(t, r.dir, map[string]string{"big.env": "DEMO_TOKEN=not-a-real-secret\n" + strings.Repeat("# padding line\n", 100000)})
	clean := localCommit(t, r.dir, map[string]string{"README.md": "y\n"})

	scan, err := r.ScanPush(ctx, clean, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []Finding{
		{big, "big.env", KindEnvSecret},
		{pemCommit, "keys/id.txt", KindPrivateKey},
		{env, ".env", KindEnvSecret},
		{env, "db/app.env", KindEnvSecret},
		{secrets, "web/secrets.env", KindSecretsFile},
	}
	if scan.Total != len(want) || len(scan.Findings) != len(want) {
		t.Fatalf("scan = %+v, want %d findings", scan, len(want))
	}
	for _, w := range want {
		found := false
		for _, f := range scan.Findings {
			found = found || f == w
		}
		if !found {
			t.Errorf("missing %+v in %+v", w, scan.Findings)
		}
	}
	shown := fmt.Sprintf("%v %+v", scan, scan)
	body := strings.Split(strings.TrimSpace(key), "\n")[1]
	for _, secret := range []string{"not-a-real-secret", body} {
		if strings.Contains(shown, secret) {
			t.Fatalf("the result holds a value: %s", shown)
		}
	}

	// Only what the remote does not have is scanned.
	scan, err = r.ScanPush(ctx, clean, env)
	if err != nil || scan.Total != 2 {
		t.Fatalf("scan from %s = %+v, %v", env[:7], scan, err)
	}
	if scan, err = r.ScanPush(ctx, clean, clean); err != nil || scan.Total != 0 {
		t.Fatalf("nothing to push: %+v, %v", scan, err)
	}
	if _, err := r.ScanPush(ctx, "--all", ""); err == nil {
		t.Fatal("accepted a revision that is not a commit id")
	}
}

func TestEnvValueDetection(t *testing.T) {
	for in, want := range map[string]bool{
		"DEMO_TOKEN=abc":                  true,
		"export DEMO_API_KEY = 'abc'":     true,
		"demo_password=\"abc\" # note":    true,
		"DEMO_SECRET=":                    false,
		"DEMO_SECRET=''":                  false,
		"DEMO_SECRET=\"  \"":              false,
		"DEMO_SECRET= # only a comment":   false,
		"DEMO_SECRET=#x":                  false,
		"APP_VERSION=1":                   false,
		"# DEMO_TOKEN=abc":                false,
		"DEMO_TOKEN":                      false,
		"A=1\r\nDEMO_TOKEN=abc\r\n":       true,
		"A=1\n\n  DEMO_PASSWORD=abc  \n":  true,
		"DEMO_NOTE=DEMO_TOKEN=\nB=1\n":    false,
		"DEMO_TOKEN=\nDEMO_SECRET=value ": true,
	} {
		if got := envHoldsSecret([]byte(in)); got != want {
			t.Errorf("envHoldsSecret(%q) = %v, want %v", in, got, want)
		}
	}
}
