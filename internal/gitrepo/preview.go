package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Remotes returns the sorted names of the configured remotes; none for a repository without a remote.
func (r *Repo) Remotes(ctx context.Context) ([]string, error) {
	out, err := r.run(ctx, true, maxStdout, "remote")
	if err != nil {
		return nil, fmt.Errorf("remote failed: %w", err)
	}
	names := strings.Fields(out)
	slices.Sort(names)
	return names, nil
}

// Preview is a read-only view of a folder that is not a repository: it answers what a first "git add" there
// would take. Git runs against a temporary repository outside the folder with the folder as its work tree, so
// the folder itself is never written, and the ignore rules in the folder apply as they would after git init.
type Preview struct {
	r      *Repo
	tmp    string
	gitDir string
}

// OpenPreview prepares the view of dir. skip are paths relative to dir that git need not look into; the answer
// of Untracked and IsIgnored leaves them out only if git honours the hint, so the caller filters as well.
// Close removes the temporary repository.
func OpenPreview(ctx context.Context, cfg Config, dir string, skip []string) (*Preview, error) {
	r, err := Open(cfg, dir)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "qdeploy-preview-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary directory: %w", err)
	}
	p := &Preview{r: r, tmp: tmp, gitDir: filepath.Join(tmp, "git")}
	if _, err := r.run(ctx, false, maxErrorText, "init", "--quiet", "--bare", "--template=", "--", p.gitDir); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("prepare the preview: %w", err)
	}
	var patterns strings.Builder
	for _, s := range skip {
		if pat, ok := excludePattern(s); ok {
			patterns.WriteString(pat + "\n")
		}
	}
	if patterns.Len() > 0 {
		info := filepath.Join(p.gitDir, "info")
		err := os.MkdirAll(info, 0o700)
		if err == nil {
			err = os.WriteFile(filepath.Join(info, "exclude"), []byte(patterns.String()), 0o600)
		}
		if err != nil {
			_ = p.Close()
			return nil, fmt.Errorf("prepare the preview: %w", err)
		}
	}
	return p, nil
}

// excludePattern is the ignore pattern that matches exactly the path p, anchored at the root. Git ignores
// trailing spaces unless they are escaped, so the last one is; a path with a line break or NUL has none.
func excludePattern(p string) (string, bool) {
	if p == "" || strings.ContainsAny(p, "\n\r\x00") {
		return "", false
	}
	var b strings.Builder
	b.WriteByte('/')
	for _, c := range p {
		if strings.ContainsRune(`\*?[`, c) {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	out := b.String()
	if strings.HasSuffix(out, " ") {
		out = out[:len(out)-1] + `\ `
	}
	return out, true
}

func (p *Preview) env() []string {
	return []string{"GIT_DIR=" + p.gitDir, "GIT_WORK_TREE=" + p.r.dir}
}

// Untracked returns the sorted paths git would add, relative to the folder: every file the ignore rules do not
// cover. A folder that holds a repository of its own is listed once, with a trailing slash. Files of any
// type are listed; the caller decides what it can take.
func (p *Preview) Untracked(ctx context.Context) ([]string, error) {
	out, err := p.r.runEnv(ctx, true, maxStdout, p.env(), "--literal-pathspecs", "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("ls-files failed: %w", err)
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	slices.Sort(files)
	return files, nil
}

// IsIgnored reports whether the ignore rules in the folder cover path, a clean path relative to the folder,
// whether or not it exists.
func (p *Preview) IsIgnored(ctx context.Context, path string) (bool, error) {
	if err := checkRepoPath(path); err != nil {
		return false, err
	}
	_, err := p.r.runEnv(ctx, true, maxErrorText, p.env(), "check-ignore", "--quiet", "--no-index", "--", path)
	var failure *runFailure
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &failure) && failure.code == 1:
		return false, nil
	}
	return false, fmt.Errorf("check-ignore failed: %w", err)
}

// Close removes the temporary repository.
func (p *Preview) Close() error { return os.RemoveAll(p.tmp) }
