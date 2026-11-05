package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func setValue(old []byte) ([]byte, error) {
	return bytes.Replace(old, []byte("APP_VERSION=1.0.0"), []byte("APP_VERSION=1.1.0"), 1), nil
}

func TestCommitFileChangesOnlyTheFileOnTheRemoteHead(t *testing.T) {
	ctx := context.Background()
	f := newRemote(t)
	f.commit(map[string]string{"web/.env": "# demo\nAPP_VERSION=1.0.0\nOTHER=x\n"})
	r, _ := cloneFixture(t, f)
	head, _ := r.Head(ctx)
	other := f.commit(map[string]string{"db/compose.yaml": "other\n"})
	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	idxBefore, bodyBefore := indexAndFiles(t, r)

	id, err := r.CommitFile(ctx, "web/.env", setValue, "Update web APP_VERSION from 1.0.0 to 1.1.0\n\x1b[31m")
	if err != nil {
		t.Fatal(err)
	}
	if idxAfter, bodyAfter := indexAndFiles(t, r); idxAfter != idxBefore || bodyAfter != bodyBefore {
		t.Fatal("CommitFile touched the index or the working copy")
	}
	if h, _ := r.Head(ctx); h != head {
		t.Fatal("CommitFile moved HEAD")
	}
	if got := fixtureGit(t, r.dir, "rev-parse", id+"^"); got != other {
		t.Fatalf("parent = %s, want remote head %s", got, other)
	}
	if got := fixtureGit(t, r.dir, "diff", "--name-status", other, id); got != "M\tweb/.env" {
		t.Fatalf("diff = %q", got)
	}
	diff := fixtureGit(t, r.dir, "diff", "-U0", other, id)
	if !strings.Contains(diff, "\n-APP_VERSION=1.0.0\n+APP_VERSION=1.1.0") || strings.Count(diff, "\n-") != 2 || strings.Count(diff, "\n+") != 2 {
		t.Fatalf("diff = %q", diff)
	}
	if got := fixtureGit(t, r.dir, "show", "-s", "--format=%an <%ae>|%B", id); got != "qdeploy <qdeploy@localhost.invalid>|Update web APP_VERSION from 1.0.0 to 1.1.0 [31m" {
		t.Fatalf("commit = %q", got)
	}

	if err := r.Push(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := r.FastForward(ctx, id); err != nil {
		t.Fatal(err)
	}
	if h, _ := r.Head(ctx); h != id {
		t.Fatalf("head = %s, want %s", h, id)
	}
	if got := fixtureGit(t, r.dir, "status", "--porcelain"); got != "" {
		t.Fatalf("status = %q", got)
	}
	for _, tc := range []struct {
		a, b string
		want bool
	}{{head, id, true}, {id, id, true}, {id, head, false}} {
		if got, err := r.IsAncestor(ctx, tc.a, tc.b); err != nil || got != tc.want {
			t.Errorf("IsAncestor(%s, %s) = %v, %v", tc.a, tc.b, got, err)
		}
	}
	if _, err := r.IsAncestor(ctx, "HEAD", id); err == nil {
		t.Error("IsAncestor took a revision name")
	}
}

func TestCommitFileRefusals(t *testing.T) {
	ctx := context.Background()
	f := newRemote(t)
	f.commit(map[string]string{"web/.env": "APP_VERSION=1.0.0\n"})
	r, _ := cloneFixture(t, f)
	same := func(old []byte) ([]byte, error) { return old, nil }
	errEdit := errors.New("the demo edit failed")
	failing := func([]byte) ([]byte, error) { return nil, errEdit }
	for name, tc := range map[string]struct {
		path, message string
		edit          func([]byte) ([]byte, error)
		want          error
	}{
		"unchanged":    {"web/.env", "m", same, ErrUnchanged},
		"edit error":   {"web/.env", "m", failing, errEdit},
		"missing":      {"web/missing.env", "m", setValue, nil},
		"folder":       {"web", "m", setValue, nil},
		"outside":      {"../web/.env", "m", setValue, nil},
		"git dir":      {"web/.git/config", "m", setValue, nil},
		"no message":   {"web/.env", " \n", setValue, nil},
		"absolute":     {"/web/.env", "m", setValue, nil},
		"option-like":  {"-web/.env", "m", setValue, nil},
		"dotdot inner": {"web/../db/.env", "m", setValue, nil},
	} {
		id, err := r.CommitFile(ctx, tc.path, tc.edit, tc.message)
		if err == nil || id != "" || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Errorf("%s: id %q, err %v", name, id, err)
		}
	}
}
