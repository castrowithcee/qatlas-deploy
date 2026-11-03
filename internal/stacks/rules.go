package stacks

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/castrowithcee/qatlas-deploy/internal/changelog"
)

// Update modes, ceilings and major strategies.
const (
	ModeNone      = "none" // no qdeploy.yaml: deployed, never updated
	ModeManual    = "manual"
	ModeAutomatic = "automatic"

	CeilingPatch = "patch"
	CeilingMinor = "minor"
	CeilingMajor = "major"

	MajorDirect   = "direct"
	MajorStepwise = "stepwise"
)

// Rules are the validated contents of qdeploy.yaml with defaults applied.
type Rules struct {
	Update Update
	Backup *Backup // nil: no backup configured
	Check  Check
}

// Update says whether and how far a stack is updated.
type Update struct {
	Mode    string
	Ceiling string
	// Major is how a major jump is taken: directly, or one major version at a time.
	Major string
	// Tags restricts the candidate tags per image variable.
	Tags map[string]*regexp.Regexp
	// Changelog names the GitHub repository ("owner/name") whose releases describe the versions of an image
	// variable. It takes precedence over the image label.
	Changelog map[string]string
}

// Backup describes what is saved before an update. Nothing here is executed by this package.
type Backup struct {
	// Paths are data directories relative to the stack folder.
	Paths   []string
	Volumes []string
	// Command is an argument vector, never a shell line.
	Command []string
	// Keep is the number of newest backups to keep; MaxAgeDays the age limit in days. A backup is kept only
	// while it satisfies every limit that is set.
	Keep       int
	MaxAgeDays int
}

// Check says when a deployed stack counts as healthy.
type Check struct {
	// Containers requires every container of the stack to be running.
	Containers bool
	HTTP       *HTTPCheck
}

// HTTPCheck is an HTTP probe.
type HTTPCheck struct {
	URL     string
	Status  int
	Timeout time.Duration
}

type rawRules struct {
	Version *int `yaml:"version"`
	Update  *struct {
		Mode    *string           `yaml:"mode"`
		Ceiling *string           `yaml:"ceiling"`
		Major   *string           `yaml:"major"`
		Tags    map[string]string `yaml:"tags"`

		Changelog map[string]string `yaml:"changelog"`
	} `yaml:"update"`
	Backup *struct {
		Paths     []string `yaml:"paths"`
		Volumes   []string `yaml:"volumes"`
		Command   []string `yaml:"command"`
		Retention *struct {
			Keep       *int `yaml:"keep"`
			MaxAgeDays *int `yaml:"max_age_days"`
		} `yaml:"retention"`
	} `yaml:"backup"`
	Check *struct {
		Containers *bool `yaml:"containers"`
		HTTP       *struct {
			URL     string  `yaml:"url"`
			Status  *int    `yaml:"status"`
			Timeout *string `yaml:"timeout"`
		} `yaml:"http"`
	} `yaml:"check"`
}

const (
	defaultKeep    = 7
	defaultTimeout = 10 * time.Second
)

var (
	volumeName   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	unknownField = regexp.MustCompile(`field (\S+) not found in type \S+`)
)

// readRules loads qdeploy.yaml; nil, nil when the stack has none. Errors name the file and the field path.
func readRules(dir string) (*Rules, error) {
	data, err := readBounded(dir, RulesFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var raw rawRules
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: file is empty; at least 'version: 1' is required", RulesFile)
		}
		msg := unknownField.ReplaceAllString(err.Error(), `unknown field $1`)
		msg = strings.TrimPrefix(msg, "yaml: ")
		return nil, fmt.Errorf("%s: %s", RulesFile, strings.ReplaceAll(msg, "\n", "; "))
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: only one YAML document is allowed", RulesFile)
	}
	rules, err := validateRules(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", RulesFile, err)
	}
	return rules, nil
}

func oneOf(path, got string, allowed ...string) error {
	for _, a := range allowed {
		if got == a {
			return nil
		}
	}
	return fmt.Errorf("%s: must be one of %s (got %q)", path, strings.Join(allowed, ", "), got)
}

