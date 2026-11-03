// Package updates finds, for the image variables of the stacks, the version each could move to. It only
// reads: it asks the registries for tag lists and applies the rules of the stack, and changes nothing.
package updates

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/httpx"

	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/registry"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
	"github.com/castrowithcee/qatlas-deploy/internal/version"
)

// The texts stored for a variable that could not be judged. They are fixed: what a registry or a lookup
// answered never reaches the result.
const (
	textUnreachable   = "the registry could not be reached"
	textTimeout       = "the registry did not answer in time"
	textAuth          = "the registry requires a login or did not accept the credential"
	textNotFound      = "the repository does not exist on the registry"
	textRateLimited   = "the registry limits requests; try again later"
	textInvalidAnswer = "the registry answered in an unexpected way"
	textNoCredential  = "no credential could be determined for the registry"
	textBadImage      = "the image reference is not valid"
	textBadCeiling    = "the update ceiling of the stack is not valid"
	textNoValue       = "the version variable has no value"
	textNotSemver     = "the current tag is not a complete semantic version"
	textPrerelease    = "the current tag is a pre-release"

	maxText = 200 // runes; with MaxUpdateText bytes this fits for any UTF-8 text
)

// Detector runs detections. The zero value asks every registry anonymously.
type Detector struct {
	// Credential returns the login for a registry host, called at most once per host and run. Nil, like a
	// returned zero registry.Credential, means anonymous access.
	Credential func(host string) (registry.Credential, error)
	// Options configure the registry clients. Credential in it is ignored.
	Options registry.Options
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Check asks the registries once per repository and returns the result for each stack, keyed by name. A
// stack that cannot be read has no entry. One registry client per host serves the whole run, so bearer
// tokens are reused within it and dropped with it. It returns the context's error if the run is cancelled;
// the results are then incomplete and must not be stored.
func (d *Detector) Check(ctx context.Context, list []stacks.Stack) (map[string]engine.UpdateCheck, error) {
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	run := &run{d: d, clients: map[string]*registry.Client{}, creds: map[string]error{}, tags: map[string]tagResult{}}
	out := make(map[string]engine.UpdateCheck, len(list))
	for _, st := range list {
		if st.Err != nil {
			continue
		}
		check := engine.UpdateCheck{Checked: now().UTC()}
		seen := map[[2]string]bool{}
		for _, img := range st.Images {
			key := [2]string{img.Var, img.Image}
			if seen[key] || len(check.Items) == engine.MaxUpdateItems {
				continue
			}
			seen[key] = true
			check.Items = append(check.Items, run.item(ctx, st, img))
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		out[st.Name] = check
	}
	return out, nil
}

type tagResult struct {
	tags []string
	text string // set when the listing failed
}

type run struct {
	d       *Detector
	clients map[string]*registry.Client
	creds   map[string]error // hosts whose credential lookup failed
	tags    map[string]tagResult
}

func (r *run) item(ctx context.Context, st stacks.Stack, img stacks.ImageVar) engine.UpdateItem {
	it := engine.UpdateItem{Variable: clip(img.Var), Image: clip(img.Image), Current: clip(img.Value)}
	fail := func(status, text string) engine.UpdateItem {
		it.Status, it.Error = status, text
		return it
	}
	if img.Value == "" {
		return fail(engine.UpdateNotComparable, textNoValue)
	}
	if len(img.Value) > 128 {
		return fail(engine.UpdateNotComparable, textNotSemver)
	}
	repo, err := registry.ParseRepo(img.Image)
	if err != nil {
		return fail(engine.UpdateError, textBadImage)
	}
	res := r.list(ctx, repo)
	if res.text != "" {
		return fail(engine.UpdateError, res.text)
	}
	in := version.Input{Current: img.Value, Candidates: res.tags}
	if st.Rules != nil {
		in.Ceiling = st.Rules.Update.Ceiling
		in.Filter = st.Rules.Update.Tags[img.Var]
	}
	sel, err := version.Select(in)
	if err != nil {
		return fail(engine.UpdateError, textBadCeiling)
	}
	switch sel.Status {
	case version.StatusNotComparable:
		text := textNotSemver
		if sel.Reason == version.ReasonPrerelease {
			text = textPrerelease
		}
		return fail(engine.UpdateNotComparable, text)
	case version.StatusUpdate:
		it.Status = engine.UpdateAvailable
	default:
		it.Status = engine.UpdateCurrent
	}
	it.Selected, it.MajorAvailable = clip(sel.Selected), clip(sel.MajorAvailable)
	return it
}

// list returns the tags of the repository, asking the registry only the first time in a run.
func (r *run) list(ctx context.Context, repo registry.Repo) tagResult {
	key := repo.String()
	if res, ok := r.tags[key]; ok {
		return res
	}
	res := tagResult{}
	if c, text := r.client(repo.Host); c == nil {
		res.text = text
	} else if tags, err := c.Tags(ctx, repo); err != nil {
		res.text = classify(ctx, err)
	} else {
		res.tags = tags
	}
	r.tags[key] = res
	return res
}

func (r *run) client(host string) (*registry.Client, string) {
	if c, ok := r.clients[host]; ok {
		return c, ""
	}
	if _, failed := r.creds[host]; failed {
		return nil, textNoCredential
	}
	var cred registry.Credential
	var err error
	if r.d.Credential != nil {
		cred, err = r.d.Credential(host)
	}
	if err != nil {
		r.creds[host] = err
		return nil, textNoCredential
	}
	opts := r.d.Options
	opts.Credential = cred
	c := registry.New(opts)
	r.clients[host] = c
	return c, ""
}

func classify(ctx context.Context, err error) string {
	var he *httpx.Error
	if !errors.As(err, &he) {
		if ctx.Err() != nil {
			return textTimeout
		}
		return textInvalidAnswer
	}
	switch he.Class {
	case httpx.ClassAuth, httpx.ClassPermission:
		return textAuth
	case httpx.ClassNotFound:
		return textNotFound
	case httpx.ClassRateLimited:
		return textRateLimited
	case httpx.ClassTimeout:
		return textTimeout
	case httpx.ClassUnreachable:
		return textUnreachable
	}
	return textInvalidAnswer
}

// clip makes a text from the working copy or a registry fit for the state: printable and bounded.
func clip(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n == maxText {
			break
		}
		if unicode.IsPrint(r) && r != utf8.RuneError {
			b.WriteRune(r)
			n++
		}
	}
	return b.String()
}
