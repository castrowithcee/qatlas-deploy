package stacks

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// checkRelative rejects absolute paths, parent references, empty and non-canonical paths. The check is lexical.
func checkRelative(p string) error {
	switch {
	case p == "":
		return errors.New("path is empty")
	case filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`):
		return errors.New("absolute paths are not allowed")
	case strings.ContainsAny(p, "\x00\\"):
		return errors.New("path holds an invalid character")
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return errors.New("'..' is not allowed")
		}
	}
	if filepath.Clean(p) != p || p == "." {
		return errors.New("path must be clean and relative to the stack folder")
	}
	return nil
}

var errNotFound = errors.New("not found")

// checkRegular requires rel to be a regular file inside root with no symlink on the way. Symlinks are rejected
// instead of resolved: resolving would need a containment check that races with the working copy.
func checkRegular(root, rel string) error {
	if err := checkRelative(rel); err != nil {
		return err
	}
	cur := root
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return errNotFound
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlinks are not allowed")
		}
		last := i == len(parts)-1
		if last && !info.Mode().IsRegular() {
			return errors.New("not a regular file")
		}
		if !last && !info.IsDir() {
			return errors.New("not a directory")
		}
	}
	return nil
}

// readBounded reads a regular, non-symlink file of at most MaxFileSize bytes. A missing file returns
// os.ErrNotExist.
func readBounded(root, rel string) ([]byte, error) {
	if _, err := os.Lstat(filepath.Join(root, rel)); errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err := checkRegular(root, rel); err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	f, err := os.Open(filepath.Join(root, rel))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("%s: larger than %d bytes", rel, MaxFileSize)
	}
	return data, nil
}
