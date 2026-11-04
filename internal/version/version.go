// Package version selects the update target of one image variable from the tags of its repository. It is
// pure: it does no I/O.
//
// Rules:
//   - A tag is comparable if it is a complete MAJOR.MINOR.PATCH version (SemVer or CalVer) with an optional
//     leading "v". Each part is compared as a number, so "26.09.1" and "26.9.1" are the same version; for CalVer
//     the first part (the year) counts as major. Incomplete versions such as "1.2", "22.04" or "16" and
//     everything else are ignored as candidates.
//   - Candidates with the same numbers differ only in zero padding. The one padded like the current tag wins,
//     otherwise the smaller string.
//   - Text after the first "-" is the variant ("alpine", "apache"). Only tags with exactly the prefix and the
//     variant of the current tag are candidates. A suffix whose first dot- or dash-separated part is a number or
//     one of alpha, beta, rc, pre, preview, dev, snapshot, nightly, canary, next, test or milestone (optionally
//     followed by digits) is a SemVer pre-release, not a variant, and is never offered.
//   - The filter is applied unanchored to the whole candidate tag.
package version

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Ceilings: how far an automatic update may move.
const (
	CeilingPatch = "patch"
	CeilingMinor = "minor"
	CeilingMajor = "major"
)

// Status is the outcome of a selection.
type Status string

const (
	// StatusCurrent means no candidate is allowed that is higher than the current tag.
	StatusCurrent Status = "current"
	// StatusUpdate means Selected is higher than the current tag and within the ceiling.
	StatusUpdate Status = "update"
	// StatusNotComparable means the current tag cannot be ordered against other tags; see Reason.
	StatusNotComparable Status = "not-comparable"
)

// Reasons for StatusNotComparable.
const (
	// ReasonNotSemver: the current tag is not a complete MAJOR.MINOR.PATCH version, for example "latest", "16" or "1.2".
	ReasonNotSemver = "not-semver"
	// ReasonPrerelease: the current tag is a pre-release, whose variant cannot be told from its suffix.
	ReasonPrerelease = "prerelease"
)

// Input is one selection request.
type Input struct {
	// Current is the tag the variable is on.
	Current string
	// Candidates are the tags of the repository, in any order.
	Candidates []string
	// Ceiling is CeilingPatch, CeilingMinor or CeilingMajor; empty means CeilingMinor.
	Ceiling string
	// Filter, if set, restricts the candidates; it is not applied to Current.
	Filter *regexp.Regexp
}

// Result is the outcome for one variable.
type Result struct {
	Status Status
	// Reason is set for StatusNotComparable.
	Reason string
	// Current is the tag as given.
	Current string
	// Selected is the tag to move to. Without an allowed higher candidate it equals Current. It is empty for
	// StatusNotComparable.
	Selected string
	// MajorAvailable is the highest stable, filter-passing tag of the same variant with a higher major version
	// than Current. It is set only if the ceiling is below major, and such an update is never automatic.
	MajorAvailable string
}

type parsedTag struct {
	tag                 string
	prefix              string
	major, minor, patch uint64
	// digits are the lengths of the three number strings, which tell the zero padding.
	digits  [3]int
	variant string
}

type parseKind int

const (
	kindInvalid parseKind = iota
	kindPrerelease
	kindOK
)

var (
	tagRe = regexp.MustCompile(`^(v?)([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([0-9A-Za-z][0-9A-Za-z.-]*))?$`)
	preRe = regexp.MustCompile(`(?i)^(alpha|beta|rc|pre|preview|dev|snapshot|nightly|canary|next|test|milestone)[0-9]*$`)
	numRe = regexp.MustCompile(`^[0-9]+$`)
)

func parseTag(tag string) (parsedTag, parseKind) {
	m := tagRe.FindStringSubmatch(tag)
	if m == nil {
		return parsedTag{}, kindInvalid
	}
	p := parsedTag{tag: tag, prefix: m[1], variant: m[5]}
	var err error
	for i, dst := range []*uint64{&p.major, &p.minor, &p.patch} {
		if *dst, err = strconv.ParseUint(m[2+i], 10, 63); err != nil {
			return parsedTag{}, kindInvalid
		}
		p.digits[i] = len(m[2+i])
	}
	if p.variant != "" {
		first, _, _ := strings.Cut(p.variant, ".")
		first, _, _ = strings.Cut(first, "-")
		if numRe.MatchString(first) || preRe.MatchString(first) {
			return p, kindPrerelease
		}
	}
	return p, kindOK
}

func (a parsedTag) less(b parsedTag) bool {
	if a.major != b.major {
		return a.major < b.major
	}
	if a.minor != b.minor {
		return a.minor < b.minor
	}
	return a.patch < b.patch
}

// preferred reports whether a beats b, which has the same numbers, as the tag to offer for cur.
func preferred(cur, a, b parsedTag) bool {
	if sa, sb := a.digits == cur.digits, b.digits == cur.digits; sa != sb {
		return sa
	}
	return a.tag < b.tag
}

// higher reports whether a is a better pick than b: higher numbers, then preferred.
func higher(cur, a, b parsedTag) bool {
	if b.less(a) {
		return true
	}
	return !a.less(b) && preferred(cur, a, b)
}

