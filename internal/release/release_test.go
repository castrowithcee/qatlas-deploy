package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrustedKeysMatchAllowedSigners(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "release-allowed-signers"))
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != Namespace {
			t.Fatalf("unexpected allowed signers line %q", line)
		}
		want = append(want, fields[1]+" "+fields[2])
	}
	if strings.Join(trustedKeyLines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("compiled keys %q differ from .github/release-allowed-signers %q", trustedKeyLines, want)
	}
	if len(TrustedKeys()) != len(trustedKeyLines) {
		t.Fatalf("TrustedKeys() has %d keys without a build-time addition, want %d", len(TrustedKeys()), len(trustedKeyLines))
	}
}

func TestExtraKeyLinesAddKeys(t *testing.T) {
	old := extraKeyLines
	t.Cleanup(func() { extraKeyLines = old })
	extraKeyLines = "\n" + trustedKeyLines[0] + "\n  \n"
	if got, want := len(TrustedKeys()), len(trustedKeyLines)+1; got != want {
		t.Fatalf("TrustedKeys() = %d keys, want %d", got, want)
	}
}
