// Package window holds the maintenance window of the host: the setting, when it opens, and the scheduler that
// runs the window's work once per opening. It knows nothing of stacks; the work is the caller's.
package window

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Limits and defaults of the setting.
const (
	// ZoneLocal is the zone name that stands for the host's local time zone.
	ZoneLocal = "local"
	// MaxDuration bounds a window, so two openings on consecutive days never overlap, also across a DST change.
	MaxDuration = 12 * time.Hour
	maxZoneLen  = 64
	minutesDay  = 24 * 60
)

// InvalidError is a setting refused by validation. Its text is fixed and never repeats the refused value.
type InvalidError struct{ Text string }

func (e *InvalidError) Error() string { return e.Text }

func invalid(text string) error { return &InvalidError{Text: text} }

// Days is a set of weekdays; bit i stands for time.Weekday(i).
type Days uint8

// EveryDay holds all seven weekdays.
const EveryDay Days = 1<<7 - 1

var (
	shortDays = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
	weekOrder = [7]time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}
)

// Has reports whether the set holds w.
func (d Days) Has(w time.Weekday) bool { return d&(1<<w) != 0 }

// Names returns the three-letter names of the set, Monday first.
func (d Days) Names() []string {
	out := []string{}
	for _, w := range weekOrder {
		if d.Has(w) {
			out = append(out, shortDays[w])
		}
	}
	return out
}

// ParseDays reads weekday names: the English three-letter abbreviation or the full name, in any case, each at
// most once, or the single word "daily" for every day.
func ParseDays(names []string) (Days, error) {
	if len(names) == 1 && strings.EqualFold(names[0], "daily") {
		return EveryDay, nil
	}
	if len(names) == 0 || len(names) > 7 {
		return 0, invalid("name one to seven weekdays, or daily")
	}
	var d Days
	for _, n := range names {
		w, ok := parseDay(n)
		if !ok {
			return 0, invalid("a weekday is not one of mon, tue, wed, thu, fri, sat, sun")
		}
		if d.Has(w) {
			return 0, invalid("a weekday is named more than once")
		}
		d |= 1 << w
	}
	return d, nil
}

func parseDay(s string) (time.Weekday, bool) {
	s = strings.ToLower(s)
	for i, short := range shortDays {
		if s == short || s == strings.ToLower(time.Weekday(i).String()) {
			return time.Weekday(i), true
		}
	}
	return 0, false
}

var startRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

// ParseStart reads a start time HH:MM on the 24-hour clock and returns the minutes after midnight.
func ParseStart(s string) (int, error) {
	m := startRe.FindStringSubmatch(s)
	if m == nil {
		return 0, invalid("the start time must be HH:MM on the 24-hour clock, from 00:00 to 23:59")
	}
	h, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	return h*60 + min, nil
}

