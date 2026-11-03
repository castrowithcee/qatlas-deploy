// Package hostkey learns the SSH host key of a git server without authenticating to it and keeps the keys the
// operator has confirmed in a known_hosts file of the service. Nothing here trusts a key on its own: a key is
// pinned only by an explicit call, and a pinned host never takes a different key.
package hostkey

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-kit/configfile"
)

// ProbeTimeout bounds connecting and the key exchange of one probe.
const ProbeTimeout = 10 * time.Second

const maxKnownHostsBytes = 1 << 20

// Fixed failures; their texts never contain what the server sent.
var (
	ErrUnreachable = errors.New("cannot reach the SSH server")
	ErrNoHostKey   = errors.New("the SSH server did not present a host key")
	// ErrConflict means the host is already pinned with another key.
	ErrConflict = errors.New("a different host key is already pinned for this host")
)

// Key is a host key in the forms a person compares and a known_hosts file stores.
type Key struct {
	// Type is the SSH key type, for example ssh-ed25519.
	Type string
	// Authorized is "<type> <base64>" as in an authorized_keys or known_hosts line.
	Authorized string
	// Fingerprint is SHA256:... as OpenSSH prints it.
	Fingerprint string
}

// KeyOf describes a public key.
func KeyOf(pub ssh.PublicKey) Key {
	return Key{
		Type:        pub.Type(),
		Authorized:  strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))),
		Fingerprint: ssh.FingerprintSHA256(pub),
	}
}

var errCaptured = errors.New("host key captured")

// Probe connects to host:port, reads the host key during the key exchange and ends the connection before any
// authentication: no user, credential or command is sent. Certificates are not offered, so the result is
// always the server's plain key.
func Probe(ctx context.Context, host, port string) (Key, error) {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	addr := net.JoinHostPort(host, port)
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return Key{}, ErrUnreachable
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	var got ssh.PublicKey
	cfg := &ssh.ClientConfig{
		User: "qdeploy",
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			got = k
			return errCaptured
		},
		HostKeyAlgorithms: []string{
			ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521,
			ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256,
		},
		Timeout: ProbeTimeout,
	}
	_, _, _, err = ssh.NewClientConn(conn, addr, cfg)
	if got == nil {
		if ctx.Err() != nil {
			return Key{}, ErrUnreachable
		}
		return Key{}, ErrNoHostKey
	}
	return KeyOf(got), nil
}

// Pattern is the host field of a known_hosts line: the bare host on port 22, otherwise [host]:port.
func Pattern(host, port string) string {
	host = strings.ToLower(host)
	if port == "" || port == "22" {
		return host
	}
	return "[" + host + "]:" + port
}

// State is what a known_hosts file says about a host and a key.
type State int

const (
	// Unknown: the host has no entry.
	Unknown State = iota
	// Pinned: the host has an entry with exactly this key.
	Pinned
	// Changed: the host has entries, none with this key.
	Changed
)

// Check reads the file at path; a missing file means Unknown.
func Check(path, host, port string, k Key) (State, error) {
	data, err := readKnownHosts(path)
	if err != nil {
		return Unknown, err
	}
	return stateIn(string(data), Pattern(host, port), k), nil
}

func stateIn(text, pattern string, k Key) State {
	state := Unknown
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || strings.HasPrefix(fields[0], "#") || strings.HasPrefix(fields[0], "@") {
			continue
		}
		matches := false
		for _, h := range strings.Split(fields[0], ",") {
			if h == pattern {
				matches = true
			}
		}
		if !matches {
			continue
		}
		if fields[1]+" "+fields[2] == k.Authorized {
			return Pinned
		}
		state = Changed
	}
	return state
}

// Pin adds the key for the host to the file, which is created private when missing. A host that already has
// another key is ErrConflict and the file stays as it was; the same key again changes nothing. The caller
// serialises calls.
func Pin(path, host, port string, k Key) error {
	data, err := readKnownHosts(path)
	if err != nil {
		return err
	}
	pattern := Pattern(host, port)
	switch stateIn(string(data), pattern, k) {
	case Pinned:
		return nil
	case Changed:
		return ErrConflict
	}
	text := string(data)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += pattern + " " + k.Authorized + "\n"

	tmp := filepath.Join(filepath.Dir(path), fmt.Sprintf(".tmp-known-hosts-%d", time.Now().UnixNano()))
	if err := configfile.WriteNewPrivateFile(tmp, []byte(text)); err != nil {
		return fmt.Errorf("write known_hosts: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write known_hosts: %w", err)
	}
	return nil
}

func readKnownHosts(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read known_hosts: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("known_hosts is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read known_hosts: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxKnownHostsBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read known_hosts: %w", err)
	}
	if len(data) > maxKnownHostsBytes {
		return nil, errors.New("known_hosts is too large")
	}
	return data, nil
}
