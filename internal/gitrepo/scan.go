package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// FindingKind says why a path of a commit counts as secret-like.
type FindingKind string

const (
	// KindSecretsFile is a file named like the stack secrets file, in any folder.
	KindSecretsFile FindingKind = "secrets-file"
	// KindEnvSecret is an env file (.env or *.env) with a secret-like key and a non-empty value.
	KindEnvSecret FindingKind = "env-secret"
	// KindPrivateKey is a file with a line that starts a PEM private key.
	KindPrivateKey FindingKind = "private-key"
)

// Finding is one secret-like file in one commit. It names the place only, never a value.
type Finding struct {
	Commit, Path string
	Kind         FindingKind
}

// Scan is the result of ScanPush: the first MaxFindings findings, newest commit first, and the number of all.
type Scan struct {
	Findings []Finding
	Total    int
}

const (
	// MaxFindings bounds Scan.Findings.
	MaxFindings = 100
	// ScanBlobLimit is how much of a file is read; content beyond it is not examined, only its name.
	ScanBlobLimit = 1 << 20

	scanCommitChunk = 100
	scanBatchBytes  = 8 << 20
	scanOutLimit    = 32 << 20
)

var pemBeginRe = regexp.MustCompile(`(?m)^[ \t]*-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----[ \t]*\r?$`)

type scanEntry struct {
	commit, path, blob string
	regular            bool
}

// ScanPush scans every commit a push of head would send: those reachable from head and not from base, all of
// them if base is empty. base is the commit of the remote branch, which must be in the repository. A commit is
// examined by what it adds or changes against its parents, in every folder: a file named secrets.env, an env
// file with a secret-like key (see stacks.SecretLikeKey) that has a value, and a file with a PEM private key
// line. Findings hold commit and path, never content. At most ScanBlobLimit bytes of a file are read.
//
// A history too large for the output bounds of the git layer is an error, never a pass.
func (r *Repo) ScanPush(ctx context.Context, head, base string) (Scan, error) {
	if !hexRe.MatchString(head) || (base != "" && !hexRe.MatchString(base)) {
		return Scan{}, errors.New("commits must be full lowercase commit ids")
	}
	args := []string{"rev-list", head}
	if base != "" {
		args = append(args, "--not", base)
	}
	out, err := r.run(ctx, true, maxStdout, append(args, "--")...)
	if err != nil {
		return Scan{}, fmt.Errorf("rev-list failed: %w", err)
	}
	commits := strings.Fields(out)

	var entries []scanEntry
	for len(commits) > 0 {
		n := min(len(commits), scanCommitChunk)
		got, err := r.changedFiles(ctx, commits[:n])
		if err != nil {
			return Scan{}, err
		}
		entries = append(entries, got...)
		commits = commits[n:]
	}

	var wanted []string
	seen := map[string]bool{}
	for _, e := range entries {
		if e.regular && !seen[e.blob] {
			seen[e.blob] = true
			wanted = append(wanted, e.blob)
		}
	}
	content, err := r.scanBlobs(ctx, wanted)
	if err != nil {
		return Scan{}, err
	}

	var scan Scan
	add := func(e scanEntry, kind FindingKind) {
		scan.Total++
		if len(scan.Findings) < MaxFindings {
			scan.Findings = append(scan.Findings, Finding{Commit: e.commit, Path: e.path, Kind: kind})
		}
	}
	reported := map[[3]string]bool{}
	for _, e := range entries {
		var kinds []FindingKind
		if path.Base(e.path) == stacks.SecretsFile {
			kinds = append(kinds, KindSecretsFile)
		} else if e.regular && isEnvName(e.path) && content[e.blob].envSecret {
			kinds = append(kinds, KindEnvSecret)
		}
		if e.regular && content[e.blob].privateKey {
			kinds = append(kinds, KindPrivateKey)
		}
		for _, k := range kinds {
			if key := [3]string{e.commit, e.path, string(k)}; !reported[key] {
				reported[key] = true
				add(e, k)
			}
		}
	}
	return scan, nil
}

// isEnvName reports whether the file is read as an env file: .env, or a name ending in .env.
func isEnvName(p string) bool {
	return strings.HasSuffix(path.Base(p), ".env")
}

