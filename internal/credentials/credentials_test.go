package credentials

import (
	"context"
	"testing"
)

type fakeSource struct {
	user, pass string
	ok         bool
}

func (f fakeSource) Lookup(context.Context, string) (string, string, bool) {
	return f.user, f.pass, f.ok
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

func TestEnvSource(t *testing.T) {
	t.Setenv("TEST_REG_USER", "alice")
	t.Setenv("TEST_REG_PASS", "secret")

	e := EnvSource{Registries: map[string]RegistryAuth{
		"ghcr.io": {UsernameEnv: "TEST_REG_USER", PasswordEnv: "TEST_REG_PASS"},
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

func TestEnvSource_EmptyEnvVarsNoMatch(t *testing.T) {
	e := EnvSource{Registries: map[string]RegistryAuth{
		"ghcr.io": {UsernameEnv: "UNSET_VAR_1", PasswordEnv: "UNSET_VAR_2"},
	}}
	_, _, ok := e.Lookup(context.Background(), "ghcr.io")
	if ok {
		t.Fatal("expected no match when neither env var is set")
	}
}
