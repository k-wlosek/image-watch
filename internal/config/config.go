// Package config loads and validates image-watch's YAML configuration,
// with environment-variable overrides for a limited, deliberately
// non-exhaustive set of fields.
package config

import (
	"time"

	"github.com/k-wlosek/image-watch/internal/policy"
)

// Config is the resolved daemon configuration.
type Config struct {
	Runtime       RuntimeConfig
	CheckInterval time.Duration
	Policy        policy.Policy
	Notifications NotificationsConfig
	Metrics       MetricsConfig
	State         StateConfig
	Enrichment    EnrichmentConfig
	Concurrency   ConcurrencyConfig
	Registries    map[string]RegistryAuthConfig
}

// RuntimeConfig selects the container runtime adapter.
type RuntimeConfig struct {
	Type     string
	Endpoint string
}

// NotificationsConfig configures notification delivery.
type NotificationsConfig struct {
	Mode    string // "batch" or "individual"
	Targets []NotificationTarget

	// RegistryOutage configures aggregated outage notifications.
	RegistryOutage RegistryOutageConfig
}

// NotificationTarget is one delivery destination.
type NotificationTarget struct {
	Type string // "stdout", "ntfy", "webhook"

	// ntfy fields
	ServerURL   string
	Topic       string
	UsernameEnv string
	PasswordEnv string
	Priority    string
	Title       string

	// webhook fields
	URL string
}

// RegistryOutageConfig controls aggregated outage notifications.
type RegistryOutageConfig struct {
	Enabled             bool
	ConsecutiveFailures int
}

// DefaultRegistryOutageConfig returns the default outage config.
func DefaultRegistryOutageConfig() RegistryOutageConfig {
	return RegistryOutageConfig{
		Enabled:             false,
		ConsecutiveFailures: 3,
	}
}

// MetricsConfig configures the metrics endpoint.
type MetricsConfig struct {
	Enabled bool
	Listen  string
}

// StateConfig configures SQLite persistence.
type StateConfig struct {
	Path string
}

// EnrichmentConfig bounds the best-effort enrichment of opaque tags
// (e.g. identifying which release tag `latest` serves).
type EnrichmentConfig struct {
	MaxTags int
	Timeout time.Duration
}

// DefaultEnrichmentConfig returns the default enrichment limits.
func DefaultEnrichmentConfig() EnrichmentConfig {
	return EnrichmentConfig{
		MaxTags: 100,
		Timeout: 30 * time.Second,
	}
}

// ConcurrencyConfig bounds how many registry operations run in parallel
// during a check cycle.
type ConcurrencyConfig struct {
	// Workers is the maximum number of image groups checked at once; it also
	// bounds how many candidate manifests an enrichment scan resolves
	// concurrently.
	Workers int
}

// DefaultConcurrencyConfig returns the default concurrency limits.
func DefaultConcurrencyConfig() ConcurrencyConfig {
	return ConcurrencyConfig{Workers: 4}
}

// RegistryAuthConfig configures credentials and TLS trust for one registry host.
type RegistryAuthConfig struct {
	UsernameEnv string
	PasswordEnv string

	// Scheme is the connection scheme: "https" (default) or "http" for
	// registries that serve plaintext (an explicit, insecure opt-in).
	Scheme string

	// CAFile is an optional path to a PEM bundle holding the private CA
	// that issued the registry's certificate. Empty uses the system trust
	// store. When set, this bundle replaces the system trust store for the
	// host.
	CAFile string
}

// Default returns the built-in configuration defaults.
func Default() Config {
	return Config{
		Runtime: RuntimeConfig{
			Type:     "docker",
			Endpoint: "unix:///var/run/docker.sock",
		},
		CheckInterval: 6 * time.Hour,
		Policy:        policy.Default(),
		Notifications: NotificationsConfig{
			Mode:           "batch",
			RegistryOutage: DefaultRegistryOutageConfig(),
		},
		Metrics: MetricsConfig{
			Enabled: true,
			Listen:  "0.0.0.0:9090",
		},
		State: StateConfig{
			Path: "/var/lib/image-watch/state.db",
		},
		Enrichment:  DefaultEnrichmentConfig(),
		Concurrency: DefaultConcurrencyConfig(),
		Registries:  map[string]RegistryAuthConfig{},
	}
}
