// Package stacks reads the stacks of a stacks-repo working copy: which stacks exist, which image variables
// their compose files use, and which update rules (qdeploy.yaml) they carry. It only reads; nothing from the
// working copy is executed, interpolated through a shell, or written back.
//
// The working copy is untrusted data. Paths from it never leave the stack folder, symlinked compose, .env and
// rule files are rejected rather than followed, and every file read is bounded.
package stacks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// MaxFileSize bounds every file read from the working copy.
	MaxFileSize = 1 << 20
	// MaxStacks bounds the number of stack folders.
	MaxStacks = 256
	// MaxComposeFiles bounds the compose files of one stack.
	MaxComposeFiles = 16

	// RulesFile is the rule file of a stack.
	RulesFile = "qdeploy.yaml"
	envFile   = ".env"
)

// Stack is one top-level folder of the working copy that holds a compose file.
type Stack struct {
	Name string
	// Dir is the stack folder inside the working copy.
	Dir string
	// ComposeFiles are the compose files in merge order, relative to Dir.
	ComposeFiles []string
	// Env holds the values of .env; never of secrets.env.
	Env map[string]string
	// Images lists every service image whose tag is a variable.
	Images []ImageVar
	// Findings lists images the standard does not allow, for qdeploy stack check.
	Findings []Finding
	// Rules is nil when the stack has no qdeploy.yaml: it is deployed but never updated.
	Rules *Rules
	// Err is set when the stack cannot be read; the other fields are then incomplete. One broken stack does not
	// hide the others.
	Err error
}

// RulesError is the Err of a stack whose qdeploy.yaml cannot be used. Variable is set when the file names an
// image variable the stack does not have.
type RulesError struct {
	Err      error
	Variable string
}

func (e *RulesError) Error() string { return e.Err.Error() }
func (e *RulesError) Unwrap() error { return e.Err }

// ComposeFileProblem is one entry of COMPOSE_FILE that cannot be used. Missing says the file does not exist;
// otherwise the entry is unsafe or not a regular file.
type ComposeFileProblem struct {
	Entry   string
	Missing bool
	Reason  string
}

// ComposeFileError is the Err of a stack whose COMPOSE_FILE names unusable files; it lists every one.
type ComposeFileError struct {
	Problems []ComposeFileProblem
}

func (e *ComposeFileError) Error() string {
	p := e.Problems[0]
	return fmt.Sprintf("%s: COMPOSE_FILE entry %q: %s", envFile, p.Entry, p.Reason)
}

// UpdateMode reports how the stack is updated: ModeNone without rules.
func (s Stack) UpdateMode() string {
	if s.Rules == nil {
		return ModeNone
	}
	return s.Rules.Update.Mode
}

// Load reads every stack below dir. Hidden folders, symlinked folders and folders without a compose file are
// skipped. It fails only when dir itself cannot be read or holds more than MaxStacks stack folders; a stack
// that cannot be read comes back with Err set.
func Load(dir string) ([]Stack, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read stacks directory: %w", err)
	}
	var stacks []Stack
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || !e.IsDir() {
			continue // e.IsDir is false for symlinks: a link is never followed out of the working copy
		}
		s, ok := loadStack(filepath.Join(dir, name), name)
		if !ok {
			continue
		}
		if len(stacks) == MaxStacks {
			return nil, fmt.Errorf("more than %d stacks in %s", MaxStacks, dir)
		}
		stacks = append(stacks, s)
	}
	sort.Slice(stacks, func(i, j int) bool { return stacks[i].Name < stacks[j].Name })
	return stacks, nil
}

var defaultComposeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

func loadStack(dir, name string) (Stack, bool) {
	s := Stack{Name: name, Dir: dir, Env: map[string]string{}}
	fail := func(err error) (Stack, bool) {
		s.Err = fmt.Errorf("stack %s: %w", name, err)
		return s, true
	}

	env, err := readEnv(dir)
	if err != nil {
		if hasAnyCompose(dir) {
			return fail(err)
		}
		return s, false
	}
	s.Env = env

	files, err := composeFiles(dir, env)
	if err != nil {
		return fail(err)
	}
	if len(files) == 0 {
		return s, false
	}
	s.ComposeFiles = files

	images, err := readImages(dir, files)
	if err != nil {
		return fail(err)
	}
	s.Images, s.Findings = resolveImages(images, env)

	rules, err := readRules(dir)
	if err != nil {
		return fail(&RulesError{Err: err})
	}
	if rules != nil {
		known := map[string]bool{}
		for _, img := range s.Images {
			known[img.Var] = true
		}
		for _, field := range []struct {
			name string
			vars []string
		}{
			{"update.tags", keysOf(rules.Update.Tags)},
			{"update.changelog", keysOf(rules.Update.Changelog)},
		} {
			sort.Strings(field.vars)
			for _, v := range field.vars {
				if !known[v] {
					return fail(&RulesError{Variable: v,
						Err: fmt.Errorf("%s: %s.%s: no service image uses this variable as its tag", RulesFile, field.name, v)})
				}
			}
		}
	}
	s.Rules = rules
	return s, true
}

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func hasAnyCompose(dir string) bool {
	for _, n := range defaultComposeNames {
		if _, err := os.Lstat(filepath.Join(dir, n)); err == nil {
			return true
		}
	}
	return false
}

var composeSeparator = regexp.MustCompile(`^[^\x00-\x1f]$`)

// composeFiles applies COMPOSE_FILE from .env, or Compose's default lookup: the first default name that exists,
// plus the override file of the same extension.
func composeFiles(dir string, env map[string]string) ([]string, error) {
	if list, ok := env["COMPOSE_FILE"]; ok && list != "" {
		sep := ":"
		if v, ok := env["COMPOSE_PATH_SEPARATOR"]; ok && v != "" {
			if !composeSeparator.MatchString(v) {
				return nil, fmt.Errorf("%s: COMPOSE_PATH_SEPARATOR must be a single character", envFile)
			}
			sep = v
		}
		parts := strings.Split(list, sep)
		if len(parts) > MaxComposeFiles {
			return nil, fmt.Errorf("%s: COMPOSE_FILE names more than %d files", envFile, MaxComposeFiles)
		}
		var problems []ComposeFileProblem
		for _, p := range parts {
			if err := checkRegular(dir, p); err != nil {
				problems = append(problems, ComposeFileProblem{Entry: p, Missing: errors.Is(err, errNotFound), Reason: err.Error()})
			}
		}
		if len(problems) > 0 {
			return nil, &ComposeFileError{Problems: problems}
		}
		return parts, nil
	}
	for _, base := range defaultComposeNames {
		info, err := os.Lstat(filepath.Join(dir, base))
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s: not a regular file", base)
		}
		files := []string{base}
		ext := filepath.Ext(base)
		override := strings.TrimSuffix(base, ext) + ".override" + ext
		if _, err := os.Lstat(filepath.Join(dir, override)); err == nil {
			if err := checkRegular(dir, override); err != nil {
				return nil, fmt.Errorf("%s: %w", override, err)
			}
			files = append(files, override)
		}
		return files, nil
	}
	return nil, nil
}
