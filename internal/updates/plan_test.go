package updates

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/registry/registrytest"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

var planTags = []string{"1.0.0", "1.0.1", "1.1.0", "2.0.0", "2.2.0", "2.3.0-rc.1", "3.0.0", "3.1.0", "3.1.0-alpine"}

func planStack(ceiling, major string, imgs ...stacks.ImageVar) stacks.Stack {
	st := stackOf("web", ceiling, nil, imgs...)
	st.Rules.Update.Major = major
	return st
}

func steps(variable string, versions ...string) []engine.VersionStep {
	var out []engine.VersionStep
	for i := 1; i < len(versions); i++ {
		out = append(out, engine.VersionStep{Variable: variable, From: versions[i-1], To: versions[i]})
	}
	return out
}

func TestPlanTargets(t *testing.T) {
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": planTags, "demo/db": {"5.0.0", "5.0.1"}}})
	d := detector(srv.URL, anonymous)
	app := imgVar("APP_VERSION", host+"/demo/app", "1.0.0")
	for _, tc := range []struct {
		name   string
		st     stacks.Stack
		target string
		want   []engine.VersionStep
	}{
		{"within the ceiling", planStack("minor", stacks.MajorStepwise, app), "", steps("APP_VERSION", "1.0.0", "1.1.0")},
		{"major ceiling stepwise", planStack("major", stacks.MajorStepwise, app), "", steps("APP_VERSION", "1.0.0", "1.1.0", "2.2.0", "3.1.0")},
		{"major ceiling direct", planStack("major", stacks.MajorDirect, app), "", steps("APP_VERSION", "1.0.0", "3.1.0")},
		{"explicit above the ceiling", planStack("patch", stacks.MajorStepwise, app), "2.0.0", steps("APP_VERSION", "1.0.0", "1.1.0", "2.0.0")},
		{"explicit direct", planStack("patch", stacks.MajorDirect, app), "3.0.0", steps("APP_VERSION", "1.0.0", "3.0.0")},
		{"nothing newer", planStack("minor", stacks.MajorStepwise, imgVar("APP_VERSION", host+"/demo/app", "1.1.0")), "", nil},
		{"not comparable is skipped", planStack("minor", stacks.MajorStepwise, imgVar("APP_VERSION", host+"/demo/app", "latest")), "", nil},
		{"each variable in turn", planStack("minor", stacks.MajorStepwise, app, imgVar("DB_VERSION", host+"/demo/db", "5.0.0"), app),
			"", append(steps("APP_VERSION", "1.0.0", "1.1.0"), steps("DB_VERSION", "5.0.0", "5.0.1")...)},
	} {
		got, err := d.Plan(context.Background(), tc.st, tc.target)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%s: %+v, %v; want %+v", tc.name, got, err, tc.want)
		}
	}
}

func TestPlanRefusals(t *testing.T) {
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": planTags}})
	d := detector(srv.URL, anonymous)
	app := imgVar("APP_VERSION", host+"/demo/app", "2.0.0")
	defaulted := app
	defaulted.Source = stacks.SourceDefault
	filtered := planStack("minor", stacks.MajorStepwise, app)
	filtered.Rules.Update.Tags = map[string]*regexp.Regexp{"APP_VERSION": regexp.MustCompile(`^2\.`)}
	for _, tc := range []struct {
		name, target, kind string
		st                 stacks.Stack
	}{
		{"below", "1.1.0", PlanVersion, planStack("minor", stacks.MajorStepwise, app)},
		{"equal", "2.0.0", PlanVersion, planStack("minor", stacks.MajorStepwise, app)},
		{"unknown", "9.0.0", PlanVersion, planStack("minor", stacks.MajorStepwise, app)},
		{"other variant", "3.1.0-alpine", PlanVersion, planStack("minor", stacks.MajorStepwise, app)},
		{"pre-release", "2.3.0-rc.1", PlanVersion, planStack("minor", stacks.MajorStepwise, app)},
		{"filtered", "3.1.0", PlanVersion, filtered},
		{"not comparable", "3.1.0", PlanVersion, planStack("minor", stacks.MajorStepwise, imgVar("APP_VERSION", host+"/demo/app", "latest"))},
		{"two variables", "3.1.0", PlanVersion, planStack("minor", stacks.MajorStepwise, app, imgVar("DB_VERSION", host+"/demo/app", "1.0.0"))},
		{"no rules", "", PlanRefused, stacks.Stack{Name: "web", Images: []stacks.ImageVar{app}}},
		{"not in .env", "", PlanRefused, planStack("minor", stacks.MajorStepwise, defaulted)},
		{"one variable, two images", "", PlanRefused, planStack("minor", stacks.MajorStepwise, app, imgVar("APP_VERSION", host+"/demo/other", "2.0.0"))},
		{"registry", "", PlanRegistry, planStack("minor", stacks.MajorStepwise, imgVar("APP_VERSION", host+"/demo/missing", "2.0.0"))},
	} {
		got, err := d.Plan(context.Background(), tc.st, tc.target)
		var pe *PlanError
		if !errors.As(err, &pe) || pe.Kind != tc.kind || got != nil {
			t.Errorf("%s: %+v, %v; want a %s error", tc.name, got, err, tc.kind)
		}
	}
}
