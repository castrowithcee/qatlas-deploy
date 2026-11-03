package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

func TestSetKeepsOtherBytes(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", "A=1\nTAG=1.0\nB=2\n", "A=1\nTAG=1.1\nB=2\n"},
		{"comments and blanks", "# top\n\nTAG=1.0\n\n# bottom\nB=\"x y\"\n", "# top\n\nTAG=1.1\n\n# bottom\nB=\"x y\"\n"},
		{"inline comment", "TAG=1.0 # pinned\nB=2\n", "TAG=1.1 # pinned\nB=2\n"},
		{"inline comment tab", "TAG=1.0\t#pinned\n", "TAG=1.1\t#pinned\n"},
		{"double quotes", "TAG=\"1.0\" # c\n", "TAG=\"1.1\" # c\n"},
		{"single quotes", "TAG='1.0'\n", "TAG='1.1'\n"},
		{"export", "export TAG=1.0\n", "export TAG=1.1\n"},
		{"indent and spaces", "  TAG = 1.0  \n", "  TAG = 1.1  \n"},
		{"crlf", "A=1\r\nTAG=1.0\r\nB=2\r\n", "A=1\r\nTAG=1.1\r\nB=2\r\n"},
		{"crlf quoted", "TAG='1.0'\r\nB=2\r\n", "TAG='1.1'\r\nB=2\r\n"},
		{"no final newline", "A=1\nTAG=1.0", "A=1\nTAG=1.1"},
		{"empty old value", "TAG=\nB=2\n", "TAG=1.1\nB=2\n"},
		{"similar names", "TAG2=x\nMY_TAG=y\n#TAG=z\nTAG=1.0\n", "TAG2=x\nMY_TAG=y\n#TAG=z\nTAG=1.1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Set([]byte(tt.in), "TAG", "1.1")
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetErrors(t *testing.T) {
	tests := []struct{ name, in, key, value string }{
		{"missing", "A=1\n", "TAG", "1"},
		{"commented out", "#TAG=1\n", "TAG", "1"},
		{"duplicate", "TAG=1\nTAG=2\n", "TAG", "3"},
		{"duplicate export", "TAG=1\nexport TAG=2\n", "TAG", "3"},
		{"invalid name", "TAG=1\n", "1TAG", "x"},
		{"empty name", "TAG=1\n", "", "x"},
		{"newline", "TAG=1\n", "TAG", "a\nb"},
		{"cr", "TAG=1\n", "TAG", "a\rb"},
		{"nul", "TAG=1\n", "TAG", "a\x00b"},
		{"control", "TAG=1\n", "TAG", "a\x1bb"},
		{"tab", "TAG='1'\n", "TAG", "a\tb"},
		{"single quote in single", "TAG='1'\n", "TAG", "it's"},
		{"unquoted leading space", "TAG=1\n", "TAG", " 1"},
		{"unquoted comment marker", "TAG=1\n", "TAG", "a #b"},
		{"unquoted leading quote", "TAG=1\n", "TAG", `"a`},
		{"empty before comment", "TAG=1 # c\n", "TAG", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := Set([]byte(tt.in), tt.key, tt.value); err == nil {
				t.Fatalf("expected error, got %q", got)
			}
		})
	}
}

func TestSetDoesNotModifyInput(t *testing.T) {
	in := []byte("TAG=1.0\n")
	if _, err := Set(in, "TAG", "2"); err != nil {
		t.Fatal(err)
	}
	if string(in) != "TAG=1.0\n" {
		t.Fatalf("input changed: %q", in)
	}
}

func TestSetRoundTripWithStacks(t *testing.T) {
	inputs := []string{
		"TAG=1.0\n",
		"TAG=1.0 # c\n",
		"TAG='1.0' # c\n",
		"TAG=\"1.0\"\r\nB=2\r\n",
		"export TAG=1.0",
		"TAG=\n",
	}
	values := []string{"1.1", "v2-rc.1", "a b", "it's", `q"uote`, `back\slash`, `a\"b`, "x#y", "$HOME", "", "a  b", `\n`, "ünï"}
	for _, in := range inputs {
		for _, v := range values {
			out, err := Set([]byte(in), "TAG", v)
			if err != nil {
				continue // refused values are covered by TestSetErrors
			}
			if got := loadTag(t, out); got != v {
				t.Errorf("input %q value %q: stacks reads %q from %q", in, v, got, out)
			}
		}
	}
	// every combination of style and value must be either accepted and correct or refused
	out, err := Set([]byte("TAG=\"1\"\n"), "TAG", `a"b\c`)
	if err != nil || loadTag(t, out) != `a"b\c` {
		t.Fatalf("double-quoted escape failed: %q %v", out, err)
	}
	if !strings.Contains(string(out), `"a\"b\\c"`) {
		t.Fatalf("unexpected escape: %q", out)
	}
}

func loadTag(t *testing.T, env []byte) string {
	t.Helper()
	dir := t.TempDir()
	stack := filepath.Join(dir, "app")
	if err := os.Mkdir(stack, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stack, ".env"), env, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stack, "compose.yaml"), []byte("services:\n  web:\n    image: example.invalid/web:${TAG}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := stacks.Load(dir)
	if err != nil || len(list) != 1 {
		t.Fatalf("load: %v %d", err, len(list))
	}
	return list[0].Env["TAG"]
}
