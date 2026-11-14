package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/output"
)

func TestFailureOfNotFoundHintsFitTheObject(t *testing.T) {
	for _, tc := range []struct{ code, hint string }{
		{control.CodeNoSuchKey, "qdeploy key generate"},
		{control.CodeNoSuchCredential, "qdeploy credential list"},
		{control.CodeNoSuchSecret, "qdeploy stack secret list <stack>"},
		{control.CodeNoSuchSource, "qdeploy source list"},
		{control.CodeNoSuchStack, "qdeploy stack list"},
		{control.CodeNotFound, ""},
	} {
		t.Run(tc.code, func(t *testing.T) {
			var f *cli.Failure
			if !errors.As(failureOf(control.NewError(tc.code, "gone")), &f) {
				t.Fatal("not a failure")
			}
			if f.Code != output.CodeNotFound {
				t.Fatalf("class %q", f.Code)
			}
			if tc.hint == "" && f.NextStep != "" || tc.hint != "" && !strings.Contains(f.NextStep, tc.hint) {
				t.Fatalf("next step %q, want %q", f.NextStep, tc.hint)
			}
			if tc.code != control.CodeNoSuchKey && strings.Contains(f.NextStep, "key generate") {
				t.Fatalf("next step %q names the key", f.NextStep)
			}
		})
	}
}

func TestFailureOfNoSuchKeyAddsNoSecondHintWhenTheMessageNamesTheCommand(t *testing.T) {
	for msg, want := range map[string]string{
		"no deploy key exists": "qdeploy key generate",
		"no deploy key exists for stack folder /srv/a; run 'qdeploy key generate --dir /srv/a' to create one": "",
	} {
		var f *cli.Failure
		if !errors.As(failureOf(control.NewError(control.CodeNoSuchKey, msg)), &f) {
			t.Fatal("not a failure")
		}
		if want == "" && f.NextStep != "" || want != "" && !strings.Contains(f.NextStep, want) {
			t.Fatalf("%q: next step %q", msg, f.NextStep)
		}
	}
}

func TestStackCheckUnknownStackNamesStackList(t *testing.T) {
	startFakeService(t, fakeHandlers{control.StacksCheckCommand: func(json.RawMessage) any {
		return control.NewError(control.CodeNoSuchStack, "no such stack in the working copy")
	}})
	code, _, stderr := run(t, "stack", "check", "unknown")
	if code == 0 || !strings.Contains(stderr, "not-found") || !strings.Contains(stderr, "qdeploy stack list") || strings.Contains(stderr, "key generate") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
}

func TestSourceRemoteUnknownFolderNamesSourceList(t *testing.T) {
	useFolderHost(t)
	fake := &remoteFake{compare: differentCompare(control.SourceCaseLocal),
		refusal: control.NewError(control.CodeNoSuchSource, "no source is bound to this folder")}
	startFakeService(t, fake.handlers(localInspect(false)))
	useStdin(t, strings.NewReader(""))
	code, _, stderr := run(t, "source", "remote", t.TempDir(), remoteURL, "--fingerprint", remoteKeyPrint, "--yes")
	if code == 0 || !strings.Contains(stderr, "not-found") || !strings.Contains(stderr, "qdeploy source list") || strings.Contains(stderr, "key generate") {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
}
