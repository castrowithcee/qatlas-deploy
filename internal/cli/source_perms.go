package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// gitSearchPath is where the root client looks for git; nothing of the caller's PATH is used.
const gitSearchPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

const gitCallLimit = 2 * time.Minute

// permPlan is what source add changes in a folder so that the service group can write the versioned files:
// the group of every listed path and its group bits. The owner is never part of it.
type permPlan struct {
	dir string
	gid int
	// dirs get group rwx and setgid, files group rw.
	dirs  []string
	files []string
	// mounts are bind-mount sources, as paths below the folder, that stay as they are with everything below.
	mounts []string
	// links counts versioned entries and entries of .git that are symbolic links and stay as they are.
	links int
	// hardlinks counts files with further hard links, which may lie outside the folder; they stay as they are.
	hardlinks int
	// repo says the folder holds a repository: core.sharedRepository is set before the changes.
	repo bool
}

func (p *permPlan) changed() int { return len(p.dirs) + len(p.files) }

// summary is the preview the operator confirms.
func (p *permPlan) summary() []string {
	lines := []string{fmt.Sprintf("%d paths get the group of the service account (gid %d) with group write; the owner stays",
		p.changed(), p.gid)}
	if !p.repo {
		return append(lines, "  the folder: group rwx and setgid")
	}
	lines = append(lines,
		"  the folder, the folders of versioned files and .git: group rwx and setgid; versioned files and files in .git: group rw")
	if len(p.mounts) > 0 {
		lines = append(lines, "  left alone, bind-mount sources and everything below: "+strings.Join(p.mounts, ", "))
	}
	if p.links > 0 {
		lines = append(lines, fmt.Sprintf("  left alone, symbolic links: %d", p.links))
	}
	if p.hardlinks > 0 {
		lines = append(lines, fmt.Sprintf("  left alone, files with further hard links: %d", p.hardlinks))
	}
	return append(lines, "  unversioned files stay as they are", "  git config core.sharedRepository group is set")
}

// planFolder gives the group write access to the folder alone, for an existing empty folder a clone goes into.
func planFolder(dir string, gid int) (*permPlan, error) {
	if err := requireRealDir(dir); err != nil {
		return nil, err
	}
	return &permPlan{dir: dir, gid: gid, dirs: []string{dir}}, nil
}

// singleLink reports whether a regular file has no further hard link: changing a file with one would change
// it wherever else it is linked, possibly outside the folder.
func singleLink(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Nlink == 1
}

// planPermissions reads the versioned files of the repository in dir as root and computes the changes. It
// changes nothing. mounts are the bind-mount sources below dir that stay out.
func planPermissions(ctx context.Context, dir string, gid int, mounts []string) (*permPlan, error) {
	if err := requireRealDir(dir); err != nil {
		return nil, err
	}
	if err := requireRealDir(filepath.Join(dir, ".git")); err != nil {
		return nil, err
	}
	out, err := runGit(ctx, dir, "ls-files", "-z")
	if err != nil {
		return nil, folderFailure("cannot list the versioned files of "+dir, err)
	}
	p := &permPlan{dir: dir, gid: gid, mounts: cleanMounts(dir, mounts), repo: true}
	dirs := map[string]bool{dir: true}
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" || !filepath.IsLocal(rel) || underAny(rel, p.mounts) {
			continue
		}
		parents, ok := p.versionedFile(rel)
		if !ok {
			continue
		}
		p.files = append(p.files, filepath.Join(dir, rel))
		for _, d := range parents {
			dirs[d] = true
		}
	}
	err = filepath.WalkDir(filepath.Join(dir, ".git"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch t := d.Type(); {
		case t&fs.ModeSymlink != 0:
			p.links++
		case t.IsDir():
			dirs[path] = true
		case t.IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !singleLink(info) {
				p.hardlinks++
				return nil
			}
			p.files = append(p.files, path)
		}
		return nil
	})
	if err != nil {
		return nil, folderFailure("cannot read "+filepath.Join(dir, ".git"), err)
	}
	for d := range dirs {
		p.dirs = append(p.dirs, d)
	}
	sort.Strings(p.dirs)
	sort.Strings(p.files)
	return p, nil
}

// versionedFile reports whether rel is a regular file that no symbolic link leads to, and returns the folders
// between it and the stack folder. A link on the way or as the entry itself is counted and skipped; a missing
// path, a folder (a submodule) or anything that is not a regular file is skipped.
func (p *permPlan) versionedFile(rel string) (parents []string, ok bool) {
	cur := p.dir
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		switch {
		case err != nil:
			return nil, false
		case info.Mode()&fs.ModeSymlink != 0:
			p.links++
			return nil, false
		case i < len(parts)-1:
			if !info.IsDir() {
				return nil, false
			}
			parents = append(parents, cur)
		case !info.Mode().IsRegular():
			return nil, false
		case !singleLink(info):
			p.hardlinks++
			return nil, false
		}
	}
	return parents, true
}

