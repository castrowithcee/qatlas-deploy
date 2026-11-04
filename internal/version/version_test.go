package version

import (
	"errors"
	"regexp"
	"slices"
	"testing"
)

func TestSelect(t *testing.T) {
	plain := []string{"1.2.3", "1.2.4", "1.3.0", "2.0.0", "2.1.0"}
	tests := []struct {
		name      string
		current   string
		tags      []string
		ceiling   string
		filter    string
		want      string
		wantMajor string
		status    Status
	}{
		{"default ceiling is minor", "1.2.3", plain, "", "", "1.3.0", "2.1.0", StatusUpdate},
		{"patch stays in major.minor", "1.2.3", plain, "patch", "", "1.2.4", "2.1.0", StatusUpdate},
		{"minor stays in major", "1.2.3", plain, "minor", "", "1.3.0", "2.1.0", StatusUpdate},
		{"major takes the highest and has no hint", "1.2.3", plain, "major", "", "2.1.0", "", StatusUpdate},
		{"patch without patch release stays", "1.3.0", plain, "patch", "", "1.3.0", "2.1.0", StatusCurrent},
		{"never lower than current", "2.1.0", plain, "major", "", "2.1.0", "", StatusCurrent},
		{"current above all candidates stays", "9.0.0", plain, "", "", "9.0.0", "", StatusCurrent},
		{"numeric order, not text order", "1.2.3", []string{"1.9.0", "1.10.0", "1.2.4"}, "minor", "", "1.10.0", "", StatusUpdate},
		{"major hint only with a major above", "1.2.3", []string{"1.2.4"}, "minor", "", "1.2.4", "", StatusUpdate},
		{"major hint is the highest major candidate", "1.2.3", []string{"2.0.0", "3.4.5", "3.1.0"}, "patch", "", "1.2.3", "3.4.5", StatusCurrent},
		{"variant suffix is kept", "29.0.1-apache",
			[]string{"29.0.1-apache", "29.0.2", "29.0.2-fpm", "29.0.2-apache", "29.0.1-fpm", "30.0.0-apache", "30.0.0-fpm"},
			"minor", "", "29.0.2-apache", "30.0.0-apache", StatusUpdate},
		{"variant alpine with digits", "1.0.0-alpine3.19", []string{"1.0.1-alpine3.19", "1.0.2-alpine3.20", "1.0.3"}, "", "", "1.0.1-alpine3.19", "", StatusUpdate},
		{"plain current ignores variants", "1.0.0", []string{"1.0.1-alpine", "1.0.1"}, "", "", "1.0.1", "", StatusUpdate},
		{"v prefix must match", "v1.0.0", []string{"1.1.0", "v1.0.5", "v1.1.0"}, "", "", "v1.1.0", "", StatusUpdate},
		{"v prefix current without v candidates stays", "v1.0.0", []string{"1.1.0"}, "", "", "v1.0.0", "", StatusCurrent},
		{"pre-releases are skipped", "1.0.0",
			[]string{"1.1.0-rc1", "1.1.0-beta.2", "2.0.0-alpha", "1.0.1-1", "1.0.1-snapshot", "1.0.1-dev", "1.0.1-RC2"}, "major", "", "1.0.0", "", StatusCurrent},
		{"pre-release is skipped but stable wins", "1.0.0", []string{"1.2.0-rc1", "1.1.0"}, "", "", "1.1.0", "", StatusUpdate},
		{"incomplete and foreign tags are ignored", "1.0.0",
			[]string{"1.2", "16", "latest", "1.1.0.1", "v1", "stable", "20240101", "22.04", "main", "1.0.1"}, "", "", "1.0.1", "", StatusUpdate},
		{"calver orders by number", "26.05.5", []string{"26.9.1", "27.01.0", "26.05.6"}, "major", "", "27.01.0", "", StatusUpdate},
		{"calver minor ceiling stays in the year", "26.05.5", []string{"26.09.1", "27.01.0"}, "", "", "26.09.1", "27.01.0", StatusUpdate},
		{"calver year change is manual", "26.05.5", []string{"27.01.0"}, "", "", "26.05.5", "27.01.0", StatusCurrent},
		{"equal core prefers current padding", "26.05.5", []string{"26.09.1", "26.9.1"}, "", "", "26.09.1", "", StatusUpdate},
		{"equal core prefers current padding unpadded", "26.5.5", []string{"26.09.1", "26.9.1"}, "", "", "26.9.1", "", StatusUpdate},
		{"equal core otherwise smallest string", "26.5.5", []string{"26.010.1", "26.10.1", "26.0010.1"}, "", "", "26.0010.1", "", StatusUpdate},
		{"same numbers are not an update", "26.05.5", []string{"26.5.5"}, "", "", "26.05.5", "", StatusCurrent},
		{"filter narrows the candidates", "1.0.0", []string{"1.1.0", "1.2.0", "2.0.0"}, "major", `^1\.1\.`, "1.1.0", "", StatusUpdate},
		{"filter can empty the candidates", "1.0.0", []string{"1.1.0"}, "", `^9\.`, "1.0.0", "", StatusCurrent},
		{"filter applies to the major hint", "1.0.0", []string{"1.1.0", "2.0.0", "3.0.0"}, "minor", `^[12]\.`, "1.1.0", "2.0.0", StatusUpdate},
		{"duplicate tags are harmless", "1.0.0", []string{"1.0.1", "1.0.1"}, "", "", "1.0.1", "", StatusUpdate},
		{"no candidates", "1.0.0", nil, "", "", "1.0.0", "", StatusCurrent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := Input{Current: tc.current, Candidates: tc.tags, Ceiling: tc.ceiling}
			if tc.filter != "" {
				in.Filter = regexp.MustCompile(tc.filter)
			}
			got, err := Select(in)
			if err != nil {
				t.Fatal(err)
			}
			if got.Selected != tc.want || got.MajorAvailable != tc.wantMajor || got.Status != tc.status || got.Current != tc.current {
				t.Errorf("got %+v, want selected %q major %q status %s", got, tc.want, tc.wantMajor, tc.status)
			}
		})
	}
}

