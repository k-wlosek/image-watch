// Package secret reads secret values from files (e.g. Docker secrets).
package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ReadFile reads a secret from the file at path and trims surrounding whitespace.
func ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// WriteFile creates a temporary file with the given content and returns its path.
// It is intended for use in tests.
func WriteFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
