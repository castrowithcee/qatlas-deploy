package updates

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/registry"
	"github.com/castrowithcee/qatlas-deploy/internal/registry/registrytest"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const (
	fakeUser  = "fake-user-77"
	fakeToken = "fake-token-88"
	fakeWrong = "fake-token-99"
	host      = "registry.example.test"
)

var demoTags = []string{"1.0.0", "1.0.1", "1.1.0", "2.0.0", "2.1.0-rc.1"}

func stackOf(name, ceiling string, filter map[string]*regexp.Regexp, imgs ...stacks.ImageVar) stacks.Stack {
	return stacks.Stack{Name: name, Images: imgs, Rules: &stacks.Rules{Update: stacks.Update{Ceiling: ceiling, Tags: filter}}}
}

func imgVar(variable, image, value string) stacks.ImageVar {
	return stacks.ImageVar{Service: "app", Image: image, Var: variable, Value: value, Source: stacks.SourceEnv}
}

func detector(url string, cred func(string) (registry.Credential, error)) *Detector {
	return &Detector{
		Credential: cred,
		Options:    registry.Options{BaseURL: func(string) string { return url }, MinInterval: -1},
		Now:        func() time.Time { return time.Date(2026, 5, 4, 3, 2, 1, 0, time.UTC) },
	}
}

func anonymous(string) (registry.Credential, error) { return registry.Credential{}, nil }

