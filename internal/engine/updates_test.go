package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var sampleCheck = UpdateCheck{
	Checked: time.Date(2026, 5, 4, 3, 2, 1, 0, time.UTC),
	Items: []UpdateItem{
		{Variable: "APP_VERSION", Image: "registry.example.test/demo/app", Current: "1.0.0", Selected: "1.1.0", MajorAvailable: "2.0.0", Status: UpdateAvailable},
		{Variable: "DB_VERSION", Image: "registry.example.test/demo/db", Current: "latest", Status: UpdateNotComparable, Error: "the current tag is not a complete semantic version"},
	},
}

func TestUpdatesRoundTripAndValidation(t *testing.T) {
	s := State{Schema: stateSchema, Stacks: map[string]StackState{"web": {Updates: &sampleCheck}}}
	if err := validateState(s); err != nil {
		t.Fatal(err)
	}
	raw, err := encodeState(s)
	if err != nil {
		t.Fatal(err)
	}
	back, err := decodeState(raw)
	if err != nil || validateState(back) != nil {
		t.Fatalf("decode: %v", err)
	}
	got := back.Stacks["web"].Updates
	if got == nil || !got.Checked.Equal(sampleCheck.Checked) || len(got.Items) != 2 || got.Items[0] != sampleCheck.Items[0] {
		t.Fatalf("round trip = %+v", got)
	}

	bad := map[string]UpdateCheck{
		"no time":     {Items: sampleCheck.Items},
		"bad status":  {Checked: sampleCheck.Checked, Items: []UpdateItem{{Variable: "V", Image: "i", Status: "weird"}}},
		"no variable": {Checked: sampleCheck.Checked, Items: []UpdateItem{{Image: "i", Status: UpdateCurrent}}},
		"long text":   {Checked: sampleCheck.Checked, Items: []UpdateItem{{Variable: "V", Image: "i", Status: UpdateError, Error: strings.Repeat("x", MaxUpdateText+1)}}},
		"too many":    {Checked: sampleCheck.Checked, Items: make([]UpdateItem, MaxUpdateItems+1)},
	}
	for name, u := range bad {
		u := u
		if err := validateState(State{Schema: stateSchema, Stacks: map[string]StackState{"web": {Updates: &u}}}); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestStateWithoutUpdatesStillDecodes(t *testing.T) {
	old := `{"schema":1,"stacks":{"web":{"deployed":"` + commit(1) + `"}}}`
	s, err := decodeState([]byte(old))
	if err != nil || validateState(s) != nil {
		t.Fatalf("old state: %v", err)
	}
	if s.Stacks["web"].Updates != nil {
		t.Fatal("absent field decoded as a check")
	}
	if raw, _ := encodeState(s); bytes.Contains(raw, []byte("updates")) {
		t.Fatalf("a never-checked stack writes the field: %s", raw)
	}
}

func TestRecordUpdatesSurvivesRunsAndIsIndependent(t *testing.T) {
	r := newRig(t, "web", "db")
	r.deployAll()
	if err := r.eng.RecordUpdates("web", sampleCheck); err != nil {
		t.Fatal(err)
	}
	if r.stack("db").Updates != nil {
		t.Fatal("another stack got the check")
	}
	// A deploy run rewrites the stack's result; the check must stay.
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
	})
	r.poll()
	st := r.stack("web")
	if st.Deployed != commit(2) || st.Updates == nil || len(st.Updates.Items) != 2 {
		t.Fatalf("after a run: %+v", st)
	}
	// A restart reads it back from the file.
	again := r.start()
	if got, _, err := again.Stack("web"); err != nil || got.Updates == nil || !got.Updates.Checked.Equal(sampleCheck.Checked) {
		t.Fatalf("after reload: %+v, %v", got, err)
	}
	// A new check replaces the old one as a whole.
	next := UpdateCheck{Checked: sampleCheck.Checked.Add(time.Hour)}
	if err := r.eng.RecordUpdates("web", next); err != nil {
		t.Fatal(err)
	}
	if got := r.stack("web").Updates; len(got.Items) != 0 || !got.Checked.Equal(next.Checked) {
		t.Fatalf("replaced = %+v", got)
	}
}

func TestRecordUpdatesRedactsAndRejectsInvalid(t *testing.T) {
	r := newRig(t, "web")
	r.redac.Add("fake-secret-123")
	c := UpdateCheck{Checked: sampleCheck.Checked, Items: []UpdateItem{{Variable: "V", Image: "i", Status: UpdateError, Error: "failed with fake-secret-123"}}}
	if err := r.eng.RecordUpdates("web", c); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(r.state, engineDir, stateFile))
	if err != nil || bytes.Contains(raw, []byte("fake-secret-123")) {
		t.Fatalf("state: %v %s", err, raw)
	}
	if err := r.eng.RecordUpdates("web", UpdateCheck{Items: c.Items}); err == nil {
		t.Fatal("a check without a time was stored")
	}
}
