package gitrepo

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// ErrInvalidURL is returned for a repository URL that is not a plain SSH address.
var ErrInvalidURL = errors.New("invalid git url: use user@host:path or ssh://[user@]host[:port]/path")

const maxURLBytes = 2048

var (
	scpRe       = regexp.MustCompile(`^(?:([A-Za-z0-9_][A-Za-z0-9._-]*)@)?([A-Za-z0-9][A-Za-z0-9.-]*):([A-Za-z0-9._~+/][A-Za-z0-9._~+/-]*)$`)
	sshUserRe   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
	sshHostRe   = regexp.MustCompile(`^(?:[A-Za-z0-9][A-Za-z0-9.-]*|[0-9A-Fa-f:.]*:[0-9A-Fa-f:.]*)$`)
	sshPortRe   = regexp.MustCompile(`^[0-9]{1,5}$`)
	transportRe = regexp.MustCompile(`^[A-Za-z0-9+.-]+::`)
)

// ValidateURL accepts only the two SSH forms of a git remote. Everything that could turn the address into an
// option, a helper program, a local path or another transport is refused.
func ValidateURL(raw string) error {
	if err := checkCommon(raw); err != nil {
		return err
	}
	if rest, ok := strings.CutPrefix(raw, "ssh://"); ok {
		return validateSSHURL(raw, rest)
	}
	if strings.Contains(raw, "://") || !scpRe.MatchString(raw) {
		return ErrInvalidURL
	}
	return nil
}

func checkCommon(raw string) error {
	if raw == "" || len(raw) > maxURLBytes || strings.HasPrefix(raw, "-") || transportRe.MatchString(raw) ||
		strings.ContainsAny(raw, `\%`) {
		return ErrInvalidURL
	}
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r > unicode.MaxASCII {
			return ErrInvalidURL
		}
	}
	return nil
}

func validateSSHURL(raw, rest string) error {
	u, err := url.Parse(raw)
	if err != nil || rest == "" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(raw, "?") ||
		strings.Contains(raw, "#") || len(u.Path) < 2 {
		return ErrInvalidURL
	}
	if u.User != nil {
		if _, hasPass := u.User.Password(); hasPass || !sshUserRe.MatchString(u.User.Username()) {
			return ErrInvalidURL
		}
	}
	if !sshHostRe.MatchString(u.Hostname()) || (u.Port() != "" && !sshPortRe.MatchString(u.Port())) {
		return ErrInvalidURL
	}
	return nil
}

// validateLocalURL is the test-only widening: an absolute local path or a file URL.
func validateLocalURL(raw string) error {
	if err := checkCommon(raw); err != nil {
		return err
	}
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "file:///") {
		return nil
	}
	return ValidateURL(raw)
}
