// Package credentials is the service's private store: the SSH deploy keys and named secret values. Only the
// service process writes it; every file stays inside a directory that belongs to the process alone.
package credentials

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-kit/configfile"
)

// The limits of a stored value. The upper bound keeps even a value that JSON escapes to six bytes per byte
// inside one request frame; the lower bound is the length below which the kit's redactor ignores a value.
const (
	MaxValueBytes = 8 << 10
	minValueBytes = 4
)

const (
	dirMode   = 0o700
	fileMode  = 0o600
	keyFile   = "deploy-key"
	valuesDir = "credentials"
	tmpPrefix = ".tmp-"
	// keyComment labels the public key in the git platform's key list.
	keyComment = "qdeploy"
)

// The failures a caller may tell apart. Their texts are fixed and never contain a name or a value.
var (
	ErrInvalidName  = errors.New("invalid name: use 1 to 64 characters, lowercase letters, digits, '.', '_' or '-', starting with a letter")
	ErrInvalidValue = errors.New("invalid value: it must be 4 to 8192 bytes of valid UTF-8 text without control characters")
	ErrKeyExists    = errors.New("a deploy key already exists")
	ErrNoKey        = errors.New("no deploy key exists")
	ErrNotFound     = errors.New("no such credential")
	// ErrUnsafe means the store is not as the service left it: wrong owner, mode, type or link count.
	ErrUnsafe = errors.New("the credential store is not private to the service")
)

var (
	namePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	keyPattern  = regexp.MustCompile(`^deploy-key(?:-[A-Za-z0-9_.@+-]{1,200})?$`)
)

// DefaultKey names the deploy key of a store that serves one stacks repository, and of the first one bound.
const DefaultKey = keyFile

// SourceKey names the deploy key of the source with the given id: a git platform accepts one deploy key in
// one repository only.
func SourceKey(id string) string { return keyFile + "-" + id }

// ValidKeyName reports whether name may name a deploy key; it becomes a file name in the store.
func ValidKeyName(name string) bool { return keyPattern.MatchString(name) }

// ValidName reports whether name may name a credential. The name becomes a file name, so the pattern
// admits no separator and nothing that starts with a dot.
func ValidName(name string) bool { return namePattern.MatchString(name) }

// ValidateValue checks a value against the limits without revealing it in the error.
func ValidateValue(value string) error {
	if len(value) < minValueBytes || len(value) > MaxValueBytes || !utf8.ValidString(value) ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return ErrInvalidValue
	}
	return nil
}

// Entry describes one stored credential without its value.
type Entry struct {
	Name     string
	Modified time.Time
}

// PublicKey is the part of the deploy key that is entered in the git platform.
type PublicKey struct {
	Authorized  string
	Fingerprint string
}

// Store is the credential store in one directory.
type Store struct {
	mu       sync.Mutex
	dir      string
	values   string
	uid      uint32
	onSecret func(...string)
}

// Open prepares dir and returns the store. The directory and its credentials subdirectory are created when
// missing (the parent must exist) and must be real directories owned by this process's user with mode 0700;
// anything else fails closed. onSecret, if set, receives every secret before it is written and every secret
// already stored, so the caller can keep them out of its output.
func Open(dir string, onSecret func(...string)) (*Store, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return nil, errors.New("the state directory must be an absolute path")
	}
	s := &Store{dir: dir, values: filepath.Join(dir, valuesDir), uid: uint32(os.Geteuid()), onSecret: onSecret}
	for _, d := range []string{s.dir, s.values} {
		if err := s.ensureDir(d); err != nil {
			return nil, err
		}
	}
	if err := s.sweepTemp(s.dir); err != nil {
		return nil, err
	}
	if err := s.sweepTemp(s.values); err != nil {
		return nil, err
	}
	secrets, err := s.loadSecrets()
	if err != nil {
		return nil, err
	}
	s.register(secrets...)
	return s, nil
}

func (s *Store) register(secrets ...string) {
	if s.onSecret != nil && len(secrets) > 0 {
		s.onSecret(secrets...)
	}
}

func (s *Store) ensureDir(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err = os.Mkdir(path, dirMode); err == nil {
			// The directory is empty, so widening it to 0700 against an odd umask exposes nothing.
			err = os.Chmod(path, dirMode)
		}
		if err != nil {
			return fmt.Errorf("create %s: %w", path, err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.IsDir() || info.Mode().Perm() != dirMode || s.ownerOf(info) != s.uid {
		return fmt.Errorf("%w: %s must be a directory with mode 0700 owned by the service user", ErrUnsafe, path)
	}
	return nil
}

func (s *Store) ownerOf(info fs.FileInfo) uint32 {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ^uint32(0)
	}
	return st.Uid
}

// checkFile accepts a regular file with mode 0600, this owner and one link only.
func (s *Store) checkFile(info fs.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != fileMode || st.Uid != s.uid || st.Nlink != 1 {
		return ErrUnsafe
	}
	return nil
}

// sweepTemp removes the leftovers of an interrupted write. They are regular files this process created.
func (s *Store) sweepTemp(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tmpPrefix) && !e.IsDir() {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return fmt.Errorf("remove leftover %s: %w", e.Name(), err)
			}
		}
	}
	return nil
}

// read returns the content of a store file, opened without following a link and checked on the descriptor
// it reads from.
func (s *Store) read(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, ErrUnsafe
		}
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if err := s.checkFile(info); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil {
		return nil, err
	}
	return data, nil
}

