package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

func TestBlockedStackIsNeverDeployed(t *testing.T) {
	r := newRig(t, "web", "db")
	r.cfg.Source = func() (Binding, error) {
		return Binding{Dir: r.wc, Copy: r.copy, Blocked: func() map[string]string {
			return map[string]string{"web": "another stack folder uses the name web"}
		}}, nil
	}
	r.eng = r.start()
	if res := r.poll(); len(res.Started) != 1 || res.Started[0] != "db" {
		t.Fatalf("started = %v", res.Started)
	}
	web := r.stack("web")
	if web.Deployed != "" || web.Pending != "" || len(web.Findings) != 1 || web.Findings[0].Kind != FindingNameCollision ||
		web.Findings[0].Detail != "another stack folder uses the name web" {
		t.Fatalf("web = %+v", web)
	}
	ctx := context.Background()
	if _, err := r.eng.Trigger(ctx, "web"); !errors.Is(err, ErrNameCollision) {
		t.Fatalf("trigger: %v", err)
	}
	if err := r.eng.WithStack(ctx, "web", "backup", func(context.Context, stacks.Stack, string) error { return nil }); !errors.Is(err, ErrNameCollision) {
		t.Fatalf("with stack: %v", err)
	}
	if _, err := r.eng.CheckUpdate(ctx, "web"); !errors.Is(err, ErrNameCollision) {
		t.Fatalf("check update: %v", err)
	}
	if steps := r.dep.stepsOf("web"); len(steps) != 0 {
		t.Fatalf("web steps: %v", steps)
	}
}
