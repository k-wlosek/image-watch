package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/k-wlosek/image-watch/internal/config"
	"github.com/k-wlosek/image-watch/internal/metrics"
	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/notify/ntfy"
	"github.com/k-wlosek/image-watch/internal/notify/stdout"
	"github.com/k-wlosek/image-watch/internal/notify/webhook"
	"github.com/k-wlosek/image-watch/internal/observer"
	"github.com/k-wlosek/image-watch/internal/policy"
	"github.com/k-wlosek/image-watch/internal/registry"
	"github.com/k-wlosek/image-watch/internal/registry/distribution"
	"github.com/k-wlosek/image-watch/internal/runtime/docker"
	"github.com/k-wlosek/image-watch/internal/state"
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

	var registryMu sync.Mutex
	registryClients := make(map[string]registry.Registry)
	credentials := credentialProviderFor(cfg)

	// Pre-build clients for configured hosts: custom TLS trust and plain
	// HTTP are per-host wiring concerns that can't be expressed by the
	// default client the resolver builds lazily.
	for host, auth := range cfg.Registries {
		httpClient, err := httpClientForRegistry(auth)
		if err != nil {
			return nil, fmt.Errorf("registry %s: %w", host, err)
		}
		c := distribution.New(host, httpClient, credentials)
		c.Scheme = auth.Scheme
		if m != nil {
			c.Instrumentation = registryInstrumentation{m}
		}
		registryClients[host] = c
	}

	resolver := func(host string) registry.Registry {
		registryMu.Lock()
		defer registryMu.Unlock()
		if c, ok := registryClients[host]; ok {
			return c
		}
		c := distribution.New(host, nil, credentials)
		if m != nil {
			c.Instrumentation = registryInstrumentation{m}
		}
		registryClients[host] = c
		return c
	}

	obs := &observer.Observer{
		Runtime:            dockerClient,
		Registries:         resolver,
		Store:              store,
		DefaultPolicy:      cfg.Policy,
		EnrichmentMaxTags:  cfg.Enrichment.MaxTags,
		EnrichmentTimeout:  cfg.Enrichment.Timeout,
		ConcurrencyWorkers: cfg.Concurrency.Workers,
	}
	if m != nil {
		obs.Metrics = enrichmentObserver{m}
	}
	return obs, nil
}

// httpClientForRegistry builds an http.Client honoring a registry host's
// TLS trust configuration. It returns nil when no custom trust is
// configured, letting distribution.New fall back to the default transport
// (system trust store). The scheme is applied to the client's base URL by
// the caller, not here.
func httpClientForRegistry(auth config.RegistryAuthConfig) (*http.Client, error) {
	if auth.CAFile == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(auth.CAFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read ca_file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("ca_file %s contains no valid PEM certificates", auth.CAFile)
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: transport}, nil
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
	var out strings.Builder
	out.WriteString(parts[0])
	for _, p := range parts[1:] {
		out.WriteString(", ")
		out.WriteString(p)
	}
	return out.String()
}
