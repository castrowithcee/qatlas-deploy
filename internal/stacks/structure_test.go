package stacks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func named(name string) string {
	return "name: " + name + "\nservices:\n  app:\n    image: registry.example.test/demo/app:${DEMO_VERSION}\n"
}

func loadFolder(t *testing.T, folder string, files map[string]string) Stack {
	t.Helper()
	root := t.TempDir()
	for k, v := range files {
		write(t, root, folder+"/"+k, v)
	}
	list, err := Load(root)
	if err != nil || len(list) != 1 {
		t.Fatalf("Load: %v, %d stacks", err, len(list))
	}
	return list[0]
}

func TestProjectNameOrder(t *testing.T) {
	cases := []struct {
		name   string
		folder string
		files  map[string]string
		want   string
	}{
		{"folder", "demo-stack", map[string]string{"compose.yaml": appCompose}, "demo-stack"},
		{"folder is normalized", "My Stack.X!", map[string]string{"compose.yaml": appCompose}, "mystackx"},
		{"folder leading separators", "_-9Abc", map[string]string{"compose.yaml": appCompose}, "9abc"},
		{"compose name beats folder", "demo-stack", map[string]string{"compose.yaml": named("from-file")}, "from-file"},
		{"compose name is normalized", "demo-stack", map[string]string{"compose.yaml": named("Up.Per")}, "upper"},
		{"env beats compose name", "demo-stack", map[string]string{"compose.yaml": named("from-file"), ".env": "COMPOSE_PROJECT_NAME=from-env\n"}, "from-env"},
		{"empty env value is ignored", "demo-stack", map[string]string{"compose.yaml": named("from-file"), ".env": "COMPOSE_PROJECT_NAME=\n"}, "from-file"},
		{"later file overrides name", "demo-stack", map[string]string{"compose.yaml": named("first"), "compose.override.yaml": "name: second\n"}, "second"},
		{"later file without name keeps it", "demo-stack", map[string]string{"compose.yaml": named("first"), "compose.override.yaml": "services: {}\n"}, "first"},
		{"later empty name keeps it", "demo-stack", map[string]string{"compose.yaml": named("first"), "compose.override.yaml": "name: \"\"\n"}, "first"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := loadFolder(t, c.folder, c.files)
			if s.Err != nil || s.Name != c.want || !s.NameDerived {
				t.Fatalf("name = %q derived = %v err = %v, want %q", s.Name, s.NameDerived, s.Err, c.want)
			}
			if s.Folder != c.folder {
				t.Errorf("folder = %q, want %q", s.Folder, c.folder)
			}
		})
	}
}

func TestProjectNameRefusals(t *testing.T) {
	cases := []struct {
		name   string
		folder string
		files  map[string]string
		want   string
	}{
		{"folder without usable character", "___", map[string]string{"compose.yaml": appCompose}, "yields no valid project name"},
		{"invalid env name", "demo-stack", map[string]string{"compose.yaml": appCompose, ".env": "COMPOSE_PROJECT_NAME=Upper\n"}, "COMPOSE_PROJECT_NAME"},
		{"env name with variable", "demo-stack", map[string]string{"compose.yaml": appCompose, ".env": "COMPOSE_PROJECT_NAME=a${B}\n"}, "COMPOSE_PROJECT_NAME"},
		{"interpolated compose name", "demo-stack", map[string]string{"compose.yaml": named("x${B}")}, "interpolation"},
		{"unusable compose name", "demo-stack", map[string]string{"compose.yaml": named("'!!!'")}, "no usable character"},
		{"non-string compose name", "demo-stack", map[string]string{"compose.yaml": named("123")}, "name must be a string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := loadFolder(t, c.folder, c.files)
			wantErr(t, s, c.want)
			if s.Name != c.folder {
				t.Errorf("name = %q, want the folder %q", s.Name, c.folder)
			}
		})
	}
}

func TestDuplicateProjectNameMarksTheLaterFolder(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a-stack/compose.yaml", named("shared"))
	write(t, root, "b-stack/compose.yaml", named("shared"))
	list, err := Load(root)
	if err != nil || len(list) != 2 {
		t.Fatalf("Load: %v, %d", err, len(list))
	}
	if list[0].Err != nil || list[1].Err == nil || !strings.Contains(list[1].Err.Error(), "already used by the folder a-stack") {
		t.Fatalf("stacks = %+v / %+v", list[0].Err, list[1].Err)
	}
}

func TestComposeFilesFromRules(t *testing.T) {
	s := one(t, map[string]string{
		"compose.yaml":      "services:\n  app:\n    image: registry.example.test/demo/app:1.0.0\n",
		"compose.prod.yaml": appCompose,
		"qdeploy.yaml":      "version: 1\ncompose:\n  files: [compose.yaml, compose.prod.yaml]\n",
		".env":              "COMPOSE_FILE=compose.yaml\nDEMO_VERSION=2.0.0\n",
	})
	if s.Err != nil || strings.Join(s.ComposeFiles, ",") != "compose.yaml,compose.prod.yaml" {
		t.Fatalf("stack = %+v", s)
	}
	if s.Rules == nil || len(s.Images) != 1 || s.Images[0].Value != "2.0.0" {
		t.Errorf("rules/images = %+v / %+v", s.Rules, s.Images)
	}
}

