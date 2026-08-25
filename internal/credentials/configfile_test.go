package credentials

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAuthFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDecodeBasicAuth(t *testing.T) {
	enc := base64.StdEncoding.EncodeToString([]byte("bob:hunter2"))
	u, p, ok := decodeBasicAuth(enc)
	if !ok || u != "bob" || p != "hunter2" {
		t.Fatalf("got %q %q %v", u, p, ok)
	}
}

func TestDecodeBasicAuth_Invalid(t *testing.T) {
	if _, _, ok := decodeBasicAuth("not-base64!!!"); ok {
		t.Fatal("expected failure on invalid base64")
	}
	noColon := base64.StdEncoding.EncodeToString([]byte("nopass"))
	if _, _, ok := decodeBasicAuth(noColon); ok {
		t.Fatal("expected failure on missing colon")
	}
}

func TestConfigFileSource_StaticAuth(t *testing.T) {
	enc := base64.StdEncoding.EncodeToString([]byte("bob:hunter2"))
	path := writeAuthFile(t, `{"auths":{"ghcr.io":{"auth":"`+enc+`"}}}`)

	c := ConfigFileSource{Paths: []string{path}}
	u, p, ok := c.Lookup(context.Background(), "ghcr.io")
	if !ok || u != "bob" || p != "hunter2" {
		t.Fatalf("got %q %q %v", u, p, ok)
	}
}

func TestConfigFileSource_NoMatchingHost(t *testing.T) {
	enc := base64.StdEncoding.EncodeToString([]byte("bob:hunter2"))
	path := writeAuthFile(t, `{"auths":{"ghcr.io":{"auth":"`+enc+`"}}}`)

	c := ConfigFileSource{Paths: []string{path}}
	_, _, ok := c.Lookup(context.Background(), "docker.io")
	if ok {
		t.Fatal("expected no match for unconfigured host")
	}
}

func TestConfigFileSource_IdentityTokenNotSupported(t *testing.T) {
	path := writeAuthFile(t, `{"auths":{"ghcr.io":{"identitytoken":"xyz"}}}`)
	c := ConfigFileSource{Paths: []string{path}}
	_, _, ok := c.Lookup(context.Background(), "ghcr.io")
	if ok {
		t.Fatal("expected no match: identitytoken-only entries aren't supported")
	}
}

func TestConfigFileSource_MissingFileSkipped(t *testing.T) {
	c := ConfigFileSource{Paths: []string{"/does/not/exist.json"}}
	_, _, ok := c.Lookup(context.Background(), "ghcr.io")
	if ok {
		t.Fatal("expected no match")
	}
}

func TestConfigFileSource_TriesMultiplePathsInOrder(t *testing.T) {
	enc := base64.StdEncoding.EncodeToString([]byte("bob:hunter2"))
	found := writeAuthFile(t, `{"auths":{"ghcr.io":{"auth":"`+enc+`"}}}`)

	c := ConfigFileSource{Paths: []string{"/does/not/exist.json", found}}
	u, p, ok := c.Lookup(context.Background(), "ghcr.io")
	if !ok || u != "bob" || p != "hunter2" {
		t.Fatalf("got %q %q %v", u, p, ok)
	}
}

func TestConfigFile_Malformed(t *testing.T) {
	path := writeAuthFile(t, `not json`)
	c := ConfigFileSource{Paths: []string{path}}
	_, _, ok := c.Lookup(context.Background(), "ghcr.io")
	if ok {
		t.Fatal("expected no match for a malformed config file")
	}
}

func TestConfigFileSource_LogsMalformedConfig(t *testing.T) {
	path := writeAuthFile(t, `not json`)
	var logged string
	c := ConfigFileSource{
		Paths: []string{path},
		Logf:  func(format string, args ...any) { logged = fmt.Sprintf(format, args...) },
	}
	c.Lookup(context.Background(), "ghcr.io")
	if logged == "" {
		t.Fatal("expected a log message for a malformed config file")
	}
	if !strings.Contains(logged, path) {
		t.Errorf("log message should contain the file path, got: %s", logged)
	}
}

func TestConfigFileSource_LogsCredHelperFailure(t *testing.T) {
	staticEnc := base64.StdEncoding.EncodeToString([]byte("static-user:static-pass"))
	path := writeAuthFile(t, `{
		"auths":{"ghcr.io":{"auth":"`+staticEnc+`"}},
		"credHelpers":{"ghcr.io":"nonexistent-helper"}
	}`)

	var logged string
	c := ConfigFileSource{
		Paths: []string{path},
		Logf:  func(format string, args ...any) { logged = fmt.Sprintf(format, args...) },
	}
	u, p, ok := c.Lookup(context.Background(), "ghcr.io")
	if !ok || u != "static-user" || p != "static-pass" {
		t.Fatalf("got %q %q %v, want fallback to static auths", u, p, ok)
	}
	if logged == "" {
		t.Fatal("expected a log message for a failed credHelper")
	}
	if !strings.Contains(logged, "nonexistent-helper") {
		t.Errorf("log message should mention the helper name, got: %s", logged)
	}
}

func TestConfigFileSource_LogsCredsStoreFailure(t *testing.T) {
	staticEnc := base64.StdEncoding.EncodeToString([]byte("static-user:static-pass"))
	path := writeAuthFile(t, `{
		"auths":{"ghcr.io":{"auth":"`+staticEnc+`"}},
		"credsStore":"nonexistent-store"
	}`)

	var logged string
	c := ConfigFileSource{
		Paths: []string{path},
		Logf:  func(format string, args ...any) { logged = fmt.Sprintf(format, args...) },
	}
	u, p, ok := c.Lookup(context.Background(), "ghcr.io")
	if !ok || u != "static-user" || p != "static-pass" {
		t.Fatalf("got %q %q %v, want fallback to static auths", u, p, ok)
	}
	if logged == "" {
		t.Fatal("expected a log message for a failed credsStore")
	}
	if !strings.Contains(logged, "nonexistent-store") {
		t.Errorf("log message should mention the store name, got: %s", logged)
	}
}

func TestConfigFileSource_NoLogfWhenNil(t *testing.T) {
	path := writeAuthFile(t, `{"credsStore":"nonexistent"}`)
	c := ConfigFileSource{Paths: []string{path}}
	// Must not panic with nil Logf
	_, _, _ = c.Lookup(context.Background(), "ghcr.io")
}
