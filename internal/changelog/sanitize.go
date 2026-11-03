package changelog

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const truncationNotice = "\n\n[truncated; see the full release notes on GitHub]"

// Sanitize turns untrusted release text into plain text for a terminal: it removes escape sequences (CSI,
// OSC and the other string sequences), all control characters except newline and tab, and bidirectional
// override characters, and normalizes line breaks to "\n".
func Sanitize(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == 0x1b:
			i = skipEscape(s, i+1)
			continue
		case r == 0x9b: // C1 CSI
			i = skipCSI(s, i+n)
			continue
		case r == 0x90 || r == 0x98 || r == 0x9d || r == 0x9e || r == 0x9f: // C1 string introducers
			i = skipString(s, i+n)
			continue
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case unicode.IsControl(r), isBidiControl(r):
		default:
			b.WriteRune(r)
		}
		i += n
	}
	return b.String()
}

func isBidiControl(r rune) bool {
	return r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 || r == 0x200e || r == 0x200f || r == 0x061c
}

// skipEscape returns the index after the sequence that starts after an ESC byte at i-1.
func skipEscape(s string, i int) int {
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[':
		return skipCSI(s, i+1)
	case ']', 'P', 'X', '^', '_': // OSC, DCS, SOS, PM, APC
		return skipString(s, i+1)
	}
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
		i++
	}
	if i < len(s) && s[i] >= 0x30 && s[i] <= 0x7e {
		i++
	}
	return i
}

func skipCSI(s string, i int) int {
	for i < len(s) && s[i] >= 0x30 && s[i] <= 0x3f {
		i++
	}
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
		i++
	}
	if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7e {
		i++
	}
	return i
}

// skipString skips a string sequence up to BEL or ST. An unterminated sequence ends at the line end so it
// cannot swallow the rest of the text.
func skipString(s string, i int) int {
	for i < len(s) {
		switch {
		case s[i] == 0x07:
			return i + 1
		case s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\':
			return i + 2
		case s[i] == 0xc2 && i+1 < len(s) && s[i+1] == 0x9c: // C1 ST
			return i + 2
		case s[i] == 0x1b || s[i] == '\n':
			return i
		}
		i++
	}
	return i
}

// truncate cuts s to at most max bytes on a rune boundary and appends the notice when it cut.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], " \t\n") + truncationNotice
}

// oneLine collapses all white space of a sanitized text into single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
