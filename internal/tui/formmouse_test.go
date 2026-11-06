package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

const (
	saveButton    = "[ F2 save ]"
	discardButton = "[ esc discard ]"
)

func newFormMouseModel(t *testing.T, to ...string) *Model {
	t.Helper()
	m := newSettingsModel(t, settingsBackend())
	m.mouseOn = true
	send(m, to...)
	m.render()
	return m
}

func clickOn(t *testing.T, m *Model, needle string) {
	t.Helper()
	x, y := cellOf(t, m, needle)
	clickAt(m, x, y)
}

func TestAClickOnSaveActsLikeEnterOnSave(t *testing.T) {
	m := newFormMouseModel(t, toRepo...)
	send(m, "b")
	clickOn(t, m, saveButton)
	if !formOpen(m) || m.modal != nil {
		t.Fatal("save of an empty form")
	}
	contains(t, m, "SSH address is required")

	m = newFormMouseModel(t, secretFormKeysFromStart()...)
	clickOn(t, m, saveButton)
	if m.modal == nil {
		t.Fatal("a click on save did not ask for the confirmation F2 asks for")
	}
}

func TestAClickOnDiscardActsLikeEscape(t *testing.T) {
	m := newFormMouseModel(t, toRepo...)
	send(m, "b")
	clickOn(t, m, discardButton)
	if formOpen(m) || m.modal != nil {
		t.Fatal("an unchanged form stays or asks")
	}

	m = newFormMouseModel(t, secretFormKeysFromStart()...)
	form := m.set.form
	clickOn(t, m, discardButton)
	if m.modal == nil || m.set.form != form {
		t.Fatal("a changed form was not asked about")
	}
	values := form.form.Values()
	clickOn(t, m, saveButton) // the question is open: nothing reaches the form
	if m.set.form != form || !equalValues(values, form.form.Values()) {
		t.Fatal("a click reached the form under the question")
	}
	send(m, "enter") // cancel
	if m.modal != nil || m.set.form != form {
		t.Fatal("cancel did not keep the form")
	}
	clickOn(t, m, discardButton)
	send(m, "y")
	if formOpen(m) {
		t.Fatal("yes keeps the form")
	}
}

func TestAClickOnATextFieldTakesTheFocusAndTypesNothing(t *testing.T) {
	m := newFormMouseModel(t, seq(toRepo, keys("b"))...)
	form := m.set.form.form
	before := form.Focused()
	clickOn(t, m, "Directory")
	if form.Focused() == before {
		t.Fatalf("the focus stayed on %s", before)
	}
	for _, v := range form.Values() {
		if v.Text != "" {
			t.Fatalf("a click typed %q", v.Text)
		}
	}
	if form.Changed() {
		t.Fatal("a click changed the form")
	}
}

func TestAClickOnAChoiceValueSelectsIt(t *testing.T) {
	m := newFormMouseModel(t, seq(toCreds, keys("a"))...)
	form := m.set.form.form
	clickOn(t, m, "registry login")
	clickOn(t, m, "GitHub token for changelogs")
	if got := form.Values()["kind"].Choice; got != credGitHub {
		t.Fatalf("kind = %q", got)
	}
}

func TestTheWheelMovesTheFocusOfTheForm(t *testing.T) {
	m := newFormMouseModel(t, seq(toCreds, keys("a"))...)
	form := m.set.form.form
	before := form.Focused()
	x, y := cellOf(t, m, "Registry host")
	_, cmd := m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	drain(m, cmd)
	if form.Focused() == before {
		t.Fatal("the wheel did not move the focus")
	}
}

func TestNoClickActsWhileAKeyIsCaptured(t *testing.T) {
	m := newFormMouseModel(t, seq(toRepo, keys("b"))...)
	f := m.set.form
	key, err := kittui.NewForm(kittui.FormSpec{Fields: []kittui.FormField{
		{ID: "k", Kind: kittui.FieldKey, Label: textFieldDir},
	}, Keys: formKeys(), Buttons: []kittui.FormButton{{Action: actBack, Label: kittui.TextKeyDiscard}}}, m.keys, m.text)
	if err != nil {
		t.Fatal(err)
	}
	f.form = key
	send(m, "enter") // the field waits for a key
	if key.Input() != kittui.InputKeyCapture {
		t.Fatalf("input = %v", key.Input())
	}
	m.render()
	clickOn(t, m, discardButton)
	if !formOpen(m) {
		t.Fatal("a click acted while a key was captured")
	}
}

func TestAFormTargetOfAnEarlierFrameDoesNotAct(t *testing.T) {
	m := newFormMouseModel(t, seq(toRepo, keys("b"))...)
	x, y := cellOf(t, m, discardButton)
	m.set.form = nil
	m.render() // the Settings page: the old button target is gone
	clickAt(m, x, y)
	if m.modal != nil || m.set.form != nil {
		t.Fatal("a target of the closed form acted")
	}
}
