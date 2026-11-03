package stacks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func load(t *testing.T, root string) map[string]Stack {
	t.Helper()
	list, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]Stack{}
	for _, s := range list {
		m[s.Name] = s
	}
	return m
}

const appCompose = "services:\n  app:\n    image: registry.example.test/demo/app:${DEMO_VERSION}\n"

func one(t *testing.T, files map[string]string) Stack {
	t.Helper()
	root := t.TempDir()
	for k, v := range files {
		write(t, root, "demo-stack/"+k, v)
	}
	s, ok := load(t, root)["demo-stack"]
	if !ok {
		t.Fatal("stack not detected")
	}
	return s
}

func wantErr(t *testing.T, s Stack, parts ...string) {
	t.Helper()
	if s.Err == nil {
		t.Fatalf("expected an error containing %q", parts)
	}
	for _, p := range parts {
		if !strings.Contains(s.Err.Error(), p) {
			t.Errorf("error %q lacks %q", s.Err, p)
		}
	}
}

func TestDetection(t *testing.T) {
	root := t.TempDir()
	write(t, root, "b-stack/compose.yml", appCompose)
	write(t, root, "a-stack/docker-compose.yaml", appCompose)
	write(t, root, "no-compose/readme.txt", "x")
	write(t, root, ".git/compose.yaml", appCompose)
	write(t, root, ".github/compose.yaml", appCompose)
	write(t, root, "loose-file.yaml", appCompose)
	if err := os.Symlink(filepath.Join(root, "a-stack"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	list, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "a-stack" || list[1].Name != "b-stack" {
		t.Fatalf("stacks = %+v", list)
	}
	if list[0].ComposeFiles[0] != "docker-compose.yaml" || list[0].UpdateMode() != ModeNone || list[0].Rules != nil {
		t.Errorf("a-stack = %+v", list[0])
	}
}

func TestLoadMissingDirectory(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestComposeFileFromEnv(t *testing.T) {
	s := one(t, map[string]string{
		"compose.yaml":      "services:\n  app:\n    image: registry.example.test/demo/app:1.0.0\n",
		"compose.prod.yaml": appCompose,
		".env":              "COMPOSE_FILE=compose.yaml:compose.prod.yaml\nDEMO_VERSION=2.1.0\n",
	})
	if s.Err != nil {
		t.Fatal(s.Err)
	}
	if len(s.ComposeFiles) != 2 || len(s.Images) != 1 || s.Images[0].Value != "2.1.0" || len(s.Findings) != 0 {
		t.Errorf("stack = %+v", s)
	}
}

func TestComposeFileCustomSeparator(t *testing.T) {
	s := one(t, map[string]string{
		"a.yaml": appCompose, "b.yaml": appCompose,
		".env": "COMPOSE_PATH_SEPARATOR=;\nCOMPOSE_FILE=a.yaml;b.yaml\nDEMO_VERSION=1\n",
	})
	if s.Err != nil || len(s.ComposeFiles) != 2 {
		t.Fatalf("stack = %+v", s)
	}
}

func TestDefaultOverrideFile(t *testing.T) {
	s := one(t, map[string]string{
		"compose.yaml":          appCompose,
		"compose.override.yaml": "services:\n  app:\n    image: registry.example.test/demo/app:${OTHER}\n",
		".env":                  "OTHER=3.0.0\n",
	})
	if s.Err != nil || len(s.ComposeFiles) != 2 || s.Images[0].Var != "OTHER" {
		t.Fatalf("stack = %+v", s)
	}
}

func TestComposeFilePathSafety(t *testing.T) {
	for _, entry := range []string{"/etc/passwd", "../x.yaml", "sub/../../x.yaml", "compose.yaml::x", "./compose.yaml", "missing.yaml"} {
		s := one(t, map[string]string{"compose.yaml": appCompose, ".env": "COMPOSE_FILE=" + entry + "\n"})
		wantErr(t, s, "demo-stack", "COMPOSE_FILE")
	}
}

func TestSymlinksAreRejected(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "evil.yaml", appCompose)
	write(t, outside, "evil.env", "DEMO_VERSION=1\n")
	write(t, outside, "evil-rules.yaml", "version: 1\n")

	for name, link := range map[string]string{"compose.yaml": "evil.yaml", ".env": "evil.env", "qdeploy.yaml": "evil-rules.yaml"} {
		root := t.TempDir()
		write(t, root, "demo-stack/compose.yaml", appCompose)
		if name == "compose.yaml" {
			os.Remove(filepath.Join(root, "demo-stack/compose.yaml"))
		}
		if err := os.Symlink(filepath.Join(outside, link), filepath.Join(root, "demo-stack", name)); err != nil {
			t.Fatal(err)
		}
		s := load(t, root)["demo-stack"]
		if name == "compose.yaml" {
			wantErr(t, s, "not a regular file")
			continue
		}
		wantErr(t, s, "symlinks are not allowed")
	}

	// A symlinked directory on the COMPOSE_FILE path.
	root := t.TempDir()
	write(t, root, "demo-stack/compose.yaml", appCompose)
	write(t, root, "demo-stack/.env", "COMPOSE_FILE=dir/evil.yaml\n")
	if err := os.Symlink(outside, filepath.Join(root, "demo-stack", "dir")); err != nil {
		t.Fatal(err)
	}
	wantErr(t, load(t, root)["demo-stack"], "symlinks are not allowed")
}

func TestSizeBound(t *testing.T) {
	big := "# " + strings.Repeat("x", MaxFileSize) + "\n"
	wantErr(t, one(t, map[string]string{"compose.yaml": appCompose, ".env": big}), "larger than")
	wantErr(t, one(t, map[string]string{"compose.yaml": big + appCompose}), "larger than")
	wantErr(t, one(t, map[string]string{"compose.yaml": appCompose, "qdeploy.yaml": big}), "larger than")
}

func TestStackCountBound(t *testing.T) {
	root := t.TempDir()
	for i := 0; i <= MaxStacks; i++ {
		write(t, root, "s"+strings.Repeat("x", 1)+string(rune('a'+i%26))+strings.Repeat("0", i/26)+"/compose.yaml", appCompose)
	}
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("err = %v", err)
	}
}

func TestBrokenStackDoesNotHideOthers(t *testing.T) {
	root := t.TempDir()
	write(t, root, "good/compose.yaml", appCompose)
	write(t, root, "bad/compose.yaml", "services: [unclosed\n")
	m := load(t, root)
	if m["good"].Err != nil || m["bad"].Err == nil {
		t.Fatalf("good=%v bad=%v", m["good"].Err, m["bad"].Err)
	}
}

func TestEnvParsing(t *testing.T) {
	env, err := parseEnv("# comment\n\nA=1\nexport B = two # trailing\nC=\"x y\\n#z\" # c\nD='$HOME $(id)'\nE=${A}\nF=a#b\r\n")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "1", "B": "two", "C": "x y\n#z", "D": "$HOME $(id)", "E": "${A}", "F": "a#b"}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
	for _, bad := range []string{"NOEQUALS", "1A=x", "A='open", "A=\"open", "A=\"x\" y"} {
		if _, err := parseEnv(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestImageVariablesAndFindings(t *testing.T) {
	compose := `services:
  braces:
    image: registry.example.test:5000/demo/app:${A_VERSION}
  plain:
    image: demo/app:$B_VERSION
  default:
    image: demo/app:${C_VERSION:-3.4.5}
  unset:
    image: demo/app:${D_VERSION}
  floatvar:
    image: demo/app:${E_VERSION}
  fixed:
    image: demo/app:1.2.3
  latest:
    image: demo/app:latest
  untagged:
    image: demo/app
  digest:
    image: demo/app@sha256:0123
  built:
    build: .
`
	s := one(t, map[string]string{"compose.yaml": compose, ".env": "A_VERSION=1.0.0\nB_VERSION=2.0.0\nE_VERSION=latest\n"})
	if s.Err != nil {
		t.Fatal(s.Err)
	}
	got := map[string]ImageVar{}
	for _, iv := range s.Images {
		got[iv.Service] = iv
	}
	check := func(svc, image, v, val, src string) {
		t.Helper()
		g := got[svc]
		if g.Image != image || g.Var != v || g.Value != val || g.Source != src {
			t.Errorf("%s = %+v", svc, g)
		}
	}
	check("braces", "registry.example.test:5000/demo/app", "A_VERSION", "1.0.0", SourceEnv)
	check("plain", "demo/app", "B_VERSION", "2.0.0", SourceEnv)
	check("default", "demo/app", "C_VERSION", "3.4.5", SourceDefault)
	check("unset", "demo/app", "D_VERSION", "", SourceUnset)

	kinds := map[string]string{}
	for _, f := range s.Findings {
		kinds[f.Service] = f.Kind
	}
	want := map[string]string{
		"unset": FindingUnsetVariable, "floatvar": FindingFloatingTag, "fixed": FindingFixedTag,
		"latest": FindingFloatingTag, "untagged": FindingFloatingTag, "digest": FindingFixedTag,
	}
	if len(kinds) != len(want) {
		t.Errorf("findings = %v", kinds)
	}
	for k, v := range want {
		if kinds[k] != v {
			t.Errorf("finding %s = %q, want %q", k, kinds[k], v)
		}
	}
}

func TestMissingRulesMeansNoUpdates(t *testing.T) {
	s := one(t, map[string]string{"compose.yaml": appCompose, ".env": "DEMO_VERSION=1\n"})
	if s.Rules != nil || s.UpdateMode() != ModeNone || s.Err != nil {
		t.Fatalf("stack = %+v", s)
	}
}

func TestRulesDefaults(t *testing.T) {
	s := one(t, map[string]string{"compose.yaml": appCompose, "qdeploy.yaml": "version: 1\n"})
	if s.Err != nil {
		t.Fatal(s.Err)
	}
	r := s.Rules
	if r.Update.Mode != ModeManual || r.Update.Ceiling != CeilingMinor || r.Update.Major != MajorStepwise ||
		r.Backup != nil || !r.Check.Containers || r.Check.HTTP != nil {
		t.Errorf("rules = %+v", r)
	}
}

func TestRulesFull(t *testing.T) {
	s := one(t, map[string]string{"compose.yaml": appCompose, "qdeploy.yaml": `
version: 1
update:
  mode: automatic
  ceiling: major
  major: direct
  tags:
    DEMO_VERSION: '^[0-9]+\.[0-9]+\.[0-9]+$'
backup:
  paths: [data/db, data/files]
  volumes: [demo_db]
  command: [pg_dump, --clean]
  retention: {keep: 3, max_age_days: 30}
check:
  containers: false
  http: {url: 'http://localhost:8080/health', status: 204, timeout: 5s}
`})
	if s.Err != nil {
		t.Fatal(s.Err)
	}
	r := s.Rules
	if s.UpdateMode() != ModeAutomatic || r.Update.Ceiling != CeilingMajor || r.Update.Major != MajorDirect ||
		!r.Update.Tags["DEMO_VERSION"].MatchString("1.2.3") || r.Update.Tags["DEMO_VERSION"].MatchString("1.2") {
		t.Errorf("update = %+v", r.Update)
	}
	b := r.Backup
	if len(b.Paths) != 2 || b.Volumes[0] != "demo_db" || b.Command[1] != "--clean" || b.Keep != 3 || b.MaxAgeDays != 30 {
		t.Errorf("backup = %+v", b)
	}
	if r.Check.Containers || r.Check.HTTP.Status != 204 || r.Check.HTTP.Timeout != 5*time.Second {
		t.Errorf("check = %+v", r.Check)
	}
}

func TestRulesChangelogRepositories(t *testing.T) {
	for _, value := range []string{"example-org/example-app", "https://github.com/example-org/example-app", "https://github.com/example-org/example-app.git"} {
		s := one(t, map[string]string{"compose.yaml": appCompose, "qdeploy.yaml": "version: 1\nupdate: {changelog: {DEMO_VERSION: '" + value + "'}}\n"})
		if s.Err != nil || s.Rules.Update.Changelog["DEMO_VERSION"] != "example-org/example-app" {
			t.Errorf("%s: err = %v, rules = %+v", value, s.Err, s.Rules)
		}
	}
}

func TestRulesBackupDefaultsAndHTTPDefaults(t *testing.T) {
	s := one(t, map[string]string{"compose.yaml": appCompose, "qdeploy.yaml": "version: 1\nbackup: {paths: [data]}\ncheck: {http: {url: 'https://demo.example.test/'}}\n"})
	if s.Err != nil || s.Rules.Backup.Keep != 7 || s.Rules.Check.HTTP.Status != 200 || s.Rules.Check.HTTP.Timeout != 10*time.Second {
		t.Fatalf("stack = %+v", s)
	}
}

func TestInvalidRules(t *testing.T) {
	cases := []struct {
		name, yaml string
		want       []string
	}{
		{"empty", "", []string{"qdeploy.yaml", "empty"}},
		{"no version", "update: {mode: manual}\n", []string{"version: required"}},
		{"bad version", "version: 2\n", []string{"version: unsupported"}},
		{"unknown top", "version: 1\nfoo: 1\n", []string{"unknown field foo"}},
		{"unknown nested", "version: 1\nupdate: {modee: manual}\n", []string{"unknown field modee"}},
		{"bad mode", "version: 1\nupdate: {mode: sometimes}\n", []string{"update.mode", "automatic, manual"}},
		{"bad ceiling", "version: 1\nupdate: {ceiling: huge}\n", []string{"update.ceiling"}},
		{"bad major", "version: 1\nupdate: {major: fast}\n", []string{"update.major"}},
		{"bad regex", "version: 1\nupdate: {tags: {DEMO_VERSION: '('}}\n", []string{"update.tags.DEMO_VERSION", "regular expression"}},
		{"unknown var", "version: 1\nupdate: {tags: {OTHER: '.*'}}\n", []string{"update.tags.OTHER", "no service image"}},
		{"bad var name", "version: 1\nupdate: {tags: {'a b': '.*'}}\n", []string{"update.tags.a b"}},
		{"changelog bad var name", "version: 1\nupdate: {changelog: {'a b': example-org/example-app}}\n", []string{"update.changelog.a b", "not a variable name"}},
		{"changelog bad repository", "version: 1\nupdate: {changelog: {DEMO_VERSION: 'not a repo'}}\n", []string{"update.changelog.DEMO_VERSION", "GitHub repository"}},
		{"changelog other host", "version: 1\nupdate: {changelog: {DEMO_VERSION: 'https://git.example.test/team/app'}}\n", []string{"update.changelog.DEMO_VERSION"}},
		{"changelog unknown var", "version: 1\nupdate: {changelog: {OTHER: example-org/example-app}}\n", []string{"update.changelog.OTHER", "no service image"}},
		{"empty backup", "version: 1\nbackup: {}\n", []string{"backup:"}},
		{"absolute path", "version: 1\nbackup: {paths: [/etc]}\n", []string{"backup.paths[0]", "absolute"}},
		{"parent path", "version: 1\nbackup: {paths: [data, ../x]}\n", []string{"backup.paths[1]", ".."}},
		{"dot path", "version: 1\nbackup: {paths: ['.']}\n", []string{"backup.paths[0]"}},
		{"bad volume", "version: 1\nbackup: {volumes: ['a/b']}\n", []string{"backup.volumes[0]"}},
		{"keep zero", "version: 1\nbackup: {paths: [d], retention: {keep: 0}}\n", []string{"backup.retention.keep"}},
		{"age zero", "version: 1\nbackup: {paths: [d], retention: {max_age_days: -1}}\n", []string{"backup.retention.max_age_days"}},
		{"command string", "version: 1\nbackup: {command: 'rm -rf /'}\n", []string{"qdeploy.yaml"}},
		{"bad url", "version: 1\ncheck: {http: {url: 'ftp://x'}}\n", []string{"check.http.url"}},
		{"bad status", "version: 1\ncheck: {http: {url: 'http://x', status: 99}}\n", []string{"check.http.status"}},
		{"bad timeout", "version: 1\ncheck: {http: {url: 'http://x', timeout: 1h}}\n", []string{"check.http.timeout"}},
		{"two documents", "version: 1\n---\nversion: 1\n", []string{"one YAML document"}},
		{"duplicate key", "version: 1\nversion: 1\n", []string{"qdeploy.yaml"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := one(t, map[string]string{"compose.yaml": appCompose, "qdeploy.yaml": c.yaml})
			wantErr(t, s, append([]string{"stack demo-stack"}, c.want...)...)
		})
	}
}

func TestDemoStacks(t *testing.T) {
	m := load(t, filepath.Join("..", "..", "e2e", "demo-stacks"))
	for _, name := range []string{"demo-stack", "demo-cache"} {
		s, ok := m[name]
		if !ok || s.Err != nil {
			t.Fatalf("%s = %+v", name, s)
		}
		if len(s.Images) != 1 || s.Images[0].Var != "DEMO_APP_VERSION" || s.Images[0].Image != "registry.qdeploye2e.test/demo/app" {
			t.Errorf("%s images = %+v", name, s.Images)
		}
	}
}