// FormatStart writes minutes after midnight as HH:MM.
func FormatStart(m int) string {
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

var durationRe = regexp.MustCompile(`^[0-9]{1,4}(h|m|h[0-9]{1,2}m)$`)

// ParseDuration reads a duration of whole minutes such as 2h, 90m or 1h30m, from one minute to MaxDuration.
func ParseDuration(s string) (time.Duration, error) {
	text := "the duration must be whole minutes such as 2h, 90m or 1h30m, from 1m to " + FormatDuration(MaxDuration)
	if !durationRe.MatchString(s) {
		return 0, invalid(text)
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < time.Minute || d > MaxDuration {
		return 0, invalid(text)
	}
	return d, nil
}

// FormatDuration writes a duration of whole minutes as 2h, 45m or 1h30m.
func FormatDuration(d time.Duration) string {
	h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
	switch {
	case m == 0:
		return strconv.Itoa(h) + "h"
	case h == 0:
		return strconv.Itoa(m) + "m"
	}
	return strconv.Itoa(h) + "h" + strconv.Itoa(m) + "m"
}

var zoneRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+)*$`)

// ParseZone checks a zone name: ZoneLocal or an IANA time zone name the host's zone database knows.
func ParseZone(s string) (string, error) {
	if s == ZoneLocal {
		return s, nil
	}
	if !validZoneName(s) {
		return "", invalid("the zone must be local or an IANA time zone name such as Europe/Berlin or UTC")
	}
	if _, err := time.LoadLocation(s); err != nil {
		return "", invalid("the time zone is not known to the host's zone database")
	}
	return s, nil
}

// validZoneName checks the form only. "Local" is refused because the zone database would read it as the host
// zone under a second name.
func validZoneName(s string) bool {
	return s == ZoneLocal || (len(s) <= maxZoneLen && zoneRe.MatchString(s) && s != "Local")
}

// Window is the maintenance window: the weekdays it opens on, its start on the wall clock of Zone and how long
// it stays open. A window that opens shortly before midnight stays open into the next day; the day is the day
// it opens.
type Window struct {
	Days     Days
	Start    int // minutes after midnight
	Duration time.Duration
	Zone     string
}

// Default is the window of a host that never set one: every day at 03:00 for two hours, local time.
func Default() Window {
	return Window{Days: EveryDay, Start: 3 * 60, Duration: 2 * time.Hour, Zone: ZoneLocal}
}

// check validates the form of every part; it does not ask the zone database.
func (w Window) check() error {
	switch {
	case w.Days == 0 || w.Days&^EveryDay != 0:
		return invalid("name one to seven weekdays, or daily")
	case w.Start < 0 || w.Start >= minutesDay:
		return invalid("the start time must be from 00:00 to 23:59")
	case w.Duration < time.Minute || w.Duration > MaxDuration || w.Duration%time.Minute != 0:
		return invalid("the duration must be whole minutes from 1m to " + FormatDuration(MaxDuration))
	case !validZoneName(w.Zone):
		return invalid("the zone must be local or an IANA time zone name")
	}
	return nil
}

// Location resolves the zone; ZoneLocal is the host's local time zone as the process sees it.
func (w Window) Location() (*time.Location, error) {
	if w.Zone == ZoneLocal {
		return time.Local, nil
	}
	return time.LoadLocation(w.Zone)
}

// Occurrence is one opening of the window, Start inclusive and End exclusive. Duration is elapsed time, so a
// window that spans a DST change is open as long as on any other day.
type Occurrence struct {
	Start, End time.Time
}

// At returns the opening that holds now, if any. The times are in the window's zone.
func (w Window) At(now time.Time) (Occurrence, bool, error) {
	var found Occurrence
	ok := false
	err := w.each(now, func(o Occurrence) bool {
		if !o.Start.After(now) && now.Before(o.End) {
			found, ok = o, true
			return false
		}
		return o.Start.Before(now)
	})
	return found, ok, err
}

// Next returns the first opening that starts after now. The times are in the window's zone.
func (w Window) Next(now time.Time) (Occurrence, error) {
	var found Occurrence
	err := w.each(now, func(o Occurrence) bool {
		if o.Start.After(now) {
			found = o
			return false
		}
		return true
	})
	if err == nil && found.Start.IsZero() {
		err = invalid("the window never opens")
	}
	return found, err
}

// each calls fn for the openings from the day before now on, in order, until fn returns false. Eight days
// ahead always hold an opening of a window with at least one weekday.
func (w Window) each(now time.Time, fn func(Occurrence) bool) error {
	if err := w.check(); err != nil {
		return err
	}
	loc, err := w.Location()
	if err != nil {
		return invalid("the time zone is not known to the host's zone database")
	}
	y, m, d := now.In(loc).Date()
	for off := -1; off <= 8; off++ {
		// The weekday of a civil date, independent of the zone's offsets.
		if !w.Days.Has(time.Date(y, m, d+off, 12, 0, 0, 0, time.UTC).Weekday()) {
			continue
		}
		start := time.Date(y, m, d+off, w.Start/60, w.Start%60, 0, 0, loc)
		if !fn(Occurrence{Start: start, End: start.Add(w.Duration)}) {
			return nil
		}
	}
	return nil
}
