// Package stackcheck reports per stack of a stacks-repo working copy which functions of qdeploy work with the
// stack's files, which do not, why, and what a person would change, together with the findings about those
// files. It reads; it never changes a file, and it never reads or reports a value from secrets.env, only the
// key names.
package stackcheck

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/castrowithcee/qatlas-deploy/internal/backup"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
	"github.com/castrowithcee/qatlas-deploy/internal/version"
)

// Severities. An error is a stack that cannot be deployed or a secret that would reach a remote; everything
// else is a warning.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Rule IDs. They are stable and may be matched by other tools.
const (
	RuleUnsetImageVariable = "unset-image-variable"
	RuleDuplicateKey       = "duplicate-key"
	RuleUnreadableSecrets  = "unreadable-secrets-env"
	RuleTrackedSecretsEnv  = "tracked-secrets-env"
	RuleSecretsNotIgnored  = "secrets-env-not-ignored"
	RuleTrackedData        = "tracked-data"
	RuleMissingComposeFile = "missing-compose-file"
	RuleInvalidComposeFile = "invalid-compose-file"
	RuleInvalidRules       = "invalid-rules-file"
	RuleUnknownRulesVar    = "unknown-rules-variable"
	RuleOldRulesFile       = "old-rules-file"
	RuleUnreadableStack    = "unreadable-stack"
	RuleIgnoredFolder      = "ignored-stack-folder"
)

// Functions, in the order of a report.
const (
	FuncDeploy   = "deploy"
	FuncUpdate   = "update"
	FuncRollback = "rollback"
	FuncBackup   = "backup"
	FuncRedact   = "redact"
)

// Finding is one problem of a stack's files.
type Finding struct {
	Stack    string
	Rule     string
	Severity string
	Cause    string
	Fix      string
}

// Function says whether one function of qdeploy works with the stack's files. Mode is the update mode, set for
// FuncUpdate only; Action is empty when there is nothing to change.
type Function struct {
	Name   string
	Works  bool
	Mode   string
	Reason string
	Action string
}

// Report is the answer for one stack: its functions in the order deploy, update, rollback, backup, redact, and
// its findings sorted by severity, rule and cause.
type Report struct {
	Stack     string
	Functions []Function
	Findings  []Finding
}

// Git is what the check needs to know about the repository. Both calls only read.
type Git interface {
	// TrackedFiles lists the tracked files below the top-level folder dir, or of the whole repository for
	// stacks.RootFolder, as paths from the repository root.
	TrackedFiles(ctx context.Context, dir string) ([]string, error)
	// IsIgnored reports whether the ignore rules cover path, a path from the repository root.
	IsIgnored(ctx context.Context, path string) (bool, error)
}

func isComplete(tag string) bool {
	_, ok := version.ParseCore(tag)
	return ok
}

