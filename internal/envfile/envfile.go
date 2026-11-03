// Package envfile edits the value of one variable in the bytes of a .env file and leaves every other byte
// untouched.
package envfile

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// The line rules follow the .env parser of package stacks: one statement per line, an optional "export "
// prefix, double quotes honour \" and \\, single quotes are verbatim, an unquoted value ends at " #" or "\t#".
var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// Set returns content with the value of the variable name replaced by value. The quoting style of the
// existing assignment is kept; it is an error if value cannot be written in that style so that it reads back
// as exactly value. The variable must be assigned in exactly one line.
func Set(content []byte, name, value string) ([]byte, error) {
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("invalid variable name %q", name)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return nil, errors.New("value contains a control character")
		}
	}

	var hit []byte
	var from, to int // byte range of the old value text (including quotes) within content
	pos := 0
	for pos < len(content) {
		end := len(content)
		if i := bytes.IndexByte(content[pos:], '\n'); i >= 0 {
			end = pos + i
		}
		line := content[pos:end]
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
		if s, e, ok := valueSpan(string(line), name); ok {
			if hit != nil {
				return nil, fmt.Errorf("variable %s is defined more than once", name)
			}
			hit, from, to = line, pos+s, pos+e
		}
		pos = end + 1
	}
	if hit == nil {
		return nil, fmt.Errorf("variable %s not found", name)
	}

	old := content[from:to]
	var repl string
	switch {
	case len(old) > 0 && old[0] == '\'':
		if strings.Contains(value, "'") {
			return nil, errors.New("value with a single quote cannot be written in a single-quoted assignment")
		}
		repl = "'" + value + "'"
	case len(old) > 0 && old[0] == '"':
		repl = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
	default:
		if err := checkUnquoted(value, content[to:], hit); err != nil {
			return nil, err
		}
		repl = value
	}

	out := make([]byte, 0, len(content)+len(repl))
	out = append(out, content[:from]...)
	out = append(out, repl...)
	out = append(out, content[to:]...)
	return out, nil
}

// checkUnquoted refuses values that would not read back as value without quotes.
func checkUnquoted(value string, after, line []byte) error {
	if value != strings.TrimSpace(value) {
		return errors.New("value with leading or trailing whitespace needs quotes")
	}
	if strings.HasPrefix(value, "'") || strings.HasPrefix(value, `"`) {
		return errors.New("value starting with a quote needs quotes")
	}
	if strings.Contains(value, " #") || strings.Contains(value, "\t#") {
		return errors.New("value with a comment marker needs quotes")
	}
	if value == "" {
		rest := after
		if i := bytes.IndexAny(rest, "\r\n"); i >= 0 {
			rest = rest[:i]
		}
		if strings.HasPrefix(strings.TrimSpace(string(rest)), "#") {
			return errors.New("empty value before an inline comment needs quotes")
		}
	}
	return nil
}

// valueSpan reports the byte range of the value text of line (quotes included) if the line assigns name.
func valueSpan(line, name string) (start, end int, ok bool) {
	i := len(line) - len(strings.TrimLeftFunc(line, unicode.IsSpace))
	if i == len(line) || line[i] == '#' {
		return 0, 0, false
	}
	if strings.HasPrefix(line[i:], "export ") {
		i += len("export ")
		i += len(line[i:]) - len(strings.TrimLeftFunc(line[i:], unicode.IsSpace))
	}
	eq := strings.IndexByte(line[i:], '=')
	if eq < 0 || strings.TrimSpace(line[i:i+eq]) != name {
		return 0, 0, false
	}
	start = i + eq + 1
	start += len(line[start:]) - len(strings.TrimLeftFunc(line[start:], unicode.IsSpace))
	raw := line[start:]
	switch {
	case raw == "":
		return start, start, true
	case raw[0] == '\'':
		c := strings.IndexByte(raw[1:], '\'')
		if c < 0 || !onlyComment(raw[c+2:]) {
			return 0, 0, false
		}
		return start, start + c + 2, true
	case raw[0] == '"':
		for j := 1; j < len(raw); j++ {
			switch raw[j] {
			case '\\':
				j++
			case '"':
				if !onlyComment(raw[j+1:]) {
					return 0, 0, false
				}
				return start, start + j + 1, true
			}
		}
		return 0, 0, false
	}
	cut := len(raw)
	for _, m := range []string{" #", "\t#"} {
		if k := strings.Index(raw, m); k >= 0 && k < cut {
			cut = k
		}
	}
	return start, start + len(strings.TrimRightFunc(raw[:cut], unicode.IsSpace)), true
}

func onlyComment(rest string) bool {
	rest = strings.TrimSpace(rest)
	return rest == "" || strings.HasPrefix(rest, "#")
}
