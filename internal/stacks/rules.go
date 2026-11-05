package stacks

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/castrowithcee/qatlas-deploy/internal/changelog"
)

// Update modes, ceilings and major strategies.
const (
	ModeNone      = "none" // no stack settings: deployed, never updated
	ModeManual    = "manual"
	ModeAutomatic = "automatic"

	CeilingPatch = "patch"
	CeilingMinor = "minor"
	CeilingMajor = "major"

	MajorDirect   = "direct"
	MajorStepwise = "stepwise"
)

// Rules are the validated settings of a stack with defaults applied.
type Rules struct {
	Update Update
	Backup *Backup // nil: no backup configured
	Check  Check
	// ComposeFiles are the compose files in merge order from compose.files; nil leaves the choice to
	// COMPOSE_FILE and the default names.
	ComposeFiles []string
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

// Settings is the file form of a stack's settings: the schema of .qatlas/qdeploy/<stack>.yaml and of the
// settings that travel over the service channel. A nil member is absent; ParseSettings validates it and
// applies the defaults.
type Settings struct {
	Version *int             `yaml:"version,omitempty" json:"version,omitempty"`
	Compose *ComposeSettings `yaml:"compose,omitempty" json:"compose,omitempty"`
	Update  *UpdateSettings  `yaml:"update,omitempty" json:"update,omitempty"`
	Backup  *BackupSettings  `yaml:"backup,omitempty" json:"backup,omitempty"`
	Check   *CheckSettings   `yaml:"check,omitempty" json:"check,omitempty"`
}

// ComposeSettings is the compose section of Settings.
type ComposeSettings struct {
	Files []string `yaml:"files,omitempty" json:"files,omitempty"`
}

// UpdateSettings is the update section of Settings.
type UpdateSettings struct {
	Mode      *string           `yaml:"mode,omitempty" json:"mode,omitempty"`
	Ceiling   *string           `yaml:"ceiling,omitempty" json:"ceiling,omitempty"`
	Major     *string           `yaml:"major,omitempty" json:"major,omitempty"`
	Tags      map[string]string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Changelog map[string]string `yaml:"changelog,omitempty" json:"changelog,omitempty"`
}

// BackupSettings is the backup section of Settings.
type BackupSettings struct {
	Paths     []string           `yaml:"paths,omitempty" json:"paths,omitempty"`
	Volumes   []string           `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	Command   []string           `yaml:"command,omitempty" json:"command,omitempty"`
	Retention *RetentionSettings `yaml:"retention,omitempty" json:"retention,omitempty"`
}

// RetentionSettings is the retention of BackupSettings.
type RetentionSettings struct {
	Keep       *int `yaml:"keep,omitempty" json:"keep,omitempty"`
	MaxAgeDays *int `yaml:"max_age_days,omitempty" json:"max_age_days,omitempty"`
}

// CheckSettings is the check section of Settings.
type CheckSettings struct {
	Containers *bool         `yaml:"containers,omitempty" json:"containers,omitempty"`
	HTTP       *HTTPSettings `yaml:"http,omitempty" json:"http,omitempty"`
}

// HTTPSettings is the HTTP probe of CheckSettings.
type HTTPSettings struct {
	URL     string  `yaml:"url" json:"url"`
	Status  *int    `yaml:"status,omitempty" json:"status,omitempty"`
	Timeout *string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

const (
	defaultKeep    = 7
	defaultTimeout = 10 * time.Second
)

var (
	volumeName   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	unknownField = regexp.MustCompile(`field (\S+) not found in type \S+`)
)

// settingsLabel starts every error about the settings of a stack.
const settingsLabel = "stack settings"

// readSettings loads the settings of the stack called name from the working copy at root; nil, nil when the
// stack has none.
func readSettings(root, name string) (*Rules, error) {
	data, err := readBounded(root, SettingsPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", settingsLabel, err)
	}
	return ParseSettings(data)
}

// ParseSettings decodes and validates settings given as YAML or JSON, which is YAML too: one document, no
// unknown field, no repeated key. Errors start with "stack settings" and name the field path. The checks
// that need the stack itself, such as image variables and compose files, are made when the stack is read.
func ParseSettings(data []byte) (*Rules, error) {
	var raw Settings
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: empty; at least 'version: 1' is required", settingsLabel)
		}
		msg := unknownField.ReplaceAllString(err.Error(), `unknown field $1`)
		msg = strings.TrimPrefix(msg, "yaml: ")
		return nil, fmt.Errorf("%s: %s", settingsLabel, strings.ReplaceAll(msg, "\n", "; "))
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: only one YAML document is allowed", settingsLabel)
	}
	rules, err := validateRules(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", settingsLabel, err)
	}
	return rules, nil
}

// Settings returns the rules in their file form with every default written out, so that equal rules give
// equal settings and ParseSettings gives the rules back.
func (r *Rules) Settings() Settings {
	one := 1
	s := Settings{Version: &one, Update: &UpdateSettings{
		Mode: ptr(r.Update.Mode), Ceiling: ptr(r.Update.Ceiling), Major: ptr(r.Update.Major),
	}}
	if r.ComposeFiles != nil {
		s.Compose = &ComposeSettings{Files: slices.Clone(r.ComposeFiles)}
	}
	for k, re := range r.Update.Tags {
		if s.Update.Tags == nil {
			s.Update.Tags = map[string]string{}
		}
		s.Update.Tags[k] = re.String()
	}
	if len(r.Update.Changelog) > 0 {
		s.Update.Changelog = maps.Clone(r.Update.Changelog)
	}
	if b := r.Backup; b != nil {
		bs := &BackupSettings{Paths: slices.Clone(b.Paths), Volumes: slices.Clone(b.Volumes), Command: slices.Clone(b.Command),
			Retention: &RetentionSettings{}}
		if b.Keep > 0 {
			bs.Retention.Keep = ptr(b.Keep)
		}
		if b.MaxAgeDays > 0 {
			bs.Retention.MaxAgeDays = ptr(b.MaxAgeDays)
		}
		s.Backup = bs
	}
	s.Check = &CheckSettings{Containers: ptr(r.Check.Containers)}
	if h := r.Check.HTTP; h != nil {
		s.Check.HTTP = &HTTPSettings{URL: h.URL, Status: ptr(h.Status), Timeout: ptr(h.Timeout.String())}
	}
	return s
}

// EncodeSettings returns the file content of the rules: r.Settings() as YAML in block form, indented by two
// spaces. Equal rules give equal bytes.
func EncodeSettings(r *Rules) ([]byte, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(r.Settings()); err != nil {
		return nil, fmt.Errorf("encode the stack settings: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode the stack settings: %w", err)
	}
	return b.Bytes(), nil
}

func ptr[T any](v T) *T { return &v }

func oneOf(path, got string, allowed ...string) error {
	for _, a := range allowed {
		if got == a {
			return nil
		}
	}
	return fmt.Errorf("%s: must be one of %s (got %q)", path, strings.Join(allowed, ", "), got)
}

func validateRules(raw Settings) (*Rules, error) {
	if raw.Version == nil {
		return nil, errors.New("version: required (use 1)")
	}
	if *raw.Version != 1 {
		return nil, fmt.Errorf("version: unsupported value %d (use 1)", *raw.Version)
	}
	r := &Rules{Update: Update{Mode: ModeManual, Ceiling: CeilingMinor, Major: MajorStepwise}, Check: Check{Containers: true}}

	if c := raw.Compose; c != nil {
		if len(c.Files) == 0 {
			return nil, errors.New("compose.files: needs at least one file")
		}
		if len(c.Files) > MaxComposeFiles {
			return nil, fmt.Errorf("compose.files: more than %d files", MaxComposeFiles)
		}
		seen := map[string]bool{}
		for i, f := range c.Files {
			if err := checkRelative(f); err != nil {
				return nil, fmt.Errorf("compose.files[%d]: %w", i, err)
			}
			if strings.HasPrefix(f, "-") {
				return nil, fmt.Errorf("compose.files[%d]: a file name must not start with '-'", i)
			}
			if seen[f] {
				return nil, fmt.Errorf("compose.files[%d]: %q is listed twice", i, f)
			}
			seen[f] = true
		}
		r.ComposeFiles = c.Files
	}

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