// changedFiles lists, for each commit, the files it adds or changes against each of its parents; a root commit
// adds all its files.
func (r *Repo) changedFiles(ctx context.Context, commits []string) ([]scanEntry, error) {
	out, err := r.runIO(ctx, true, scanOutLimit, nil, ioOptions{stdin: strings.NewReader(strings.Join(commits, "\n") + "\n")},
		"diff-tree", "--stdin", "-r", "-z", "--root", "-m", "--no-renames", "--no-abbrev", "--diff-filter=AMT")
	if err != nil {
		return nil, fmt.Errorf("diff-tree failed: %w", err)
	}
	var entries []scanEntry
	commit := ""
	tokens := strings.Split(out, "\x00")
	for i := 0; i < len(tokens); i++ {
		tok := strings.TrimPrefix(tokens[i], "\n")
		switch {
		case strings.HasPrefix(tok, ":"):
			// ":<src mode> <dst mode> <src id> <dst id> <status>" and then the path.
			f := strings.Fields(tok)
			if len(f) < 5 || i+1 >= len(tokens) || commit == "" {
				return nil, errors.New("diff-tree gave an unexpected answer")
			}
			i++
			entries = append(entries, scanEntry{commit: commit, path: tokens[i], blob: f[3],
				regular: f[1] == "100644" || f[1] == "100755"})
		case hexRe.MatchString(tok):
			commit = tok
		}
	}
	return entries, nil
}

type blobScan struct{ envSecret, privateKey bool }

// scanBlobs reads each blob, up to ScanBlobLimit bytes, and examines it. Blobs that fit go through one
// cat-file process per batch, larger ones are read one by one and cut.
func (r *Repo) scanBlobs(ctx context.Context, ids []string) (map[string]blobScan, error) {
	res := make(map[string]blobScan, len(ids))
	sizes := map[string]int{}
	for len(ids) > 0 {
		n := min(len(ids), 2000)
		out, err := r.runIO(ctx, true, maxStdout, nil, ioOptions{stdin: strings.NewReader(strings.Join(ids[:n], "\n") + "\n")},
			"cat-file", "--batch-check")
		if err != nil {
			return nil, fmt.Errorf("cat-file failed: %w", err)
		}
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) == 3 && f[1] == "blob" {
				if size, err := strconv.Atoi(f[2]); err == nil {
					sizes[f[0]] = size
				}
			}
		}
		ids = ids[n:]
	}

	var batch []string
	total := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		defer func() { batch, total = nil, 0 }()
		out, err := r.runIO(ctx, true, scanBatchBytes+maxStdout, nil, ioOptions{stdin: strings.NewReader(strings.Join(batch, "\n") + "\n")},
			"cat-file", "--batch")
		if err != nil {
			return fmt.Errorf("cat-file failed: %w", err)
		}
		data := []byte(out)
		for len(data) > 0 {
			head, rest, ok := bytes.Cut(data, []byte("\n"))
			f := strings.Fields(string(head))
			if !ok || len(f) != 3 {
				return errors.New("cat-file gave an unexpected answer")
			}
			size, err := strconv.Atoi(f[2])
			if err != nil || size < 0 || size+1 > len(rest) {
				return errors.New("cat-file gave an unexpected answer")
			}
			res[f[0]] = examine(rest[:size])
			data = rest[size+1:]
		}
		return nil
	}
	for id, size := range sizes {
		if size > ScanBlobLimit {
			head, err := r.runIO(ctx, true, ScanBlobLimit, nil, ioOptions{truncate: true}, "cat-file", "blob", id)
			if err != nil {
				return nil, fmt.Errorf("cat-file failed: %w", err)
			}
			res[id] = examine([]byte(head))
			continue
		}
		if total+size > scanBatchBytes {
			if err := flush(); err != nil {
				return nil, err
			}
		}
		batch = append(batch, id)
		total += size
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return res, nil
}

// examine looks for the two content findings; it keeps nothing of the data.
func examine(data []byte) blobScan {
	return blobScan{envSecret: envHoldsSecret(data), privateKey: pemBeginRe.Match(data)}
}

// envHoldsSecret reports whether some KEY=VALUE line has a secret-like key and a value that is not empty.
func envHoldsSecret(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, raw, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if ok && key != "" && stacks.SecretLikeKey(key) && envValueSet(strings.TrimSpace(raw)) {
			return true
		}
	}
	return false
}

// envValueSet reports whether a raw value holds anything but quotes, space and a trailing comment.
func envValueSet(raw string) bool {
	if len(raw) >= 2 && (raw[0] == '"' || raw[0] == '\'') && raw[len(raw)-1] == raw[0] {
		return strings.TrimSpace(raw[1:len(raw)-1]) != ""
	}
	if strings.HasPrefix(raw, "#") {
		return false
	}
	if i := strings.Index(raw, " #"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.Index(raw, "\t#"); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(raw) != ""
}