// Check returns one report per stack, in the order of list. remote says the repository pushes to a remote, so a
// versioned secret leaves the host. It fails only when git cannot answer; a stack that cannot be read is a
// finding.
func Check(ctx context.Context, list []stacks.Stack, git Git, remote bool) ([]Report, error) {
	out := make([]Report, 0, len(list))
	for _, s := range list {
		r, err := checkStack(ctx, s, git, remote)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func checkStack(ctx context.Context, s stacks.Stack, git Git, remote bool) (Report, error) {
	var out []Finding
	add := func(rule, severity, cause, fix string) {
		out = append(out, Finding{Stack: s.Name, Rule: rule, Severity: severity, Cause: cause, Fix: fix})
	}
	out = append(out, errFindings(s)...)
	if s.OldRulesFile {
		add(RuleOldRulesFile, SeverityWarning,
			stacks.OldRulesFile+" in the stack folder is no longer read; the stack has "+settingsState(s),
			"Set the stack settings in the qdeploy TUI or with 'qdeploy stack settings set "+s.Name+" --file <file>', then delete "+
				stacks.OldRulesFile+" from the stack folder.")
	}
	for _, f := range s.Ignored {
		add(RuleIgnoredFolder, SeverityWarning,
			"the folder "+f+" holds a compose file but is not a stack of its own: the root of the stack folder holds a compose file, so the whole folder is one stack",
			"To deploy "+f+" as a stack of its own, move the compose files of the root into a subfolder; otherwise nothing needs to change.")
	}

	keys, secretsExist, secretsErr := stacks.SecretKeys(s.Dir)
	if secretsErr != nil {
		add(RuleUnreadableSecrets, SeverityError, secretsErr.Error(), fixSecretsFile)
	}
	for _, k := range keys {
		if _, both := s.Env[k]; both {
			add(RuleDuplicateKey, SeverityWarning, fmt.Sprintf("key %s is set in both .env and secrets.env", k),
				fmt.Sprintf("Keep %s in exactly one file: secrets.env for a secret, .env for anything else.", k))
		}
	}
	// A version variable without a value leaves the image reference with an empty tag, which Compose refuses,
	// unless secrets.env sets it.
	for _, img := range s.Images {
		if img.Source == stacks.SourceUnset && !slices.Contains(keys, img.Var) {
			add(RuleUnsetImageVariable, SeverityError,
				fmt.Sprintf("service %s, image %s: variable %s has no value in .env and no default, so the image has an empty tag", img.Service, img.Image, img.Var),
				"Set "+img.Var+" in .env to a release with a MAJOR.MINOR.PATCH core such as 1.2.3.")
		}
	}

	tracked, err := git.TrackedFiles(ctx, s.Folder)
	if err != nil {
		return Report{}, err
	}
	secretsPath, dataPrefix := s.RepoPath(stacks.SecretsFile), s.RepoPath("data")+"/"
	var data []string
	for _, p := range tracked {
		switch {
		case p == secretsPath:
			severity, cause := SeverityWarning, "secrets.env is tracked by git; the repository has no remote, so the secrets stay on this host"
			if remote {
				severity, cause = SeverityError, "secrets.env is tracked by git, so its secrets reach the remote"
			}
			add(RuleTrackedSecretsEnv, severity, cause,
				"Run 'git rm --cached "+stacks.SecretsFile+"' in the stack folder, commit, add secrets.env to .gitignore, and rotate every secret that was committed.")
		case strings.HasPrefix(p, dataPrefix):
			data = append(data, "data/"+strings.TrimPrefix(p, dataPrefix))
		}
	}
	if len(data) > 0 {
		add(RuleTrackedData, SeverityWarning,
			fmt.Sprintf("%d file(s) below data/ are tracked by git (for example %s)", len(data), data[0]),
			"Remove data/ from the repository with 'git rm -r --cached data', add it to .gitignore, and keep the data on the host only.")
	}
	ignored, err := git.IsIgnored(ctx, secretsPath)
	if err != nil {
		return Report{}, err
	}
	if !ignored {
		add(RuleSecretsNotIgnored, SeverityWarning, "secrets.env is not listed in .gitignore, so it can be committed by accident",
			"Add the line 'secrets.env' to the .gitignore of the repository or of the stack folder.")
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.Severity != b.Severity:
			return a.Severity == SeverityError
		case a.Rule != b.Rule:
			return a.Rule < b.Rule
		}
		return a.Cause < b.Cause
	})

	deploy := deployFunction(s, out)
	bk := backupFunction(s)
	return Report{Stack: s.Name, Findings: out, Functions: []Function{
		deploy,
		updateFunction(s, deploy.Works, keys),
		rollbackFunction(deploy.Works, bk),
		bk,
		redactFunction(s, len(keys), secretsExist, secretsErr),
	}}, nil
}

const (
	fixDeployFirst     = "Resolve the deploy problem first."
	fixVersionVariable = "Write the image as image: <name>:${SERVICE_VERSION} and set SERVICE_VERSION=<x.y.z> in .env."
)

