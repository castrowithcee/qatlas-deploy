package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

func folderModel(t *testing.T, b *fakeBackend) *Model {
	t.Helper()
	m := newTestModel(t, i18n.English, b, wideWidth, 40)
	m.mouseOn = true
	m.render()
	return m
}

// Every bound folder has its row, with stacks or without, with one folder and with several.
func TestEveryFolderHasItsGroupRow(t *testing.T) {
	for name, c := range map[string]struct {
		b       func() *fakeBackend
		folders []string
	}{
		"one":      {oneFolderBackend, []string{stackDir}},
		"two":      {twoFoldersBackend, []string{stackDir, localDir}},
		"an empty": {threeFoldersBackend, []string{stackDir, localDir, "/srv/empty-example"}},
		"only an empty one": {func() *fakeBackend {
			b := threeFoldersBackend()
			b.status.Engine.Stacks, b.stacks.Stacks = nil, nil
			return b
		}, []string{stackDir, localDir, "/srv/empty-example"}},
	} {
		t.Run(name, func(t *testing.T) {
			m := folderModel(t, c.b())
			for _, dir := range c.folders {
				onFolder(t, m, dir)
				f, ok := m.selectedFolder()
				if !ok || f.Path != dir {
					t.Fatalf("marked %q, want %q", f.Path, dir)
				}
				contains(t, m, dir)
			}
		})
	}
}

func TestAGroupRowShowsTheKindAndThePollOrItsFailure(t *testing.T) {
	m := folderModel(t, twoFoldersBackend())
	contains(t, m, "clone", "poll failed: cannot reach", "local", "polled 2026-10-30 14:25")
}

func TestTheDetailOfTheMarkedFolderStandsBelowTheList(t *testing.T) {
	m := folderModel(t, twoFoldersBackend())
	if strings.Contains(m.render(), "address: "+repoURL) {
		t.Fatal("the detail shows without a marked folder")
	}
	onFolder(t, m, stackDir)
	contains(t, m, "address: "+repoURL, "last poll: ", "poll failed: cannot reach git.example.test: connection refused")
	send(m, "down")
	if strings.Contains(m.render(), "address: "+repoURL) {
		t.Fatal("the detail stays on a stack row")
	}
}

func TestFolderKeysActOnTheMarkedFolderAndNotOnAStack(t *testing.T) {
	b := foldersBackend()
	m := folderModel(t, b)
	// The first row marked is a stack.
	if _, ok := m.selectedFolder(); ok {
		t.Fatal("a folder is marked at the start")
	}
	send(m, "F", "x", "e")
	if m.modal != nil || m.set.keyOpen || len(b.actions()) != 0 {
		t.Fatalf("a folder key acted on a stack: calls %v", b.actions())
	}
	if hints := hintLabels(m); slices.ContainsFunc(hints, func(h string) bool { return strings.HasPrefix(h, "fetch") || h == "release" || h == "deploy key" }) {
		t.Fatalf("the key line offers folder keys on a stack: %v", hints)
	}
	onFolder(t, m, stackDir)
	hints := hintLabels(m)
	for _, want := range []string{"fetch now", "deploy key", "release", "add"} {
		if !slices.Contains(hints, want) {
			t.Fatalf("the key line lacks %q on a folder: %v", want, hints)
		}
	}
	send(m, "x")
	contains(t, m, "Release the stack folder "+stackDir)
	send(m, "y")
	if !slices.Equal(b.removes, []string{stackDir}) {
		t.Fatalf("released %v", b.removes)
	}
}

func TestALocalFolderOffersNeitherFetchNorKey(t *testing.T) {
	b := foldersBackend()
	m := folderModel(t, b)
	onFolder(t, m, localDir)
	hints := hintLabels(m)
	if slices.Contains(hints, "fetch now") || slices.Contains(hints, "deploy key") || !slices.Contains(hints, "release") {
		t.Fatalf("hints %v", hints)
	}
	send(m, "e")
	if m.set.keyOpen {
		t.Fatal("a local folder opened the key view")
	}
	contains(t, m, "A local stack folder has no deploy key.")
}

func TestAddOpensTheFormFromTheEmptyAndFromTheFilledList(t *testing.T) {
	for name, b := range map[string]*fakeBackend{"empty": emptyHost(), "filled": settingsBackend(), "folders": foldersBackend()} {
		t.Run(name, func(t *testing.T) {
			m := folderModel(t, b)
			send(m, "a")
			if m.set.form == nil || m.set.form.kind != formFolder {
				t.Fatal("a did not open the form to add a folder")
			}
			contains(t, m, "Add a stack folder")
			send(m, "esc")
			if m.set.form != nil {
				t.Fatal("esc left the form open")
			}
		})
	}
}

