package cli

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/channeltest"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

const inspectURL = "ssh://git.example.test/demo/stacks.git"

// fakeInspect answers source.inspect with reply and records the arguments it got.
type fakeInspect struct {
	mu   sync.Mutex
	args []control.SourceInspectArgs
}

func (f *fakeInspect) calls() []control.SourceInspectArgs {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]control.SourceInspectArgs(nil), f.args...)
}

func startFakeInspect(t *testing.T, reply func(control.SourceInspectArgs) control.SourceInspectResult) *fakeInspect {
	t.Helper()
	ep := channeltest.Endpoint(t)
	useEndpoint(t, ep)
	l, err := ep.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	f := &fakeInspect{}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				raw, err := control.ReadFrame(bufio.NewReader(conn), control.MaxRequestBytes)
				if err != nil {
					return
				}
				var req control.Request
				var a control.SourceInspectArgs
				if json.Unmarshal(raw, &req) != nil || req.Command != control.SourceInspectCommand || json.Unmarshal(req.Args, &a) != nil {
					return
				}
				f.mu.Lock()
				f.args = append(f.args, a)
				f.mu.Unlock()
				result, _ := json.Marshal(reply(a))
				_ = control.WriteFrame(conn, control.Response{V: control.Version, OK: true, Result: result}, control.MaxResponseBytes)
			}(conn)
		}
	}()
	channeltest.WaitReady(t, ep)
	return f
}

func inspectAccess(path string) *control.SourceAccess {
	return &control.SourceAccess{ModePath: path, OwnerID: 0, Owner: "root", GroupID: 0, Group: "root", Mode: "0755",
		SetsPermissions: true}
}

func filesResult(a control.SourceInspectArgs) control.SourceInspectResult {
	return control.SourceInspectResult{
		Path: a.Path, Case: control.SourceCaseFiles, Access: inspectAccess(a.Path),
		PlannedFiles: []string{"demo/compose.yaml", "demo/.env"},
		Excluded: []control.SourceExcluded{
			{Path: "demo/data", Reason: control.SourceExcludedBindMount, Stack: "demo"},
			{Path: "dump.bin", Reason: control.SourceExcludedTooLarge}},
		Stacks: []control.SourceStack{{
			Name: "demo", Folder: a.Path + "/demo",
			Functions: []control.StackFunction{
				{Function: "deploy", Works: true, Reason: "the stack has a compose file"},
				{Function: "update", Works: false, Mode: "none", Reason: "no stack settings", Action: "Add settings."}},
			Findings: []control.CheckFinding{{Stack: "demo", Rule: "latest-tag", Severity: "warning",
				Cause: "an image uses the latest tag", Fix: "Pin the version."}},
			Conflicts: []control.SourceConflict{{Container: "demo-app-1", Project: "old-demo"}},
		}},
		ContainersChecked: true,
	}
}

func TestSourceInspectFilesText(t *testing.T) {
	useFolderHost(t)
	f := startFakeInspect(t, filesResult)
	// The command never reads the folder: a value in its env file cannot reach the output.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DEMO_TOKEN=demo-planted-not-real\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := run(t, "source", "inspect", dir, "--url", inspectURL)
	if code != 0 || stderr != "" {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"CASE", "files", "OWNER", "root", "MODE", "0755", "SERVICE_CAN_WRITE", "false",
		"SOURCE_ADD_SETS_PERMISSIONS", "true", "PLANNED_FILE", "demo/compose.yaml",
		"demo/data: bind-mount (stack demo)", "dump.bin: too-large",
		"STACK.DEMO.DEPLOY", "STACK.DEMO.UPDATE", "no, none: no stack settings Action: Add settings.",
		"STACK.DEMO.FINDING.LATEST-TAG", "warning: an image uses the latest tag Fix: Pin the version.",
		"container demo-app-1 of project old-demo", "duplicate containers"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "demo-planted-not-real") || strings.Contains(stdout, "MODE_PATH") {
		t.Fatalf("unexpected output:\n%s", stdout)
	}
	if got := f.calls(); len(got) != 1 || got[0].Path != dir || got[0].URL != inspectURL {
		t.Fatalf("service calls = %v", got)
	}
}

func TestSourceInspectCasesText(t *testing.T) {
	useFolderHost(t)
	gw := true
	results := map[string]control.SourceInspectResult{
		"clone": {Case: control.SourceCaseClone, URL: inspectURL, Access: &control.SourceAccess{ModePath: "/srv", Mode: "0755",
			OwnerID: 7, GroupID: 8, ServiceCanWrite: true}, Stacks: []control.SourceStack{}},
		"needs": {Case: control.SourceCaseClone, NeedsURL: true, Access: inspectAccess("/srv/x"), Stacks: []control.SourceStack{}},
		"adopt": {Case: control.SourceCaseAdopt, URL: inspectURL, Access: func() *control.SourceAccess {
			a := inspectAccess("/srv/x")
			a.GitWritable = &gw
			return a
		}(), Stacks: []control.SourceStack{}, StacksError: "cannot read the stacks", ContainersError: "docker is not reachable"},
		"local": {Case: control.SourceCaseLocal, Access: inspectAccess("/srv/x"), Stacks: []control.SourceStack{}},
		"reject": {Case: control.SourceCaseReject, Reason: "the folder is not empty", Fix: "Choose another folder.",
			Stacks: []control.SourceStack{}},
	}
	startFakeInspect(t, func(a control.SourceInspectArgs) control.SourceInspectResult {
		r := results[filepath.Base(a.Path)]
		r.Path = a.Path
		return r
	})
	tests := []struct {
		name  string
		wants []string
		not   []string
	}{
		{"clone", []string{"clone", inspectURL, "OWNER", "7", "GROUP", "8", "MODE_PATH", "/srv", "SERVICE_CAN_WRITE", "true"}, nil},
		{"needs", []string{"pass --url <ssh-url> to clone"}, nil},
		{"adopt", []string{"adopt-clone", inspectURL, "GIT_WRITABLE", "true", "STACKS_ERROR", "CONTAINERS_ERROR", "docker is not reachable"}, nil},
		{"local", []string{"local", "SOURCE_ADD_SETS_PERMISSIONS"}, []string{"URL"}},
		{"reject", []string{"reject", "the folder is not empty", "Choose another folder."}, []string{"OWNER", "MODE"}},
	}
	for _, tc := range tests {
		code, stdout, stderr := run(t, "source", "inspect", "/srv/"+tc.name)
		if code != 0 || stderr != "" {
			t.Fatalf("%s: code %d, stderr %q", tc.name, code, stderr)
		}
		for _, w := range tc.wants {
			if !strings.Contains(stdout, w) {
				t.Errorf("%s: output lacks %q:\n%s", tc.name, w, stdout)
			}
		}
		for _, n := range tc.not {
			if strings.Contains(stdout, n) {
				t.Errorf("%s: output has %q:\n%s", tc.name, n, stdout)
			}
		}
	}
}

