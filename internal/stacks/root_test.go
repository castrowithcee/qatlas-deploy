package stacks

import (
	"path/filepath"
	"slices"
	"testing"
)

// rootDir is a stack folder whose directory name gives the project name demo-root.
func rootDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "Demo-Root")
}

func TestRootComposeFileMakesOneStack(t *testing.T) {
	root := rootDir(t)
	write(t, root, "compose.yaml", appCompose)
	write(t, root, ".env", "DEMO_VERSION=1.2.3\n")
	write(t, root, "a-stack/compose.yaml", appCompose)
	write(t, root, "b-stack/.env", "COMPOSE_FILE=app.yaml\n")
	write(t, root, "b-stack/app.yaml", appCompose)
	write(t, root, "no-compose/readme.txt", "x")
	write(t, root, ".github/compose.yaml", appCompose)
	list, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("stacks = %+v", list)
	}
	s := list[0]
	if s.Err != nil || s.Name != "demo-root" || !s.NameDerived || s.Folder != RootFolder || s.Dir != root {
		t.Fatalf("stack = %+v", s)
	}
	if !slices.Equal(s.ComposeFiles, []string{"compose.yaml"}) || len(s.Images) != 1 || s.Images[0].Value != "1.2.3" {
		t.Errorf("stack = %+v", s)
	}
	if !slices.Equal(s.Ignored, []string{"a-stack", "b-stack"}) {
		t.Errorf("ignored = %v", s.Ignored)
	}
	if s.RepoPath(".env") != ".env" || s.RepoPath("data") != "data" {
		t.Errorf("repo paths = %s, %s", s.RepoPath(".env"), s.RepoPath("data"))
	}
}

func TestRootStackNameAndSettings(t *testing.T) {
	root := rootDir(t)
	write(t, root, "compose.yaml", "name: Demo Project\n"+appCompose)
	write(t, root, ".env", "DEMO_VERSION=1.2.3\n")
	write(t, root, SettingsPath("demoproject"), "version: 1\nupdate:\n  mode: manual\n")
	s := load(t, root)["demoproject"]
	if s.Err != nil || s.Folder != RootFolder || s.UpdateMode() != ModeManual || len(s.Ignored) != 0 {
		t.Fatalf("stack = %+v", s)
	}
}

func TestRootStackFromComposeFileOrSettings(t *testing.T) {
	t.Run("COMPOSE_FILE", func(t *testing.T) {
		root := rootDir(t)
		write(t, root, ".env", "COMPOSE_FILE=deploy/app.yaml\nDEMO_VERSION=1.0.0\n")
		write(t, root, "deploy/app.yaml", appCompose)
		s := load(t, root)["demo-root"]
		if s.Err != nil || s.Folder != RootFolder || !slices.Equal(s.ComposeFiles, []string{"deploy/app.yaml"}) {
			t.Fatalf("stack = %+v", s)
		}
	})
	t.Run("broken COMPOSE_FILE", func(t *testing.T) {
		root := rootDir(t)
		write(t, root, ".env", "COMPOSE_FILE=absent.yaml\n")
		write(t, root, "a-stack/compose.yaml", appCompose)
		list, err := Load(root)
		if err != nil || len(list) != 1 || list[0].Folder != RootFolder || list[0].Err == nil {
			t.Fatalf("stacks = %+v, %v", list, err)
		}
	})
	t.Run("settings", func(t *testing.T) {
		root := rootDir(t)
		write(t, root, "app.yaml", appCompose)
		write(t, root, SettingsPath("demo-root"), "version: 1\ncompose:\n  files: [app.yaml]\n")
		s := load(t, root)["demo-root"]
		if s.Err != nil || s.Folder != RootFolder || !slices.Equal(s.ComposeFiles, []string{"app.yaml"}) {
			t.Fatalf("stack = %+v", s)
		}
	})
}

// Settings or a broken .env in the root that hold no compose file of the root keep the subfolder layout.
func TestRootWithoutComposeFileKeepsTheSubfolders(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"settings of a subfolder stack": {SettingsPath("demo-root"): "version: 1\ncompose:\n  files: [app.yaml]\n", "demo-root/app.yaml": appCompose},
		"unreadable .env":               {".env": "COMPOSE_FILE app.yaml\n", "demo-root/compose.yaml": appCompose},
		"invalid project name":          {".env": "COMPOSE_PROJECT_NAME=Not Valid\n", "demo-root/compose.yaml": appCompose},
	} {
		t.Run(name, func(t *testing.T) {
			root := rootDir(t)
			for rel, body := range files {
				write(t, root, rel, body)
			}
			write(t, root, "other/compose.yaml", appCompose)
			list, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(list) != 2 || list[0].Folder != "demo-root" || list[1].Folder != "other" || list[0].Err != nil {
				t.Fatalf("stacks = %+v", list)
			}
			if list[0].RepoPath(".env") != "demo-root/.env" || list[0].Ignored != nil {
				t.Errorf("stack = %+v", list[0])
			}
		})
	}
}

func TestWithSettingsReadsTheRootStack(t *testing.T) {
	root := rootDir(t)
	write(t, root, "compose.yaml", appCompose)
	write(t, root, "app.yaml", appCompose)
	s, ok := WithSettings(root, RootFolder, &Rules{ComposeFiles: []string{"app.yaml"}, Update: Update{Mode: ModeManual}})
	if !ok || s.Err != nil || s.Name != "demo-root" || !slices.Equal(s.ComposeFiles, []string{"app.yaml"}) {
		t.Fatalf("stack = %+v, %v", s, ok)
	}
}
