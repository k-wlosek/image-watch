package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	return path
}

func TestLoad_NoFileUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	nonexistent := filepath.Join(dir, "does-not-exist.yaml")
	_ = nonexistent

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") should not error when no config file exists anywhere expected: %v", err)
	}
	if cfg.CheckInterval != 6*time.Hour {
		t.Errorf("expected default check interval, got %s", cfg.CheckInterval)
	}
}

func TestLoad_ExplicitMissingFileErrors(t *testing.T) {
	_, err := Load("/definitely/does/not/exist/config.yaml")
	if err == nil {
		t.Fatal("expected an error when an explicitly-named config file doesn't exist")
	}
}

func TestLoad_YAMLOverridesDefaults(t *testing.T) {
	path := writeConfig(t, `
check_interval: 30m

policy:
  patch: true
  minor: false
  major: false
  other_platform: true

notifications:
  mode: individual
  targets:
    - type: stdout
    - type: ntfy
      topic: docker-updates
      priority: high

state:
  path: /custom/state.db
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.CheckInterval != 30*time.Minute {
		t.Errorf("CheckInterval = %s, want 30m", cfg.CheckInterval)
	}
	if !cfg.Policy.Patch {
		t.Errorf("expected Policy.Patch = true")
	}
	if cfg.Policy.Minor {
		t.Errorf("expected Policy.Minor = false")
	}
	if !cfg.Policy.OtherPlatform {
		t.Errorf("expected Policy.OtherPlatform = true")
	}
	// Fields not mentioned in the YAML should retain their built-in
	// default, not silently become the zero value.
	if !cfg.Policy.TagChanged {
		t.Errorf("expected unmentioned Policy.TagChanged to keep its true default")
	}

	if cfg.Notifications.Mode != "individual" {
		t.Errorf("Notifications.Mode = %q, want individual", cfg.Notifications.Mode)
	}
	if len(cfg.Notifications.Targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(cfg.Notifications.Targets))
	}
	if cfg.Notifications.Targets[1].Priority != "high" {
		t.Errorf("expected ntfy target priority 'high', got %q", cfg.Notifications.Targets[1].Priority)
	}

	if cfg.State.Path != "/custom/state.db" {
		t.Errorf("State.Path = %q, want /custom/state.db", cfg.State.Path)
	}
}

func TestLoad_PolicyBooleanFalseIsRespected(t *testing.T) {
	path := writeConfig(t, `
policy:
  tag_changed: false
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Policy.TagChanged {
		t.Errorf("expected explicit tag_changed: false to be respected, got true")
	}
	// Everything else should remain default.
	if !cfg.Policy.Patch {
		t.Errorf("expected unrelated Policy.Patch to remain at its default (true)")
	}
}

func TestLoad_RegistriesParsed(t *testing.T) {
	path := writeConfig(t, `
registries:
  ghcr.io:
    username_env: GHCR_USERNAME
    password_env: GHCR_PASSWORD
    scheme: https
    ca_file: /etc/ssl/private-ca.pem
  registry.local:
    scheme: http
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	auth, ok := cfg.Registries["ghcr.io"]
	if !ok {
		t.Fatalf("expected ghcr.io registry config to be present")
	}
	if auth.UsernameEnv != "GHCR_USERNAME" || auth.PasswordEnv != "GHCR_PASSWORD" {
		t.Errorf("unexpected registry auth config: %+v", auth)
	}
	if auth.Scheme != "https" || auth.CAFile != "/etc/ssl/private-ca.pem" {
		t.Errorf("expected scheme/ca_file to be parsed, got %+v", auth)
	}
	if plain, ok := cfg.Registries["registry.local"]; !ok || plain.Scheme != "http" {
		t.Errorf("expected registry.local to parse scheme: http, got %+v", plain)
	}
}

func TestLoad_InvalidRegistrySchemeErrors(t *testing.T) {
	path := writeConfig(t, `
registries:
  registry.local:
    scheme: invalid
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected an error for an unsupported registry scheme")
	}
}

func TestLoad_InvalidDurationErrors(t *testing.T) {
	path := writeConfig(t, `check_interval: not-a-duration`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected an error for an invalid check_interval")
	}
}

func TestLoad_InvalidRuntimeTypeErrors(t *testing.T) {
	path := writeConfig(t, `
runtime:
  type: containerd
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected an error for an unsupported runtime type")
	}
}

func TestLoad_PodmanRuntimeTypeAccepted(t *testing.T) {
	path := writeConfig(t, `
runtime:
  type: podman
  endpoint: unix:///run/podman/podman.sock
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Runtime.Type != "podman" {
		t.Errorf("Runtime.Type = %q, want podman", cfg.Runtime.Type)
	}
	if cfg.Runtime.Endpoint != "unix:///run/podman/podman.sock" {
		t.Errorf("Runtime.Endpoint = %q, want the podman socket", cfg.Runtime.Endpoint)
	}
}

func TestLoad_PodmanTypeDefaultsEndpoint(t *testing.T) {
	path := writeConfig(t, `
runtime:
  type: podman
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if want := "unix:///run/podman/podman.sock"; cfg.Runtime.Endpoint != want {
		t.Errorf("Runtime.Endpoint = %q, want podman default %q", cfg.Runtime.Endpoint, want)
	}
}

func TestApplyEnvOverrides_PodmanRuntimeType(t *testing.T) {
	t.Setenv("IMAGE_WATCH_RUNTIME_TYPE", "podman")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Runtime.Type != "podman" {
		t.Errorf("Runtime.Type = %q, want podman (from env override)", cfg.Runtime.Type)
	}
	if want := "unix:///run/podman/podman.sock"; cfg.Runtime.Endpoint != want {
		t.Errorf("Runtime.Endpoint = %q, want %q (podman default socket)", cfg.Runtime.Endpoint, want)
	}
}

func TestNoEnvOverride_ImplicitEndpoint(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Runtime.Type != "docker" {
		t.Errorf("Runtime.Type = %q, want docker (default)", cfg.Runtime.Type)
	}
	if want := "unix:///var/run/docker.sock"; cfg.Runtime.Endpoint != want {
		t.Errorf("Runtime.Endpoint = %q, want %q (docker default socket)", cfg.Runtime.Endpoint, want)
	}
}

func TestLoad_InvalidNotificationModeErrors(t *testing.T) {
	path := writeConfig(t, `
notifications:
  mode: sometimes
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected an error for an invalid notifications.mode")
	}
}

func TestLoad_EnrichmentParsed(t *testing.T) {
	path := writeConfig(t, `
enrichment:
  max_tags: 250
  timeout: 45s
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Enrichment.MaxTags != 250 {
		t.Errorf("Enrichment.MaxTags = %d, want 250", cfg.Enrichment.MaxTags)
	}
	if cfg.Enrichment.Timeout != 45*time.Second {
		t.Errorf("Enrichment.Timeout = %s, want 45s", cfg.Enrichment.Timeout)
	}
}

func TestLoad_EnrichmentDefaults(t *testing.T) {
	path := writeConfig(t, `check_interval: 30m`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	def := DefaultEnrichmentConfig()
	if cfg.Enrichment != def {
		t.Errorf("expected enrichment defaults when unmentioned, got %+v, want %+v", cfg.Enrichment, def)
	}
}

func TestLoad_InvalidEnrichmentErrors(t *testing.T) {
	for name, content := range map[string]string{
		"bad timeout":   "enrichment:\n  timeout: not-a-duration",
		"zero timeout":  "enrichment:\n  timeout: 0s",
		"negative tags": "enrichment:\n  max_tags: -1",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeConfig(t, content))
			if err == nil {
				t.Fatal("expected an error for invalid enrichment config")
			}
		})
	}
}

func TestLoad_ConcurrencyParsed(t *testing.T) {
	path := writeConfig(t, `
concurrency:
  workers: 8
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Concurrency.Workers != 8 {
		t.Errorf("Concurrency.Workers = %d, want 8", cfg.Concurrency.Workers)
	}
}

func TestLoad_ConcurrencyDefaults(t *testing.T) {
	path := writeConfig(t, `check_interval: 30m`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Concurrency.Workers != DefaultConcurrencyConfig().Workers {
		t.Errorf("expected concurrency.workers to keep its default, got %d", cfg.Concurrency.Workers)
	}
}

func TestLoad_InvalidConcurrencyErrors(t *testing.T) {
	for _, workers := range []string{"0", "-1"} {
		t.Run("workers="+workers, func(t *testing.T) {
			_, err := Load(writeConfig(t, "concurrency:\n  workers: "+workers))
			if err == nil {
				t.Fatal("expected an error for concurrency.workers < 1")
			}
		})
	}
}

func TestApplyEnvOverrides_Precedence(t *testing.T) {
	path := writeConfig(t, `check_interval: 30m`)

	t.Setenv("IMAGE_WATCH_CHECK_INTERVAL", "1h")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	// Env var must win over YAML.
	// default -> YAML -> environment.
	if cfg.CheckInterval != time.Hour {
		t.Errorf("CheckInterval = %s, want 1h (env override should win over YAML's 30m)", cfg.CheckInterval)
	}
}

func TestApplyEnvOverrides_InvalidValueErrors(t *testing.T) {
	t.Setenv("IMAGE_WATCH_CHECK_INTERVAL", "not-a-duration")
	_, err := Load("")
	if err == nil {
		t.Fatal("expected Load to return an error for an invalid IMAGE_WATCH_CHECK_INTERVAL, not silently fall back to the default")
	}
}

func TestLoad_NoFileStillValidates(t *testing.T) {
	t.Setenv("IMAGE_WATCH_RUNTIME_TYPE", "kubernetes") // unsupported runtime type
	_, err := Load("")
	if err == nil {
		t.Fatal("expected Load(\"\") with no config file to still validate the final config and reject an unsupported runtime type")
	}
}

func TestLoad_ConfigPathEnvVar(t *testing.T) {
	path := writeConfig(t, `check_interval: 45m`)
	t.Setenv(envConfigPathVar, path)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.CheckInterval != 45*time.Minute {
		t.Errorf("expected IMAGE_WATCH_CONFIG_PATH to be honored, got %s", cfg.CheckInterval)
	}
}

func TestLoad_ConfigPathIsDirectoryErrors(t *testing.T) {
	dir := t.TempDir() // a directory, not a file
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected a read failure for a directory path, got %v", err)
	}
}

func TestLoad_InvalidYAMLErrors(t *testing.T) {
	path := writeConfig(t, "check_interval: [unclosed")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected a YAML parse error")
	}
}

func TestLoad_MergeRawOverrides(t *testing.T) {
	path := writeConfig(t, `
runtime:
  endpoint: tcp://127.0.0.1:2375

notifications:
  registry_outage:
    enabled: true
    consecutive_failures: 5

metrics:
  listen: "127.0.0.1:9999"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Runtime.Endpoint != "tcp://127.0.0.1:2375" {
		t.Errorf("Runtime.Endpoint = %q", cfg.Runtime.Endpoint)
	}
	if !cfg.Notifications.RegistryOutage.Enabled || cfg.Notifications.RegistryOutage.ConsecutiveFailures != 5 {
		t.Errorf("RegistryOutage = %+v, want enabled + 5 failures", cfg.Notifications.RegistryOutage)
	}
	if cfg.Metrics.Listen != "127.0.0.1:9999" {
		t.Errorf("Metrics.Listen = %q", cfg.Metrics.Listen)
	}
}

func TestApplyEnvOverrides_StateRuntimeMetrics(t *testing.T) {
	t.Setenv("IMAGE_WATCH_STATE_PATH", "/tmp/test-state.db")
	t.Setenv("IMAGE_WATCH_RUNTIME_TYPE", "docker")
	t.Setenv("IMAGE_WATCH_RUNTIME_ENDPOINT", "tcp://10.0.0.1:2375")
	t.Setenv("IMAGE_WATCH_METRICS_LISTEN", "127.0.0.1:8080")

	cfg := Default()
	got, err := applyEnvOverrides(cfg)
	if err != nil {
		t.Fatalf("applyEnvOverrides: %v", err)
	}
	if got.State.Path != "/tmp/test-state.db" || got.Runtime.Endpoint != "tcp://10.0.0.1:2375" || got.Metrics.Listen != "127.0.0.1:8080" {
		t.Errorf("env overrides not applied: %+v", got)
	}
}

func TestLoad_InvalidIntervalEnvOverrideAfterFile(t *testing.T) {
	path := writeConfig(t, "check_interval: 30m")
	t.Setenv("IMAGE_WATCH_CHECK_INTERVAL", "not-a-duration")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected the invalid env override to fail the load")
	}
}

func TestLoad_NonPositiveIntervalErrors(t *testing.T) {
	t.Setenv("IMAGE_WATCH_CHECK_INTERVAL", "0s")
	_, err := Load("")
	if err == nil {
		t.Fatal("expected a non-positive check interval to be rejected")
	}
}
