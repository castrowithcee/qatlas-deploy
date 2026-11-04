// Package stackcheck compares the stacks of a stacks-repo working copy with the standard: image versions as
// variables in .env, secrets only in an unversioned secrets.env, no data in the repository. It reads; it never
// fixes anything, and it never reads or reports a value from secrets.env, only the key names.
package stackcheck

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
	"github.com/castrowithcee/qatlas-deploy/internal/version"
)

// Severities. An error breaks or endangers a deploy or leaks secrets; a warning leaves the stack working.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Rule IDs. They are stable and may be matched by other tools.
const (
	RuleFixedImageTag       = "fixed-image-tag"
	RuleImageVersionDefault = "image-version-default"
	RuleFloatingImageTag    = "floating-image-tag"
	RuleUnsetImageVariable  = "unset-image-variable"
	RuleDuplicateKey        = "duplicate-key"
	RuleMissingSecretsEnv   = "missing-secrets-env"
	RuleUnreadableSecrets   = "unreadable-secrets-env"
	RuleTrackedSecretsEnv   = "tracked-secrets-env"
	RuleSecretsNotIgnored   = "secrets-env-not-ignored"
	RuleTrackedData         = "tracked-data"
	RuleMissingComposeFile  = "missing-compose-file"
	RuleInvalidComposeFile  = "invalid-compose-file"
	RuleInvalidRules        = "invalid-rules-file"
	RuleUnknownRulesVar     = "unknown-rules-variable"
	RuleUnreadableStack     = "unreadable-stack"
)

// Finding is one deviation from the standard.
type Finding struct {
	Stack    string
	Rule     string
	Severity string
	Cause    string
	Fix      string
}

// Git is what the check needs to know about the repository. Both calls only read.
type Git interface {
	// TrackedFiles lists the tracked files below the top-level folder dir, as paths from the repository root.
	TrackedFiles(ctx context.Context, dir string) ([]string, error)
	// IsIgnored reports whether the ignore rules cover path, a path from the repository root.
	IsIgnored(ctx context.Context, path string) (bool, error)
}

func isComplete(tag string) bool {
	_, ok := version.ParseCore(tag)
	return ok
}

// Check returns the findings for the given stacks, sorted by stack, severity, rule and cause. It fails only when
// git cannot answer; a stack that cannot be read is a finding.
func Check(ctx context.Context, list []stacks.Stack, git Git) ([]Finding, error) {
	var out []Finding
	for _, s := range list {
		fs, err := checkStack(ctx, s, git)
		if err != nil {
			return nil, err
		}
		out = append(out, fs...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.Stack != b.Stack:
			return a.Stack < b.Stack
		case a.Severity != b.Severity:
			return a.Severity == SeverityError
		case a.Rule != b.Rule:
			return a.Rule < b.Rule
		}
		return a.Cause < b.Cause
	})
	return out, nil
}

func checkStack(ctx context.Context, s stacks.Stack, git Git) ([]Finding, error) {
	var out []Finding
	add := func(rule, severity, cause, fix string) {
		out = append(out, Finding{Stack: s.Name, Rule: rule, Severity: severity, Cause: cause, Fix: fix})
	}
	out = append(out, imageFindings(s)...)
	out = append(out, errFindings(s)...)

	keys, exists, err := stacks.SecretKeys(s.Dir)
	switch {
	case err != nil:
		add(RuleUnreadableSecrets, SeverityError, err.Error(),
			"Make secrets.env a regular file inside the stack folder, not a link and not larger than "+
				fmt.Sprint(stacks.MaxFileSize)+" bytes.")
	case !exists:
		add(RuleMissingSecretsEnv, SeverityError, "secrets.env does not exist in the working copy",
			"Create secrets.env on the host with the stack's secrets (KEY=VALUE per line); Compose runs with --env-file secrets.env and refuses to start without it. Do not commit it.")
	}
	for _, k := range keys {
		if _, both := s.Env[k]; both {
			add(RuleDuplicateKey, SeverityError, fmt.Sprintf("key %s is set in both .env and secrets.env", k),
				fmt.Sprintf("Keep %s in exactly one file: secrets.env for a secret, .env for anything else.", k))
		}
	}

	tracked, err := git.TrackedFiles(ctx, s.Name)
	if err != nil {
		return nil, err
	}
	secretsPath, dataPrefix := s.Name+"/"+stacks.SecretsFile, s.Name+"/data/"
	var data []string
	for _, p := range tracked {
		switch {
		case p == secretsPath:
			add(RuleTrackedSecretsEnv, SeverityError, "secrets.env is tracked by git, so its secrets are in the history",
				"Run 'git rm --cached "+stacks.SecretsFile+"' in the stack folder, commit, add secrets.env to .gitignore, and rotate every secret that was committed.")
		case strings.HasPrefix(p, dataPrefix):
			data = append(data, strings.TrimPrefix(p, s.Name+"/"))
		}
	}
	if len(data) > 0 {
		add(RuleTrackedData, SeverityError,
			fmt.Sprintf("%d file(s) below data/ are tracked by git (for example %s)", len(data), data[0]),
			"Remove data/ from the repository with 'git rm -r --cached data', add it to .gitignore, and keep the data on the host only.")
	}
	ignored, err := git.IsIgnored(ctx, secretsPath)
	if err != nil {
		return nil, err
	}
	if !ignored {
		add(RuleSecretsNotIgnored, SeverityWarning, "secrets.env is not listed in .gitignore, so it can be committed by accident",
			"Add the line 'secrets.env' to the .gitignore of the repository or of the stack folder.")
	}
	return out, nil
}

