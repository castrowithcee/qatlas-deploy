package stacks

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSettingsAreFoundByProjectName(t *testing.T) {
	root := t.TempDir()
	write(t, root, "demo-folder/compose.yaml", named("demo-app"))
	write(t, root, SettingsPath("demo-app"), "version: 1\nupdate:\n  mode: automatic\n")
	write(t, root, SettingsPath("demo-folder"), "version: 1\nupdate:\n  mode: sometimes\n")
	list, err := Load(root)
	if err != nil || len(list) != 1 {
		t.Fatalf("Load: %v, %d stacks", err, len(list))
	}
	if s := list[0]; s.Err != nil || s.Name != "demo-app" || s.UpdateMode() != ModeAutomatic {
		t.Fatalf("stack = %+v", s)
	}
}

func TestSettingsFolderIsNoStack(t *testing.T) {
	root := t.TempDir()
	write(t, root, "demo-stack/compose.yaml", appCompose)
	write(t, root, SettingsPath("demo-stack"), "version: 1\n")
	write(t, root, ".qatlas/compose.yaml", appCompose)
	if m := load(t, root); len(m) != 1 || m["demo-stack"].Rules == nil {
		t.Fatalf("stacks = %+v", m)
	}
}

func TestOldRulesFileIsNotRead(t *testing.T) {
	s := one(t, map[string]string{"compose.yaml": appCompose, OldRulesFile: "version: 1\nupdate:\n  mode: automatic\n"})
	if s.Err != nil || s.Rules != nil || s.UpdateMode() != ModeNone || !s.OldRulesFile {
		t.Fatalf("stack = %+v", s)
	}
	if s := one(t, map[string]string{"compose.yaml": appCompose}); s.OldRulesFile {
		t.Error("OldRulesFile set without the file")
	}
}

func TestComposeFilesOfSettingsMustKeepTheName(t *testing.T) {
	s := one(t, map[string]string{
		"compose.yaml": appCompose, "other.yaml": named("other-name"),
		settingsKey: "version: 1\ncompose:\n  files: [compose.yaml, other.yaml]\n",
	})
	wantErr(t, s, "stack demo-stack", "project name other-name", "belong to demo-stack")
	var re *RulesError
	if !errors.As(s.Err, &re) || s.Name != "demo-stack" {
		t.Fatalf("err = %#v, name = %q", s.Err, s.Name)
	}

	// COMPOSE_PROJECT_NAME fixes the name, whatever the files say.
	s = loadFolder(t, "demo-stack", map[string]string{
		"compose.yaml": appCompose, "other.yaml": named("other-name"), ".env": "COMPOSE_PROJECT_NAME=demo-stack\n",
		settingsKey: "version: 1\ncompose:\n  files: [compose.yaml, other.yaml]\n",
	})
	if s.Err != nil || len(s.ComposeFiles) != 2 {
		t.Fatalf("stack = %+v", s)
	}
}

func TestSettingsReplaceABrokenComposeFileWhenTheNameIsFixed(t *testing.T) {
	s := loadFolder(t, "demo-stack", map[string]string{
		"stack.yaml": appCompose, ".env": "COMPOSE_PROJECT_NAME=demo-stack\nCOMPOSE_FILE=missing.yaml\n",
		settingsKey: "version: 1\ncompose:\n  files: [stack.yaml]\n",
	})
	if s.Err != nil || strings.Join(s.ComposeFiles, ",") != "stack.yaml" {
		t.Fatalf("stack = %+v", s)
	}
	wantErr(t, loadFolder(t, "demo-stack", map[string]string{
		"compose.yaml": appCompose, ".env": "COMPOSE_PROJECT_NAME=demo-stack\nCOMPOSE_FILE=missing.yaml\n",
	}), "COMPOSE_FILE")
}

