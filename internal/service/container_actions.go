package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/castrowithcee/qatlas-kit/docker"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const (
	// containerReadyWait is how long a start or restart waits for the container to run and be healthy.
	containerReadyWait = time.Minute
	// containerActionMargin is the time beyond grace and wait that an action may take at the engine.
	containerActionMargin = 30 * time.Second
)

// action returns the handler of one container action: "start", "stop" or "restart".
func (s *containerService) action(verb string) control.Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var a control.ContainerActionArgs
		if err := decodeArgs(raw, &a); err != nil {
			return nil, err
		}
		if a.Name == "" {
			return nil, control.NewError(control.CodeBadRequest, "a container name is required")
		}
		grace := time.Duration(a.GraceSeconds) * time.Second
		if a.GraceSeconds == 0 {
			grace = control.ContainerDefaultGraceSeconds * time.Second
		}
		if a.GraceSeconds < 0 || grace > docker.MaxGrace {
			return nil, control.NewError(control.CodeInvalid, "the grace period is not valid")
		}
		res, kind, err := s.act(ctx, verb, a.Name, grace)
		target := maskControl(a.Name)
		if err != nil {
			result := "failed"
			var ce *control.Error
			if errors.As(err, &ce) {
				result = ce.Code
			}
			s.log.Info("container action", "action", verb, "target", target, "kind", kind, "result", result)
			return nil, err
		}
		result := "ok"
		if res.NotReady != "" {
			result = "not-ready: " + res.NotReady
		}
		s.log.Info("container action", "action", verb, "target", target, "kind", kind, "result", result)
		return res, nil
	}
}

// act looks the container up, runs the action under the run slot of its stack if it belongs to an own stack,
// and answers with the container afterwards. kind is empty if the container was not found.
func (s *containerService) act(ctx context.Context, verb, ref string, grace time.Duration) (control.ContainerActionResult, string, error) {
	var res control.ContainerActionResult
	d, err := s.docker.Inspect(ctx, ref)
	if err != nil {
		return res, "", s.dockerAnswer(err)
	}
	project, dir := d.Labels["com.docker.compose.project"], d.Labels["com.docker.compose.project.working_dir"]
	kind, _ := classify(project, dir, s.stackDirs())
	res.Kind = kind
	// The ID keeps every step on the container found, even if its name is reused meanwhile.
	id := d.ID
	run := func(ctx context.Context) error {
		budget := containerActionMargin
		if verb != "start" {
			budget += grace
		}
		if verb != "stop" {
			budget += s.wait
		}
		ctx, cancel := context.WithTimeout(ctx, budget)
		defer cancel()
		var err error
		switch verb {
		case "start":
			err = s.docker.Start(ctx, id)
		case "stop":
			err = s.docker.Stop(ctx, id, grace)
		default:
			err = s.docker.Restart(ctx, id, grace)
		}
		if err != nil {
			return err
		}
		if verb != "stop" {
			r, err := s.docker.WaitHealthy(ctx, []string{id}, docker.WaitOptions{Deadline: s.wait})
			if err != nil {
				return err
			}
			if len(r) == 1 && !r[0].OK {
				res.NotReady = string(r[0].Reason)
			}
		}
		after, err := s.docker.Inspect(ctx, id)
		if err != nil {
			return err
		}
		res.ContainerSummary = s.summary(after.Container)
		if kind == control.ContainerKindOwn {
			var add, remove []string
			if verb == "stop" {
				add = []string{id}
			} else {
				remove = []string{id}
			}
			if err := s.eng.UpdateStoppedContainers(project, add, remove); err != nil {
				s.log.Warn("container stop marker not stored", "error", err)
			}
		}
		return nil
	}
	if kind == control.ContainerKindOwn {
		err = s.eng.WithStack(ctx, project, "container "+verb, func(ctx context.Context, _ stacks.Stack, _ string) error {
			return run(ctx)
		})
	} else {
		err = run(ctx)
	}
	if err != nil {
		var de *docker.Error
		if errors.As(err, &de) {
			return res, kind, s.dockerAnswer(err)
		}
		return res, kind, s.containerRunAnswer(err)
	}
	return res, kind, fitAnswer(res, "the container answer is too large for one answer")
}

// containerRunAnswer maps what is not an engine failure: the refusals of the run slot, with fixed texts, and
// anything else as an internal failure without its text.
func (s *containerService) containerRunAnswer(err error) error {
	var ce *control.Error
	if ans := runsAnswer(err); errors.As(ans, &ce) {
		return ans
	}
	s.log.Error("container action failed")
	return control.NewError(control.CodeInternal, "the container action failed")
}
