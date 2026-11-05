package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestNoArgumentsShowsHelp(t *testing.T) {
	code, stdout, _ := run(t)
	if code != 0 || !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "run") {
		t.Fatalf("code %d, stdout %q", code, stdout)
	}
}

func TestVersionJSON(t *testing.T) {
	code, stdout, stderr := run(t, "version", "--output", "json")
	if code != 0 || stderr != "" {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout, err)
	}
	if got["version"] != version {
		t.Fatalf("version = %v, want %s", got["version"], version)
	}
}

func TestVersionTable(t *testing.T) {
	code, stdout, _ := run(t, "version")
	if code != 0 || !strings.Contains(stdout, version) {
		t.Fatalf("code %d, stdout %q", code, stdout)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	if code, _, _ := run(t, "nonsense"); code != 2 {
		t.Fatalf("code %d, want 2", code)
	}
}

func TestHelpTopic(t *testing.T) {
	code, stdout, _ := run(t, "help", "qdeploy")
	if code != 0 || !strings.Contains(stdout, "Docker stacks") {
		t.Fatalf("code %d, stdout %q", code, stdout)
	}
}

func TestManpage(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := run(t, "manpage", "--dir", dir); code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	page, err := os.ReadFile(filepath.Join(dir, "qdeploy.1"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "qdeploy") || !strings.Contains(string(page), "HELP TOPICS") {
		t.Fatalf("unexpected manpage:\n%s", page)
	}
	if code, _, _ := run(t, "manpage"); code != 2 {
		t.Fatalf("manpage without --dir: code %d, want 2", code)
	}
}

func TestCommandTree(t *testing.T) {
	stackCommands := []string{"backup", "changelog", "check", "datadir", "deploy", "list", "log", "pause", "pin", "restart", "resume", "runs",
		"secret", "settings", "start", "stop", "unpause", "unpin", "update", "updates"}
	root, _, _, err := newRoot()
	if err != nil {
		t.Fatal(err)
	}
	// Help topics are registered by the kit as commands without an action; they are not part of the surface.
	var top []string
	for _, c := range root.Commands() {
		if c.Runnable() || c.HasSubCommands() {
			top = append(top, c.Name())
		}
	}
	slices.Sort(top)
	want := []string{"container", "credential", "key", "manpage", "notify", "repo", "run", "stack", "status", "tui", "update", "version", "window"}
	// Cobra adds help and completion itself; they are not part of the qdeploy surface.
	var got []string
	for _, n := range top {
		if n != "completion" && n != "help" {
			got = append(got, n)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("top level commands = %v, want %v", got, want)
	}
	var stack *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "stack" {
			stack = c
		}
	}
	var sub []string
	for _, c := range stack.Commands() {
		sub = append(sub, c.Name())
	}
	slices.Sort(sub)
	if !slices.Equal(sub, stackCommands) {
		t.Fatalf("stack commands = %v, want %v", sub, stackCommands)
	}
}
