package main

import (
	"fmt"
	"os"

	"github.com/example/image-watch/internal/config"
	"github.com/example/image-watch/internal/observer"
	"github.com/example/image-watch/internal/policy"
	"github.com/example/image-watch/internal/registry"
	"github.com/example/image-watch/internal/registry/distribution"
	"github.com/example/image-watch/internal/runtime/docker"
	"github.com/example/image-watch/internal/state"
)

// buildObserver wires the concrete implementations together.
func buildObserver(cfg config.Config) (*observer.Observer, error) {
	dockerClient, err := docker.New(cfg.Runtime.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize docker runtime: %w", err)
	}

	store, err := state.NewSQLiteStore(cfg.State.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open state store at %s: %w", cfg.State.Path, err)
	}

	registryClients := make(map[string]registry.Registry)
	resolver := func(host string) registry.Registry {
		if c, ok := registryClients[host]; ok {
			return c
		}
		c := distribution.New(host, nil, credentialProviderFor(cfg, host))
		registryClients[host] = c
		return c
	}

	return &observer.Observer{
		Runtime:       dockerClient,
		Registries:    resolver,
		Store:         store,
		DefaultPolicy: cfg.Policy,
	}, nil
}

// credentialProviderFor resolves environment-backed registry credentials.
func credentialProviderFor(cfg config.Config, targetHost string) distribution.CredentialProvider {
	return func(host string) (string, string, bool) {
		auth, ok := cfg.Registries[host]
		if !ok {
			return "", "", false
		}
		username := os.Getenv(auth.UsernameEnv)
		password := os.Getenv(auth.PasswordEnv)
		if username == "" && password == "" {
			return "", "", false
		}
		return username, password, true
	}
}

// enabledCategories returns a short string of enabled policy categories.
func enabledCategories(p policy.Policy) string {
	var parts []string
	add := func(name string, enabled bool) {
		if enabled {
			parts = append(parts, name)
		}
	}
	add("patch", p.Patch)
	add("minor", p.Minor)
	add("major", p.Major)
	add("family-advancement", p.FamilyAdvancement)
	add("base-advancement", p.BaseAdvancement)
	add("tag-changed", p.TagChanged)
	add("tag-mutated", p.TagMutated)
	add("other-platform", p.OtherPlatform)
	if len(parts) == 0 {
		return "(none)"
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", " + p
	}
	return out
}
