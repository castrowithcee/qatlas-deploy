package changelog

import (
	"fmt"
	"io"
	"log/slog"
)

// Token is an optional GitHub token. The zero value is anonymous access. Every way of printing it hides the
// value.
type Token string

// String never contains the token.
func (t Token) String() string {
	if t == "" {
		return "changelog.Token(anonymous)"
	}
	return "changelog.Token(redacted)"
}

// GoString is the %#v form; it equals String.
func (t Token) GoString() string { return t.String() }

// Format makes every fmt verb print String.
func (t Token) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, t.String()) }

// LogValue keeps the token out of structured logs.
func (t Token) LogValue() slog.Value { return slog.StringValue(t.String()) }
