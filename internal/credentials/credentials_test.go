package credentials

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type fakeSource struct {
	user, pass string
	ok         bool
}

func (f fakeSource) Lookup(context.Context, string) (string, string, bool) {
	return f.user, f.pass, f.ok
}

func writeSecretFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestChain_FirstMatchWins(t *testing.T) {
	c := Chain{
		fakeSource{ok: false},
		fakeSource{user: "a", pass: "b", ok: true},
		fakeSource{user: "c", pass: "d", ok: true},
	}
	u, p, ok := c.Lookup(context.Background(), "host")
	if !ok || u != "a" || p != "b" {
		t.Fatalf("got %q %q %v, want a b true", u, p, ok)
	}
}

func TestChain_NoMatch(t *testing.T) {
	c := Chain{fakeSource{ok: false}, fakeSource{ok: false}}
	_, _, ok := c.Lookup(context.Background(), "host")
	if ok {
		t.Fatal("expected no match")
	}
}

func TestChain_Empty(t *testing.T) {
	var c Chain
	_, _, ok := c.Lookup(context.Background(), "host")
	if ok {
		t.Fatal("expected no match on empty chain")
	}
}

func TestFileSource(t *testing.T) {
	userFile := writeSecretFile(t, "alice")
	passFile := writeSecretFile(t, "secret")

	e := FileSource{Registries: map[string]RegistryAuth{
		"ghcr.io": {UsernameFile: userFile, PasswordFile: passFile},
	}}

	u, p, ok := e.Lookup(context.Background(), "ghcr.io")
	if !ok || u != "alice" || p != "secret" {
		t.Fatalf("got %q %q %v", u, p, ok)
	}

	_, _, ok = e.Lookup(context.Background(), "docker.io")
	if ok {
		t.Fatal("expected no match for unconfigured host")
	}
}

func TestFileSource_MissingFilesNoMatch(t *testing.T) {
	e := FileSource{Registries: map[string]RegistryAuth{
		"ghcr.io": {UsernameFile: "/nonexistent/user", PasswordFile: "/nonexistent/pass"},
	}}
	_, _, ok := e.Lookup(context.Background(), "ghcr.io")
	if ok {
		t.Fatal("expected no match when neither file exists")
	}
}