func check(t *testing.T, d *Detector, list ...stacks.Stack) map[string]engine.UpdateCheck {
	t.Helper()
	out, err := d.Check(context.Background(), list)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSelectionPerCeiling(t *testing.T) {
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": demoTags}})
	for _, tc := range []struct{ ceiling, selected, major, status string }{
		{"patch", "1.0.1", "2.0.0", engine.UpdateAvailable},
		{"minor", "1.1.0", "2.0.0", engine.UpdateAvailable},
		{"major", "2.0.0", "", engine.UpdateAvailable},
	} {
		got := check(t, detector(srv.URL, anonymous), stackOf("s", tc.ceiling, nil, imgVar("APP_VERSION", host+"/demo/app", "1.0.0")))
		items := got["s"].Items
		if len(items) != 1 || items[0].Selected != tc.selected || items[0].MajorAvailable != tc.major || items[0].Status != tc.status || items[0].Current != "1.0.0" {
			t.Errorf("ceiling %s: %+v", tc.ceiling, items)
		}
		if got["s"].Checked.IsZero() {
			t.Errorf("ceiling %s: no check time", tc.ceiling)
		}
	}
}

func TestCurrentAndFilterAndNoRules(t *testing.T) {
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": demoTags}})
	plain := stacks.Stack{Name: "plain", Images: []stacks.ImageVar{imgVar("V", host+"/demo/app", "2.0.0")}}
	filtered := stackOf("filtered", "major", map[string]*regexp.Regexp{"V": regexp.MustCompile(`^1\.0\.`)}, imgVar("V", host+"/demo/app", "1.0.0"))
	got := check(t, detector(srv.URL, anonymous), plain, filtered)
	if it := got["plain"].Items[0]; it.Status != engine.UpdateCurrent || it.Selected != "2.0.0" {
		t.Errorf("plain: %+v", it)
	}
	if it := got["filtered"].Items[0]; it.Status != engine.UpdateAvailable || it.Selected != "1.0.1" {
		t.Errorf("filtered: %+v", it)
	}
}

func TestErrorsAreRecordedPerVariable(t *testing.T) {
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": demoTags}})
	dead := httptest.NewServer(nil)
	deadURL := dead.URL
	dead.Close()
	d := detector(srv.URL, anonymous)
	d.Options.BaseURL = func(h string) string {
		if h == "down.example.test" {
			return deadURL
		}
		return srv.URL
	}
	got := check(t, d, stackOf("s", "", nil,
		imgVar("GOOD", host+"/demo/app", "1.0.0"),
		imgVar("MISSING", host+"/demo/nothing", "1.0.0"),
		imgVar("DOWN", "down.example.test/demo/app", "1.0.0"),
		imgVar("LATEST", host+"/demo/app", "latest"),
		imgVar("EMPTY", host+"/demo/app", ""),
		imgVar("BADREF", "bad ref", "1.0.0"),
	))
	by := map[string]engine.UpdateItem{}
	for _, it := range got["s"].Items {
		by[it.Variable] = it
	}
	if it := by["GOOD"]; it.Status != engine.UpdateAvailable || it.Selected != "1.1.0" || it.Error != "" {
		t.Errorf("GOOD: %+v", it)
	}
	for v, want := range map[string]struct{ status, text string }{
		"MISSING": {engine.UpdateError, textNotFound},
		"DOWN":    {engine.UpdateError, textUnreachable},
		"LATEST":  {engine.UpdateNotComparable, textNotSemver},
		"EMPTY":   {engine.UpdateNotComparable, textNoValue},
		"BADREF":  {engine.UpdateError, textBadImage},
	} {
		if it := by[v]; it.Status != want.status || it.Error != want.text || it.Selected != "" {
			t.Errorf("%s: %+v", v, it)
		}
	}
}

func TestOneListingPerRepositoryAndRun(t *testing.T) {
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": demoTags}})
	img := host + "/demo/app"
	check(t, detector(srv.URL, anonymous),
		stackOf("a", "", nil, imgVar("ONE", img, "1.0.0"), imgVar("TWO", img, "1.0.1")),
		stackOf("b", "major", nil, imgVar("ONE", img, "1.0.0")))
	n := 0
	for _, r := range srv.Requests() {
		if strings.HasSuffix(r, "/tags/list") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("tag lists = %d, want 1: %v", n, srv.Requests())
	}
}

func TestCredentialLookupPerHostAndNoLeak(t *testing.T) {
	srv := registrytest.New(t, registrytest.Config{
		Auth: registrytest.AuthBasic, User: fakeUser, Password: fakeToken, Repos: map[string][]string{"demo/app": demoTags},
	})
	var mu sync.Mutex
	var hosts []string
	lookup := func(h string) (registry.Credential, error) {
		mu.Lock()
		hosts = append(hosts, h)
		mu.Unlock()
		switch h {
		case host:
			return registry.Credential{User: fakeUser, Token: fakeToken}, nil
		case "wrong.example.test":
			return registry.Credential{User: fakeUser, Token: fakeWrong}, nil
		}
		return registry.Credential{}, errors.New("lookup failed for " + fakeToken)
	}
	got := check(t, detector(srv.URL, lookup), stackOf("s", "", nil,
		imgVar("OK", host+"/demo/app", "1.0.0"),
		imgVar("OK2", host+"/demo/app", "1.0.1"),
		imgVar("WRONG", "wrong.example.test/demo/app", "1.0.0"),
		imgVar("NONE", "none.example.test/demo/app", "1.0.0"),
	))
	slices.Sort(hosts)
	if !slices.Equal(hosts, []string{"none.example.test", host, "wrong.example.test"}) {
		t.Errorf("lookup hosts = %v", hosts)
	}
	by := map[string]engine.UpdateItem{}
	for _, it := range got["s"].Items {
		by[it.Variable] = it
	}
	if by["OK"].Selected != "1.1.0" || by["OK2"].Status != engine.UpdateAvailable {
		t.Errorf("authorized: %+v %+v", by["OK"], by["OK2"])
	}
	if it := by["WRONG"]; it.Status != engine.UpdateError || it.Error != textAuth {
		t.Errorf("WRONG: %+v", it)
	}
	if it := by["NONE"]; it.Status != engine.UpdateError || it.Error != textNoCredential {
		t.Errorf("NONE: %+v", it)
	}
	raw, _ := json.Marshal(got)
	for _, secret := range []string{fakeUser, fakeToken, fakeWrong} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("result holds %q: %s", secret, raw)
		}
	}
}

func TestUnreadableStackHasNoEntryAndCancelStops(t *testing.T) {
	srv := registrytest.New(t, registrytest.Config{Repos: map[string][]string{"demo/app": demoTags}})
	broken := stacks.Stack{Name: "broken", Err: errors.New("broken")}
	got := check(t, detector(srv.URL, anonymous), broken)
	if _, ok := got["broken"]; ok {
		t.Errorf("unreadable stack has an entry: %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := detector(srv.URL, anonymous).Check(ctx, []stacks.Stack{stackOf("s", "", nil, imgVar("V", host+"/demo/app", "1.0.0"))}); err == nil {
		t.Error("a cancelled run returned results")
	}
}

func TestLongValuesAreBounded(t *testing.T) {
	srv := registrytest.New(t, registrytest.Config{})
	long := strings.Repeat("v", 5000)
	got := check(t, detector(srv.URL, anonymous), stackOf("s", "", nil, imgVar(long, host+"/demo/app", long)))
	it := got["s"].Items[0]
	if len(it.Variable) > engine.MaxUpdateText || len(it.Current) > engine.MaxUpdateText || it.Status != engine.UpdateNotComparable {
		t.Errorf("unbounded: %d %d %s", len(it.Variable), len(it.Current), it.Status)
	}
}