var fixSecretsFile = "Make secrets.env a regular file inside the stack folder, not a link and not larger than " +
	fmt.Sprint(stacks.MaxFileSize) + " bytes."

// deployFunction: every error finding keeps the stack from being deployed, except a versioned secrets.env.
func deployFunction(s stacks.Stack, findings []Finding) Function {
	var blockers []Finding
	for _, f := range findings {
		if f.Severity == SeverityError && f.Rule != RuleTrackedSecretsEnv {
			blockers = append(blockers, f)
		}
	}
	if len(blockers) > 0 {
		reason := blockers[0].Cause
		if len(blockers) > 1 {
			reason += fmt.Sprintf(" (%d more error(s) in the findings)", len(blockers)-1)
		}
		return Function{Name: FuncDeploy, Reason: reason, Action: blockers[0].Fix}
	}
	reason := "Compose starts the stack from " + strings.Join(s.ComposeFiles, ", ")
	if s.EnvErr != nil {
		reason += "; qdeploy cannot read .env, Compose reads it itself"
	}
	return Function{Name: FuncDeploy, Works: true, Reason: reason}
}

// problem is why an image is not updated, and what a person would change.
type problem struct{ cause, fix string }

func updateFunction(s stacks.Stack, deployOK bool, secretKeys []string) Function {
	f := Function{Name: FuncUpdate, Mode: s.UpdateMode()}
	switch {
	case !deployOK:
		f.Reason, f.Action = "the stack cannot be deployed", fixDeployFirst
		return f
	case s.EnvErr != nil:
		f.Reason = "qdeploy cannot read .env, so it cannot change the image versions: " + s.EnvErr.Error()
		f.Action = "Correct .env (KEY=VALUE per line)."
		return f
	}

	// skipped are images the update leaves alone; blocked stops the update of the whole stack.
	var skipped []problem
	var blocked *problem
	floating := map[string]bool{}
	for _, fd := range s.Findings {
		cause := fmt.Sprintf("service %s, image %s: %s", fd.Service, fd.Image, fd.Detail)
		switch fd.Kind {
		case stacks.FindingFixedTag:
			skipped = append(skipped, problem{cause, fixVersionVariable})
		case stacks.FindingFloatingTag:
			floating[fd.Service] = true
			skipped = append(skipped, problem{cause + ", so there is no version to compare",
				"Set the version in .env to a release with a MAJOR.MINOR.PATCH core such as 1.2.3; do not use latest or other floating tags."})
		}
	}
	imageOf := map[string]string{}
	var vars []string
	for _, img := range s.Images {
		if first, seen := imageOf[img.Var]; !seen {
			imageOf[img.Var] = img.Image
		} else if first != img.Image && blocked == nil {
			blocked = &problem{fmt.Sprintf("variable %s is the tag of more than one image", img.Var),
				"Give every image a version variable of its own in the compose file and in .env."}
		}
		switch {
		case img.Source == stacks.SourceUnset:
			if slices.Contains(secretKeys, img.Var) {
				skipped = append(skipped, problem{fmt.Sprintf("service %s, image %s: variable %s is set in secrets.env, not in .env", img.Service, img.Image, img.Var),
					"Move " + img.Var + " from secrets.env to .env."})
			}
		case floating[img.Service]:
		case img.Source == stacks.SourceDefault:
			if blocked == nil {
				blocked = &problem{fmt.Sprintf("service %s, image %s: the version of %s comes from a default in the compose file, not from .env", img.Service, img.Image, img.Var),
					"Set " + img.Var + " in .env and drop the default from the image reference."}
			}
		case !isComplete(img.Value):
			skipped = append(skipped, problem{fmt.Sprintf("service %s, image %s: variable %s holds %q, which has no MAJOR.MINOR.PATCH core", img.Service, img.Image, img.Var, img.Value),
				"Set " + img.Var + " in .env to a release with a MAJOR.MINOR.PATCH core such as 1.2.3."})
		case !slices.Contains(vars, img.Var):
			vars = append(vars, img.Var)
		}
	}

	names := strings.Join(vars, ", ")
	switch {
	case blocked != nil:
		f.Reason, f.Action = blocked.cause, blocked.fix
		return f
	case len(vars) == 0 && len(skipped) > 0:
		f.Reason, f.Action = skipped[0].cause+": deploy only", skipped[0].fix
		if len(skipped) > 1 {
			f.Reason += fmt.Sprintf(" (%d more image(s) alike)", len(skipped)-1)
		}
		return f
	case len(vars) == 0:
		f.Reason, f.Action = "no image takes its version from a variable in .env: deploy only", fixVersionVariable
		return f
	case f.Mode == stacks.ModeNone:
		f.Reason = "the image versions (" + names + ") can be updated, but updates are not switched on"
		f.Action = "Switch on updates in the stack's settings in the qdeploy TUI, or with 'qdeploy stack settings set " + s.Name + " --file <file>'."
		return f
	case f.Mode == stacks.ModeAutomatic:
		f.Works, f.Reason = true, "the maintenance window updates "+names+" by itself"
	default:
		f.Works, f.Reason = true, "the maintenance window reports new versions of "+names+"; a person starts the update in the qdeploy TUI or with 'qdeploy stack update "+s.Name+"'"
	}
	if len(skipped) > 0 {
		f.Reason += "; not updated: " + skipped[0].cause
		if len(skipped) > 1 {
			f.Reason += fmt.Sprintf(" (%d more image(s))", len(skipped)-1)
		}
		f.Action = skipped[0].fix
	}
	return f
}

