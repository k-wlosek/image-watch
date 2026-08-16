package main

import (
	"fmt"
	"os"
	"time"

	"github.com/example/image-watch/internal/config"
	"github.com/example/image-watch/internal/metrics"
	"github.com/example/image-watch/internal/notify"
	"github.com/example/image-watch/internal/notify/ntfy"
	"github.com/example/image-watch/internal/notify/stdout"
	"github.com/example/image-watch/internal/notify/webhook"
	"github.com/example/image-watch/internal/observer"
	"github.com/example/image-watch/internal/policy"
	"github.com/example/image-watch/internal/registry"
	"github.com/example/image-watch/internal/registry/distribution"
	"github.com/example/image-watch/internal/runtime/docker"
	"github.com/example/image-watch/internal/state"
)

// buildObserver wires the runtime, registry, and store implementations together.
func buildObserver(cfg config.Config, m *metrics.Metrics) (*observer.Observer, error) {
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
		c := distribution.New(host, nil, credentialProviderFor(cfg))
		if m != nil {
			c.Instrumentation = registryInstrumentation{m}
		}
		registryClients[host] = c
		return c
	}

	obs := &observer.Observer{
		Runtime:       dockerClient,
		Registries:    resolver,
		Store:         store,
		DefaultPolicy: cfg.Policy,
	}
	if m != nil {
		obs.Metrics = enrichmentObserver{m}
	}
	return obs, nil
}

// registryInstrumentation adapts metrics to distribution.Instrumentation.
type registryInstrumentation struct{ m *metrics.Metrics }

func (r registryInstrumentation) ObserveRequest(host string, duration time.Duration, err error) {
	r.m.RecordRegistryRequest(host, duration, err)
}

// enrichmentObserver adapts metrics to observer.EnrichmentObserver.
type enrichmentObserver struct{ m *metrics.Metrics }

func (e enrichmentObserver) ObserveEnrichment(success bool) {
	e.m.RecordEnrichment(success)
}

// buildNotifiers constructs the configured notifier set.
func buildNotifiers(cfg config.Config) []notify.Notifier {
	if len(cfg.Notifications.Targets) == 0 {
		return []notify.Notifier{stdout.New()}
	}

	var notifiers []notify.Notifier
	for _, t := range cfg.Notifications.Targets {
		switch t.Type {
		case "stdout":
			notifiers = append(notifiers, stdout.New())
		case "ntfy":
			username, password := resolveEnvCredential(t.UsernameEnv, t.PasswordEnv)
			notifiers = append(notifiers, ntfy.New(ntfy.Config{
				ServerURL: t.ServerURL,
				Topic:     t.Topic,
				Username:  username,
				Password:  password,
				Priority:  t.Priority,
				Title:     t.Title,
			}, nil))
		case "webhook":
			notifiers = append(notifiers, webhook.New(webhook.Config{URL: t.URL}, nil))
		default:
			fmt.Fprintf(os.Stderr, "warning: skipping unrecognized notification target type %q\n", t.Type)
		}
	}
	return notifiers
}

// resolveEnvCredential reads a username/password pair from environment variables.
func resolveEnvCredential(usernameEnv, passwordEnv string) (username, password string) {
	if usernameEnv != "" {
		username = os.Getenv(usernameEnv)
	}
	if passwordEnv != "" {
		password = os.Getenv(passwordEnv)
	}
	return username, password
}

// credentialProviderFor resolves registry credentials from the environment.
func credentialProviderFor(cfg config.Config) distribution.CredentialProvider {
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
