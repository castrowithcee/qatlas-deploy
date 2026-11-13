package tui

import (
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

const foreignDir = "/srv/demo-foreign"

func demoForeign() foreignSource {
	return foreignSource{project: "legacy-blog", dir: foreignDir,
		composeFiles: []string{foreignDir + "/compose.yaml"}, envFiles: []string{foreignDir + "/.env"}}
}

// foreignStack is the stack a deploy of the folder brings, by default the same project and files as the labels.
func foreignStack() control.SourceStack {
	return control.SourceStack{Name: "legacy-blog", Folder: ".", Project: "legacy-blog",
		Functions:    []control.StackFunction{{Function: "deploy", Works: true}, {Function: "backup"}},
		ComposeFiles: []string{foreignDir + "/compose.yaml"}, EnvFiles: []string{foreignDir + "/.env"}}
}

// adoptBackend is containersBackend with the files of the project legacy-blog and the inspection of its folder.
func adoptBackend(res control.SourceInspectResult) *fakeBackend {
	b := containersBackend()
	src := demoForeign()
	for i := range b.containers.Groups {
		if g := &b.containers.Groups[i]; g.Name == "legacy-blog" {
			g.WorkingDir, g.ConfigFiles, g.EnvFiles = src.dir, src.composeFiles, src.envFiles
		}
	}
	d := b.containerDetails["legacy-blog-db-1"]
	d.WorkingDir, d.ConfigFiles, d.EnvFiles = src.dir, src.composeFiles, src.envFiles
	b.containerDetails["legacy-blog-db-1"] = d
	res.Path = foreignDir
	b.inspects = map[string]control.SourceInspectResult{foreignDir: res}
	b.host = &fakeFolders{}
	return b
}

func localResult(st ...control.SourceStack) control.SourceInspectResult {
	return control.SourceInspectResult{Case: control.SourceCaseLocal, Stacks: st,
		Access: &control.SourceAccess{ModePath: foreignDir, Owner: "root", Group: "root", Mode: "0755", ServiceCanWrite: true}}
}

func TestAdoptRejection(t *testing.T) {
	with := func(f func(*foreignSource)) foreignSource {
		s := demoForeign()
		f(&s)
		return s
	}
	two := &control.SourceInspectResult{Case: control.SourceCaseLocal, Stacks: []control.SourceStack{foreignStack(), foreignStack()}}
	sub := foreignStack()
	sub.Folder = "blog"
	rejected := &control.SourceInspectResult{Case: control.SourceCaseReject}
	for _, c := range []struct {
		name string
		src  foreignSource
		res  *control.SourceInspectResult
		want kitID
	}{
		{"fine", demoForeign(), nil, ""},
		{"fine with one stack", demoForeign(), &control.SourceInspectResult{Case: control.SourceCaseLocal, Stacks: []control.SourceStack{foreignStack()}}, ""},
		{"no folder", with(func(s *foreignSource) { s.dir = "" }), nil, kitID(textAdoptNoDir)},
		{"relative folder", with(func(s *foreignSource) { s.dir = "srv/blog" }), nil, kitID(textAdoptDirRelative)},
		{"compose outside", with(func(s *foreignSource) { s.composeFiles = []string{"/srv/other/compose.yaml"} }), nil, kitID(textAdoptOutside)},
		{"compose in the parent", with(func(s *foreignSource) { s.composeFiles = []string{"/srv/compose.yaml"} }), nil, kitID(textAdoptOutside)},
		{"compose with dots", with(func(s *foreignSource) { s.composeFiles = []string{foreignDir + "/../x/compose.yaml"} }), nil, kitID(textAdoptOutside)},
		{"env outside", with(func(s *foreignSource) { s.envFiles = []string{"/etc/blog.env"} }), nil, kitID(textAdoptOutside)},
		{"relative file", with(func(s *foreignSource) { s.envFiles = []string{".env"} }), nil, kitID(textAdoptOutside)},
		{"file in a subfolder is inside", with(func(s *foreignSource) { s.composeFiles = []string{foreignDir + "/conf/a.yaml"} }), nil, ""},
		{"two stacks", demoForeign(), two, kitID(textAdoptStacks)},
		{"no stack", demoForeign(), &control.SourceInspectResult{Case: control.SourceCaseLocal, Stacks: []control.SourceStack{}}, kitID(textAdoptStacks)},
		{"stack in a subfolder", demoForeign(), &control.SourceInspectResult{Case: control.SourceCaseLocal, Stacks: []control.SourceStack{sub}}, kitID(textAdoptNotRoot)},
		{"rejected by the inspection is left to it", demoForeign(), rejected, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, bad := adoptRejection(c.src, c.res)
			if bad != (c.want != "") || kitID(p.id) != c.want {
				t.Fatalf("got %q (%v), want %q", p.id, bad, c.want)
			}
		})
	}
}

