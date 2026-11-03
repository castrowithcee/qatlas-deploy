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

// ResolveDir returns the absolute path of the directory rel below root. root must be absolute and clean. rel
// must pass the same lexical checks as every path of the working copy and no component on the way may be a
// symlink or a non-directory. exists is false when the path or a tail of it is missing; the part that exists
// has been checked all the same. The result is a snapshot: the caller must not assume it stays valid.
func ResolveDir(root, rel string) (abs string, exists bool, err error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", false, errors.New("root must be an absolute, clean path")
	}
	if err := checkRelative(rel); err != nil {
		return "", false, err
	}
	cur := root
	missing := false
	for _, part := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, part)
		if missing {
			continue
		}
		info, err := os.Lstat(cur)
		switch {
		case errors.Is(err, os.ErrNotExist):
			missing = true
		case err != nil:
			return "", false, err
		case info.Mode()&os.ModeSymlink != 0:
			return "", false, errors.New("symlinks are not allowed")
		case !info.IsDir():
			return "", false, errors.New("not a directory")
		}
	}
	return cur, !missing, nil
}
