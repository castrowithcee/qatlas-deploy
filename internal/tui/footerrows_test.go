package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

var counter = regexp.MustCompile(`\s\d+/\d+\s*$`)

// lastLine is the last line of lines that shows text.
func lastLine(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(ansi.Strip(lines[i])); s != "" {
			return s
		}
	}
	return ""
}

// A scrolling view sizes its content with the footer that is drawn, wrapped or not: at the end the last
// line is visible and the counter reads total/total.
func TestScrolledToTheEndShowsTheLastLineAndTheCounter(t *testing.T) {
	cases := []struct {
		name  string
		keys  []string
		lines func(m *Model, width int) []string
		total func(m *Model) int
	}{
		{"detail", []string{"enter", "end"},
			func(m *Model, w int) []string { return m.detailLines(m.rows[m.chosen], w) },
			func(m *Model) int { return m.detail.total }},
		{"preview", []string{"u", "end"},
			func(m *Model, w int) []string { return m.previewLines(w) },
			func(m *Model) int { return m.modal.total }},
	}
	for _, c := range cases {
		for name, lang := range goldenLangs {
			for width := 56; width <= 140; width++ {
				for _, height := range []int{16, 20, 24} {
					m := newTestModel(t, lang, demoBackend(), width, height)
					send(m, c.keys...)
					screen := m.render()
					want := lastLine(c.lines(m, width))
					if !strings.Contains(ansi.Strip(screen), want) {
						t.Errorf("%s %s %dx%d: last line %q is hidden", c.name, name, width, height, want)
					}
					// The kit leaves the counter out when it does not fit; a counter shown must be at the end.
					foot := strings.Join(strings.Split(ansi.Strip(screen), "\n")[height-2:], " ")
					if n := c.total(m); counter.MatchString(foot) && !strings.HasSuffix(strings.TrimSpace(foot), fmt.Sprintf(" %d/%d", n, n)) {
						t.Errorf("%s %s %dx%d: counter is not %d/%d: %q", c.name, name, width, height, n, n, foot)
					}
				}
			}
		}
	}
}

// At this width the hints fit one footer line and only the counter wraps it to a second.
func TestGoldenDetailEndWrappedByTheCounter(t *testing.T) {
	m := newTestModel(t, goldenLangs["en"], demoBackend(), 99, scrolledRows)
	send(m, "enter", "end")
	screen := m.render()
	checkScreen(t, screen, 99, scrolledRows)
	checkGolden(t, "detail.end.en.99.golden", screen+"\n")
}