func TestComposeFilesFromRulesWithoutDefaultNames(t *testing.T) {
	s := one(t, map[string]string{"stack.yaml": appCompose, "qdeploy.yaml": "version: 1\ncompose: {files: [stack.yaml]}\n"})
	if s.Err != nil || len(s.ComposeFiles) != 1 || s.ComposeFiles[0] != "stack.yaml" {
		t.Fatalf("stack = %+v", s)
	}
}

func TestComposeFilesFromRulesAreValidated(t *testing.T) {
	for _, c := range []struct{ name, yaml, want string }{
		{"empty list", "compose: {files: []}", "compose.files: needs at least one file"},
		{"absolute", "compose: {files: [/etc/passwd]}", "compose.files[0]"},
		{"parent", "compose: {files: [compose.yaml, ../x.yaml]}", "compose.files[1]"},
		{"not clean", "compose: {files: ['./compose.yaml']}", "compose.files[0]"},
		{"dash", "compose: {files: [-f]}", "must not start with '-'"},
		{"twice", "compose: {files: [compose.yaml, compose.yaml]}", "listed twice"},
		{"unknown field", "compose: {file: [compose.yaml]}", "unknown field file"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := one(t, map[string]string{"compose.yaml": appCompose, "qdeploy.yaml": "version: 1\n" + c.yaml + "\n"})
			wantErr(t, s, c.want)
		})
	}
	s := one(t, map[string]string{"compose.yaml": appCompose, "qdeploy.yaml": "version: 1\ncompose: {files: [compose.yaml, missing.yaml]}\n"})
	wantErr(t, s, "compose.files entry \"missing.yaml\"")
	var ce *ComposeFileError
	if !asComposeFileError(s.Err, &ce) || !ce.FromRules || !ce.Problems[0].Missing {
		t.Errorf("err = %#v", s.Err)
	}
}

func TestComposeFilesFromRulesRejectLinks(t *testing.T) {
	root := t.TempDir()
	write(t, root, "outside.yaml", appCompose)
	write(t, root, "demo-stack/compose.yaml", appCompose)
	write(t, root, "demo-stack/qdeploy.yaml", "version: 1\ncompose: {files: [linked.yaml]}\n")
	if err := symlink(filepath.Join(root, "outside.yaml"), filepath.Join(root, "demo-stack", "linked.yaml")); err != nil {
		t.Fatal(err)
	}
	list, _ := Load(root)
	wantErr(t, list[0], "compose.files", "symlinks")
}

func TestUnreadableEnvIsDeployOnly(t *testing.T) {
	s := one(t, map[string]string{
		"compose.yaml": appCompose,
		"qdeploy.yaml": "version: 1\nupdate: {mode: automatic}\n",
		".env":         "DEMO_VERSION=1.0.0\nthis line is broken\nDEMO_PASSWORD=not-a-real-secret\n",
	})
	if s.Err != nil {
		t.Fatalf("a stack with an unparsable .env must stay deployable: %v", s.Err)
	}
	if s.EnvErr == nil || !strings.Contains(s.EnvErr.Error(), ".env: line 2") || strings.Contains(s.EnvErr.Error(), "broken") {
		t.Fatalf("EnvErr = %v", s.EnvErr)
	}
	if s.NameDerived || s.Name != "demo-stack" || len(s.ComposeFiles) != 1 {
		t.Errorf("stack = %+v", s)
	}
	if len(s.Images) != 0 || len(s.Findings) != 0 {
		t.Errorf("images = %+v findings = %+v", s.Images, s.Findings)
	}
	if s.Env["DEMO_PASSWORD"] != "not-a-real-secret" {
		t.Errorf("the lines that parse are kept: %v", s.Env)
	}
	if s.Rules == nil {
		t.Error("rules are still read")
	}
}

func TestUnreadableEnvUsesNameAndFilesOfParsableLines(t *testing.T) {
	s := loadFolder(t, "demo-stack", map[string]string{
		"compose.yaml": appCompose, "other.yaml": appCompose,
		".env": "broken\nCOMPOSE_PROJECT_NAME=from-env\nCOMPOSE_FILE=other.yaml\n",
	})
	if s.Err != nil || s.EnvErr == nil || s.Name != "from-env" || strings.Join(s.ComposeFiles, ",") != "other.yaml" {
		t.Fatalf("stack = %+v", s)
	}
}

func TestUnreadableComposeLineInEnvIsNotDeployable(t *testing.T) {
	for _, line := range []string{"COMPOSE_PROJECT_NAME=\"open", "export COMPOSE_FILE", "compose_project_name"} {
		s := one(t, map[string]string{"compose.yaml": appCompose, ".env": line + "\n"})
		wantErr(t, s, ".env")
		if s.EnvErr != nil {
			t.Errorf("%q: the stack must not be deploy-only", line)
		}
	}
}

func TestSecretLikeKey(t *testing.T) {
	for k, want := range map[string]bool{
		"DEMO_PASSWORD": true, "db_secret": true, "ApiToken": true, "DEMO_KEY": true, "SSH_PRIVATE_KEY": true,
		"DEMO_VERSION": false, "COMPOSE_FILE": false, "TZ": false,
	} {
		if SecretLikeKey(k) != want {
			t.Errorf("SecretLikeKey(%q) = %v", k, !want)
		}
	}
}

func asComposeFileError(err error, target **ComposeFileError) bool { return errors.As(err, target) }

func symlink(oldname, newname string) error { return os.Symlink(oldname, newname) }