func TestWithSettingsChecksAgainstTheStack(t *testing.T) {
	root := t.TempDir()
	write(t, root, "demo-stack/compose.yaml", appCompose)
	write(t, root, SettingsPath("demo-stack"), "version: 1\nupdate:\n  mode: sometimes\n")
	good, err := ParseSettings([]byte("version: 1\nupdate:\n  tags:\n    DEMO_VERSION: '^1\\.'\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := WithSettings(root, "demo-stack", good); !ok || s.Err != nil || s.Rules != good {
		t.Fatalf("stack = %+v", s)
	}
	bad, err := ParseSettings([]byte("version: 1\nupdate:\n  tags:\n    OTHER_VERSION: '.*'\n"))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := WithSettings(root, "demo-stack", bad)
	var re *RulesError
	if !errors.As(s.Err, &re) || re.Variable != "OTHER_VERSION" {
		t.Fatalf("err = %v", s.Err)
	}
	if _, ok := WithSettings(root, "missing", good); ok {
		t.Error("a missing folder is a stack")
	}
}

func TestParseSettingsTakesJSON(t *testing.T) {
	r, err := ParseSettings([]byte(`{"version":1,"update":{"mode":"automatic"},"backup":{"paths":["data"]}}`))
	if err != nil || r.Update.Mode != ModeAutomatic || r.Backup.Paths[0] != "data" {
		t.Fatalf("rules = %+v, err = %v", r, err)
	}
	if _, err := ParseSettings([]byte(`{"version":1,"updates":{}}`)); err == nil || !strings.Contains(err.Error(), "unknown field updates") {
		t.Fatalf("err = %v", err)
	}
}

func TestEncodeSettingsIsCanonicalBlockYAML(t *testing.T) {
	r, err := ParseSettings([]byte(`{"version":1,
		"compose":{"files":["compose.yaml","compose.prod.yaml"]},
		"update":{"mode":"automatic","tags":{"DEMO_VERSION":"^[0-9]+\\.[0-9]+$","APP_VERSION":"^1"},
			"changelog":{"DEMO_VERSION":"https://github.com/example-org/example-app.git"}},
		"backup":{"paths":["data/db"],"volumes":["demo_db"],"command":["pg_dump","--clean"],"retention":{"max_age_days":30}},
		"check":{"containers":false,"http":{"url":"http://localhost:8080/health","timeout":"90s"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := EncodeSettings(r)
	if err != nil {
		t.Fatal(err)
	}
	want := `version: 1
compose:
  files:
    - compose.yaml
    - compose.prod.yaml
update:
  mode: automatic
  ceiling: minor
  major: stepwise
  tags:
    APP_VERSION: ^1
    DEMO_VERSION: ^[0-9]+\.[0-9]+$
  changelog:
    DEMO_VERSION: example-org/example-app
backup:
  paths:
    - data/db
  volumes:
    - demo_db
  command:
    - pg_dump
    - --clean
  retention:
    max_age_days: 30
check:
  containers: false
  http:
    url: http://localhost:8080/health
    status: 200
    timeout: 1m30s
`
	if string(data) != want {
		t.Fatalf("encoded:\n%s\nwant:\n%s", data, want)
	}
	back, err := ParseSettings(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := EncodeSettings(back)
	if err != nil || string(again) != want {
		t.Fatalf("second encoding differs: %v\n%s", err, again)
	}
	if !reflect.DeepEqual(back.Settings(), r.Settings()) {
		t.Errorf("settings differ after a round trip")
	}
}

func TestEncodeSettingsWritesTheDefaults(t *testing.T) {
	r, err := ParseSettings([]byte("version: 1\nbackup: {paths: [data]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := EncodeSettings(r)
	if err != nil {
		t.Fatal(err)
	}
	want := "version: 1\nupdate:\n  mode: manual\n  ceiling: minor\n  major: stepwise\nbackup:\n  paths:\n    - data\n" +
		"  retention:\n    keep: 7\ncheck:\n  containers: true\n"
	if string(data) != want {
		t.Fatalf("encoded:\n%s", data)
	}
}
