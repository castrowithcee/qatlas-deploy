// Package registrytest is a small registry server for tests of code that reads registries: a distribution API
// with anonymous, Basic and Bearer access, paginated tag lists, and the manifests and labels of the tags.
package registrytest

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Bearer is the token the server's token endpoint hands out.
const Bearer = "fake-bearer-456"

// Auth is how the server protects the /v2/ endpoints.
type Auth int

// The access modes of the server.
const (
	// AuthNone serves every request.
	AuthNone Auth = iota
	// AuthBearer answers 401 with a Bearer challenge until the request carries the token of /token. The token
	// endpoint accepts User and Password as Basic credentials, or anyone if User is empty.
	AuthBearer
	// AuthBasic answers 401 with a Basic challenge until the request carries User and Password.
	AuthBasic
)

// Config describes the server.
type Config struct {
	Auth           Auth
	User, Password string
	// Repos maps a repository name such as "team/app" to its tags, in listing order.
	Repos map[string][]string
	// PageSize is the number of tags per page of a tag list; zero lists all tags on one page.
	PageSize int
	// Labels are the image labels of every tag.
	Labels map[string]string
	// Index makes a tag resolve to a multi-platform index (linux/amd64 and linux/arm64) instead of an image
	// manifest.
	Index bool
	// Redirect makes every /v2/ request answer a redirect to another host.
	Redirect bool
	// OversizeTags makes the tag list answer more bytes than any reasonable page limit.
	OversizeTags int
}

// Server is a running test registry.
type Server struct {
	// URL is the base URL of the server.
	URL string

	cfg    Config
	srv    *httptest.Server
	mu     sync.Mutex
	reqs   []string
	tokens []url.Values
}

// New starts a server that stops with the test.
func New(t testing.TB, cfg Config) *Server {
	t.Helper()
	s := &Server{cfg: cfg}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	s.URL = s.srv.URL
	t.Cleanup(s.srv.Close)
	return s
}

// Requests returns "METHOD path" of every request so far.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reqs...)
}

// TokenRequests returns the query of every request to the token endpoint.
func (s *Server) TokenRequests() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.tokens...)
}

func sum(b []byte) string { d := sha256.Sum256(b); return fmt.Sprintf("sha256:%x", d[:]) }

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.reqs = append(s.reqs, r.Method+" "+r.URL.Path)
	if r.URL.Path == "/token" {
		s.tokens = append(s.tokens, r.URL.Query())
	}
	s.mu.Unlock()
	if r.URL.Path == "/token" {
		if u, p, ok := r.BasicAuth(); s.cfg.User != "" && (!ok || u != s.cfg.User || p != s.cfg.Password) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": Bearer})
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/v2/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if s.cfg.Redirect {
		http.Redirect(w, r, "https://other.example.test"+r.URL.Path, http.StatusFound)
		return
	}
	if !s.authorized(w, r, rest) {
		return
	}
	for name, tags := range s.cfg.Repos {
		switch {
		case rest == name+"/tags/list":
			s.tagList(w, r, name, tags)
			return
		case strings.HasPrefix(rest, name+"/manifests/"):
			s.manifest(w, r, strings.TrimPrefix(rest, name+"/manifests/"), tags)
			return
		case strings.HasPrefix(rest, name+"/blobs/"):
			_, _ = w.Write(s.configBlob())
			return
		}
	}
	http.NotFound(w, r)
}

func (s *Server) authorized(w http.ResponseWriter, r *http.Request, rest string) bool {
	switch s.cfg.Auth {
	case AuthBearer:
		if r.Header.Get("Authorization") == "Bearer "+Bearer {
			return true
		}
		name, _, _ := strings.Cut(rest, "/tags/")
		w.Header().Set("WWW-Authenticate",
			fmt.Sprintf(`Bearer realm="%s/token",service="registry.example.test",scope="repository:%s:pull"`, s.URL, name))
	case AuthBasic:
		if u, p, ok := r.BasicAuth(); ok && u == s.cfg.User && p == s.cfg.Password {
			return true
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="fake"`)
	default:
		return true
	}
	w.WriteHeader(http.StatusUnauthorized)
	return false
}

func (s *Server) tagList(w http.ResponseWriter, r *http.Request, name string, tags []string) {
	if s.cfg.OversizeTags > 0 {
		_, _ = w.Write([]byte(`{"tags":["` + strings.Repeat("a", s.cfg.OversizeTags) + `"]}`))
		return
	}
	start := 0
	if last := r.URL.Query().Get("last"); last != "" {
		start, _ = strconv.Atoi(last)
	}
	start = min(max(start, 0), len(tags))
	end := len(tags)
	if s.cfg.PageSize > 0 {
		end = min(start+s.cfg.PageSize, len(tags))
	}
	if end < len(tags) {
		// Alternate relative and absolute links, as registries do both.
		link := fmt.Sprintf("/v2/%s/tags/list?n=%d&last=%d", name, s.cfg.PageSize, end)
		if (end/max(s.cfg.PageSize, 1))%2 == 0 {
			link = s.URL + link
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, link))
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"name": name, "tags": tags[start:end]})
}

func (s *Server) configBlob() []byte {
	cfg, _ := json.Marshal(map[string]any{"config": map[string]any{"Labels": s.cfg.Labels}})
	return cfg
}

func (s *Server) manifest(w http.ResponseWriter, r *http.Request, ref string, tags []string) {
	cfgDigest := sum(s.configBlob())
	image, _ := json.Marshal(map[string]any{
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config":    map[string]any{"digest": cfgDigest},
	})
	imageDigest := sum(image)
	doc, mediaType := image, "application/vnd.oci.image.manifest.v1+json"
	known := ref == imageDigest
	for _, tag := range tags {
		known = known || tag == ref
	}
	if !known {
		http.NotFound(w, r)
		return
	}
	if s.cfg.Index && ref != imageDigest {
		doc, _ = json.Marshal(map[string]any{
			"mediaType": "application/vnd.oci.image.index.v1+json",
			"manifests": []map[string]any{
				{"digest": "sha256:bbbb", "platform": map[string]string{"os": "linux", "architecture": "arm64"}},
				{"digest": imageDigest, "platform": map[string]string{"os": "linux", "architecture": "amd64"}},
			},
		})
		mediaType = "application/vnd.oci.image.index.v1+json"
	}
	w.Header().Set("Docker-Content-Digest", sum(doc))
	w.Header().Set("Content-Type", mediaType)
	if r.Method != http.MethodHead {
		_, _ = w.Write(doc)
	}
}