type kitID string

func TestAdoptWarnings(t *testing.T) {
	src := demoForeign()
	mod := func(f func(*control.SourceStack)) control.SourceStack {
		st := foreignStack()
		f(&st)
		return st
	}
	for _, c := range []struct {
		name    string
		st      control.SourceStack
		want    []kitID
		noLabel bool
	}{
		{"same", foreignStack(), nil, false},
		{"same after cleaning", mod(func(s *control.SourceStack) { s.ComposeFiles = []string{foreignDir + "/./compose.yaml"} }), nil, false},
		{"other project", mod(func(s *control.SourceStack) { s.Project = "blog" }), []kitID{kitID(textAdoptWarnProject)}, false},
		{"other compose files", mod(func(s *control.SourceStack) { s.ComposeFiles = []string{foreignDir + "/compose.yml"} }), []kitID{kitID(textAdoptWarnCompose)}, false},
		{"other order", mod(func(s *control.SourceStack) {
			s.ComposeFiles = []string{foreignDir + "/b.yaml", foreignDir + "/compose.yaml"}
		}),
			[]kitID{kitID(textAdoptWarnCompose)}, false},
		{"no env file", mod(func(s *control.SourceStack) { s.EnvFiles = nil }), []kitID{kitID(textAdoptWarnEnv)}, false},
		{"label missing, only .env", mod(func(s *control.SourceStack) {}), nil, true},
		{"label missing, .env and secrets.env", mod(func(s *control.SourceStack) { s.EnvFiles = []string{foreignDir + "/.env", foreignDir + "/secrets.env"} }), []kitID{kitID(textAdoptWarnEnv)}, true},
		{"label missing, no file", mod(func(s *control.SourceStack) { s.EnvFiles = nil }), nil, true},
		{"not deployable", control.SourceStack{Folder: "."}, []kitID{kitID(textAdoptWarnProject), kitID(textAdoptWarnCompose), kitID(textAdoptWarnEnv)}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var got []kitID
			src := src
			if c.noLabel {
				src.envFiles = nil
			}
			for _, w := range adoptWarnings(src, c.st) {
				got = append(got, kitID(w.id))
			}
			if !slices.Equal(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
	a := src
	a.composeFiles = []string{foreignDir + "/a.yaml", foreignDir + "/b.yaml"}
	st := foreignStack()
	st.ComposeFiles = []string{foreignDir + "/b.yaml", foreignDir + "/a.yaml"}
	if samePaths(a.composeFiles, st.ComposeFiles) {
		t.Fatal("the order of the compose files does not matter")
	}
}

func TestContainerDetailShowsFolderAndFilesOfAForeignContainer(t *testing.T) {
	m := newTestModel(t, i18n.English, adoptBackend(localResult(foreignStack())), wideWidth, 40)
	send(m, "5", "enter")
	contains(t, m, "folder", foreignDir, "Compose files", foreignDir+"/compose.yaml", "Env files", foreignDir+"/.env")
}

func TestAdoptNeedsAForeignProject(t *testing.T) {
	b := adoptBackend(localResult(foreignStack()))
	m := newTestModel(t, i18n.English, b, wideWidth, 40)
	// Down to the single container: the own stack and the single containers are no foreign project.
	send(m, "5", "end", "a")
	if m.modal != nil || b.called("sourceinspect") != 0 {
		t.Fatalf("a key on a single container did something: %v", m.modal)
	}
	send(m, "home", "down", "down", "down", "down", "down", "a")
	if m.modal != nil || b.called("sourceinspect") != 0 {
		t.Fatalf("a key on an own container did something: %v", m.modal)
	}
}

func TestAdoptFromListGroupAndDetailChangesNothingWithoutYes(t *testing.T) {
	for name, keys := range map[string][]string{
		"container": {"5", "a", "n"}, "group": {"5", "up", "a", "n"}, "detail": {"5", "enter", "a", "n"},
		"enter does not confirm": {"5", "a", "enter"},
	} {
		t.Run(name, func(t *testing.T) {
			b := adoptBackend(localResult(foreignStack()))
			m := newTestModel(t, i18n.English, b, wideWidth, 40)
			send(m, keys...)
			if b.called("sourceinspect") == 0 {
				t.Fatal("the folder was not inspected")
			}
			noChange(t, b)
			if len(b.sourceAdds) != 0 {
				t.Fatalf("adds %v", b.sourceAdds)
			}
		})
	}
}

func TestAdoptShowsTheFormAndAddsTheFolderAfterYes(t *testing.T) {
	b := adoptBackend(localResult(foreignStack()))
	m := newTestModel(t, i18n.English, b, wideWidth, 40)
	send(m, "5", "a")
	contains(t, m, "Foreign project", "folder: "+foreignDir, "project: legacy-blog",
		"compose files (labels): "+foreignDir+"/compose.yaml", "env files (labels): "+foreignDir+"/.env", "works: deploy", "does not work: backup")
	if m.modal == nil || b.called("sourceinspect") != 1 {
		t.Fatalf("modal %v, inspections %d", m.modal, b.called("sourceinspect"))
	}
	if screen := m.render(); containsText(screen, "Warning") {
		t.Fatalf("a warning without a difference:\n%s", screen)
	}
	reads := b.called("containers")
	send(m, "y")
	if got := b.host.changes(); !slices.Equal(got, []string{"grant:" + foreignDir + ":false"}) {
		t.Fatalf("host changes = %v", got)
	}
	if len(b.sourceAdds) != 1 || b.sourceAdds[0].Path != foreignDir {
		t.Fatalf("adds = %+v", b.sourceAdds)
	}
	if b.called("containers") <= reads {
		t.Fatal("the Containers area was not read again")
	}
}

func containsText(screen, want string) bool { return contains2(screen, want) }

func TestAdoptWarnsAboutAnotherProjectAndFilesWithoutBlocking(t *testing.T) {
	st := foreignStack()
	st.Project = "blog"
	st.ComposeFiles = []string{foreignDir + "/compose.yml"}
	st.EnvFiles = nil
	b := adoptBackend(localResult(st))
	m := newTestModel(t, i18n.English, b, wideWidth, 50)
	send(m, "5", "a")
	contains(t, m, "Warning", "belong to legacy-blog, a deploy would use blog", "compose files: the running containers use", "a deploy would pass -", "create new containers instead of taking over")
	send(m, "y")
	if len(b.sourceAdds) != 1 {
		t.Fatalf("the warning blocked the adoption: %+v", b.sourceAdds)
	}
}

func TestAdoptWarningStaysBeforeTheLastQuestionOfAClone(t *testing.T) {
	st := foreignStack()
	st.Project = "blog"
	res := control.SourceInspectResult{Case: control.SourceCaseAdopt, URL: repoURL, Stacks: []control.SourceStack{st},
		Access: &control.SourceAccess{ModePath: foreignDir, Owner: "admin", Group: "admin", Mode: "0755", ServiceCanWrite: true}}
	b := adoptBackend(res)
	b.dirKeys = map[string]control.KeyResult{foreignDir: {Fingerprint: fingerprint}}
	m := newTestModel(t, i18n.English, b, wideWidth, 50)
	send(m, "5", "a")
	contains(t, m, "Warning")
	send(m, "y", "y")
	contains(t, m, "Warning", "create new containers instead of taking over", "Changes after yes")
	noChange(t, b)
	send(m, "y")
	if len(b.repoSet) != 1 || b.repoSet[0].Dir != foreignDir {
		t.Fatalf("binds = %+v", b.repoSet)
	}
}

func TestAdoptRejectsWithTheCauseAndNoWayOn(t *testing.T) {
	outside := foreignStack()
	for name, c := range map[string]struct {
		res  control.SourceInspectResult
		edit func(b *fakeBackend)
		want string
		call int
	}{
		"inspection rejects": {res: control.SourceInspectResult{Case: control.SourceCaseReject, Reason: "the folder is a repository without a branch",
			Stacks: []control.SourceStack{}}, want: "the folder is a repository without a branch", call: 1},
		"two stacks": {res: localResult(outside, outside), want: "exactly one stack, the inspection found 2", call: 1},
		"files outside": {res: localResult(outside), want: "/srv/elsewhere/compose.yaml lies outside the working directory " + foreignDir,
			edit: func(b *fakeBackend) {
				b.containers.Groups[1].ConfigFiles = []string{"/srv/elsewhere/compose.yaml"}
			}},
	} {
		t.Run(name, func(t *testing.T) {
			b := adoptBackend(c.res)
			if c.edit != nil {
				c.edit(b)
			}
			m := newTestModel(t, i18n.English, b, wideWidth, 40)
			send(m, "5", "up", "a")
			contains(t, m, "cannot be taken over", c.want)
			if b.called("sourceinspect") != c.call {
				t.Fatalf("inspections = %d, want %d", b.called("sourceinspect"), c.call)
			}
			send(m, "y")
			noChange(t, b)
			if len(b.sourceAdds) != 0 || len(b.repoSet) != 0 {
				t.Fatalf("a rejected project was added: %+v %+v", b.sourceAdds, b.repoSet)
			}
		})
	}
}