// rollbackFunction is the rollback with data: a failed run always goes back to the previous commit, and
// restores the data only from the backup the run made.
func rollbackFunction(deployOK bool, bk Function) Function {
	f := Function{Name: FuncRollback}
	switch {
	case !deployOK:
		f.Reason, f.Action = "the stack cannot be deployed", fixDeployFirst
	case !bk.Works:
		f.Reason = "a failed run goes back to the previous commit, but leaves the data as the failed run changed it: " + bk.Reason
		f.Action = bk.Action
	default:
		f.Works = true
		f.Reason = "a failed run goes back to the previous commit and restores the backup it made before the change"
	}
	return f
}

// dataRoot is the only folder below which a backup saves data directories.
const dataRoot = "data"

func backupFunction(s stacks.Stack) Function {
	f := Function{Name: FuncBackup}
	settings := "in the qdeploy TUI or with 'qdeploy stack settings set " + s.Name + " --file <file>'"
	switch {
	case s.Err != nil:
		f.Reason, f.Action = "the stack cannot be read", fixDeployFirst
		return f
	case s.Rules == nil || s.Rules.Backup == nil:
		f.Reason = "the stack settings have no backup section"
		f.Action = "Add backup.paths, backup.volumes or backup.command to the stack settings " + settings + " (see qdeploy help backups)."
		return f
	case !backup.ValidStack(s.Name):
		f.Reason = "the stack name " + s.Name + " cannot be used for backups: it needs lowercase letters, digits, - and _ and at most 63 characters"
		f.Action = "Name the stack so with COMPOSE_PROJECT_NAME in .env or name: in the compose file."
		return f
	}
	b := s.Rules.Backup
	for _, p := range b.Paths {
		if p != dataRoot && !strings.HasPrefix(p, dataRoot+"/") {
			f.Reason = fmt.Sprintf("backup path %s is not below %s/, and a backup saves only data directories", p, dataRoot)
			f.Action = "List only data directories below " + dataRoot + "/ in backup.paths of the stack settings " + settings + "."
			return f
		}
	}
	var parts []string
	if len(b.Paths) > 0 {
		parts = append(parts, "the data directories "+strings.Join(b.Paths, ", "))
	}
	if len(b.Volumes) > 0 {
		parts = append(parts, "the volumes "+strings.Join(b.Volumes, ", "))
	}
	if len(b.Command) > 0 {
		parts = append(parts, "the output of the backup command")
	}
	f.Works, f.Reason = true, "a backup saves "+strings.Join(parts, " and ")
	return f
}

