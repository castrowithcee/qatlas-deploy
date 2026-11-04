// Package datadir creates the missing data directories of a stack with a given owner and mode. The list of
// directories comes from the service and is treated as untrusted: every path is validated again, the walk
// never follows a symlink, and nothing outside the stack folder is touched.
package datadir

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/castrowithcee/qatlas-deploy/internal/engine"
)

// DefaultMode is the mode of a created directory when the caller names none.
const DefaultMode fs.FileMode = 0o750

// MaxPaths bounds one request.
const MaxPaths = 64

// Owner is the numeric owner of a created directory.
type Owner struct{ UID, GID uint32 }

func (o Owner) String() string {
	return strconv.FormatUint(uint64(o.UID), 10) + ":" + strconv.FormatUint(uint64(o.GID), 10)
}

var ownerRe = regexp.MustCompile(`^[0-9]{1,10}:[0-9]{1,10}$`)

// ParseOwner parses "UID:GID". Both are decimal numbers; the all-ones value, which chown reads as "no change",
// is refused.
func ParseOwner(s string) (Owner, error) {
	if !ownerRe.MatchString(s) {
		return Owner{}, fmt.Errorf("owner %q must be UID:GID with decimal numbers", s)
	}
	uid, gid, _ := strings.Cut(s, ":")
	u, errU := strconv.ParseUint(uid, 10, 32)
	g, errG := strconv.ParseUint(gid, 10, 32)
	if errU != nil || errG != nil || u == 1<<32-1 || g == 1<<32-1 {
		return Owner{}, fmt.Errorf("owner %q is out of range", s)
	}
	return Owner{UID: uint32(u), GID: uint32(g)}, nil
}

// ParseMode parses an octal permission mode such as 0750. Only the permission bits are accepted: setuid,
// setgid, sticky and anything above 0777 are refused.
func ParseMode(s string) (fs.FileMode, error) {
	if s == "" || len(s) > 5 {
		return 0, fmt.Errorf("mode %q must be an octal number such as 0750", s)
	}
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("mode %q must be an octal number such as 0750", s)
	}
	if v > 0o777 {
		return 0, fmt.Errorf("mode %q is not a plain permission mode: setuid, setgid and sticky bits are refused", s)
	}
	return fs.FileMode(v), nil
}

// Request is one creation. StackDir is the absolute, clean path of the stack folder; Paths are the missing
// directories, relative to it and slash-separated.
type Request struct {
	StackDir string
	Paths    []string
	Owner    Owner
	// Mode is the mode of every created directory; zero means DefaultMode.
	Mode fs.FileMode
	// Versioned, when set, reports whether git tracks rel (a path below the stack folder) or something below it.
	// A versioned path is refused. An error from it stops the creation.
	Versioned func(rel string) (bool, error)
}

// Result says what a creation did. Created are the requested directories that were made, Parents the missing
// intermediate directories made on the way (same owner and mode), Existing the requested paths that already
// existed and were left untouched.
type Result struct {
	Created  []string
	Parents  []string
	Existing []string
}

// chownFd and chmodFd are replaced in tests.
var (
	chownFd = unix.Fchown
	chmodFd = unix.Fchmod
)

const openDir = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC

// Create makes the missing directories of req. It validates everything before it changes anything. On an
// error it returns what was done up to then.
func Create(req Request) (Result, error) {
	var res Result
	mode := req.Mode
	if mode == 0 {
		mode = DefaultMode
	}
	if mode&^0o777 != 0 {
		return res, errors.New("mode must contain permission bits only")
	}
	if !strings.HasPrefix(req.StackDir, "/") || req.StackDir == "/" || strings.ContainsRune(req.StackDir, 0) {
		return res, errors.New("stack folder must be an absolute path below /")
	}
	if len(req.Paths) == 0 || len(req.Paths) > MaxPaths {
		return res, errors.New("no directories, or too many, to create")
	}
	paths := slices.Clone(req.Paths)
	for _, p := range paths {
		if err := validate(p); err != nil {
			return res, err
		}
		if req.Versioned != nil {
			v, err := req.Versioned(p)
			if err != nil {
				return res, fmt.Errorf("check whether %s is versioned: %w", p, err)
			}
			if v {
				return res, fmt.Errorf("%s is versioned and is not created", p)
			}
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	// Parents first, so a nested pair reports the parent as requested and not as an intermediate.
	slices.SortStableFunc(paths, func(a, b string) int { return strings.Count(a, "/") - strings.Count(b, "/") })

	root, err := unix.Open(req.StackDir, openDir, 0)
	if err != nil {
		return res, fmt.Errorf("open the stack folder: %w", err)
	}
	defer unix.Close(root)

	requested := map[string]bool{}
	for _, p := range paths {
		requested[p] = true
	}
	for _, p := range paths {
		if err := createOne(root, p, mode, req, requested, &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func validate(p string) error {
	if !engine.ValidDirPath(p) {
		return fmt.Errorf("%q is not a valid data directory path", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || seg == ".git" {
			return fmt.Errorf("%q is not a valid data directory path", p)
		}
	}
	return nil
}

func createOne(root int, rel string, mode fs.FileMode, req Request, requested map[string]bool, res *Result) error {
	cur, err := unix.Dup(root)
	if err != nil {
		return err
	}
	segs := strings.Split(rel, "/")
	made := false
	for i, seg := range segs {
		prefix := strings.Join(segs[:i+1], "/")
		next, err := unix.Openat(cur, seg, openDir, 0)
		if err == nil {
			unix.Close(cur)
			cur = next
			continue
		}
		if !errors.Is(err, unix.ENOENT) {
			unix.Close(cur)
			return fmt.Errorf("%s: exists and is not a plain directory, or cannot be opened: %w", prefix, err)
		}
		if i < len(segs)-1 && req.Versioned != nil {
			if v, verr := req.Versioned(prefix); verr != nil || v {
				unix.Close(cur)
				return fmt.Errorf("%s is versioned or cannot be checked and is not created", prefix)
			}
		}
		// 0700 until the owner is set, so the directory is never wider than asked for.
		if err := unix.Mkdirat(cur, seg, 0o700); err != nil {
			unix.Close(cur)
			return fmt.Errorf("create %s: %w", prefix, err)
		}
		next, err = unix.Openat(cur, seg, openDir, 0)
		unix.Close(cur)
		if err != nil {
			return fmt.Errorf("open %s after creating it: %w", prefix, err)
		}
		cur = next
		if err := chownFd(cur, int(req.Owner.UID), int(req.Owner.GID)); err != nil {
			unix.Close(cur)
			return fmt.Errorf("set the owner of %s: %w", prefix, err)
		}
		if err := chmodFd(cur, uint32(mode)); err != nil {
			unix.Close(cur)
			return fmt.Errorf("set the mode of %s: %w", prefix, err)
		}
		if requested[prefix] {
			res.Created = append(res.Created, prefix)
		} else {
			res.Parents = append(res.Parents, prefix)
		}
		made = true
	}
	unix.Close(cur)
	if !made {
		res.Existing = append(res.Existing, rel)
	}
	return nil
}
