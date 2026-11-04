package packaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/installer"
	"github.com/castrowithcee/qatlas-kit/release/releasebuild"
	"github.com/castrowithcee/qatlas-kit/sysservice"
	"golang.org/x/crypto/ssh"
)

const (
	program   = "qdeploy"
	namespace = "qdeploy-release"
	repoRoot  = ".."
)

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func loadConfig(t *testing.T) releasebuild.Config {
	t.Helper()
	c, err := releasebuild.LoadConfig(filepath.Join(repoRoot, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Service == nil {
		t.Fatal("release.json has no service section")
	}
	return c
}

func TestUnitPassesKitValidation(t *testing.T) {
	c := loadConfig(t)
	unit := readFile(t, c.Service.Unit)
	rules := sysservice.UnitRules{Program: program, Groups: c.Service.Groups, WritableConfig: c.Service.WritableConfig}
	if err := sysservice.ValidateUnit(unit, rules); err != nil {
		t.Fatal(err)
	}
}

// The service refuses to start unless /run/qdeploy belongs to qdeploy:qdeploy without access for others and
// /var/lib/qdeploy has mode 0700, so the unit must ask systemd for exactly that.
func TestUnitMeetsRuntimeDirectoryContract(t *testing.T) {
	unit := string(readFile(t, loadConfig(t).Service.Unit))
	lines := map[string]bool{}
	for _, l := range strings.Split(unit, "\n") {
		lines[strings.TrimSpace(l)] = true
	}
	for _, want := range []string{
		"ExecStart=/usr/local/bin/qdeploy run",
		"User=qdeploy",
		"Group=qdeploy",
		"SupplementaryGroups=docker",
		"RuntimeDirectory=qdeploy",
		"RuntimeDirectoryMode=0750",
		"StateDirectory=qdeploy",
		"StateDirectoryMode=0700",
		"NoNewPrivileges=yes",
		"ProtectSystem=strict",
		"ReadWritePaths=-/opt/stacks",
	} {
		if !lines[want] {
			t.Errorf("unit lacks the line %q", want)
		}
	}
}

func TestReleaseConfigSelectsTheSystemVariant(t *testing.T) {
	c := loadConfig(t)
	if got, want := strings.Join(c.Targets, " "), "linux/amd64 linux/arm64"; got != want {
		t.Errorf("targets = %q, want %q", got, want)
	}
	if c.VersionVariable != "github.com/castrowithcee/qatlas-deploy/internal/cli.version" {
		t.Errorf("version variable = %q", c.VersionVariable)
	}
	if strings.Join(c.Service.Groups, " ") != "docker" {
		t.Errorf("service groups = %v, want [docker]", c.Service.Groups)
	}
	if c.Service.AddInstallingUser || c.Service.WritableConfig {
		t.Error("the service channel admits root only, so no installing user is added and the configuration stays read-only")
	}
}

func TestAllowedSignersListsTheReleaseKey(t *testing.T) {
	keys, err := releasebuild.ParseAllowedSigners(readFile(t, ".github/release-allowed-signers"), namespace)
	if err != nil {
		t.Fatal(err)
	}
	const fingerprint = "SHA256:mF2Jh6YNweMA/fy5TM/VjNiLRD512PReMHVoWP52iek"
	if len(keys) != 1 || ssh.FingerprintSHA256(keys[0]) != fingerprint {
		t.Fatalf("allowed signers hold %d keys, want exactly the key %s", len(keys), fingerprint)
	}
}

func TestInstallerRendersTheSystemVariant(t *testing.T) {
	c := loadConfig(t)
	keys, err := releasebuild.ParseAllowedSigners(readFile(t, ".github/release-allowed-signers"), namespace)
	if err != nil {
		t.Fatal(err)
	}
	script, err := installer.Shell(installer.Params{
		Program:     program,
		DisplayName: c.Installer.DisplayName,
		ReleasesURL: "https://github.com/castrowithcee/qatlas-deploy/releases",
		Namespace:   namespace,
		TrustedKeys: keys,
		Targets:     c.Targets,
		NextStep:    c.Installer.NextStep,
		Service: &installer.Service{
			Groups:            c.Service.Groups,
			AddInstallingUser: c.Service.AddInstallingUser,
			WritableConfig:    c.Service.WritableConfig,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	for _, want := range []string{
		"unit_entry='" + sysservice.UnitEntry(program) + "'",
		"groups='docker'",
		"namespace='" + namespace + "'",
		"--uninstall",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("install.sh lacks %q", want)
		}
	}
}
