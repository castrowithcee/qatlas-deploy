package tui

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

func TestAClickOnAnAnswerActsLikeItsKey(t *testing.T) {
	m, b := newMouseModel(t)
	send(m, "d")
	x, y := cellOf(t, m, "cancel <")
	clickAt(m, x, y)
	if m.modal != nil || len(b.actions()) != 0 {
		t.Fatalf("cancel by click: modal=%v calls=%v", m.modal != nil, b.actions())
	}
	send(m, "d")
	x, y = cellOf(t, m, "yes, do it")
	clickAt(m, x, y)
	if got := b.actions(); !slices.Equal(got, []string{"deploy:demo-stack"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestAClickOnADestructiveAnswerTakesTheFollowUpQuestion(t *testing.T) {
	m, _ := newMouseModel(t)
	followed := 0
	m.ask(textAnswerYes, "Remove the thing?", "It is gone.", nil, true, func() tea.Cmd {
		m.ask(textAnswerYes, "Really remove it?", "Really gone.", nil, true, func() tea.Cmd { followed++; return nil })
		return nil
	})
	m.render()
	x, y := cellOf(t, m, "[ yes, do it ]")
	clickAt(m, x, y)
	if m.modal == nil || !strings.Contains(m.render(), "Really remove it?") || followed != 0 {
		t.Fatalf("the follow-up question did not come:\n%s", m.render())
	}
	x, y = cellOf(t, m, "[ yes, do it ]")
	clickAt(m, x, y)
	if followed != 1 || m.modal != nil {
		t.Fatalf("followed = %d", followed)
	}
}

func TestAClickOutsideTheDialogTargetsDoesNothing(t *testing.T) {
	m, b := newMouseModel(t)
	send(m, "d")
	for _, needle := range []string{"Runs", "[?]", "photo-album"} {
		if !strings.Contains(m.render(), needle) {
			continue
		}
		x, y := cellOf(t, m, needle)
		clickAt(m, x, y)
		if m.modal == nil || m.area != areaStacks || m.helpOpen || len(b.actions()) != 0 {
			t.Fatalf("a click on %q acted under the question", needle)
		}
	}
	clickAt(m, 0, 0)
	if m.modal == nil {
		t.Fatal("a click on the corner closed the question")
	}
}

func TestADialogWithTextInputTakesNoClick(t *testing.T) {
	m, _ := newMouseModel(t)
	d, err := kittui.NewDialog(kittui.DialogSpec{Title: textAnswerYes, Question: "Name?",
		Input:   &kittui.DialogInput{},
		Answers: []kittui.Answer{{Action: actEnter, Label: textAnswerYes}, {Action: actBack, Label: textAnswerCancel}}}, m.keys, m.text)
	if err != nil {
		t.Fatal(err)
	}
	m.modal = &modal{kind: modalAsk, dialog: d, yes: func() tea.Cmd { t.Fatal("answered"); return nil }}
	m.render()
	x, y := cellOf(t, m, "[ yes, do it ]")
	clickAt(m, x, y)
	if m.modal == nil {
		t.Fatal("a click answered a dialog that captures text")
	}
}

func TestAFooterAnswerInTheNarrowFallbackIsClickable(t *testing.T) {
	for _, width := range []int{minWidth, 44, 56} {
		b := demoBackend()
		m := newTestModel(t, i18n.English, b, width, minHeight)
		m.mouseOn = true
		send(m, "d")
		m.render()
		x, y := cellOf(t, m, "y yes")
		clickAt(m, x, y)
		if got := b.actions(); !slices.Equal(got, []string{"deploy:demo-stack"}) {
			t.Fatalf("width %d: calls = %v", width, got)
		}
	}
}

func TestASheetFooterAnswerCanBeClicked(t *testing.T) {
	m, b := newMouseModel(t)
	cmd := press(m, "u")
	drain(m, cmd)
	m.render()
	x, y := cellOf(t, m, "y yes, do it")
	clickAt(m, x, y)
	if got := b.actions(); !slices.Equal(got, []string{"update:demo-stack"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestTheWheelScrollsASheetAndStopsAtBothEnds(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, 14)
	m.mouseOn = true
	drain(m, press(m, "u"))
	m.render()
	md := m.modal
	if md.total <= md.rows {
		t.Skipf("sheet fits: %d/%d", md.total, md.rows)
	}
	wheelAt(m, 5, 5, tea.MouseWheelUp)
	if md.top != 0 {
		t.Fatalf("top = %d above the start", md.top)
	}
	wheelAt(m, 5, 5, tea.MouseWheelDown)
	if md.top != min(3, md.total-md.rows) || !strings.HasSuffix(m.scrollNote, "/"+itoa(md.total)) {
		t.Fatalf("top = %d note = %q", md.top, m.scrollNote)
	}
	for range md.total {
		wheelAt(m, 5, 5, tea.MouseWheelDown)
	}
	if md.top != md.total-md.rows || m.scrollNote != itoa(md.total)+"/"+itoa(md.total) {
		t.Fatalf("top = %d note = %q at the end", md.top, m.scrollNote)
	}
	for range md.total {
		wheelAt(m, 5, 5, tea.MouseWheelUp)
	}
	if md.top != 0 {
		t.Fatalf("top = %d at the start", md.top)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestTheWheelUnderAQuestionScrollsNoBackground(t *testing.T) {
	m, _ := newMouseModel(t)
	before := m.render()
	send(m, "d")
	m.render()
	for range 5 {
		wheelAt(m, 5, 10, tea.MouseWheelDown)
	}
	send(m, "esc")
	if got := m.render(); got != before {
		t.Fatalf("the background moved:\n%s\nwas:\n%s", got, before)
	}
}

func TestATargetOfAnEarlierFrameDoesNotAnswer(t *testing.T) {
	m, b := newMouseModel(t)
	send(m, "d")
	x, y := cellOf(t, m, "yes, do it")
	send(m, "esc")
	clickAt(m, x, y)
	if len(b.actions()) != 0 {
		t.Fatalf("a target of the closed question acted: %v", b.actions())
	}
}
