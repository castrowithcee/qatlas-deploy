package tui

import (
	"testing"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// goldenForeign is goldenContainers with the mark on the container of the foreign project, whose row depends on
// how the kit orders the groups in the language.
func goldenForeign(t *testing.T, prefix string, setup func() *fakeBackend, keys ...string) {
	t.Helper()
	for name, lang := range goldenLangs {
		for size, dim := range map[string][2]int{"narrow": {ctNarrow, ctNarrowRows}, "wide": {ctWide, ctWideRows}} {
			t.Run(name+"."+size, func(t *testing.T) {
				m := newTestModel(t, lang, setup(), dim[0], dim[1])
				send(m, "5")
				if !m.ctList.Select(kittui.ItemID("legacy-blog-db-1")) {
					t.Fatal("the container of the foreign project is not in the list")
				}
				send(m, keys...)
				screen := m.render()
				checkScreen(t, screen, dim[0], dim[1])
				checkGolden(t, prefix+"."+name+"."+size+".golden", screen+"\n")
			})
		}
	}
}

func sameFiles() *fakeBackend { return adoptBackend(localResult(foreignStack())) }

func TestGoldenForeignContainerDetail(t *testing.T) {
	goldenForeign(t, "containers.detail.foreign", sameFiles, "enter")
}

func TestGoldenAdoptForm(t *testing.T) {
	goldenForeign(t, "containers.adopt", sameFiles, "a")
}

func TestGoldenAdoptWarning(t *testing.T) {
	goldenForeign(t, "containers.adopt.warning", func() *fakeBackend {
		st := foreignStack()
		st.Project = "blog"
		st.ComposeFiles = []string{foreignDir + "/compose.yml"}
		st.EnvFiles = nil
		return adoptBackend(localResult(st))
	}, "a")
}

func TestGoldenAdoptRejected(t *testing.T) {
	goldenForeign(t, "containers.adopt.rejected", func() *fakeBackend {
		b := sameFiles()
		for i := range b.containers.Groups {
			if g := &b.containers.Groups[i]; g.Name == "legacy-blog" {
				g.EnvFiles = []string{"/srv/elsewhere/blog.env"}
			}
		}
		return b
	}, "a")
}
