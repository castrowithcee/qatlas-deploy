package stackcheck

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

type fakeGit struct {
	tracked    []string
	notIgnored bool
	err        error
}

func (g fakeGit) TrackedFiles(_ context.Context, dir string) ([]string, error) {
	var out []string
	for _, p := range g.tracked {
		if strings.HasPrefix(p, dir+"/") {
			out = append(out, p)
		}
	}
	return out, g.err
}

func (g fakeGit) IsIgnored(context.Context, string) (bool, error) { return !g.notIgnored, g.err }

const (
	goodCompose = "services:\n  app:\n    image: registry.example.test/demo/app:${DEMO_VERSION}\n"
	goodEnv     = "DEMO_VERSION=1.2.3\n"
	goodSecrets = "DEMO_PASSWORD=not-a-real-secret\n"
)

func demoFiles() map[string]string {
	return map[string]string{"compose.yaml": goodCompose, ".env": goodEnv, "secrets.env": goodSecrets}
}

func with(files map[string]string, overrides map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range files {
		out[k] = v
	}
	for k, v := range overrides {
		if v == "-" {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	return out
}

func checkFiles(t *testing.T, files map[string]string, git fakeGit) []Finding {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, "demo-stack", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list, err := stacks.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Check(context.Background(), list, git)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func rules(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	return out
}

func has(fs []Finding, rule string) *Finding {
	for i := range fs {
		if fs[i].Rule == rule {
			return &fs[i]
		}
	}
	return nil
}

func TestConformantStackHasNoFinding(t *testing.T) {
	fs := checkFiles(t, with(demoFiles(), map[string]string{
		"compose.prod.yaml": "services:\n  app:\n    restart: always\n",
		".env":              goodEnv + "COMPOSE_FILE=compose.yaml:compose.prod.yaml\n",
		"qdeploy.yaml":      "version: 1\nupdate:\n  tags:\n    DEMO_VERSION: '^[0-9.]+$'\n",
	}), fakeGit{tracked: []string{"demo-stack/compose.yaml", "demo-stack/.env", "demo-stack/compose.prod.yaml"}})
	if len(fs) != 0 {
		t.Fatalf("findings: %+v", fs)
	}
}

func TestRules(t *testing.T) {
	semverFix := "MAJOR.MINOR.PATCH"
	cases := []struct {
		name     string
		files    map[string]string
		git      fakeGit
		rule     string
		severity string
		cause    string
		fix      string
	}{
		{"fixed tag", with(demoFiles(), map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:1.2.3\n"}),
			fakeGit{}, RuleFixedImageTag, SeverityError, "fixed in the compose file", ".env"},
		{"digest", with(demoFiles(), map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app@sha256:0000\n"}),
			fakeGit{}, RuleFixedImageTag, SeverityError, "digest", ".env"},
		{"default version", with(demoFiles(), map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:${OTHER_VERSION:-2.0.0}\n"}),
			fakeGit{}, RuleImageVersionDefault, SeverityWarning, "default in the compose file", "OTHER_VERSION"},
		{"latest", with(demoFiles(), map[string]string{".env": "DEMO_VERSION=latest\n"}),
			fakeGit{}, RuleFloatingImageTag, SeverityError, "latest", semverFix},
		{"no tag", with(demoFiles(), map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app\n"}),
			fakeGit{}, RuleFloatingImageTag, SeverityError, "no tag", semverFix},
		{"no semver core", with(demoFiles(), map[string]string{".env": "DEMO_VERSION=1.2\n"}),
			fakeGit{}, RuleFloatingImageTag, SeverityError, "no MAJOR.MINOR.PATCH core", semverFix},
		{"unset variable", with(demoFiles(), map[string]string{".env": "OTHER=1\n"}),
			fakeGit{}, RuleUnsetImageVariable, SeverityError, "DEMO_VERSION", ".env"},
		{"duplicate key", with(demoFiles(), map[string]string{".env": goodEnv + "DEMO_PASSWORD=x\n"}),
			fakeGit{}, RuleDuplicateKey, SeverityError, "DEMO_PASSWORD", "exactly one file"},
		{"secrets tracked", demoFiles(), fakeGit{tracked: []string{"demo-stack/secrets.env"}},
			RuleTrackedSecretsEnv, SeverityError, "tracked by git", "rotate"},
		{"secrets not ignored", demoFiles(), fakeGit{notIgnored: true},
			RuleSecretsNotIgnored, SeverityWarning, ".gitignore", "secrets.env"},
		{"data tracked", demoFiles(), fakeGit{tracked: []string{"demo-stack/data/db/x.bin", "demo-stack/data/y"}},
			RuleTrackedData, SeverityError, "2 file(s)", "data"},
		{"compose file missing", with(demoFiles(), map[string]string{".env": goodEnv + "COMPOSE_FILE=compose.yaml:compose.prod.yaml\n"}),
			fakeGit{}, RuleMissingComposeFile, SeverityError, "compose.prod.yaml", "COMPOSE_FILE"},
		{"compose file unsafe", with(demoFiles(), map[string]string{".env": goodEnv + "COMPOSE_FILE=../compose.yaml\n"}),
			fakeGit{}, RuleInvalidComposeFile, SeverityError, "..", "relative path"},
		{"rules invalid", with(demoFiles(), map[string]string{"qdeploy.yaml": "version: 1\nupdate:\n  mode: sometimes\n"}),
			fakeGit{}, RuleInvalidRules, SeverityError, "qdeploy.yaml", "qdeploy.yaml"},
		{"rules unknown variable", with(demoFiles(), map[string]string{"qdeploy.yaml": "version: 1\nupdate:\n  tags:\n    NOPE_VERSION: '^1$'\n"}),
			fakeGit{}, RuleUnknownRulesVar, SeverityError, "NOPE_VERSION", "qdeploy.yaml"},
		{"unreadable stack", with(demoFiles(), map[string]string{".env": "not a line\n"}),
			fakeGit{}, RuleUnreadableStack, SeverityError, ".env", "Correct"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := checkFiles(t, tc.files, tc.git)
			f := has(fs, tc.rule)
			if f == nil {
				t.Fatalf("no %s finding in %v", tc.rule, rules(fs))
			}
			if f.Stack != "demo-stack" || f.Severity != tc.severity ||
				!strings.Contains(f.Cause, tc.cause) || !strings.Contains(f.Fix, tc.fix) {
				t.Fatalf("finding = %+v", *f)
			}
			if len(fs) != 1 {
				t.Fatalf("unexpected extra findings: %v", rules(fs))
			}
		})
	}
}

func TestRulesStayQuietOnConformantInput(t *testing.T) {
	// Each case is the near miss of a rule above and must yield nothing.
	for name, files := range map[string]map[string]string{
		"semver with suffix":  with(demoFiles(), map[string]string{".env": "DEMO_VERSION=v1.2.3-alpine\n"}),
		"env default is set":  with(demoFiles(), map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:${DEMO_VERSION:-9.9.9}\n"}),
		"distinct keys":       with(demoFiles(), map[string]string{"secrets.env": "# note\nexport OTHER_SECRET=x\n"}),
		"valid compose files": with(demoFiles(), map[string]string{".env": goodEnv + "COMPOSE_FILE=compose.yaml\n"}),
		"rules with variable": with(demoFiles(), map[string]string{"qdeploy.yaml": "version: 1\nupdate:\n  tags:\n    DEMO_VERSION: '^1$'\n"}),
	} {
		if fs := checkFiles(t, files, fakeGit{tracked: []string{"demo-stack/compose.yaml", "demo-stack/database-notes.txt"}}); len(fs) != 0 {
			t.Errorf("%s: findings %+v", name, fs)
		}
	}
}

func TestCompliantRuleOnlyFlagsItsOwnRule(t *testing.T) {
	// tracked files that merely look like data or secrets
	fs := checkFiles(t, demoFiles(), fakeGit{tracked: []string{"demo-stack/data.txt", "demo-stack/sub/secrets.env", "demo-stack-2/data/x"}})
	if len(fs) != 0 {
		t.Fatalf("findings %+v", fs)
	}
}

func TestEveryMissingComposeFileIsListed(t *testing.T) {
	fs := checkFiles(t, with(demoFiles(), map[string]string{".env": goodEnv + "COMPOSE_FILE=a.yaml:b.yaml\n"}), fakeGit{})
	if len(fs) != 2 || fs[0].Rule != RuleMissingComposeFile || fs[1].Rule != RuleMissingComposeFile {
		t.Fatalf("findings %+v", fs)
	}
}

func TestGitFailureFailsTheCheck(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "demo-stack"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "demo-stack", "compose.yaml"), []byte(goodCompose), 0o644); err != nil {
		t.Fatal(err)
	}
	list, _ := stacks.Load(root)
	if _, err := Check(context.Background(), list, fakeGit{err: errors.New("boom")}); err == nil {
		t.Fatal("a git failure was reported as a clean result")
	}
}

func TestSecretValuesNeverAppear(t *testing.T) {
	const value = "not-a-real-secret-4711"
	fs := checkFiles(t, with(demoFiles(), map[string]string{
		"secrets.env": "DEMO_PASSWORD=" + value + "\nbroken line " + value + "\nDEMO_VERSION=" + value + "\n",
		".env":        goodEnv + "DEMO_PASSWORD=x\n",
	}), fakeGit{tracked: []string{"demo-stack/secrets.env"}, notIgnored: true})
	if has(fs, RuleDuplicateKey) == nil || has(fs, RuleTrackedSecretsEnv) == nil {
		t.Fatalf("expected findings, got %v", rules(fs))
	}
	for _, f := range fs {
		if strings.Contains(f.Cause+f.Fix, value) {
			t.Fatalf("a secret value reached a finding: %+v", f)
		}
	}
}

func TestFindingsAreSorted(t *testing.T) {
	fs := checkFiles(t, with(demoFiles(), map[string]string{"secrets.env": "-", ".env": "DEMO_VERSION=latest\n"}), fakeGit{notIgnored: true})
	var got []string
	for _, f := range fs {
		got = append(got, f.Severity+"/"+f.Rule)
	}
	want := []string{"error/floating-image-tag", "warning/secrets-env-not-ignored"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSecretsEnvSymlinkIsRefused(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo-stack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"compose.yaml": goodCompose, ".env": goodEnv, "elsewhere.txt": goodSecrets} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "elsewhere.txt"), filepath.Join(dir, "secrets.env")); err != nil {
		t.Fatal(err)
	}
	list, _ := stacks.Load(root)
	fs, err := Check(context.Background(), list, fakeGit{})
	if err != nil || len(fs) != 1 || fs[0].Rule != RuleUnreadableSecrets || fs[0].Severity != SeverityError {
		t.Fatalf("findings %+v, err %v", fs, err)
	}
}

func TestStackWithoutSecretsEnvHasNoMissingFileFinding(t *testing.T) {
	fs := checkFiles(t, with(demoFiles(), map[string]string{"secrets.env": "-"}), fakeGit{})
	if len(fs) != 0 {
		t.Fatalf("findings: %+v", fs)
	}
}

func TestDeployOnlyEnvIsAFinding(t *testing.T) {
	fs := checkFiles(t, with(demoFiles(), map[string]string{".env": goodEnv + "broken line\n"}), fakeGit{})
	f := has(fs, RuleUnreadableStack)
	if f == nil || f.Severity != SeverityError || !strings.Contains(f.Cause, ".env: line 2") || strings.Contains(f.Cause, "broken") {
		t.Fatalf("findings: %+v", fs)
	}
}

func TestComposeFilesOfRulesAreNamedInTheFinding(t *testing.T) {
	fs := checkFiles(t, with(demoFiles(), map[string]string{"qdeploy.yaml": "version: 1\ncompose: {files: [compose.yaml, gone.yaml]}\n"}), fakeGit{})
	f := has(fs, RuleMissingComposeFile)
	if f == nil || !strings.Contains(f.Cause, "compose.files in qdeploy.yaml") || !strings.Contains(f.Cause, "gone.yaml") {
		t.Fatalf("findings: %+v", fs)
	}
}
