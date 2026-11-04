// Package release holds the identity qdeploy trusts for its releases: the signature namespace and the public
// keys that may sign a release. Verification itself lives in qatlas-kit.
package release

import (
	"strings"

	"golang.org/x/crypto/ssh"
)

// Namespace is the SSHSIG namespace every release signature is made for.
const Namespace = "qdeploy-release"

// trustedKeyLines lists the public keys that may sign a release, in authorized_keys format. A successor key
// is added one release before a rotation so that installed programs already trust it. The list is fixed at
// build time: nothing at run time extends or replaces it. It must match .github/release-allowed-signers.
var trustedKeyLines = []string{
	"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAC+0sI3eFp3ZAC/fGhUEmEb6RzeXjmNbvDnK3MXqxa5",
}

// extraKeyLines is set at build time only (-ldflags -X) to trust further keys, one authorized_keys line per
// line, so that end-to-end tests can run a real binary against test releases.
var extraKeyLines string

// TrustedKeys returns the compiled-in public keys that may sign a release.
func TrustedKeys() []ssh.PublicKey {
	lines := append([]string(nil), trustedKeyLines...)
	for _, line := range strings.Split(extraKeyLines, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	keys := make([]ssh.PublicKey, 0, len(lines))
	for _, line := range lines {
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			panic("release: invalid compiled-in public key: " + err.Error())
		}
		keys = append(keys, key)
	}
	return keys
}