// write stores data at path. Without replace an existing file is an error; with it the new content takes the
// place of the old in one rename, so a reader sees one or the other and never a partial file.
func (s *Store) write(path string, data []byte, replace bool) error {
	if !replace {
		err := configfile.WriteNewPrivateFile(path, data)
		var exists *configfile.ExistsError
		if errors.As(err, &exists) {
			return fs.ErrExist
		}
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if err := s.checkFile(info); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), tmpPrefix+hex.EncodeToString(rnd[:]))
	if err := configfile.WriteNewPrivateFile(tmp, data); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// GenerateKey creates the default deploy key, see GenerateNamedKey.
func (s *Store) GenerateKey(replace bool) (PublicKey, error) {
	return s.GenerateNamedKey(DefaultKey, replace)
}

// GenerateNamedKey creates the Ed25519 deploy key name. An existing key is replaced only with replace;
// otherwise it is ErrKeyExists. The private key is never returned.
func (s *Store) GenerateNamedKey(name string, replace bool) (PublicKey, error) {
	if !ValidKeyName(name) {
		return PublicKey{}, ErrInvalidName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, name)
	if !replace {
		if _, err := os.Lstat(path); err == nil {
			return PublicKey{}, ErrKeyExists
		}
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return PublicKey{}, err
	}
	block, err := ssh.MarshalPrivateKey(priv, keyComment)
	if err != nil {
		return PublicKey{}, err
	}
	encoded := pem.EncodeToMemory(block)
	s.register(keySecrets(block, encoded)...)
	if err := s.write(path, encoded, replace); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return PublicKey{}, ErrKeyExists
		}
		return PublicKey{}, err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return PublicKey{}, err
	}
	return publicOf(sshPub), nil
}

// KeyPath is where the private default deploy key lives, see NamedKeyPath.
func (s *Store) KeyPath() string { return s.NamedKeyPath(DefaultKey) }

// NamedKeyPath is where the private deploy key name lives, for handing the path to the SSH child of git.
func (s *Store) NamedKeyPath(name string) string { return filepath.Join(s.dir, name) }

// ShowKey returns the public part of the default deploy key.
func (s *Store) ShowKey() (PublicKey, error) { return s.ShowNamedKey(DefaultKey) }

// ShowNamedKey returns the public part of the deploy key name.
func (s *Store) ShowNamedKey(name string) (PublicKey, error) {
	if !ValidKeyName(name) {
		return PublicKey{}, ErrInvalidName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.read(filepath.Join(s.dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return PublicKey{}, ErrNoKey
	}
	if err != nil {
		return PublicKey{}, err
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return PublicKey{}, errors.New("the stored deploy key cannot be read")
	}
	if signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
		return PublicKey{}, errors.New("the stored deploy key is not an Ed25519 key")
	}
	return publicOf(signer.PublicKey()), nil
}

func publicOf(pub ssh.PublicKey) PublicKey {
	return PublicKey{
		Authorized:  strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " " + keyComment,
		Fingerprint: ssh.FingerprintSHA256(pub),
	}
}

// keySecrets lists the forms of a private key that must not reach a log: the file text and the key body as
// one line.
func keySecrets(block *pem.Block, encoded []byte) []string {
	body := base64.StdEncoding.EncodeToString(block.Bytes)
	return []string{strings.TrimSpace(string(encoded)), body}
}

// Set stores a credential, replacing one of the same name.
func (s *Store) Set(name, value string) error {
	if !ValidName(name) {
		return ErrInvalidName
	}
	if err := ValidateValue(value); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.register(value)
	return s.write(filepath.Join(s.values, name), []byte(value), true)
}

// Value returns the value of a credential. The caller must keep it out of errors, logs and results.
func (s *Store) Value(name string) (string, error) {
	if !ValidName(name) {
		return "", ErrInvalidName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.read(filepath.Join(s.values, name))
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// List returns the stored credentials by name, without their values.
func (s *Store) List() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.values)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tmpPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		if !ValidName(e.Name()) || s.checkFile(info) != nil {
			return nil, ErrUnsafe
		}
		out = append(out, Entry{Name: e.Name(), Modified: info.ModTime().UTC()})
	}
	return out, nil
}

// Delete removes a credential.
func (s *Store) Delete(name string) error {
	if !ValidName(name) {
		return ErrInvalidName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.values, name)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := s.checkFile(info); err != nil {
		return err
	}
	return os.Remove(path)
}

// loadSecrets reads every stored secret; a file that fails the checks stops the start.
func (s *Store) loadSecrets() ([]string, error) {
	var secrets []string
	keys, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.dir, err)
	}
	for _, e := range keys {
		if !ValidKeyName(e.Name()) {
			continue
		}
		data, err := s.read(filepath.Join(s.dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read the deploy key %s: %w", e.Name(), err)
		}
		if block, _ := pem.Decode(data); block != nil {
			secrets = append(secrets, keySecrets(block, data)...)
		}
	}
	entries, err := os.ReadDir(s.values)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.values, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tmpPrefix) {
			continue
		}
		if !ValidName(e.Name()) {
			return nil, fmt.Errorf("%w: unexpected entry in the credentials directory", ErrUnsafe)
		}
		value, err := s.read(filepath.Join(s.values, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read credential %s: %w", e.Name(), err)
		}
		secrets = append(secrets, string(value))
	}
	return secrets, nil
}