func TestSelectNotComparable(t *testing.T) {
	tests := map[string]string{
		"latest": ReasonNotSemver, "16": ReasonNotSemver, "1.2": ReasonNotSemver, "16-alpine": ReasonNotSemver,
		"": ReasonNotSemver, "stable": ReasonNotSemver, "V1.2.3": ReasonNotSemver, "99999999999999999999.0.0": ReasonNotSemver,
		"1.0.0-rc1": ReasonPrerelease, "v2.0.0-beta.2": ReasonPrerelease,
	}
	for current, reason := range tests {
		got, err := Select(Input{Current: current, Candidates: []string{"1.0.0", "2.0.0"}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusNotComparable || got.Reason != reason || got.Selected != "" || got.MajorAvailable != "" {
			t.Errorf("%q: got %+v, want not-comparable (%s)", current, got, reason)
		}
	}
}

func TestSelectRejectsUnknownCeiling(t *testing.T) {
	if _, err := Select(Input{Current: "1.0.0", Ceiling: "huge"}); err == nil {
		t.Error("unknown ceiling accepted")
	}
}

func TestParseTagKinds(t *testing.T) {
	tests := []struct {
		tag  string
		kind parseKind
	}{
		{"1.2.3", kindOK}, {"v1.2.3", kindOK}, {"1.2.3-alpine3.19", kindOK}, {"1.2.3-apache", kindOK},
		{"1.2.3-rc.1", kindPrerelease}, {"1.2.3-RC2", kindPrerelease}, {"1.2.3-0.3", kindPrerelease},
		{"1.2.3-beta-alpine", kindPrerelease}, {"1.2", kindInvalid}, {"V1.2.3", kindInvalid},
		{"99999999999999999999.0.0", kindInvalid},
	}
	for _, tc := range tests {
		if _, kind := parseTag(tc.tag); kind != tc.kind {
			t.Errorf("%q: kind %d, want %d", tc.tag, kind, tc.kind)
		}
	}
}

func TestPath(t *testing.T) {
	tags := []string{"1.0.1", "1.0.2", "1.4.0", "2.0.0", "2.3.1", "2.4.0-rc.1", "4.0.0", "4.1.0", "4.1.0-alpine", "v4.2.0", "5.0", "latest"}
	for _, tc := range []struct {
		current, target string
		stepwise        bool
		filter          string
		want            []string
		err             error
	}{
		{"1.0.1", "4.1.0", true, "", []string{"1.4.0", "2.3.1", "4.1.0"}, nil},
		{"1.0.1", "4.1.0", false, "", []string{"4.1.0"}, nil},
		{"1.4.0", "2.0.0", true, "", []string{"2.0.0"}, nil},
		{"1.0.1", "1.0.2", true, "", []string{"1.0.2"}, nil},
		{"1.0.1", "4.1.0", true, `^(1\.0|2|4)\.`, []string{"1.0.2", "2.3.1", "4.1.0"}, nil},
		{"1.0.1", "4.1.0", true, `^1\.`, nil, ErrFiltered},
		{"1.0.1", "1.0.1", true, "", nil, ErrNotHigher},
		{"2.0.0", "1.4.0", true, "", nil, ErrNotHigher},
		{"1.0.1", "9.9.9", true, "", nil, ErrNotOffered},
		{"1.0.1", "4.1.0-alpine", true, "", nil, ErrOtherVariant},
		{"1.0.1", "v4.2.0", true, "", nil, ErrOtherVariant},
		{"1.0.1", "2.4.0-rc.1", true, "", nil, ErrOtherVariant},
		{"1.0.1", "5.0", true, "", nil, ErrOtherVariant},
		{"latest", "4.1.0", true, "", nil, ErrNotComparable},
		{"1.0.0-rc.1", "4.1.0", true, "", nil, ErrNotComparable},
	} {
		in := Input{Current: tc.current, Candidates: tags}
		if tc.filter != "" {
			in.Filter = regexp.MustCompile(tc.filter)
		}
		got, err := Path(in, tc.target, tc.stepwise)
		if !errors.Is(err, tc.err) || !slices.Equal(got, tc.want) {
			t.Errorf("%s -> %s (stepwise %v, filter %q) = %v, %v; want %v, %v", tc.current, tc.target, tc.stepwise, tc.filter, got, err, tc.want, tc.err)
		}
	}
}

func TestParseCore(t *testing.T) {
	for tag, want := range map[string]Core{
		"1.2.3":         {1, 2, 3, false},
		"v1.2.3":        {1, 2, 3, false},
		"v1.2.3-alpine": {1, 2, 3, false},
		"1.2.3-rc1":     {1, 2, 3, true},
		"1.2.3-1":       {1, 2, 3, true},
	} {
		got, ok := ParseCore(tag)
		if !ok || got != want {
			t.Errorf("%s: %+v %v", tag, got, ok)
		}
	}
	for _, tag := range []string{"", "latest", "1.2", "16", "20240101", "22.04"} {
		if _, ok := ParseCore(tag); ok {
			t.Errorf("%q parsed", tag)
		}
	}
	if c, ok := ParseCore("v26.09.1-ls287"); !ok || c != (Core{26, 9, 1, false}) || c.String() != "26.9.1" {
		t.Errorf("calver: %+v %v", c, ok)
	}
	a, _ := ParseCore("1.9.0")
	b, _ := ParseCore("v1.10.0")
	if a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(a) != 0 || b.String() != "1.10.0" {
		t.Error("Compare or String")
	}
}
