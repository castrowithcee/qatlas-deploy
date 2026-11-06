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
		if dir == stacks.RootFolder || strings.HasPrefix(p, dir+"/") {
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

// settingsKey in the files of checkFiles stands for the settings file of demo-stack.
const settingsKey = "<settings>"

func load(t *testing.T, files map[string]string) []stacks.Stack {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, "demo-stack", name)
		if name == settingsKey {
			p = filepath.Join(root, filepath.FromSlash(stacks.SettingsPath("demo-stack")))
		}
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
	return list
}

// report checks the files as one stack in a repository with a remote.
func report(t *testing.T, files map[string]string, git fakeGit) Report {
	t.Helper()
	got, err := Check(context.Background(), load(t, files), git, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("reports: %+v", got)
	}
	return got[0]
}

func checkFiles(t *testing.T, files map[string]string, git fakeGit) []Finding {
	t.Helper()
	return report(t, files, git).Findings
}

func function(r Report, name string) Function {
	for _, f := range r.Functions {
		if f.Name == name {
			return f
		}
	}
	return Function{}
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

func TestConformantStackHasNoFindingAndEveryFunction(t *testing.T) {
	fs := checkFiles(t, with(demoFiles(), map[string]string{
		"compose.prod.yaml": "services:\n  app:\n    restart: always\n",
		".env":              goodEnv + "COMPOSE_FILE=compose.yaml:compose.prod.yaml\n",
		settingsKey:         "version: 1\nupdate:\n  tags:\n    DEMO_VERSION: '^[0-9.]+$'\n",
	}), fakeGit{tracked: []string{"demo-stack/compose.yaml", "demo-stack/.env", "demo-stack/compose.prod.yaml"}})
	if len(fs) != 0 {
		t.Fatalf("findings: %+v", fs)
	}
}

func TestFunctionsComeInFixedOrder(t *testing.T) {
	r := report(t, demoFiles(), fakeGit{})
	var got []string
	for _, f := range r.Functions {
		got = append(got, f.Name)
	}
	if strings.Join(got, ",") != "deploy,update,rollback,backup,redact" {
		t.Fatalf("functions %v", got)
	}
}

func TestFunctions(t *testing.T) {
	withBackup := "version: 1\nbackup:\n  paths: [data/db]\n  volumes: [state]\n"
	cases := []struct {
		name     string
		files    map[string]string
		function string
		works    bool
		mode     string
		reason   string
		action   string
	}{
		{"deploy works", demoFiles(), FuncDeploy, true, "", "compose.yaml", ""},
		{"deploy-only env still deploys", with(demoFiles(), map[string]string{".env": goodEnv + "broken line\n"}), FuncDeploy, true, "", "Compose reads it itself", ""},
		{"deploy refused", with(demoFiles(), map[string]string{".env": goodEnv + "COMPOSE_FILE=gone.yaml\n"}), FuncDeploy, false, "", "gone.yaml", "Add the file"},
		{"deploy refused by an unset version", with(demoFiles(), map[string]string{".env": "OTHER=1\n"}), FuncDeploy, false, "", "empty tag", "DEMO_VERSION"},

		{"update automatic", with(demoFiles(), map[string]string{settingsKey: "version: 1\nupdate:\n  mode: automatic\n"}),
			FuncUpdate, true, "automatic", "updates DEMO_VERSION by itself", ""},
		{"update manual", with(demoFiles(), map[string]string{settingsKey: "version: 1\n"}),
			FuncUpdate, true, "manual", "reports new versions of DEMO_VERSION", ""},
		{"update not switched on", demoFiles(), FuncUpdate, false, "none", "not switched on", "Switch on updates in the stack's settings in the qdeploy TUI"},
		{"fixed tag is deploy only", with(demoFiles(), map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:1.2.3\n"}),
			FuncUpdate, false, "none", "fixed in the compose file: deploy only", "${SERVICE_VERSION}"},
		{"digest is deploy only", with(demoFiles(), map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app@sha256:0000\n"}),
			FuncUpdate, false, "none", "digest", ".env"},
		{"latest", with(demoFiles(), map[string]string{".env": "DEMO_VERSION=latest\n"}), FuncUpdate, false, "none", "latest", "MAJOR.MINOR.PATCH"},
		{"no tag", with(demoFiles(), map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app\n"}),
			FuncUpdate, false, "none", "no tag", "MAJOR.MINOR.PATCH"},
		{"no semver core", with(demoFiles(), map[string]string{".env": "DEMO_VERSION=1.2\n"}), FuncUpdate, false, "none", "no MAJOR.MINOR.PATCH core", "DEMO_VERSION"},
		{"default version", with(demoFiles(), map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:${OTHER_VERSION:-2.0.0}\n"}),
			FuncUpdate, false, "none", "default in the compose file", "OTHER_VERSION"},
		{"variable of two images", with(demoFiles(), map[string]string{
			"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:${DEMO_VERSION}\n  db:\n    image: registry.example.test/demo/db:${DEMO_VERSION}\n",
			settingsKey:    "version: 1\n"}), FuncUpdate, false, "manual", "more than one image", "own"},
		{"one image fixed beside an updatable one", with(demoFiles(), map[string]string{
			"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:${DEMO_VERSION}\n  db:\n    image: registry.example.test/demo/db:16.4.0\n",
			settingsKey:    "version: 1\nupdate:\n  mode: automatic\n"}), FuncUpdate, true, "automatic", "not updated: service db", "${SERVICE_VERSION}"},
		{"update of a deploy-only stack", with(demoFiles(), map[string]string{".env": goodEnv + "broken line\n"}), FuncUpdate, false, "none", "cannot read .env", "Correct .env"},
		{"update of a broken stack", with(demoFiles(), map[string]string{".env": goodEnv + "COMPOSE_FILE=gone.yaml\n"}), FuncUpdate, false, "none", "cannot be deployed", "deploy problem"},

		{"rollback with data", with(demoFiles(), map[string]string{settingsKey: withBackup}), FuncRollback, true, "", "restores the backup", ""},
		{"rollback without data", demoFiles(), FuncRollback, false, "", "previous commit, but leaves the data", "backup.paths"},

		{"backup", with(demoFiles(), map[string]string{settingsKey: withBackup}), FuncBackup, true, "", "data/db and the volumes state", ""},
		{"backup without a section", with(demoFiles(), map[string]string{settingsKey: "version: 1\n"}), FuncBackup, false, "", "no backup section", "qdeploy stack settings set demo-stack"},
		{"backup path outside data", with(demoFiles(), map[string]string{settingsKey: "version: 1\nbackup:\n  paths: [config]\n"}),
			FuncBackup, false, "", "config is not below data/", "backup.paths"},
		{"backup of a broken stack", with(demoFiles(), map[string]string{settingsKey: "version: 1\nbackup:\n  paths: [../x]\n"}), FuncBackup, false, "", "cannot be read", "deploy problem"},

		{"redact secrets.env", with(demoFiles(), map[string]string{".env": goodEnv + "API_TOKEN=x\n"}), FuncRedact, true, "", "secrets.env (1 key(s)) and of the .env keys named like a secret (API_TOKEN)", ""},
		{"redact by name only", with(demoFiles(), map[string]string{"secrets.env": "-", ".env": goodEnv + "DB_PASSWORD=x\n"}),
			FuncRedact, true, "", "no secrets.env; the values of the .env keys named like a secret (DB_PASSWORD)", "qdeploy stack secret set demo-stack"},
		{"nothing to redact", with(demoFiles(), map[string]string{"secrets.env": "-"}), FuncRedact, false, "", "no value is redacted", "If the stack has secrets"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := function(report(t, tc.files, fakeGit{}), tc.function)
			if f.Name != tc.function || f.Works != tc.works || f.Mode != tc.mode || !strings.Contains(f.Reason, tc.reason) ||
				(tc.action == "") != (f.Action == "") || !strings.Contains(f.Action, tc.action) {
				t.Fatalf("function = %+v", f)
			}
			if tc.function != FuncUpdate && f.Mode != "" {
				t.Fatalf("mode outside update: %+v", f)
			}
		})
	}
}

func TestRedactOfAnUnreadableSecretsFile(t *testing.T) {
	list := load(t, demoFiles())
	dir := list[0].Dir
	if err := os.Remove(filepath.Join(dir, "secrets.env")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, ".env"), filepath.Join(dir, "secrets.env")); err != nil {
		t.Fatal(err)
	}
	rs, err := Check(context.Background(), list, fakeGit{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if f := function(rs[0], FuncRedact); f.Works || !strings.Contains(f.Reason, "not redacted") || f.Action == "" {
		t.Fatalf("redact = %+v", f)
	}
	if f := function(rs[0], FuncDeploy); f.Works {
		t.Fatalf("deploy = %+v", f)
	}
}

func TestSecretsInEnvWithoutSecretsFileAreNoError(t *testing.T) {
	fs := checkFiles(t, with(demoFiles(), map[string]string{"secrets.env": "-", ".env": goodEnv + "DB_PASSWORD=x\nAPI_TOKEN=y\n"}),
		fakeGit{tracked: []string{"demo-stack/.env"}})
	if len(fs) != 0 {
		t.Fatalf("findings: %+v", fs)
	}
}

func TestTrackedSecretsEnvIsAnErrorOnlyWithARemote(t *testing.T) {
	git := fakeGit{tracked: []string{"demo-stack/secrets.env"}}
	for _, remote := range []bool{true, false} {
		rs, err := Check(context.Background(), load(t, demoFiles()), git, remote)
		if err != nil {
			t.Fatal(err)
		}
		f := has(rs[0].Findings, RuleTrackedSecretsEnv)
		want := SeverityWarning
		if remote {
			want = SeverityError
		}
		if f == nil || f.Severity != want || len(rs[0].Findings) != 1 {
			t.Fatalf("remote %v: findings %+v", remote, rs[0].Findings)
		}
		if !function(rs[0], FuncDeploy).Works {
			t.Fatalf("remote %v: a versioned secrets.env stopped the deploy: %+v", remote, rs[0].Functions)
		}
	}
}

func TestRules(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		git      fakeGit
		rule     string
		severity string
		cause    string
		fix      string
	}{
		{"unset variable", with(demoFiles(), map[string]string{".env": "OTHER=1\n"}),
			fakeGit{}, RuleUnsetImageVariable, SeverityError, "DEMO_VERSION", ".env"},
		{"duplicate key", with(demoFiles(), map[string]string{".env": goodEnv + "DEMO_PASSWORD=x\n"}),
			fakeGit{}, RuleDuplicateKey, SeverityWarning, "DEMO_PASSWORD", "exactly one file"},
		{"secrets tracked", demoFiles(), fakeGit{tracked: []string{"demo-stack/secrets.env"}},
			RuleTrackedSecretsEnv, SeverityError, "reach the remote", "rotate"},
		{"secrets not ignored", demoFiles(), fakeGit{notIgnored: true},
			RuleSecretsNotIgnored, SeverityWarning, ".gitignore", "secrets.env"},
		{"data tracked", demoFiles(), fakeGit{tracked: []string{"demo-stack/data/db/x.bin", "demo-stack/data/y"}},
			RuleTrackedData, SeverityWarning, "2 file(s)", "data"},
		{"compose file missing", with(demoFiles(), map[string]string{".env": goodEnv + "COMPOSE_FILE=compose.yaml:compose.prod.yaml\n"}),
			fakeGit{}, RuleMissingComposeFile, SeverityError, "compose.prod.yaml", "COMPOSE_FILE"},
		{"compose file unsafe", with(demoFiles(), map[string]string{".env": goodEnv + "COMPOSE_FILE=../compose.yaml\n"}),
			fakeGit{}, RuleInvalidComposeFile, SeverityError, "..", "relative path"},
		{"rules invalid", with(demoFiles(), map[string]string{settingsKey: "version: 1\nupdate:\n  mode: sometimes\n"}),
			fakeGit{}, RuleInvalidRules, SeverityError, "stack settings", "qdeploy stack settings set"},
		{"rules unknown variable", with(demoFiles(), map[string]string{settingsKey: "version: 1\nupdate:\n  tags:\n    NOPE_VERSION: '^1$'\n"}),
			fakeGit{}, RuleUnknownRulesVar, SeverityError, "NOPE_VERSION", "stack settings"},
		{"old rules file", with(demoFiles(), map[string]string{"qdeploy.yaml": "version: 1\nupdate:\n  mode: automatic\n"}),
			fakeGit{}, RuleOldRulesFile, SeverityWarning, "no settings and is not updated", "qdeploy stack settings set demo-stack"},
		{"unreadable stack", with(demoFiles(), map[string]string{".env": "not a line\n"}),
			fakeGit{}, RuleUnreadableStack, SeverityWarning, ".env", "Correct"},
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

func TestImageTagsAreNoFinding(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"fixed":     with(demoFiles(), map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:1.2.3\n"}),
		"latest":    with(demoFiles(), map[string]string{".env": "DEMO_VERSION=latest\n"}),
		"no tag":    with(demoFiles(), map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app\n"}),
		"default":   with(demoFiles(), map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:${OTHER_VERSION:-2.0.0}\n"}),
		"no semver": with(demoFiles(), map[string]string{".env": "DEMO_VERSION=1.2\n"}),
	} {
		r := report(t, files, fakeGit{})
		if len(r.Findings) != 0 || !function(r, FuncDeploy).Works || function(r, FuncUpdate).Works {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

func TestVersionInSecretsEnvDeploysButIsNotUpdated(t *testing.T) {
	r := report(t, with(demoFiles(), map[string]string{".env": "-", "secrets.env": "DEMO_VERSION=1.2.3\n", settingsKey: "version: 1\n"}), fakeGit{})
	if len(r.Findings) != 0 || !function(r, FuncDeploy).Works {
		t.Fatalf("report %+v", r)
	}
	if f := function(r, FuncUpdate); f.Works || !strings.Contains(f.Reason, "set in secrets.env") || !strings.Contains(f.Action, "Move DEMO_VERSION") {
		t.Fatalf("update = %+v", f)
	}
}

func TestRulesStayQuietOnConformantInput(t *testing.T) {
	// Each case is the near miss of a rule above and must yield nothing.
	for name, files := range map[string]map[string]string{
		"semver with suffix":  with(demoFiles(), map[string]string{".env": "DEMO_VERSION=v1.2.3-alpine\n"}),
		"env default is set":  with(demoFiles(), map[string]string{"compose.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:${DEMO_VERSION:-9.9.9}\n"}),
		"distinct keys":       with(demoFiles(), map[string]string{"secrets.env": "# note\nexport OTHER_SECRET=x\n"}),
		"valid compose files": with(demoFiles(), map[string]string{".env": goodEnv + "COMPOSE_FILE=compose.yaml\n"}),
		"rules with variable": with(demoFiles(), map[string]string{settingsKey: "version: 1\nupdate:\n  tags:\n    DEMO_VERSION: '^1$'\n"}),
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
	if _, err := Check(context.Background(), list, fakeGit{err: errors.New("boom")}, true); err == nil {
		t.Fatal("a git failure was reported as a clean result")
	}
}

func TestSecretValuesNeverAppear(t *testing.T) {
	const value = "not-a-real-secret-4711"
	r := report(t, with(demoFiles(), map[string]string{
		"secrets.env": "DEMO_PASSWORD=" + value + "\nbroken line " + value + "\nDEMO_VERSION=" + value + "\n",
		".env":        goodEnv + "DEMO_PASSWORD=x\n",
	}), fakeGit{tracked: []string{"demo-stack/secrets.env"}, notIgnored: true})
	fs := r.Findings
	if has(fs, RuleDuplicateKey) == nil || has(fs, RuleTrackedSecretsEnv) == nil {
		t.Fatalf("expected findings, got %v", rules(fs))
	}
	for _, f := range fs {
		if strings.Contains(f.Cause+f.Fix, value) {
			t.Fatalf("a secret value reached a finding: %+v", f)
		}
	}
	for _, f := range r.Functions {
		if strings.Contains(f.Reason+f.Action, value) {
			t.Fatalf("a secret value reached a function: %+v", f)
		}
	}
}

func TestFindingsAreSorted(t *testing.T) {
	fs := checkFiles(t, with(demoFiles(), map[string]string{"secrets.env": "-", ".env": "OTHER=1\n"}), fakeGit{notIgnored: true})
	var got []string
	for _, f := range fs {
		got = append(got, f.Severity+"/"+f.Rule)
	}
	want := []string{"error/unset-image-variable", "warning/secrets-env-not-ignored"}
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
	rs, err := Check(context.Background(), list, fakeGit{}, true)
	if err != nil || len(rs) != 1 {
		t.Fatalf("reports %+v, err %v", rs, err)
	}
	if fs := rs[0].Findings; len(fs) != 1 || fs[0].Rule != RuleUnreadableSecrets || fs[0].Severity != SeverityError {
		t.Fatalf("findings %+v", fs)
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
	if f == nil || f.Severity != SeverityWarning || !strings.Contains(f.Cause, ".env: line 2") || strings.Contains(f.Cause, "broken") {
		t.Fatalf("findings: %+v", fs)
	}
}

func TestComposeFilesOfRulesAreNamedInTheFinding(t *testing.T) {
	fs := checkFiles(t, with(demoFiles(), map[string]string{settingsKey: "version: 1\ncompose: {files: [compose.yaml, gone.yaml]}\n"}), fakeGit{})
	f := has(fs, RuleMissingComposeFile)
	if f == nil || !strings.Contains(f.Cause, "compose.files in the stack settings") || !strings.Contains(f.Cause, "gone.yaml") {
		t.Fatalf("findings: %+v", fs)
	}
}

func TestOldRulesFileBesideSettingsNamesTheMode(t *testing.T) {
	fs := checkFiles(t, with(demoFiles(), map[string]string{
		"qdeploy.yaml": "version: 1\n", settingsKey: "version: 1\nupdate:\n  mode: automatic\n",
	}), fakeGit{})
	if len(fs) != 1 || fs[0].Rule != RuleOldRulesFile || !strings.Contains(fs[0].Cause, "update mode automatic") {
		t.Fatalf("findings: %+v", fs)
	}
}