// cleanMounts turns the bind-mount sources into clean paths below dir and drops what lies outside or is dir itself.
func cleanMounts(dir string, mounts []string) []string {
	var out []string
	for _, m := range mounts {
		if filepath.IsAbs(m) {
			rel, err := filepath.Rel(dir, m)
			if err != nil {
				continue
			}
			m = rel
		}
		m = filepath.Clean(m)
		if m == "." || !filepath.IsLocal(m) {
			continue
		}
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func underAny(rel string, mounts []string) bool {
	for _, m := range mounts {
		if rel == m || strings.HasPrefix(rel, m+"/") {
			return true
		}
	}
	return false
}

func requireRealDir(path string) error {
	info, err := os.Lstat(path)
	switch {
	case err != nil:
		return folderFailure("cannot inspect "+path, err)
	case !info.IsDir():
		return usageFailure(path+" is not a directory (a symbolic link or a file is not followed)",
			"Pass a folder with its own .git directory.")
	}
	return nil
}

// applyPermissions sets core.sharedRepository first, so that the rewritten .git/config gets the group as well,
// then changes the group and the mode of every planned path. Only the group changes: the owner argument is -1.
// A path that became a symbolic link, or lies behind one, since the plan is skipped.
func (h folderHost) applyPermissions(ctx context.Context, p *permPlan) error {
	if p.repo {
		if _, err := runGit(ctx, p.dir, "config", "core.sharedRepository", "group"); err != nil {
			return folderFailure("cannot set core.sharedRepository in "+p.dir, err)
		}
	}
	for _, path := range p.dirs {
		if err := h.regroup(p, path, true); err != nil {
			return err
		}
	}
	for _, path := range p.files {
		if err := h.regroup(p, path, false); err != nil {
			return err
		}
	}
	return nil
}

func (h folderHost) regroup(p *permPlan, path string, isDir bool) error {
	info, ok := plainBelow(p.dir, path, isDir)
	if !ok {
		return nil
	}
	if err := h.lchown(path, -1, p.gid); err != nil {
		return folderFailure("cannot change the group of "+path, err)
	}
	// Anything may have changed since the check; chmod follows links, so look again.
	if again, ok := plainBelow(p.dir, path, isDir); !ok || !os.SameFile(info, again) {
		return nil
	}
	keep := info.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
	mode := keep | 0o060
	if isDir {
		mode = keep | 0o070 | fs.ModeSetgid
	}
	if err := h.chmod(path, mode); err != nil {
		return folderFailure("cannot change the mode of "+path, err)
	}
	return nil
}

// plainBelow reports whether path lies below dir without a symbolic link at any level and is a directory or a
// regular file as asked; it returns the Lstat of path.
func plainBelow(dir, path string, isDir bool) (fs.FileInfo, bool) {
	rel, err := filepath.Rel(dir, path)
	if err != nil || (rel != "." && !filepath.IsLocal(rel)) {
		return nil, false
	}
	cur := dir
	var info fs.FileInfo
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part != "." {
			cur = filepath.Join(cur, part)
		}
		info, err = os.Lstat(cur)
		if err != nil || info.Mode()&fs.ModeSymlink != 0 {
			return nil, false
		}
	}
	if isDir != info.IsDir() || (!isDir && (!info.Mode().IsRegular() || !singleLink(info))) {
		return nil, false
	}
	return info, true
}

// runGit runs git on the repository in dir as the client user. The repository and work tree are named
// explicitly, nothing of the caller's environment or of the user's git configuration is used, and hooks and
// the file system monitor, which would run programs of the folder as root, are switched off.
func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	git, err := lookGit()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, gitCallLimit)
	defer cancel()
	full := []string{
		"--no-pager",
		"-c", "safe.directory=" + dir,
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=/dev/null",
		"--git-dir=" + filepath.Join(dir, ".git"),
		"--work-tree=" + dir,
	}
	cmd := exec.CommandContext(ctx, git, append(full, args...)...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + gitSearchPath, "HOME=/nonexistent", "XDG_CONFIG_HOME=/nonexistent", "LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func lookGit() (string, error) {
	for _, d := range filepath.SplitList(gitSearchPath) {
		path := filepath.Join(d, "git")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return path, nil
		}
	}
	return "", errors.New("git is not installed")
}
