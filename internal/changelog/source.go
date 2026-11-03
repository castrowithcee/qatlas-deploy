package changelog

import (
	"net/url"
	"regexp"
	"strings"
)

// Repo is a GitHub repository.
type Repo struct {
	Owner, Name string
}

var (
	ownerRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	nameRe  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	scpRe   = regexp.MustCompile(`^[A-Za-z0-9._-]+@github\.com:(.+)$`)
)

// String is the repository as "owner/name".
func (r Repo) String() string { return r.Owner + "/" + r.Name }

// URL is the web address of the repository.
func (r Repo) URL() string { return "https://github.com/" + r.Owner + "/" + r.Name }

// ReleasesURL is the web address of the releases page of the repository.
func (r Repo) ReleasesURL() string { return r.URL() + "/releases" }

func (r Repo) valid() bool {
	return ownerRe.MatchString(r.Owner) && nameRe.MatchString(r.Name) && r.Name != "." && r.Name != ".."
}

// Source is what ParseSource makes of a source reference.
type Source struct {
	// Repo is set when the reference names a GitHub repository.
	Repo Repo
	// GitHub reports whether Repo is set.
	GitHub bool
	// Link is a web address to show when no release text is available: the releases page of the GitHub
	// repository, or the cleaned http(s) address of another host. Empty when the reference holds no usable address.
	Link string
}

// ParseSource derives a GitHub repository from "owner/repo" or a GitHub address (https, http, git, ssh or
// scp form, with or without ".git"), such as the value of the image label org.opencontainers.image.source.
// Anything else is not recognized; an http(s) address of another host still yields a Link, stripped of user
// info, query and fragment.
func ParseSource(ref string) Source {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.ContainsFunc(ref, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return Source{}
	}
	path := ""
	switch {
	case strings.Contains(ref, "://"):
		u, err := url.Parse(ref)
		if err != nil || u.Host == "" {
			return Source{}
		}
		host := strings.ToLower(u.Hostname())
		if host == "github.com" || host == "www.github.com" {
			switch u.Scheme {
			case "https", "http", "git", "ssh":
				path = u.Path
			}
		}
		if path == "" {
			if u.Scheme != "https" && u.Scheme != "http" {
				return Source{}
			}
			clean := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
			return Source{Link: clean.String()}
		}
	case scpRe.MatchString(ref):
		path = scpRe.FindStringSubmatch(ref)[1]
	case strings.HasPrefix(strings.ToLower(ref), "github.com/"):
		path = ref[len("github.com/"):]
	default:
		path = ref
		if !strings.Contains(ref, "/") {
			return Source{}
		}
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || len(parts) > 2 && !strings.Contains(ref, "://") && !scpRe.MatchString(ref) {
		return Source{}
	}
	repo := Repo{Owner: parts[0], Name: strings.TrimSuffix(parts[1], ".git")}
	if !repo.valid() {
		return Source{}
	}
	return Source{Repo: repo, GitHub: true, Link: repo.ReleasesURL()}
}
