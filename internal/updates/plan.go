package updates

import (
	"context"
	"slices"

	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/registry"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
	"github.com/castrowithcee/qatlas-deploy/internal/version"
)

// Kinds of a PlanError.
const (
	// PlanRefused: the stack cannot be updated as it is, whatever the version.
	PlanRefused = "refused"
	// PlanVersion: the requested version cannot be taken.
	PlanVersion = "version"
	// PlanRegistry: the registry could not be asked.
	PlanRegistry = "registry"
)

// PlanError is why no update could be planned. Text is fixed, apart from the variable name, and fit to show.
type PlanError struct {
	Kind     string
	Variable string
	Text     string
}

func (e *PlanError) Error() string {
	if e.Variable == "" {
		return e.Text
	}
	return clip(e.Variable) + ": " + e.Text
}

// Plan returns the steps that update the stack, from the tags the registries list now. Without target, every
// image version variable whose rules allow a newer version moves to the version detection would select; with
// target, the stack's only image version variable moves to target, which may lie above the ceiling. A move
// across major versions under update.major stepwise visits the highest version of every major version on the
// way. The variables keep the order of the stack's images; a stack with nothing to update has no steps. It
// only reads.
func (d *Detector) Plan(ctx context.Context, st stacks.Stack, target string) ([]engine.VersionStep, error) {
	refuse := func(variable, text string) error {
		return &PlanError{Kind: PlanRefused, Variable: variable, Text: text}
	}
	if st.Err != nil {
		return nil, refuse("", "the stack cannot be read")
	}
	if st.Rules == nil {
		return nil, refuse("", "the stack has no qdeploy.yaml and is never updated")
	}
	var vars []stacks.ImageVar
	for _, img := range st.Images {
		i := slices.IndexFunc(vars, func(v stacks.ImageVar) bool { return v.Var == img.Var })
		switch {
		case i < 0:
			vars = append(vars, img)
		case vars[i].Image != img.Image:
			return nil, refuse(img.Var, "the variable is the tag of more than one image")
		}
	}
	if target != "" && len(vars) != 1 {
		return nil, &PlanError{Kind: PlanVersion, Text: "a version can only be given for a stack with exactly one image version variable"}
	}

	run := &run{d: d, clients: map[string]*registry.Client{}, creds: map[string]error{}, tags: map[string]tagResult{}}
	stepwise := st.Rules.Update.Major == stacks.MajorStepwise
	var steps []engine.VersionStep
	for _, img := range vars {
		repo, err := registry.ParseRepo(img.Image)
		if err != nil {
			return nil, refuse(img.Var, textBadImage)
		}
		res := run.list(ctx, repo)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if res.text != "" {
			return nil, &PlanError{Kind: PlanRegistry, Variable: img.Var, Text: res.text}
		}
		in := version.Input{Current: img.Value, Candidates: res.tags, Ceiling: st.Rules.Update.Ceiling, Filter: st.Rules.Update.Tags[img.Var]}
		to := target
		if to == "" {
			sel, err := version.Select(in)
			if err != nil {
				return nil, refuse(img.Var, textBadCeiling)
			}
			if sel.Status != version.StatusUpdate {
				continue
			}
			to = sel.Selected
		}
		path, err := version.Path(in, to, stepwise)
		if err != nil {
			// Path's errors are fixed texts.
			return nil, &PlanError{Kind: PlanVersion, Variable: img.Var, Text: err.Error()}
		}
		if img.Source != stacks.SourceEnv {
			return nil, refuse(img.Var, "the version is not set in the stack's .env")
		}
		from := img.Value
		for _, v := range path {
			steps = append(steps, engine.VersionStep{Variable: img.Var, From: from, To: v})
			from = v
		}
	}
	return steps, nil
}