func imageFindings(s stacks.Stack) []Finding {
	var out []Finding
	add := func(rule, severity, cause, fix string) {
		out = append(out, Finding{Stack: s.Name, Rule: rule, Severity: severity, Cause: cause, Fix: fix})
	}
	for _, f := range s.Findings {
		cause := fmt.Sprintf("service %s, image %s: %s", f.Service, f.Image, f.Detail)
		switch f.Kind {
		case stacks.FindingFixedTag:
			add(RuleFixedImageTag, SeverityError, cause,
				"Write the image as image: "+f.Image+":${SERVICE_VERSION} and set SERVICE_VERSION=<x.y.z> in .env.")
		case stacks.FindingFloatingTag:
			add(RuleFloatingImageTag, SeverityError, cause,
				"Set the version variable in .env to a release with a MAJOR.MINOR.PATCH core such as 1.2.3; do not use latest or other floating tags.")
		case stacks.FindingUnsetVariable:
			add(RuleUnsetImageVariable, SeverityError, cause,
				"Set the version variable in .env to a release with a MAJOR.MINOR.PATCH core such as 1.2.3.")
		}
	}
	floating := map[string]bool{}
	for _, f := range s.Findings {
		if f.Kind == stacks.FindingFloatingTag {
			floating[f.Service] = true
		}
	}
	for _, img := range s.Images {
		if img.Source == stacks.SourceUnset || img.Value == "" || floating[img.Service] {
			continue
		}
		if !isComplete(img.Value) {
			add(RuleFloatingImageTag, SeverityError,
				fmt.Sprintf("service %s, image %s: variable %s holds %q, which has no MAJOR.MINOR.PATCH core", img.Service, img.Image, img.Var, img.Value),
				"Set "+img.Var+" in .env to a release with a MAJOR.MINOR.PATCH core such as 1.2.3.")
		}
		if img.Source == stacks.SourceDefault {
			add(RuleImageVersionDefault, SeverityWarning,
				fmt.Sprintf("service %s, image %s: the version comes from a default in the compose file, not from .env", img.Service, img.Image),
				"Set "+img.Var+" in .env and drop the default from the image reference.")
		}
	}
	return out
}

// errFindings turns the reason a stack could not be read into findings.
func errFindings(s stacks.Stack) []Finding {
	if s.Err == nil {
		return nil
	}
	mk := func(rule, cause, fix string) Finding {
		return Finding{Stack: s.Name, Rule: rule, Severity: SeverityError, Cause: cause, Fix: fix}
	}
	cause := strings.TrimPrefix(s.Err.Error(), "stack "+s.Name+": ")
	var compose *stacks.ComposeFileError
	var rules *stacks.RulesError
	switch {
	case errors.As(s.Err, &compose):
		var out []Finding
		for _, p := range compose.Problems {
			if p.Missing {
				out = append(out, mk(RuleMissingComposeFile, fmt.Sprintf("COMPOSE_FILE in .env names %q, which does not exist in the stack folder", p.Entry),
					"Add the file, or remove it from COMPOSE_FILE in .env."))
				continue
			}
			out = append(out, mk(RuleInvalidComposeFile, fmt.Sprintf("COMPOSE_FILE in .env names %q: %s", p.Entry, p.Reason),
				"Name regular files inside the stack folder by relative path, without '..' and without links."))
		}
		return out
	case errors.As(s.Err, &rules) && rules.Variable != "":
		return []Finding{mk(RuleUnknownRulesVar, cause,
			"Use an image version variable of this stack under update.tags in qdeploy.yaml, or add the variable to the image reference.")}
	case errors.As(s.Err, &rules):
		return []Finding{mk(RuleInvalidRules, cause, "Correct qdeploy.yaml; 'qdeploy help stack-rules' lists the fields.")}
	}
	return []Finding{mk(RuleUnreadableStack, cause, "Correct the file named in the cause; the other rules cannot run on this stack before.")}
}
