package registry

import (
	"fmt"
	"io"
	"log/slog"
)

// Credential is the login for a registry. The zero value is anonymous access. With a token but no user, the
// token alone is sent as the password of an empty user name. Every way of printing it hides the values.
type Credential struct {
	User  string
	Token string
}

func (c Credential) set() bool { return c.User != "" || c.Token != "" }

// String never contains the user or the token.
func (c Credential) String() string {
	if !c.set() {
		return "registry.Credential(anonymous)"
	}
	return "registry.Credential(redacted)"
}

// GoString is the %#v form; it equals String.
func (c Credential) GoString() string { return c.String() }

// Format makes every fmt verb print String.
func (c Credential) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, c.String()) }

// LogValue keeps the credential out of structured logs.
func (c Credential) LogValue() slog.Value { return slog.StringValue(c.String()) }
