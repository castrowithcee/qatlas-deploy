package changelog

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	cacheVersion  = 1
	maxCacheBytes = 32 << 20
	cacheSuffix   = ".json"
	tmpPrefix     = ".tmp-"
)

type cacheFile struct {
	Version   int       `json:"version"`
	Repo      string    `json:"repo"`
	FetchedAt time.Time `json:"fetched_at"`
	Releases  []Release `json:"releases"`
}

// cachePath is keyed by repository only. Owner names cannot contain "_", so the key is unambiguous.
func (c *Client) cachePath(repo Repo) string {
	return filepath.Join(c.opts.CacheDir, strings.ToLower(repo.Owner)+"_"+strings.ToLower(repo.Name)+cacheSuffix)
}

// readCache returns the cache of repo; an unreadable, corrupt or foreign file counts as no cache.
func (c *Client) readCache(repo Repo) (cacheFile, bool) {
	if c.opts.CacheDir == "" {
		return cacheFile{}, false
	}
	f, err := os.Open(c.cachePath(repo))
	if err != nil {
		return cacheFile{}, false
	}
	defer f.Close()
	var cf cacheFile
	if err := json.NewDecoder(io.LimitReader(f, maxCacheBytes)).Decode(&cf); err != nil {
		return cacheFile{}, false
	}
	if cf.Version != cacheVersion || !strings.EqualFold(cf.Repo, repo.String()) {
		return cacheFile{}, false
	}
	// The cache is local but not trusted more than the network.
	for i, r := range cf.Releases {
		cf.Releases[i] = cleanRelease(r)
	}
	return cf, true
}

// writeCache stores the cache atomically. A failure only costs the cache, so it is not reported.
func (c *Client) writeCache(repo Repo, cf cacheFile) {
	if c.opts.CacheDir == "" {
		return
	}
	data, err := json.Marshal(cf)
	if err != nil || os.MkdirAll(c.opts.CacheDir, 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(c.opts.CacheDir, tmpPrefix+"*")
	if err != nil {
		return
	}
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if tmp.Chmod(0o600) != nil {
		return
	}
	if _, err := tmp.Write(data); err != nil || tmp.Sync() != nil || tmp.Close() != nil {
		return
	}
	if os.Rename(tmp.Name(), c.cachePath(repo)) == nil {
		ok = true
	}
}

// CleanCache removes the cache files in dir that are older than maxAge at now, judged by their modification
// time, and leftover temporary files. Other files are left alone. It returns the number of removed files; a
// missing directory is not an error.
func CleanCache(dir string, maxAge time.Duration, now time.Time) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasSuffix(name, cacheSuffix) && !strings.HasPrefix(name, tmpPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		if os.Remove(filepath.Join(dir, name)) == nil {
			removed++
		}
	}
	return removed
}