// Select picks the update target for in.Current. The only error is an unknown ceiling.
func Select(in Input) (Result, error) {
	ceiling := in.Ceiling
	if ceiling == "" {
		ceiling = CeilingMinor
	}
	if ceiling != CeilingPatch && ceiling != CeilingMinor && ceiling != CeilingMajor {
		return Result{}, fmt.Errorf("unsupported ceiling %q: use patch, minor or major", in.Ceiling)
	}
	res := Result{Current: in.Current}
	cur, kind := parseTag(in.Current)
	switch kind {
	case kindInvalid:
		res.Status, res.Reason = StatusNotComparable, ReasonNotSemver
		return res, nil
	case kindPrerelease:
		res.Status, res.Reason = StatusNotComparable, ReasonPrerelease
		return res, nil
	}

	var best, bestMajor *parsedTag
	for _, tag := range in.Candidates {
		p, kind := parseTag(tag)
		if kind != kindOK || p.variant != cur.variant || p.prefix != cur.prefix || !cur.less(p) {
			continue
		}
		if in.Filter != nil && !in.Filter.MatchString(tag) {
			continue
		}
		if p.major > cur.major && (bestMajor == nil || higher(cur, p, *bestMajor)) {
			bestMajor = &p
		}
		if allowed(cur, p, ceiling) && (best == nil || higher(cur, p, *best)) {
			best = &p
		}
	}
	res.Status, res.Selected = StatusCurrent, in.Current
	if best != nil {
		res.Status, res.Selected = StatusUpdate, best.tag
	}
	if ceiling != CeilingMajor && bestMajor != nil {
		res.MajorAvailable = bestMajor.tag
	}
	return res, nil
}

// allowed reports whether p, which is higher than cur, stays within the ceiling.
func allowed(cur, p parsedTag, ceiling string) bool {
	switch ceiling {
	case CeilingPatch:
		return p.major == cur.major && p.minor == cur.minor
	case CeilingMinor:
		return p.major == cur.major
	}
	return true
}

// Errors of Path. Their texts are fixed and may be shown as they are.
var (
	ErrNotComparable = errors.New("the current version is not a complete stable MAJOR.MINOR.PATCH version")
	ErrNotOffered    = errors.New("the version is not in the tag list of the registry")
	ErrOtherVariant  = errors.New("the version is not a stable version of the same variant as the current one")
	ErrNotHigher     = errors.New("the version is not higher than the current one")
	ErrFiltered      = errors.New("the version does not match the tag filter of the variable")
)

// Path returns the versions an update from in.Current to target passes through, target last. target must be
// one of in.Candidates, of the same prefix and variant as Current, higher than it and pass in.Filter; the
// ceiling is not applied, the caller has chosen target. With stepwise, the highest allowed candidate of each
// major version from Current's up to the one below target's comes first, so no major version is skipped.
func Path(in Input, target string, stepwise bool) ([]string, error) {
	cur, kind := parseTag(in.Current)
	if kind != kindOK {
		return nil, ErrNotComparable
	}
	if !slices.Contains(in.Candidates, target) {
		return nil, ErrNotOffered
	}
	to, kind := parseTag(target)
	switch {
	case kind != kindOK || to.variant != cur.variant || to.prefix != cur.prefix:
		return nil, ErrOtherVariant
	case !cur.less(to):
		return nil, ErrNotHigher
	case in.Filter != nil && !in.Filter.MatchString(target):
		return nil, ErrFiltered
	}
	if !stepwise {
		return []string{target}, nil
	}
	best := map[uint64]parsedTag{}
	for _, tag := range in.Candidates {
		p, kind := parseTag(tag)
		if kind != kindOK || p.variant != cur.variant || p.prefix != cur.prefix || !cur.less(p) || p.major >= to.major {
			continue
		}
		if in.Filter != nil && !in.Filter.MatchString(tag) {
			continue
		}
		if b, ok := best[p.major]; !ok || higher(cur, p, b) {
			best[p.major] = p
		}
	}
	majors := slices.Sorted(maps.Keys(best))
	path := make([]string, 0, len(majors)+1)
	for _, m := range majors {
		path = append(path, best[m].tag)
	}
	return append(path, target), nil
}

// Core is the ordered part of a tag: the numbers of MAJOR.MINOR.PATCH.
type Core struct {
	Major, Minor, Patch uint64
	// Prerelease reports a SemVer pre-release suffix, such as "rc1". A variant suffix such as "alpine" does not
	// set it.
	Prerelease bool
}

// ParseCore reads the numbers of a complete MAJOR.MINOR.PATCH tag with an optional leading "v" and any suffix. ok is
// false for everything else.
func ParseCore(tag string) (Core, bool) {
	p, kind := parseTag(tag)
	if kind == kindInvalid {
		return Core{}, false
	}
	return Core{p.major, p.minor, p.patch, kind == kindPrerelease}, true
}

// Compare orders by the three numbers only and returns -1, 0 or 1.
func (c Core) Compare(o Core) int {
	a, b := parsedTag{major: c.Major, minor: c.Minor, patch: c.Patch}, parsedTag{major: o.Major, minor: o.Minor, patch: o.Patch}
	switch {
	case a.less(b):
		return -1
	case b.less(a):
		return 1
	}
	return 0
}

// String is "MAJOR.MINOR.PATCH".
func (c Core) String() string { return fmt.Sprintf("%d.%d.%d", c.Major, c.Minor, c.Patch) }
