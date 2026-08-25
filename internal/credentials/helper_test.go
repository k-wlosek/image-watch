package credentials

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeFakeHelper writes an executable script implementing the
// credential helper "get" protocol and prepends its directory to PATH
// for the duration of the test.
func writeFakeHelper(t *testing.T, name, username, secret string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake helper script is POSIX shell only")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "docker-credential-"+name)
	script := "#!/bin/sh\ncat <<EOF\n{\"Username\":\"" + username + "\",\"Secret\":\"" + secret + "\"}\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRunHelper(t *testing.T) {
	writeFakeHelper(t, "fake", "alice", "s3cret")
	u, p, ok := runHelper(context.Background(), "fake", "ghcr.io")
	if !ok || u != "alice" || p != "s3cret" {
		t.Fatalf("got %q %q %v", u, p, ok)
	}
}

func TestRunHelper_MissingBinary(t *testing.T) {
	_, _, ok := runHelper(context.Background(), "definitely-does-not-exist", "ghcr.io")
	if ok {
		t.Fatal("expected no match for a missing helper binary")
	}
}

func TestRunHelper_MalformedOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake helper script is POSIX shell only")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "docker-credential-broken")
	script := "#!/bin/sh\necho not-json"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, _, ok := runHelper(context.Background(), "broken", "ghcr.io")
	if ok {
		t.Fatal("expected no match for malformed helper output")
	}
}

func TestRunHelper_RejectsUnsafeName(t *testing.T) {
	cases := []string{"../evil", "/etc/passwd", "foo; rm -rf /", "foo bar"}
	for _, name := range cases {
		if _, _, ok := runHelper(context.Background(), name, "ghcr.io"); ok {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}

func TestConfigFileSource_CredHelperTakesPrecedenceOverAuths(t *testing.T) {
	writeFakeHelper(t, "fake", "helper-user", "helper-pass")

	staticEnc := base64.StdEncoding.EncodeToString([]byte("static-user:static-pass"))
	path := writeAuthFile(t, `{
		"auths":{"ghcr.io":{"auth":"`+staticEnc+`"}},
		"credHelpers":{"ghcr.io":"fake"}
	}`)

	c := ConfigFileSource{Paths: []string{path}}
	u, p, ok := c.Lookup(context.Background(), "ghcr.io")
	if !ok || u != "helper-user" || p != "helper-pass" {
		t.Fatalf("got %q %q %v, want the credHelper result to win", u, p, ok)
	}
}

func TestConfigFileSource_CredsStoreFallsBackToAuths(t *testing.T) {
	// credsStore points at a helper that doesn't exist; the static auths
	// entry for the host should still be used.
	staticEnc := base64.StdEncoding.EncodeToString([]byte("static-user:static-pass"))
	path := writeAuthFile(t, `{
		"auths":{"ghcr.io":{"auth":"`+staticEnc+`"}},
		"credsStore":"nonexistent"
	}`)

	c := ConfigFileSource{Paths: []string{path}}
	u, p, ok := c.Lookup(context.Background(), "ghcr.io")
	if !ok || u != "static-user" || p != "static-pass" {
		t.Fatalf("got %q %q %v, want fallback to static auths", u, p, ok)
	}
}

func TestConfigFileSource_CredsStoreUsedWhenNoPerHostHelper(t *testing.T) {
	writeFakeHelper(t, "global", "store-user", "store-pass")
	path := writeAuthFile(t, `{"credsStore":"global"}`)

	c := ConfigFileSource{Paths: []string{path}}
	u, p, ok := c.Lookup(context.Background(), "ghcr.io")
	if !ok || u != "store-user" || p != "store-pass" {
		t.Fatalf("got %q %q %v", u, p, ok)
	}
}
