package stacks

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Finding kinds.
const (
	// FindingFixedTag is an image with a literal tag or digest instead of a variable.
	FindingFixedTag = "fixed-tag"
	// FindingFloatingTag is an image without a tag, with a floating tag such as latest, or whose tag variable
	// holds such a value.
	FindingFloatingTag = "floating-tag"
	// FindingUnsetVariable is a tag variable that has neither a value in .env nor a default.
	FindingUnsetVariable = "unset-variable"
)

// ImageVar is a service image whose tag is a variable.
type ImageVar struct {
	Service string
	// Image is the repository part of the reference, without the tag.
	Image string
	// Var is the name of the tag variable.
	Var string
	// Value is the current version: from .env, else the default in the reference, else empty.
	Value string
	// Source is SourceEnv, SourceDefault or SourceUnset.
	Source string
}

// Sources of an ImageVar value.
const (
	SourceEnv     = "env"
	SourceDefault = "default"
	SourceUnset   = "unset"
)

// Finding reports an image that deviates from the standard.
type Finding struct {
	Service string
	Kind    string
	Image   string
	Detail  string
}

type rawImage struct {
	service string
	ref     string
}

var floatingTags = map[string]bool{"latest": true, "stable": true, "edge": true, "main": true, "master": true, "nightly": true}

// readImages returns the image of every service; a later compose file overrides an earlier one as Compose does.
func readImages(dir string, files []string) ([]rawImage, error) {
	byService := map[string]string{}
	for _, f := range files {
		data, err := readBounded(dir, f)
		if err != nil {
			return nil, err
		}
		var doc struct {
			Services map[string]struct {
				Image string `yaml:"image"`
			} `yaml:"services"`
		}
		if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for name, svc := range doc.Services {
			if svc.Image != "" {
				byService[name] = svc.Image
			}
		}
	}
	names := make([]string, 0, len(byService))
	for n := range byService {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]rawImage, 0, len(names))
	for _, n := range names {
		out = append(out, rawImage{n, byService[n]})
	}
	return out, nil
}

// readBindMounts returns the bind-mount sources of every service of the compose files that lie below dir, as
// clean paths relative to dir; see Stack.BindMounts. A file that cannot be read or parsed adds nothing, which
// leaves the paths to the stricter treatment; the stack reports such a file elsewhere.
func readBindMounts(dir string, files []string) []string {
	set := map[string]bool{}
	for _, f := range files {
		data, err := readBounded(dir, f)
		if err != nil {
			continue
		}
		var doc struct {
			Services map[string]struct {
				Volumes []any `yaml:"volumes"`
			} `yaml:"services"`
		}
		if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil {
			continue
		}
		for _, svc := range doc.Services {
			for _, v := range svc.Volumes {
				if p, ok := bindSource(dir, v); ok {
					set[p] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// bindSource returns the source of a bind mount as a path relative to dir, for the short syntax
// "./host:/container[:mode]" and the long syntax with type bind. Compose takes a short source as a path only
// when it starts with "." or "/"; anything else is a named volume.
func bindSource(dir string, v any) (string, bool) {
	var src string
	switch m := v.(type) {
	case string:
		host, _, ok := strings.Cut(m, ":")
		if !ok || !(strings.HasPrefix(host, ".") || strings.HasPrefix(host, "/")) {
			return "", false
		}
		src = host
	case map[string]any:
		typ, _ := m["type"].(string)
		src, _ = m["source"].(string)
		if typ != "bind" || src == "" {
			return "", false
		}
	default:
		return "", false
	}
	if strings.ContainsAny(src, "$~\x00") {
		return "", false
	}
	if filepath.IsAbs(src) {
		rel, err := filepath.Rel(dir, filepath.Clean(src))
		if err != nil {
			return "", false
		}
		src = rel
	}
	src = filepath.ToSlash(filepath.Clean(src))
	if checkRelative(src) != nil {
		return "", false
	}
	return src, true
}

var tagVar = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?)([-?+])([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

func resolveImages(images []rawImage, env map[string]string) ([]ImageVar, []Finding) {
	var vars []ImageVar
	var findings []Finding
	for _, img := range images {
		repo, tag, hasTag, digest := splitImage(img.ref)
		add := func(kind, detail string) {
			findings = append(findings, Finding{Service: img.service, Kind: kind, Image: repo, Detail: detail})
		}
		switch {
		case digest != "" && !hasTag:
			add(FindingFixedTag, "image is pinned by digest, not by a version variable")
		case !hasTag:
			add(FindingFloatingTag, "image has no tag")
		default:
			m := tagVar.FindStringSubmatch(strings.ReplaceAll(tag, "$$", ""))
			if m == nil {
				if floatingTags[tag] {
					add(FindingFloatingTag, fmt.Sprintf("tag %q is floating", tag))
				} else {
					add(FindingFixedTag, fmt.Sprintf("tag %q is fixed in the compose file", tag))
				}
				continue
			}
			name, op, dflt, hasDefault := m[1], m[2], m[4], m[3] == "-"
			if name == "" {
				name = m[5]
			}
			iv := ImageVar{Service: img.service, Image: repo, Var: name, Source: SourceUnset}
			v, set := env[name]
			switch {
			case set && (v != "" || op == ""):
				iv.Value, iv.Source = v, SourceEnv
			case hasDefault:
				iv.Value, iv.Source = dflt, SourceDefault
			}
			if iv.Source == SourceUnset {
				add(FindingUnsetVariable, fmt.Sprintf("variable %s has no value in .env and no default", name))
			} else if floatingTags[iv.Value] || iv.Value == "" {
				add(FindingFloatingTag, fmt.Sprintf("variable %s holds the floating value %q", name, iv.Value))
			}
			vars = append(vars, iv)
		}
	}
	return vars, findings
}

// splitImage splits repo[:tag][@digest]; colons and slashes inside ${...} do not count.
func splitImage(ref string) (repo, tag string, hasTag bool, digest string) {
	depth := 0
	lastSlash, colon, at := -1, -1, -1
	for i := 0; i < len(ref); i++ {
		switch c := ref[i]; {
		case c == '$' && i+1 < len(ref) && ref[i+1] == '{':
			depth++
			i++
		case c == '}' && depth > 0:
			depth--
		case depth > 0:
		case c == '/':
			lastSlash, colon = i, -1
		case c == ':' && colon < 0 && at < 0:
			colon = i
		case c == '@' && at < 0:
			at = i
		}
	}
	rest := ref
	if at >= 0 {
		digest, rest = ref[at+1:], ref[:at]
	}
	if colon >= 0 && colon < len(rest) && colon > lastSlash {
		return rest[:colon], rest[colon+1:], true, digest
	}
	return rest, "", false, digest
}
