package backup

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const (
	dirMode  = 0o700
	fileMode = 0o600

	metaFile   = "meta.json"
	metaSchema = 1
	maxMeta    = 1 << 20
	idTime     = "20060102T150405Z"
)

var (
	idShape    = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)
	stackShape = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	fileShape  = regexp.MustCompile(`^(?:(?:path|volume)-[0-9]{1,3}\.tar|command\.out)$`)
	commitForm = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

// Kinds of backup content.
const (
	KindPath    = "path"
	KindVolume  = "volume"
	KindCommand = "command"
)

// commandFile holds the standard output of backup.command.
const commandFile = "command.out"

// Item is one part of a backup.
type Item struct {
	Kind string `json:"kind"`
	// Name is the path relative to the stack folder, or the volume key of the Compose file; empty for the
	// command.
	Name string `json:"name,omitempty"`
	// Volume is the Docker volume name Name resolved to.
	Volume string `json:"volume,omitempty"`
	File   string `json:"file"`
	Size   int64  `json:"size"`
}

// ImageVersion is an image version variable of the stack at backup time.
type ImageVersion struct {
	Var     string `json:"var"`
	Image   string `json:"image"`
	Version string `json:"version"`
}

// Meta describes one backup. Incomplete is set for a directory without valid metadata; only ID and Stack are
// then known.
type Meta struct {
	Schema     int            `json:"schema"`
	ID         string         `json:"id"`
	Stack      string         `json:"stack"`
	Created    time.Time      `json:"created"`
	Commit     string         `json:"commit,omitempty"`
	Images     []ImageVersion `json:"images,omitempty"`
	Items      []Item         `json:"items"`
	Incomplete bool           `json:"-"`
}

// Size is the sum of the item sizes.
func (m Meta) Size() int64 {
	var n int64
	for _, it := range m.Items {
		n += it.Size
	}
	return n
}

// ValidStack reports whether name can become a path component of the store.
func ValidStack(name string) bool { return stackShape.MatchString(name) }

// ValidID reports whether id has the shape of a backup id this package generates.
func ValidID(id string) bool { return idShape.MatchString(id) }

func newID(now time.Time) (string, error) {
	var r [4]byte
	if _, err := rand.Read(r[:]); err != nil {
		return "", err
	}
	return now.UTC().Format(idTime) + "-" + hex.EncodeToString(r[:]), nil
}

// stackDir is the directory that holds the backups of one stack.
func (m *Manager) stackDir(stack string) (string, error) {
	if !ValidStack(stack) {
		return "", fmt.Errorf("stack name %q cannot be used for backups", stack)
	}
	if m.Root == "" || !filepath.IsAbs(m.Root) || filepath.Clean(m.Root) != m.Root {
		return "", errors.New("the backup store is not configured")
	}
	return filepath.Join(m.Root, stack), nil
}

// backupDir is the directory of one existing backup: a real directory directly below the stack directory.
func (m *Manager) backupDir(stack, id string) (string, error) {
	sd, err := m.stackDir(stack)
	if err != nil {
		return "", err
	}
	if !ValidID(id) {
		return "", ErrNoSuchBackup
	}
	dir := filepath.Join(sd, id)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return "", ErrNoSuchBackup
	}
	return dir, nil
}

// create makes the directory of a new backup. The store directories are private to the service user.
func (m *Manager) create(stack string, now time.Time) (id, dir string, err error) {
	sd, err := m.stackDir(stack)
	if err != nil {
		return "", "", err
	}
	for _, d := range []string{m.Root, sd} {
		if err := os.Mkdir(d, dirMode); err != nil && !errors.Is(err, os.ErrExist) {
			return "", "", err
		}
		info, err := os.Lstat(d)
		if err != nil {
			return "", "", err
		}
		if !info.IsDir() {
			return "", "", fmt.Errorf("%s is not a directory", d)
		}
		if err := os.Chmod(d, dirMode); err != nil {
			return "", "", err
		}
	}
	for range 8 {
		if id, err = newID(now); err != nil {
			return "", "", err
		}
		dir = filepath.Join(sd, id)
		if err := os.Mkdir(dir, dirMode); err == nil {
			return id, dir, os.Chmod(dir, dirMode)
		} else if !errors.Is(err, os.ErrExist) {
			return "", "", err
		}
	}
	return "", "", errors.New("no free backup id")
}

// remove deletes one backup directory after the checks of backupDir. It never follows a link out of it.
func (m *Manager) remove(stack, id string) error {
	dir, err := m.backupDir(stack, id)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func writeMeta(dir string, meta Meta) error {
	meta.Schema = metaSchema
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, metaFile+".tmp")
	if err := os.WriteFile(tmp, append(data, '\n'), fileMode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, fileMode); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, metaFile))
}

// readMeta reads the metadata of a backup directory; any problem makes the backup incomplete.
func readMeta(dir, stack, id string) (Meta, error) {
	path := filepath.Join(dir, metaFile)
	info, err := os.Lstat(path)
	if err != nil {
		return Meta{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxMeta {
		return Meta{}, errors.New("metadata is not a regular file of a sane size")
	}
	f, err := os.Open(path)
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxMeta))
	if err != nil {
		return Meta{}, err
	}
	var meta Meta
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&meta); err != nil {
		return Meta{}, err
	}
	if meta.Schema != metaSchema || meta.ID != id || meta.Stack != stack {
		return Meta{}, errors.New("metadata does not belong to this backup")
	}
	for _, it := range meta.Items {
		if !fileShape.MatchString(it.File) {
			return Meta{}, errors.New("metadata names an unexpected file")
		}
	}
	return meta, nil
}

// List returns the backups of a stack, newest first. A directory without valid metadata is listed as
// incomplete; a stack without backups has none.
func (m *Manager) List(stack string) ([]Meta, error) {
	sd, err := m.stackDir(stack)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(sd)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Meta
	for _, e := range entries {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		meta, err := readMeta(filepath.Join(sd, e.Name()), stack, e.Name())
		if err != nil {
			meta = Meta{ID: e.Name(), Stack: stack, Incomplete: true}
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// retain removes the backups beyond the limits of the rules and the incomplete ones, except current. A backup
// is kept only while it satisfies every limit that is set; the newest complete backup is never removed.
func (m *Manager) retain(stack string, keep, maxAgeDays int, current string) (removed []string, err error) {
	list, err := m.List(stack)
	if err != nil {
		return nil, err
	}
	now := m.now()
	var errs []error
	rank := 0
	for _, meta := range list {
		drop := false
		switch {
		case meta.ID == current:
			rank++
		case meta.Incomplete:
			drop = true
		default:
			drop = rank > 0 && ((keep > 0 && rank >= keep) ||
				(maxAgeDays > 0 && now.Sub(meta.Created) > time.Duration(maxAgeDays)*24*time.Hour))
			rank++
		}
		if !drop {
			continue
		}
		if rerr := m.remove(stack, meta.ID); rerr != nil {
			errs = append(errs, rerr)
			continue
		}
		removed = append(removed, meta.ID)
	}
	return removed, errors.Join(errs...)
}
