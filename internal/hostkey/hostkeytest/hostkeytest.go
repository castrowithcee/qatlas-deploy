// Package hostkeytest runs an in-process SSH server on the loopback interface for tests. It only performs the
// key exchange; every authentication attempt fails.
package hostkeytest

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"strconv"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-deploy/internal/hostkey"
)

// Server is a running test server.
type Server struct {
	Host string
	Port string

	mu     sync.Mutex
	signer ssh.Signer
}

// NewSigner returns a fresh Ed25519 host key.
func NewSigner(t testing.TB) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Start listens on 127.0.0.1 with a fresh host key until the test ends.
func Start(t testing.TB) *Server {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(l.Addr().String())
	s := &Server{Host: host, Port: port, signer: NewSigner(t)}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

// Key is the host key the server presents now.
func (s *Server) Key() hostkey.Key {
	s.mu.Lock()
	defer s.mu.Unlock()
	return hostkey.KeyOf(s.signer.PublicKey())
}

// Rekey makes the server present a new host key from now on.
func (s *Server) Rekey(t testing.TB) {
	t.Helper()
	next := NewSigner(t)
	s.mu.Lock()
	s.signer = next
	s.mu.Unlock()
}

// URL is an ssh:// URL without a user part for a repository path on this server.
func (s *Server) URL() string {
	return "ssh://" + net.JoinHostPort(s.Host, s.Port) + "/stacks.git"
}

// Pattern is the known_hosts host field of the server.
func (s *Server) Pattern() string {
	if _, err := strconv.Atoi(s.Port); err != nil {
		return s.Host
	}
	return hostkey.Pattern(s.Host, s.Port)
}

func (s *Server) serve(conn net.Conn) {
	defer conn.Close()
	s.mu.Lock()
	signer := s.signer
	s.mu.Unlock()
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, ssh.ErrNoAuth },
	}
	cfg.AddHostKey(signer)
	if sc, _, _, err := ssh.NewServerConn(conn, cfg); err == nil {
		_ = sc.Close()
	}
}
