package stacks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Limits of a secret that qdeploy writes. The minimum is the shortest value the redactor still masks.
const (
	MinSecretValue = 4
	MaxSecretValue = 8192
	MaxSecretKey   = 128
)

// The errors of the secrets.env editing; none of them carries a value.
var (
	ErrInvalidSecretKey   = errors.New("a secret key has 1 to 128 characters: letters, digits, '_', '.' or '-', starting with a letter or '_'")
	ErrInvalidSecretValue = errors.New("a secret value has 4 to 8192 bytes of text without control characters; a value with a single quote must not contain '$'")
	ErrKeyInEnv           = errors.New("the key already stands in .env; a key stands in exactly one of .env and secrets.env")
	ErrNoSecret           = errors.New("no such key in secrets.env")
)

// ValidSecretKey reports whether key is usable as a name in secrets.env.
func ValidSecretKey(key string) bool { return len(key) <= MaxSecretKey && envKey.MatchString(key) }

// SecretValues returns the key/value pairs of secrets.env of the stack folder dir, for the redactor. A missing
// file is no error. Lines that do not parse are skipped, and no error text contains file content.
func SecretValues(dir string) (map[string]string, error) {
	data, err := readBounded(dir, SecretsFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if key, val, ok := parseSecretLine(line); ok {
			out[key] = val
		}
	}
	return out, nil
}

func parseSecretLine(line string) (key, val string, ok bool) {
	line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	k, raw, found := strings.Cut(line, "=")
	k = strings.TrimSpace(k)
	if !found || !envKey.MatchString(k) {
		return "", "", false
	}
	v, err := parseEnvValue(strings.TrimSpace(raw))
	if err != nil {
		return "", "", false
	}
	return k, v, true
}

// encodeSecret writes value as one env line so that Compose and parseEnv read back exactly value: single
// quotes are verbatim; a value with a single quote needs double quotes, in which Compose would expand '$'.
func encodeSecret(key, value string) (string, error) {
	if len(value) < MinSecretValue || len(value) > MaxSecretValue || !utf8.ValidString(value) {
		return "", ErrInvalidSecretValue
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "", ErrInvalidSecretValue
		}
	}
	var line string
	if !strings.Contains(value, "'") {
		line = key + "='" + value + "'"
	} else {
		if strings.Contains(value, "$") {
			return "", ErrInvalidSecretValue
		}
		line = key + `="` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
	}
	if k, v, ok := parseSecretLine(line); !ok || k != key || v != value {
		return "", ErrInvalidSecretValue
	}
	return line, nil
}

// SetSecret sets key in secrets.env of the stack folder st.Dir, creating the file with mode 0600, and reports
// whether the key replaced an existing one. Every other line stays as it is. The caller holds the stack's
// run slot. The key must not stand in the stack's .env.
func SetSecret(st Stack, key, value string) (replaced bool, err error) {
	if !ValidSecretKey(key) {
		return false, ErrInvalidSecretKey
	}
	line, err := encodeSecret(key, value)
	if err != nil {
		return false, err
	}
	if _, ok := st.Env[key]; ok {
		return false, ErrKeyInEnv
	}
	lines, err := secretLines(st.Dir)
	if err != nil {
		return false, err
	}
	out := make([]string, 0, len(lines)+1)
	for _, l := range lines {
		if k, _, ok := parseSecretLine(l); ok && k == key {
			if !replaced {
				out = append(out, line)
			}
			replaced = true
			continue
		}
		out = append(out, l)
	}
	if !replaced {
		out = append(out, line)
	}
	return replaced, writeSecretLines(st.Dir, out)
}

// DeleteSecret removes every assignment of key from secrets.env of st.Dir; ErrNoSecret when there is none.
func DeleteSecret(st Stack, key string) error {
	if !ValidSecretKey(key) {
		return ErrInvalidSecretKey
	}
	lines, err := secretLines(st.Dir)
	if err != nil {
		return err
	}
	out := make([]string, 0, len(lines))
	found := false
	for _, l := range lines {
		if k, _, ok := parseSecretLine(l); ok && k == key {
			found = true
			continue
		}
		out = append(out, l)
	}
	if !found {
		return ErrNoSecret
	}
	return writeSecretLines(st.Dir, out)
}

// secretLines returns the lines of secrets.env without their line end; a missing file has none. The stack
// folder itself must be a real directory.
func secretLines(dir string) ([]string, error) {
	if err := checkStackDir(dir); err != nil {
		return nil, err
	}
	data, err := readBounded(dir, SecretsFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines, nil
}

func checkStackDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("the stack folder is not an absolute path")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("stack folder: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("the stack folder is a link or not a directory")
	}
	return nil
}

// writeSecretLines replaces secrets.env atomically: a temporary file with mode 0600 in the same folder,
// synced, renamed over the target, then the folder synced. A failure leaves the old file as it was.
func writeSecretLines(dir string, lines []string) error {
	body := ""
	if len(lines) > 0 {
		body = strings.Join(lines, "\n") + "\n"
	}
	if len(body) > MaxFileSize {
		return fmt.Errorf("%s would be larger than %d bytes", SecretsFile, MaxFileSize)
	}
	if info, err := os.Lstat(filepath.Join(dir, SecretsFile)); err == nil && !info.Mode().IsRegular() {
		return errors.New("secrets.env is a link or not a regular file")
	}
	removeSecretTemps(dir)
	f, err := os.CreateTemp(dir, secretTempPattern)
	if err != nil {
		return fmt.Errorf("create a temporary file: %w", err)
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("set the file mode: %w", err)
	}
	if _, err := f.WriteString(body); err != nil {
		return fmt.Errorf("write the temporary file: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync the temporary file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close the temporary file: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, SecretsFile)); err != nil {
		return fmt.Errorf("replace %s: %w", SecretsFile, err)
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

const secretTempPattern = ".secrets.env.*"

// removeSecretTemps deletes temporary files a crash left behind: they hold secret values and would count as
// local changes of the stack. Only regular files are removed; a link is left alone.
func removeSecretTemps(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, secretTempPattern))
	for _, m := range matches {
		if info, err := os.Lstat(m); err == nil && info.Mode().IsRegular() {
			_ = os.Remove(m)
		}
	}
}
