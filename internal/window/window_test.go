package window

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("zone database lacks %s: %v", name, err)
	}
	return loc
}

func utc(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

func mustDays(t *testing.T, names ...string) Days {
	t.Helper()
	d, err := ParseDays(names)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func wantInvalid(t *testing.T, what string, err error) {
	t.Helper()
	var ie *InvalidError
	if !errors.As(err, &ie) || ie.Text == "" {
		t.Fatalf("%s: got %v, want an invalid error", what, err)
	}
}

func TestParseDays(t *testing.T) {
	if d := mustDays(t, "MON", "wednesday", "Fri"); !slices.Equal(d.Names(), []string{"mon", "wed", "fri"}) {
		t.Fatalf("names = %v", d.Names())
	}
	if d := mustDays(t, "sun", "mon"); !slices.Equal(d.Names(), []string{"mon", "sun"}) {
		t.Fatalf("week order = %v", d.Names())
	}
	if mustDays(t, "daily") != EveryDay || mustDays(t, "Daily") != EveryDay {
		t.Fatal("daily is not every day")
	}
	for _, bad := range [][]string{nil, {}, {""}, {"funday"}, {"mo"}, {"mon", "monday"}, {"daily", "mon"}, {" mon"},
		{"mon", "tue", "wed", "thu", "fri", "sat", "sun", "mon"}} {
		_, err := ParseDays(bad)
		wantInvalid(t, fmt.Sprintf("days %q", bad), err)
	}
}

func TestParseStartDurationZone(t *testing.T) {
	for s, want := range map[string]int{"00:00": 0, "03:00": 180, "23:59": 1439, "12:05": 725} {
		if got, err := ParseStart(s); err != nil || got != want || FormatStart(got) != s {
			t.Fatalf("start %s = %d, %v", s, got, err)
		}
	}
	for _, bad := range []string{"", "3:00", "24:00", "12:60", "12:5", "12:00:00", " 12:00", "12h00", "-1:00"} {
		_, err := ParseStart(bad)
		wantInvalid(t, "start "+bad, err)
	}
	for s, want := range map[string]time.Duration{"2h": 2 * time.Hour, "90m": 90 * time.Minute, "1h30m": 90 * time.Minute,
		"12h": MaxDuration, "1m": time.Minute, "720m": MaxDuration} {
		if got, err := ParseDuration(s); err != nil || got != want {
			t.Fatalf("duration %s = %v, %v", s, got, err)
		}
	}
	for _, bad := range []string{"", "0m", "0h", "12h1m", "13h", "30s", "1.5h", "-1h", "2h0m30s", "1d", "2H", "h", "99999h"} {
		_, err := ParseDuration(bad)
		wantInvalid(t, "duration "+bad, err)
	}
	for d, want := range map[time.Duration]string{2 * time.Hour: "2h", 45 * time.Minute: "45m", 90 * time.Minute: "1h30m"} {
		if got := FormatDuration(d); got != want {
			t.Fatalf("format %v = %s", d, got)
		}
	}
	for _, good := range []string{"local", "UTC", "Europe/Berlin", "America/New_York", "Etc/GMT+5"} {
		if got, err := ParseZone(good); err != nil || got != good {
			t.Fatalf("zone %s = %q, %v", good, got, err)
		}
	}
	for _, bad := range []string{"", "Local", "LOCAL", "../etc/passwd", "/etc/localtime", "Mars/Olympus_Mons", "Europe/../UTC",
		"Europe//Berlin", "Europe/Berlin/", "UTC\n", "a b"} {
		_, err := ParseZone(bad)
		wantInvalid(t, "zone "+bad, err)
	}
}

func TestDefaultWindow(t *testing.T) {
	w := Default()
	if w.Days != EveryDay || FormatStart(w.Start) != "03:00" || w.Duration != 2*time.Hour || w.Zone != ZoneLocal || w.check() != nil {
		t.Fatalf("default = %+v", w)
	}
	if loc, err := w.Location(); err != nil || loc != time.Local {
		t.Fatalf("local zone = %v, %v", loc, err)
	}
}

func TestOpeningsAcrossWeekdays(t *testing.T) {
	w := Window{Days: mustDays(t, "mon", "wed", "fri"), Start: 180, Duration: 2 * time.Hour, Zone: "UTC"}
	for _, tc := range []struct {
		now       time.Time
		open      bool
		cur, next time.Time
		desc      string
	}{
		{utc(2026, 11, 6, 5, 30), false, time.Time{}, utc(2026, 11, 9, 3, 0), "Friday after the window: next Monday"},
		{utc(2026, 11, 4, 2, 59), false, time.Time{}, utc(2026, 11, 4, 3, 0), "Wednesday just before"},
		{utc(2026, 11, 4, 3, 0), true, utc(2026, 11, 4, 3, 0), utc(2026, 11, 6, 3, 0), "the start belongs to the window"},
		{utc(2026, 11, 4, 4, 59), true, utc(2026, 11, 4, 3, 0), utc(2026, 11, 6, 3, 0), "last minute"},
		{utc(2026, 11, 4, 5, 0), false, time.Time{}, utc(2026, 11, 6, 3, 0), "the end does not"},
		{utc(2026, 11, 3, 3, 30), false, time.Time{}, utc(2026, 11, 4, 3, 0), "Tuesday is no window day"},
		{utc(2026, 11, 8, 23, 0), false, time.Time{}, utc(2026, 11, 9, 3, 0), "Sunday night"},
	} {
		cur, open, err := w.At(tc.now)
		if err != nil || open != tc.open || (open && (!cur.Start.Equal(tc.cur) || !cur.End.Equal(tc.cur.Add(2*time.Hour)))) {
			t.Fatalf("%s: at = %+v %v %v", tc.desc, cur, open, err)
		}
		next, err := w.Next(tc.now)
		if err != nil || !next.Start.Equal(tc.next) {
			t.Fatalf("%s: next = %v, %v; want %v", tc.desc, next.Start, err, tc.next)
		}
	}

	weekly := Window{Days: mustDays(t, "tue"), Start: 600, Duration: time.Hour, Zone: "UTC"}
	if next, err := weekly.Next(utc(2026, 11, 3, 10, 0)); err != nil || !next.Start.Equal(utc(2026, 11, 10, 10, 0)) {
		t.Fatalf("weekly next = %v, %v", next.Start, err)
	}
}

func TestWindowOverMidnightBelongsToItsDay(t *testing.T) {
	w := Window{Days: mustDays(t, "mon"), Start: 23*60 + 30, Duration: 2 * time.Hour, Zone: "UTC"}
	cur, open, err := w.At(utc(2026, 11, 3, 0, 30)) // Tuesday
	if err != nil || !open || !cur.Start.Equal(utc(2026, 11, 2, 23, 30)) || !cur.End.Equal(utc(2026, 11, 3, 1, 30)) {
		t.Fatalf("at = %+v %v %v", cur, open, err)
	}
	if _, open, _ := w.At(utc(2026, 11, 3, 1, 30)); open {
		t.Fatal("open after its end")
	}
}

func TestOpeningsInTimeZones(t *testing.T) {
	now := utc(2026, 11, 3, 0, 0)
	for name, want := range map[string]time.Time{
		"UTC":              utc(2026, 11, 3, 3, 0),
		"Europe/Berlin":    utc(2026, 11, 3, 2, 0),
		"America/New_York": utc(2026, 11, 3, 8, 0),
		"Asia/Tokyo":       utc(2026, 11, 3, 18, 0),
	} {
		loc := zone(t, name)
		w := Window{Days: EveryDay, Start: 180, Duration: 2 * time.Hour, Zone: name}
		next, err := w.Next(now)
		if err != nil || !next.Start.Equal(want) || next.Start.Location().String() != loc.String() {
			t.Fatalf("%s: next = %v, %v; want %v", name, next.Start, err, want)
		}
		if h, m, _ := next.Start.Clock(); h != 3 || m != 0 {
			t.Fatalf("%s: wall clock %d:%d", name, h, m)
		}
	}
}

func TestOpeningsAcrossDSTChanges(t *testing.T) {
	berlin := zone(t, "Europe/Berlin")
	w := Window{Days: EveryDay, Start: 180, Duration: 2 * time.Hour, Zone: "Europe/Berlin"}
	// spring forward on 2026-03-29: 03:00 CET, then 03:00 CEST
	if got := openings(t, w, utc(2026, 3, 28, 0, 0), 3); !slices.Equal(got, []time.Time{
		utc(2026, 3, 28, 2, 0), utc(2026, 3, 29, 1, 0), utc(2026, 3, 30, 1, 0)}) {
		t.Fatalf("spring = %v", got)
	}
	// fall back on 2026-10-25: 03:00 CEST, then 03:00 CET
	if got := openings(t, w, utc(2026, 10, 24, 0, 0), 3); !slices.Equal(got, []time.Time{
		utc(2026, 10, 24, 1, 0), utc(2026, 10, 25, 2, 0), utc(2026, 10, 26, 2, 0)}) {
		t.Fatalf("fall = %v", got)
	}

	// a window across the change lasts its duration in elapsed time
	across := Window{Days: EveryDay, Start: 90, Duration: 2 * time.Hour, Zone: "Europe/Berlin"}
	cur, open, err := across.At(utc(2026, 3, 29, 2, 0))
	if err != nil || !open || !cur.Start.Equal(utc(2026, 3, 29, 0, 30)) || !cur.End.Equal(utc(2026, 3, 29, 2, 30)) {
		t.Fatalf("across = %+v %v %v", cur, open, err)
	}
	if h, m, _ := cur.End.In(berlin).Clock(); h != 4 || m != 30 {
		t.Fatalf("end wall clock %d:%d", h, m)
	}

	// a start the clocks skip still opens exactly once that day
	gap := Window{Days: EveryDay, Start: 150, Duration: time.Hour, Zone: "Europe/Berlin"}
	day := openings(t, gap, utc(2026, 3, 28, 12, 0), 2)
	if !day[0].After(utc(2026, 3, 29, 0, 0)) || !day[0].Before(utc(2026, 3, 29, 2, 0)) || !day[1].Equal(utc(2026, 3, 30, 0, 30)) {
		t.Fatalf("gap = %v", day)
	}
}

func openings(t *testing.T, w Window, from time.Time, n int) []time.Time {
	t.Helper()
	var out []time.Time
	for range n {
		o, err := w.Next(from)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, o.Start.UTC())
		from = o.Start
	}
	return out
}

func TestUnknownZoneCannotBeComputed(t *testing.T) {
	w := Window{Days: EveryDay, Start: 0, Duration: time.Hour, Zone: "Nowhere/Atlantis"}
	if _, err := w.Next(utc(2026, 1, 1, 0, 0)); err == nil {
		t.Fatal("computed a window in an unknown zone")
	}
	if _, _, err := (Window{}).At(utc(2026, 1, 1, 0, 0)); err == nil {
		t.Fatal("computed an empty window")
	}
}
