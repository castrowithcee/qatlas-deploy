package engine

import (
	"bytes"
	"testing"
	"time"
)

func TestMonitorRecordRoundTripValidationAndOldState(t *testing.T) {
	since := time.Date(2026, 5, 4, 3, 2, 1, 0, time.UTC)
	s := State{Schema: stateSchema, Stacks: map[string]StackState{"web": {
		Deployed: commit(1), Failure: &Failure{Kind: FailureMissing, Container: "db", Since: since}, Services: []string{"app", "db"},
	}}}
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
	got := back.Stacks["web"]
	if got.Failure == nil || *got.Failure != *s.Stacks["web"].Failure || len(got.Services) != 2 {
		t.Fatalf("round trip = %+v", got)
	}
	// the clone shares nothing
	c := back.clone()
	c.Stacks["web"].Failure.Kind = "x"
	c.Stacks["web"].Services[0] = "x"
	if back.Stacks["web"].Failure.Kind != FailureMissing || back.Stacks["web"].Services[0] != "app" {
		t.Fatal("clone shares memory")
	}

	for name, st := range map[string]StackState{
		"bad kind": {Failure: &Failure{Kind: "weird", Since: since}},
		"no time":  {Failure: &Failure{Kind: FailureExited}},
		"empty":    {Services: []string{""}},
		"too many": {Services: make([]string, MaxMonitorServices+1)},
	} {
		if validateState(State{Schema: stateSchema, Stacks: map[string]StackState{"web": st}}) == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	old := `{"schema":1,"stacks":{"web":{"deployed":"` + commit(1) + `"}}}`
	o, err := decodeState([]byte(old))
	if err != nil || validateState(o) != nil || o.Stacks["web"].Failure != nil {
		t.Fatalf("old state: %v", err)
	}
	if raw, _ := encodeState(o); bytes.Contains(raw, []byte("failure")) || bytes.Contains(raw, []byte("services")) {
		t.Fatalf("empty fields are written: %s", raw)
	}
}

func TestRecordMonitorIsIndependentAndSurvivesRuns(t *testing.T) {
	r := newRig(t, "web", "db")
	r.deployAll()
	f := Failure{Kind: FailureExited, Container: "web-1", Since: r.clock.Now()}
	if err := r.eng.RecordMonitor("web", &f, []string{"web"}); err != nil {
		t.Fatal(err)
	}
	if err := r.eng.RecordMonitor("ghost", &f, nil); err != nil {
		t.Fatal(err)
	}
	if st, ok, _ := r.eng.Stack("ghost"); ok || st.Failure != nil {
		t.Fatal("the monitor created a stack")
	}
	if r.stack("db").Failure != nil {
		t.Fatal("another stack got the failure")
	}
	r.copy.set(func(f *fakeCopy) {
		f.remote = commit(2)
		f.folders[[2]string{commit(1), commit(2)}] = []string{"web"}
	})
	r.poll()
	st := r.stack("web")
	if st.Deployed != commit(2) || st.Failure == nil || st.Failure.Kind != FailureExited || len(st.Services) != 1 {
		t.Fatalf("after a run: %+v", st)
	}
	if got := r.start().stackOrZero(t, "web"); got.Failure == nil {
		t.Fatal("not reloaded")
	}
	if err := r.eng.RecordMonitor("web", nil, nil); err != nil || r.stack("web").Failure != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := r.eng.RecordMonitor("web", &Failure{Kind: "weird", Since: f.Since}, nil); err == nil {
		t.Fatal("invalid failure stored")
	}
}

func (e *Engine) stackOrZero(t *testing.T, name string) StackState {
	t.Helper()
	st, _, err := e.Stack(name)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestStoppedContainersMarkPersistsValidatesAndLoadsFromOldState(t *testing.T) {
	r := newRig(t, "web")
	r.deployAll()
	if err := r.eng.UpdateStoppedContainers("web", []string{"c1", "c2"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.eng.UpdateStoppedContainers("web", []string{"c2"}, []string{"c1"}); err != nil {
		t.Fatal(err)
	}
	if got := r.start().stackOrZero(t, "web").StoppedContainers; len(got) != 1 || got[0] != "c2" {
		t.Fatalf("after reload: %v", got)
	}
	if err := r.eng.UpdateStoppedContainers("web", []string{""}, nil); err == nil {
		t.Fatal("empty id accepted")
	}
	if err := r.eng.UpdateStoppedContainers("ghost", []string{"c"}, nil); err != nil {
		t.Fatal(err)
	}
	s := State{Schema: stateSchema, Stacks: map[string]StackState{"web": {StoppedContainers: []string{"c2"}}}}
	c := s.clone()
	c.Stacks["web"].StoppedContainers[0] = "x"
	if s.Stacks["web"].StoppedContainers[0] != "c2" {
		t.Fatal("clone shares memory")
	}
	o, err := decodeState([]byte(`{"schema":1,"stacks":{"web":{"deployed":"` + commit(1) + `"}}}`))
	if err != nil || validateState(o) != nil || o.Stacks["web"].StoppedContainers != nil {
		t.Fatalf("old state: %v", err)
	}
}
