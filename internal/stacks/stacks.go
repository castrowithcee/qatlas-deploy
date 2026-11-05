// Package stacks reads the stacks of a stacks-repo working copy: which stacks exist, which image variables
// their compose files use, and which update rules (qdeploy.yaml) they carry. It only reads; nothing from the
// working copy is executed, interpolated through a shell, or written back.
//
// The working copy is untrusted data. Paths from it never leave the stack folder, symlinked compose, .env and
// rule files are rejected rather than followed, and every file read is bounded.
package stacks

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
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
	// Name is the effective Compose project name and identifies the stack in state, backups, runs and the
	// command line. Until it is resolved, and for a stack that fails before, it is the folder name.
	Name string
	// NameDerived says Name is what Compose itself derives from the files and env files qdeploy passes, so the
	// command line needs no --project-name for it. It is false for a stack whose .env cannot be read.
	NameDerived bool
	// Folder is the directory name below the stacks repository root; every path and git pathspec uses it.
	Folder string
	// Dir is the stack folder inside the working copy.
	Dir string
	// ComposeFiles are the compose files in merge order, relative to Dir.
	ComposeFiles []string
	// BindMounts are the sorted host paths, relative to Dir, that a service of the compose files mounts as a bind
	// mount and that lie below Dir. A source with a variable, one outside Dir and Dir itself are left out.
	BindMounts []string
	// Env holds the values of .env; never of secrets.env.
	Env map[string]string
	// Images lists every service image whose tag is a variable.
	Images []ImageVar
	// Findings lists images the standard does not allow, for qdeploy stack check.
	Findings []Finding
	// Rules is nil when the stack has no qdeploy.yaml: it is deployed but never updated.
	Rules *Rules
	// EnvErr is set when .env cannot be parsed by qdeploy but the stack can still be deployed, because Compose
	// reads .env itself: the stack is deploy-only and never updated. Env then holds only the lines that parse,
	// and Images and Findings stay empty.
	EnvErr error
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

// ComposeFileProblem is one entry of the compose file list that cannot be used. Missing says the file does not exist;
// otherwise the entry is unsafe or not a regular file.
type ComposeFileProblem struct {
	Entry   string
	Missing bool
	Reason  string
}

// ComposeFileError is the Err of a stack whose file list names unusable files; it lists every one. FromRules
// says the list is compose.files of qdeploy.yaml, otherwise it is COMPOSE_FILE of .env.
type ComposeFileError struct {
	Problems  []ComposeFileProblem
	FromRules bool
}

func (e *ComposeFileError) Error() string {
	p := e.Problems[0]
	if e.FromRules {
		return fmt.Sprintf("%s: compose.files entry %q: %s", RulesFile, p.Entry, p.Reason)
	}
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
	owner := map[string]string{}
	for i := range stacks {
		st := &stacks[i]
		if first, taken := owner[st.Name]; taken {
			st.Err = fmt.Errorf("stack %s: the project name %q is already used by the folder %s", st.Name, st.Name, first)
			continue
		}
		owner[st.Name] = st.Folder
	}
	sort.Slice(stacks, func(i, j int) bool { return stacks[i].Name < stacks[j].Name })
	return stacks, nil
}

var defaultComposeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

