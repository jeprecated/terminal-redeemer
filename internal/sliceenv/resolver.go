package sliceenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const MaxContextBytes = 16 << 10
const MaxValueBytes = 4096

var allowedKeys = map[string]bool{
	"NIRI_SOCKET": true, "WAYLAND_DISPLAY": true, "XDG_RUNTIME_DIR": true,
}

type Resolver struct {
	Keys         []string
	Systemctl    string
	Environment  []string
	RunSystemctl func(context.Context, string, []string, []string) ([]byte, error)
}

// Resolve reads only explicitly allowlisted process/user-manager environment
// entries. It never reads shell profiles, logs, credential stores, or arbitrary
// files. Returned values are private process context and must not enter RPC
// responses.
func (r Resolver) Resolve(ctx context.Context) (map[string]string, error) {
	if err := ValidateKeys(r.Keys); err != nil {
		return nil, err
	}
	env := r.Environment
	if env == nil {
		env = os.Environ()
	}
	out := make(map[string]string, len(r.Keys))
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok && contains(r.Keys, key) && validValue(value) {
			out[key] = value
		}
	}
	missing := make([]string, 0)
	for _, key := range r.Keys {
		if out[key] == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 && r.Systemctl != "" {
		run := r.RunSystemctl
		if run == nil {
			run = runBounded
		}
		payload, err := run(ctx, r.Systemctl, []string{"--user", "show-environment"}, env)
		if err == nil {
			if len(payload) > MaxContextBytes {
				return nil, errors.New("graphical context response exceeds bound")
			}
			for _, line := range strings.Split(string(payload), "\n") {
				key, value, ok := strings.Cut(line, "=")
				if ok && contains(missing, key) && validValue(value) {
					out[key] = value
				}
			}
		}
	}
	for _, key := range r.Keys {
		if out[key] == "" {
			return nil, fmt.Errorf("graphical context unavailable: allowlisted key %s is missing", key)
		}
	}
	if err := ValidateContext(out); err != nil {
		return nil, fmt.Errorf("graphical context invalid: %w", err)
	}
	return out, nil
}

func ValidateKeys(keys []string) error {
	if len(keys) != len(allowedKeys) {
		return errors.New("graphical context allowlist must contain exactly NIRI_SOCKET, WAYLAND_DISPLAY, and XDG_RUNTIME_DIR")
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if !allowedKeys[key] || seen[key] {
			return fmt.Errorf("graphical context key %q is not allowed or is duplicated", key)
		}
		seen[key] = true
	}
	return nil
}

func ValidateContext(values map[string]string) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	if err := ValidateKeys(keys); err != nil {
		return err
	}
	for key, value := range values {
		if !validValue(value) {
			return fmt.Errorf("graphical context value for %s is missing or invalid", key)
		}
	}
	if !filepath.IsAbs(values["NIRI_SOCKET"]) || len(values["NIRI_SOCKET"]) > 107 {
		return errors.New("NIRI_SOCKET must be a bounded absolute Unix socket path")
	}
	if !filepath.IsAbs(values["XDG_RUNTIME_DIR"]) {
		return errors.New("XDG_RUNTIME_DIR must be absolute")
	}
	return nil
}

func validValue(value string) bool {
	return value != "" && utf8.ValidString(value) && len(value) <= MaxValueBytes && !strings.ContainsAny(value, "\x00\r\n")
}
func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func runBounded(ctx context.Context, command string, args []string, env []string) ([]byte, error) {
	var output bytes.Buffer
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = env
	cmd.Stdout = &limitedWriter{buffer: &output, max: MaxContextBytes}
	cmd.Stderr = &limitedWriter{buffer: &output, max: MaxContextBytes}
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

type limitedWriter struct {
	buffer *bytes.Buffer
	max    int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.buffer.Len()+len(p) > w.max {
		return 0, errors.New("output exceeds bound")
	}
	return w.buffer.Write(p)
}