func validateRules(raw rawRules) (*Rules, error) {
	if raw.Version == nil {
		return nil, errors.New("version: required (use 1)")
	}
	if *raw.Version != 1 {
		return nil, fmt.Errorf("version: unsupported value %d (use 1)", *raw.Version)
	}
	r := &Rules{Update: Update{Mode: ModeManual, Ceiling: CeilingMinor, Major: MajorStepwise}, Check: Check{Containers: true}}

	if u := raw.Update; u != nil {
		for _, f := range []struct {
			path    string
			in      *string
			out     *string
			allowed []string
		}{
			{"update.mode", u.Mode, &r.Update.Mode, []string{ModeAutomatic, ModeManual}},
			{"update.ceiling", u.Ceiling, &r.Update.Ceiling, []string{CeilingPatch, CeilingMinor, CeilingMajor}},
			{"update.major", u.Major, &r.Update.Major, []string{MajorDirect, MajorStepwise}},
		} {
			if f.in == nil {
				continue
			}
			if err := oneOf(f.path, *f.in, f.allowed...); err != nil {
				return nil, err
			}
			*f.out = *f.in
		}
		keys := make([]string, 0, len(u.Tags))
		for k := range u.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !envKey.MatchString(k) {
				return nil, fmt.Errorf("update.tags.%s: not a variable name", k)
			}
			re, err := regexp.Compile(u.Tags[k])
			if err != nil {
				return nil, fmt.Errorf("update.tags.%s: invalid regular expression: %v", k, err)
			}
			if r.Update.Tags == nil {
				r.Update.Tags = map[string]*regexp.Regexp{}
			}
			r.Update.Tags[k] = re
		}
		keys = keys[:0]
		for k := range u.Changelog {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !envKey.MatchString(k) {
				return nil, fmt.Errorf("update.changelog.%s: not a variable name", k)
			}
			src := changelog.ParseSource(u.Changelog[k])
			if !src.GitHub {
				return nil, fmt.Errorf("update.changelog.%s: must be a GitHub repository as owner/name or a github.com address", k)
			}
			if r.Update.Changelog == nil {
				r.Update.Changelog = map[string]string{}
			}
			r.Update.Changelog[k] = src.Repo.String()
		}
	}

	if b := raw.Backup; b != nil {
		out, err := validateBackup(b.Paths, b.Volumes, b.Command)
		if err != nil {
			return nil, err
		}
		out.Keep = 0
		if rt := b.Retention; rt != nil {
			if rt.Keep != nil {
				if *rt.Keep < 1 {
					return nil, errors.New("backup.retention.keep: must be at least 1")
				}
				out.Keep = *rt.Keep
			}
			if rt.MaxAgeDays != nil {
				if *rt.MaxAgeDays < 1 {
					return nil, errors.New("backup.retention.max_age_days: must be at least 1")
				}
				out.MaxAgeDays = *rt.MaxAgeDays
			}
		}
		if out.Keep == 0 && out.MaxAgeDays == 0 {
			out.Keep = defaultKeep
		}
		r.Backup = out
	}

	if c := raw.Check; c != nil {
		if c.Containers != nil {
			r.Check.Containers = *c.Containers
		}
		if h := c.HTTP; h != nil {
			hc, err := validateHTTP(h.URL, h.Status, h.Timeout)
			if err != nil {
				return nil, err
			}
			r.Check.HTTP = hc
		}
	}
	return r, nil
}

func validateBackup(paths, volumes, command []string) (*Backup, error) {
	if len(paths) == 0 && len(volumes) == 0 && len(command) == 0 {
		return nil, errors.New("backup: needs at least one of paths, volumes, command")
	}
	for i, p := range paths {
		if err := checkRelative(p); err != nil {
			return nil, fmt.Errorf("backup.paths[%d]: %w", i, err)
		}
	}
	for i, v := range volumes {
		if !volumeName.MatchString(v) {
			return nil, fmt.Errorf("backup.volumes[%d]: %q is not a volume name", i, v)
		}
	}
	if len(command) > 0 && command[0] == "" {
		return nil, errors.New("backup.command[0]: the program must not be empty")
	}
	return &Backup{Paths: paths, Volumes: volumes, Command: command}, nil
}

func validateHTTP(rawURL string, status *int, timeout *string) (*HTTPCheck, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("check.http.url: must be an absolute http or https URL (got %q)", rawURL)
	}
	hc := &HTTPCheck{URL: rawURL, Status: 200, Timeout: defaultTimeout}
	if status != nil {
		if *status < 100 || *status > 599 {
			return nil, fmt.Errorf("check.http.status: must be between 100 and 599 (got %d)", *status)
		}
		hc.Status = *status
	}
	if timeout != nil {
		d, err := time.ParseDuration(*timeout)
		if err != nil || d < time.Second || d > 10*time.Minute {
			return nil, fmt.Errorf("check.http.timeout: must be a duration between 1s and 10m (got %q)", *timeout)
		}
		hc.Timeout = d
	}
	return hc, nil
}