func emptyHost() *fakeBackend {
	b := settingsBackend()
	b.status.Engine.Stacks, b.stacks.Stacks = nil, nil
	b.check, b.updates = control.StacksCheckResult{}, control.UpdatesResult{}
	return b
}

func TestWithoutFolderStacksLeadsToAdding(t *testing.T) {
	m := folderModel(t, emptyHost())
	contains(t, m, "No stack folders yet.", "a adds one", "sudo qdeploy source add <path>")
	x, y := cellOf(t, m, "No stack folders yet.")
	clickAt(m, x, y)
	if m.set.form == nil || m.set.form.kind != formFolder {
		t.Fatal("a click on the text did not open the form like the key")
	}
}

func TestAClickOnAFolderRowMarksItAndTheKeysOfTheLineAct(t *testing.T) {
	b := foldersBackend()
	m := folderModel(t, b)
	x, y := cellOf(t, m, localDir)
	clickAt(m, x, y)
	if f, ok := m.selectedFolder(); !ok || f.Path != localDir {
		t.Fatalf("marked %q (%v)", f.Path, ok)
	}
	x, y = cellOf(t, m, "release")
	clickAt(m, x, y)
	contains(t, m, "Release the stack folder "+localDir)
	send(m, "n")
	x, y = cellOf(t, m, "add")
	clickAt(m, x, y)
	if m.set.form == nil || m.set.form.kind != formFolder {
		t.Fatal("a click on the key to add did not open the form")
	}
}

func TestTheMarkStaysOnTheFolderPathWhenTheListChanges(t *testing.T) {
	b := threeFoldersBackend()
	m := folderModel(t, b)
	onFolder(t, m, localDir)
	// A folder appears above the marked one and a stack below it goes away.
	b.status.Engine.Sources = append([]control.SourceStatus{{ID: "srv-a-example", Path: "/srv/a-example", Kind: control.SourceKindLocal}},
		b.status.Engine.Sources...)
	b.status.Engine.Stacks = b.status.Engine.Stacks[:len(b.status.Engine.Stacks)-1]
	load(t, m)
	if f, ok := m.selectedFolder(); !ok || f.Path != localDir {
		t.Fatalf("marked %q (%v) after the reload", f.Path, ok)
	}
	// A question names the path it was opened for, whatever the list does afterwards.
	send(m, "x")
	contains(t, m, "Release the stack folder "+localDir)
	b.status.Engine.Sources = b.status.Engine.Sources[1:]
	load(t, m)
	send(m, "y")
	if !slices.Equal(b.removes, []string{localDir}) {
		t.Fatalf("released %v", b.removes)
	}
}

func TestUpOnTheFirstFolderRowLeadsToTheTabLine(t *testing.T) {
	m := folderModel(t, threeFoldersBackend())
	send(m, "home")
	if _, ok := m.selectedFolder(); !ok {
		t.Fatal("the first row is no folder")
	}
	wantZone(t, m, kittui.ZoneRows)
	send(m, "up")
	wantZone(t, m, kittui.ZoneTabs)
	// Esc in the folder rows leads there too.
	send(m, "down", "esc")
	wantZone(t, m, kittui.ZoneTabs)
}

func TestTheKeyViewBelongsToTheStacksAreaAndLeavesWithEsc(t *testing.T) {
	m := folderModel(t, keyBackend())
	send(m, toKey...)
	if m.area != areaStacks || !m.set.keyOpen {
		t.Fatalf("area %s, key view %v", m.area, m.set.keyOpen)
	}
	contains(t, m, "Deploy key", "stack folder: "+stackDir, "No deploy key yet")
	send(m, "esc")
	if m.set.keyOpen {
		t.Fatal("esc left the key view open")
	}
	// The folder row is the first row, so up leads on to the tab line.
	if _, ok := m.selectedFolder(); !ok {
		t.Fatal("the marked row is no longer the folder")
	}
	send(m, "up")
	wantZone(t, m, kittui.ZoneTabs)
}

// hintLabels are the labels of the key line, the ones a click can take.
func hintLabels(m *Model) []string {
	var out []string
	for _, h := range m.footer() {
		out = append(out, m.text.Lookup(h.Label))
	}
	return out
}