// redactFunction: the service redacts the values of secrets.env and of the .env keys named like a secret.
func redactFunction(s stacks.Stack, secretKeys int, secretsExist bool, secretsErr error) Function {
	f := Function{Name: FuncRedact}
	var envKeys []string
	for k := range s.Env {
		if stacks.SecretLikeKey(k) {
			envKeys = append(envKeys, k)
		}
	}
	sort.Strings(envKeys)
	keepInSecrets := "Keep the secrets in secrets.env: set them in the stack detail of the qdeploy TUI or with 'qdeploy stack secret set " + s.Name + " <KEY>'."
	switch {
	case secretsErr != nil:
		f.Reason = "secrets.env cannot be read, so its values are not redacted: " + secretsErr.Error()
		f.Action = fixSecretsFile
	case secretsExist:
		f.Works = true
		f.Reason = fmt.Sprintf("the values of secrets.env (%d key(s))", secretKeys)
		if len(envKeys) > 0 {
			f.Reason += " and of the .env keys named like a secret (" + strings.Join(envKeys, ", ") + ")"
		}
		f.Reason += " are redacted in logs and messages"
	case len(envKeys) > 0:
		f.Works = true
		f.Reason = "the stack has no secrets.env; the values of the .env keys named like a secret (" + strings.Join(envKeys, ", ") +
			") are redacted, other .env values are not"
		f.Action = keepInSecrets
	default:
		f.Reason = "the stack has no secrets.env and no .env key named like a secret (PASSWORD, SECRET, TOKEN, KEY), so no value is redacted"
		f.Action = "If the stack has secrets: " + keepInSecrets
	}
	return f
}

// settingsState says which settings the stack has instead of the old file.
func settingsState(s stacks.Stack) string {
	if s.Rules == nil {
		return "no settings and is not updated"
	}
	return "settings of its own, update mode " + s.UpdateMode()
}

// errFindings turns the reason a stack could not be read into findings. Only an .env qdeploy cannot parse leaves
// the stack deployable, so that is a warning; everything else is an error.
func errFindings(s stacks.Stack) []Finding {
	if s.Err == nil && s.EnvErr != nil {
		return []Finding{{Stack: s.Name, Rule: RuleUnreadableStack, Severity: SeverityWarning, Cause: s.EnvErr.Error(),
			Fix: "Correct .env (KEY=VALUE per line); the stack is deployed, but not updated, until it parses."}}
	}
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
		list, where := "COMPOSE_FILE", ".env"
		if compose.FromRules {
			list, where = "compose.files", "the stack settings"
		}
		var out []Finding
		for _, p := range compose.Problems {
			if p.Missing {
				out = append(out, mk(RuleMissingComposeFile, fmt.Sprintf("%s in %s names %q, which does not exist in the stack folder", list, where, p.Entry),
					fmt.Sprintf("Add the file, or remove it from %s in %s.", list, where)))
				continue
			}
			out = append(out, mk(RuleInvalidComposeFile, fmt.Sprintf("%s in %s names %q: %s", list, where, p.Entry, p.Reason),
				"Name regular files inside the stack folder by relative path, without '..' and without links."))
		}
		return out
	case errors.As(s.Err, &rules) && rules.Variable != "":
		return []Finding{mk(RuleUnknownRulesVar, cause,
			"Name an image version variable of this stack in the stack settings ('qdeploy stack settings set'), or add the variable to the image reference.")}
	case errors.As(s.Err, &rules):
		return []Finding{mk(RuleInvalidRules, cause, "Correct the stack settings in the qdeploy TUI or with 'qdeploy stack settings set'; 'qdeploy help stack-settings' lists the fields.")}
	}
	return []Finding{mk(RuleUnreadableStack, cause, "Correct the file named in the cause; the other checks cannot run on this stack before.")}
}
