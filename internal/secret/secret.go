// Package secret reads secret values from files (e.g. Docker secrets).
package secret

import (
	"os"
	"strings"
)

// ReadFile reads a secret from the file at path and trims surrounding whitespace.
func ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
