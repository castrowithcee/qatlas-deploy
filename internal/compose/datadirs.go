package compose

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const (
	// maxImageFile bounds /etc/passwd and /etc/group read from an image.
	maxImageFile = 1 << 20
	// imageCallTimeout bounds each docker call that inspects an image.
	imageCallTimeout = 30 * time.Second
)

var (
	numberRe = regexp.MustCompile(`^[0-9]{1,10}$`)
	// imageNameRe keeps an image reference that starts with a dash or holds odd bytes away from the docker CLI.
	imageNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]*$`)
	containerID = regexp.MustCompile(`^[0-9a-f]{12,64}$`)
)

// MissingDataDirs implements engine.DataDirChecker.
func (d *Deployer) MissingDataDirs(ctx context.Context, st stacks.Stack,
	versioned func(context.Context, string) (bool, error), log *engine.Log) ([]engine.MissingDir, error) {
	return d.Exec.MissingDataDirs(ctx, st, versioned, log)
}

type dataService struct {
	Image   string `json:"image"`
	User    string `json:"user"`
	Volumes []struct {
		Type   string `json:"type"`
		Source string `json:"source"`
		Bind   *struct {
			CreateHostPath *bool `json:"create_host_path"`
		} `json:"bind"`
	} `json:"volumes"`
}

type dataCandidate struct {
	rel     string
	service string
}

// MissingDataDirs finds the bind-mount source directories that do not exist, lie inside the stack folder, are
// not versioned and belong to a service whose user is not root: Docker would create them owned by root. The
// proposed owner comes from the service's user or the image's. Nothing is created and no image code runs.
//
// The rendered configuration holds interpolated secrets; only service names, images, users and bind sources
// are read from it, and none of it is logged. A user that cannot be determined means "as before": no finding.
func (x *Executor) MissingDataDirs(ctx context.Context, st stacks.Stack,
	versioned func(context.Context, string) (bool, error), log Logger) ([]engine.MissingDir, error) {
	raw, err := x.renderConfig(ctx, st, log)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Services map[string]dataService `json:"services"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, errors.New("docker compose config: unexpected output")
	}
	dir, err := filepath.Abs(st.Dir)
	if err != nil {
		return nil, fmt.Errorf("stack directory: %w", err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("stack directory: %w", err)
	}

	names := make([]string, 0, len(cfg.Services))
	for n := range cfg.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	var found []dataCandidate
	seen := map[string]bool{}
	for _, n := range names {
		for _, v := range cfg.Services[n].Volumes {
			if v.Type != "bind" || (v.Bind != nil && v.Bind.CreateHostPath != nil && !*v.Bind.CreateHostPath) {
				continue
			}
			rel, ok := missingBelow(dir, real, v.Source)
			if !ok || seen[rel] {
				continue
			}
			seen[rel] = true
			found = append(found, dataCandidate{rel: rel, service: n})
		}
	}
	if len(found) == 0 {
		return nil, nil
	}

	users := &imageUsers{x: x, st: st, log: log, cfg: cfg.Services, cache: map[string]*imageInfo{}}
	var out []engine.MissingDir
	for _, c := range found {
		if versioned != nil {
			v, err := versioned(ctx, c.rel)
			if err != nil {
				return nil, fmt.Errorf("check whether %s is versioned: %w", c.rel, err)
			}
			if v {
				continue
			}
		}
		owner, needed := users.owner(ctx, c.service)
		if needed {
			out = append(out, engine.MissingDir{Path: c.rel, Owner: owner})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// missingBelow returns the path of source relative to the stack folder dir when it is a candidate: absolute,
// strictly inside dir, absent, and with the deepest existing ancestor resolving inside realDir, the stack
// folder with symlinks resolved. Anything else, also an unreadable or dangling parent, is no candidate.
func missingBelow(dir, realDir, source string) (string, bool) {
	if !filepath.IsAbs(source) || strings.ContainsRune(source, 0) {
		return "", false
	}
	p := filepath.Clean(source)
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	anc := filepath.Dir(p)
	for {
		_, err := os.Lstat(anc)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) || anc == dir {
			return "", false
		}
		anc = filepath.Dir(anc)
	}
	res, err := filepath.EvalSymlinks(anc)
	if err != nil || (res != realDir && !strings.HasPrefix(res, realDir+string(filepath.Separator))) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// imageInfo is what is known about an image: the user it runs as, and /etc/passwd and /etc/group once read.
type imageInfo struct {
	user         string
	userKnown    bool
	files        bool // passwd and group were tried
	passwd, grps []string
}

type imageUsers struct {
	x     *Executor
	st    stacks.Stack
	log   Logger
	cfg   map[string]dataService
	cache map[string]*imageInfo
}

// owner returns the proposed owner of the service's data directories and whether the service runs as a
// non-root user at all.
func (u *imageUsers) owner(ctx context.Context, service string) (string, bool) {
	svc := u.cfg[service]
	img := func() *imageInfo { return u.image(ctx, service, svc.Image) }
	user, known := svc.User, svc.User != ""
	if !known {
		if i := img(); i != nil && i.userKnown {
			user, known = i.user, true
		}
	}
	if !known {
		u.log.Printf("the user of service %s is unknown; its data directories are left to Docker", service)
		return "", false
	}
	return u.resolve(ctx, img, svc.Image, user)
}

// resolve turns a user specification (name or uid, optionally ":group") into "UID:GID". The second result is
// false for root, which Docker's own directory creation serves.
func (u *imageUsers) resolve(ctx context.Context, img func() *imageInfo, ref, user string) (string, bool) {
	uPart, gPart, _ := strings.Cut(user, ":")
	if uPart == "" || uPart == "root" || (numberRe.MatchString(uPart) && atoi(uPart) == 0) {
		return "", false
	}
	var uid, gid string
	if numberRe.MatchString(uPart) {
		uid = uPart
	}
	needFiles := uid == "" || gPart == "" || !numberRe.MatchString(gPart)
	var passwd, groups []string
	if needFiles {
		if i := img(); i != nil {
			u.readFiles(ctx, i, ref)
			passwd, groups = i.passwd, i.grps
		}
	}
	if uid == "" {
		var ok bool
		if uid, gid, ok = lookupPasswd(passwd, 0, uPart); !ok {
			return "", true
		}
		if atoi(uid) == 0 {
			return "", false
		}
	} else if _, g, ok := lookupPasswd(passwd, 2, uid); ok {
		gid = g
	}
	switch {
	case gPart == "":
		if gid == "" {
			gid = uid
		}
	case numberRe.MatchString(gPart):
		gid = gPart
	default:
		g, ok := lookupGroup(groups, gPart)
		if !ok {
			return "", true
		}
		gid = g
	}
	return uid + ":" + gid, true
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// lookupPasswd returns uid and gid of the first passwd record whose name (field 0) or uid (field 2) equals
// value.
func lookupPasswd(lines []string, field int, value string) (uid, gid string, ok bool) {
	for _, l := range lines {
		f := strings.Split(l, ":")
		if len(f) >= 4 && f[field] == value && numberRe.MatchString(f[2]) && numberRe.MatchString(f[3]) {
			return f[2], f[3], true
		}
	}
	return "", "", false
}

// lookupGroup returns the gid of the group with the given name.
func lookupGroup(lines []string, name string) (string, bool) {
	for _, l := range lines {
		f := strings.Split(l, ":")
		if len(f) >= 3 && f[0] == name && numberRe.MatchString(f[2]) {
			return f[2], true
		}
	}
	return "", false
}

// image returns the user of the service's image, pulling it first when it is not local.
func (u *imageUsers) image(ctx context.Context, service, ref string) *imageInfo {
	if ref == "" || !imageNameRe.MatchString(ref) {
		return nil
	}
	if info, ok := u.cache[ref]; ok {
		return info
	}
	info := &imageInfo{}
	u.cache[ref] = info
	user, err := u.inspectUser(ctx, ref)
	if err != nil {
		// The new images are pulled only after the stack stopped; the user is needed before. Pulling
		// an image changes no running container.
		if perr := u.x.step(ctx, u.st, u.log, orDefault(u.x.PullTimeout, DefaultPullTimeout), "pull", "pull", "--quiet", service); perr == nil {
			user, err = u.inspectUser(ctx, ref)
		}
	}
	if err != nil {
		return info
	}
	info.user, info.userKnown = user, true
	return info
}

func (u *imageUsers) inspectUser(ctx context.Context, ref string) (string, error) {
	out, err := u.docker(ctx, "image inspect", "image", "inspect", "--format", "{{json .Config.User}}", ref)
	if err != nil {
		return "", err
	}
	var user string
	if err := json.Unmarshal(bytes.TrimSpace(out), &user); err != nil {
		return "", errors.New("docker image inspect: unexpected output")
	}
	return user, nil
}

func (u *imageUsers) docker(ctx context.Context, name string, args ...string) ([]byte, error) {
	return u.x.dockerOut(ctx, u.log, name, args...)
}

// dockerOut runs one bounded docker call and returns its standard output.
func (x *Executor) dockerOut(ctx context.Context, log Logger, name string, args ...string) ([]byte, error) {
	buf := &limitBuffer{limit: maxImageFile}
	c := Command{Name: dockerProgram, Args: args, Env: x.Env, Stdout: buf}
	if _, err := x.runNamed(ctx, log, c, imageCallTimeout, "docker "+name); err != nil {
		return nil, err
	}
	if buf.over {
		return nil, fmt.Errorf("docker %s: the output is too large", name)
	}
	return buf.b.Bytes(), nil
}

// readFiles reads /etc/passwd and /etc/group from the image through a created, never started container, so no
// code of the image runs. A failure leaves the lists empty.
func (u *imageUsers) readFiles(ctx context.Context, img *imageInfo, ref string) {
	if img.files {
		return
	}
	img.files = true
	out, err := u.docker(ctx, "create", "create", "--pull", "never", "--network", "none", "--entrypoint", "/nonexistent", ref)
	id := strings.TrimSpace(string(out))
	if err != nil || !containerID.MatchString(id) {
		u.log.Printf("the users of image %s cannot be read", ref)
		return
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), imageCallTimeout)
		defer cancel()
		if _, err := u.x.dockerOut(rctx, u.log, "rm", "rm", "-f", id); err != nil {
			u.log.Printf("the temporary container %s could not be removed", id)
		}
	}()
	img.passwd = u.imageFile(ctx, id, "/etc/passwd")
	img.grps = u.imageFile(ctx, id, "/etc/group")
}

func (u *imageUsers) imageFile(ctx context.Context, id, path string) []string {
	raw, err := u.docker(ctx, "cp", "cp", "-L", id+":"+path, "-")
	if err != nil {
		return nil
	}
	return firstTarFile(raw)
}

// firstTarFile returns the lines of the first regular file of a tar stream.
func firstTarFile(raw []byte) []string {
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		h, err := tr.Next()
		if err != nil {
			return nil
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(tr, maxImageFile))
		if err != nil {
			return nil
		}
		return strings.Split(string(body), "\n")
	}
}
