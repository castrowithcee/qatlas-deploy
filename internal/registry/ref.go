package registry

import (
	"fmt"
	"strings"
)

const (
	dockerHubAlias = "docker.io"
	dockerHubHost  = "registry-1.docker.io"
)

// Repo is a repository on a registry.
type Repo struct {
	// Host is the registry host, with port if any.
	Host string
	// Name is the repository path as the distribution API expects it.
	Name string
}

// String is the repository as it is written in a reference.
func (r Repo) String() string { return r.Host + "/" + r.Name }

// ParseRepo splits a reference without tag or digest. The first path part is a host if it contains a dot or
// a port or is "localhost"; otherwise the repository lives on Docker Hub, where the host maps to the API
// host and single-part names get the "library/" prefix.
func ParseRepo(ref string) (Repo, error) {
	if ref == "" || strings.ContainsAny(ref, "@ \t?#\\") || strings.HasPrefix(ref, "/") || strings.HasSuffix(ref, "/") {
		return Repo{}, fmt.Errorf("invalid image reference %q", ref)
	}
	host, name := "", ref
	if first, rest, ok := strings.Cut(ref, "/"); ok && (strings.ContainsAny(first, ".:") || first == "localhost") {
		host, name = first, rest
	}
	if strings.Contains(name, "//") || strings.Contains(name, ":") || hasDotSegment(name) {
		return Repo{}, fmt.Errorf("invalid image reference %q: use a repository without tag or digest", ref)
	}
	if host == "" || host == dockerHubAlias || host == "index.docker.io" {
		host = dockerHubHost
		if !strings.Contains(name, "/") {
			name = "library/" + name
		}
	}
	return Repo{Host: host, Name: name}, nil
}

// hasDotSegment reports a "." or ".." path segment, which would change the request path.
func hasDotSegment(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part == "." || part == ".." {
			return true
		}
	}
	return false
}