func loadStack(dir, folder string) (Stack, bool) {
	s := Stack{Name: folder, Folder: folder, Dir: dir, Env: map[string]string{}}
	fail := func(err error) (Stack, bool) {
		s.Err = fmt.Errorf("stack %s: %w", s.Name, err)
		return s, true
	}

	env, risky, err := readEnv(dir)
	// qdeploy.yaml is read now because compose.files decides which files the stack has; its own error is
	// reported where it always was, after the compose files and images.
	rules, rulesErr := readRules(dir)
	var envParse *EnvParseError
	switch {
	case errors.As(err, &envParse) && !risky:
		s.EnvErr = err
	case err != nil:
		if hasAnyCompose(dir) || (rules != nil && rules.ComposeFiles != nil) {
			return fail(err)
		}
		return s, false
	}
	s.Env = env

	var listed []string
	if rulesErr == nil && rules != nil {
		listed = rules.ComposeFiles
	}
	files, err := composeFiles(dir, env, listed)
	if err != nil {
		return fail(err)
	}
	if len(files) == 0 {
		return s, false
	}
	s.ComposeFiles = files
	s.BindMounts = readBindMounts(dir, files)

	if s.EnvErr == nil {
		images, err := readImages(dir, files)
		if err != nil {
			return fail(err)
		}
		s.Images, s.Findings = resolveImages(images, env)
	}

	fileName, err := readComposeName(dir, files)
	if err != nil {
		return fail(err)
	}
	name, err := projectNameOf(folder, env, fileName)
	if err != nil {
		return fail(err)
	}
	s.Name, s.NameDerived = name, s.EnvErr == nil

	if rulesErr != nil {
		return fail(&RulesError{Err: rulesErr})
	}
	if rules != nil && s.EnvErr == nil {
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

// readComposeName returns the top-level name of the compose files as Compose merges them: the last file that
// sets a non-empty one wins. It is "" when no file sets one.
func readComposeName(dir string, files []string) (string, error) {
	name := ""
	for _, f := range files {
		data, err := readBounded(dir, f)
		if err != nil {
			return "", err
		}
		var doc struct {
			Name yaml.Node `yaml:"name"`
		}
		if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("%s: %w", f, err)
		}
		switch n := doc.Name; {
		case n.Kind == 0 || n.Tag == "!!null":
			continue
		case n.Kind != yaml.ScalarNode || n.Tag != "!!str":
			return "", fmt.Errorf("%s: name must be a string", f)
		case n.Value != "":
			name = n.Value
		}
	}
	return name, nil
}

// projectNameOf resolves the project name the way Compose does: COMPOSE_PROJECT_NAME from .env, else the name
// of the compose files, else the folder name. Compose normalizes the last two and rejects an invalid
// COMPOSE_PROJECT_NAME instead of normalizing it.
func projectNameOf(folder string, env map[string]string, fileName string) (string, error) {
	if v := env["COMPOSE_PROJECT_NAME"]; v != "" {
		if !validProjectName.MatchString(v) {
			return "", fmt.Errorf("%s: COMPOSE_PROJECT_NAME is not a valid project name (lowercase letters, digits, - and _, starting with a letter or digit)", envFile)
		}
		return v, nil
	}
	if fileName != "" {
		if strings.Contains(fileName, "$") {
			return "", errors.New("the compose file's name uses variable interpolation, which qdeploy does not resolve; set COMPOSE_PROJECT_NAME in .env or write the name literally")
		}
		if name := normalizeProjectName(fileName); name != "" {
			return name, nil
		}
		return "", errors.New("the compose file's name has no usable character for a project name")
	}
	if name := normalizeProjectName(folder); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("the folder name %q yields no valid project name; set COMPOSE_PROJECT_NAME in .env or name in the compose file", folder)
}

var validProjectName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// normalizeProjectName is Compose's normalization: lowercase, characters outside [a-z0-9_-] dropped, leading
// '-' and '_' removed.
func normalizeProjectName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return strings.TrimLeft(b.String(), "_-")
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

// composeFiles applies listed (compose.files of qdeploy.yaml), else COMPOSE_FILE from .env, else Compose's
// default lookup: the first default name that exists, plus the override file of the same extension.
func composeFiles(dir string, env map[string]string, listed []string) ([]string, error) {
	if listed != nil {
		var problems []ComposeFileProblem
		for _, p := range listed {
			if err := checkRegular(dir, p); err != nil {
				problems = append(problems, ComposeFileProblem{Entry: p, Missing: errors.Is(err, errNotFound), Reason: err.Error()})
			}
		}
		if len(problems) > 0 {
			return nil, &ComposeFileError{Problems: problems, FromRules: true}
		}
		return listed, nil
	}
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
