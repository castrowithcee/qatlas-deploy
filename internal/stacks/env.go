package stacks

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// readEnv parses .env of a stack; a missing file is an empty environment.
func readEnv(dir string) (map[string]string, error) {
	data, err := readBounded(dir, envFile)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	env, err := parseEnv(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envFile, err)
	}
	return env, nil
}

// parseEnv reads KEY=VALUE lines. Values are literal: no variable references, no command substitution. Double
// quotes honour \n, \t, \" and \; single quotes are verbatim; an unquoted value ends at " #".
func parseEnv(text string) (map[string]string, error) {
	env := map[string]string{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, raw, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !envKey.MatchString(key) {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", i+1)
		}
		val, err := parseEnvValue(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		env[key] = val
	}
	return env, nil
}

func parseEnvValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	switch raw[0] {
	case '\'':
		end := strings.IndexByte(raw[1:], '\'')
		if end < 0 || !onlyComment(raw[end+2:]) {
			return "", errors.New("malformed single-quoted value")
		}
		return raw[1 : end+1], nil
	case '"':
		var b strings.Builder
		for i := 1; i < len(raw); i++ {
			switch c := raw[i]; c {
			case '\\':
				if i+1 >= len(raw) {
					return "", errors.New("malformed double-quoted value")
				}
				i++
				switch raw[i] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				case '"', '\\':
					b.WriteByte(raw[i])
				default:
					b.WriteByte('\\')
					b.WriteByte(raw[i])
				}
			case '"':
				if !onlyComment(raw[i+1:]) {
					return "", errors.New("malformed double-quoted value")
				}
				return b.String(), nil
			default:
				b.WriteByte(c)
			}
		}
		return "", errors.New("unterminated double-quoted value")
	}
	if i := strings.Index(raw, " #"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.Index(raw, "\t#"); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(raw), nil
}

func onlyComment(rest string) bool {
	rest = strings.TrimSpace(rest)
	return rest == "" || strings.HasPrefix(rest, "#")
}

// SecretsFile is the file with the secrets of a stack. It is not versioned and lives only on the host.
const SecretsFile = "secrets.env"

// SecretKeys returns the sorted key names of secrets.env and whether the file exists. It never keeps, parses or
// returns a value; lines that are not KEY=VALUE are skipped, and no error text contains file content.
func SecretKeys(dir string) (keys []string, exists bool, err error) {
	data, err := readBounded(dir, SecretsFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		key, _, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if ok && envKey.MatchString(key) && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, true, nil
}