func TestSourceInspectJSON(t *testing.T) {
	useFolderHost(t)
	startFakeInspect(t, filesResult)
	code, stdout, stderr := run(t, "source", "inspect", "/srv/demo", "--output", "json")
	if code != 0 || stderr != "" {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	var got struct {
		Case         string   `json:"case"`
		PlannedFiles []string `json:"planned_files"`
		Access       struct {
			Mode            string `json:"mode"`
			SetsPermissions bool   `json:"sets_permissions"`
		} `json:"access"`
		Stacks []struct {
			Name      string `json:"name"`
			Conflicts []struct {
				Container string `json:"container"`
			} `json:"conflicts"`
		} `json:"stacks"`
		ContainersChecked bool `json:"containers_checked"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout, err)
	}
	if got.Case != "files" || len(got.PlannedFiles) != 2 || got.Access.Mode != "0755" || !got.Access.SetsPermissions ||
		len(got.Stacks) != 1 || got.Stacks[0].Name != "demo" || got.Stacks[0].Conflicts[0].Container != "demo-app-1" || !got.ContainersChecked {
		t.Fatalf("result = %+v", got)
	}
}

func TestSourceInspectRejectionIsJSONWithoutDetails(t *testing.T) {
	useFolderHost(t)
	startFakeInspect(t, func(a control.SourceInspectArgs) control.SourceInspectResult {
		return control.SourceInspectResult{Path: a.Path, Case: control.SourceCaseReject, Reason: "the path is a symbolic link",
			Fix: "Pass the real path.", Stacks: []control.SourceStack{}}
	})
	code, stdout, _ := run(t, "source", "inspect", "/srv/demo", "--output", "json")
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); code != 0 || err != nil || got["case"] != "reject" ||
		got["reason"] == nil || got["access"] != nil || got["stacks"] != nil {
		t.Fatalf("code %d, err %v, result %v", code, err, got)
	}
}

func TestSourceInspectReadOnlyFileSystemRejects(t *testing.T) {
	d := useFolderHost(t)
	d.readOnlyFS = true
	startFakeInspect(t, filesResult)
	code, stdout, stderr := run(t, "source", "inspect", "/srv/demo", "--output", "json")
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); code != 0 || stderr != "" || err != nil {
		t.Fatalf("code %d, stderr %q, err %v, stdout %q", code, stderr, err, stdout)
	}
	if got["case"] != "reject" || got["reason"] != readOnlyReason || got["fix"] == "" {
		t.Fatalf("result = %v", got)
	}
	for _, k := range []string{"access", "planned_files", "excluded", "stacks", "url"} {
		if _, ok := got[k]; ok {
			t.Errorf("the rejection still has %s: %v", k, got)
		}
	}
	if len(d.checked) != 1 || d.checked[0] != "/srv/demo" {
		t.Fatalf("checked = %v", d.checked)
	}
	_, text, _ := run(t, "source", "inspect", "/srv/demo")
	if !strings.Contains(text, "reject") || !strings.Contains(text, readOnlyReason) || strings.Contains(text, "PLANNED_FILE") {
		t.Fatalf("text:\n%s", text)
	}
}

func TestSourceInspectKeepsAServiceRejectionWithoutAsking(t *testing.T) {
	d := useFolderHost(t)
	startFakeInspect(t, func(a control.SourceInspectArgs) control.SourceInspectResult {
		return control.SourceInspectResult{Path: a.Path, Case: control.SourceCaseReject, Reason: "r", Fix: "f"}
	})
	if code, _, _ := run(t, "source", "inspect", "/srv/demo"); code != 0 || len(d.checked) != 0 {
		t.Fatalf("code %d, checked %v", code, d.checked)
	}
}

func TestSourceInspectArguments(t *testing.T) {
	useFolderHost(t)
	f := startFakeInspect(t, filesResult)
	for _, args := range [][]string{
		{"source", "inspect"},
		{"source", "inspect", "a", "b"},
		{"source", "inspect", "relative/dir"},
		{"source", "inspect", "/srv/a/../b"},
		{"source", "inspect", "/srv/demo", "--url", "not an address"},
	} {
		if code, _, _ := run(t, args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
	if len(f.calls()) != 0 {
		t.Fatalf("a refused call reached the service: %v", f.calls())
	}
}

func TestPathOnReadOnlyFS(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{dir, filepath.Join(dir, "missing", "deeper")} {
		if ro, err := pathOnReadOnlyFS(p); err != nil || ro {
			t.Errorf("%s: read-only %v, err %v", p, ro, err)
		}
	}
}
