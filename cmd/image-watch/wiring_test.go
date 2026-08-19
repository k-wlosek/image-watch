package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/k-wlosek/image-watch/internal/config"
	"github.com/k-wlosek/image-watch/internal/metrics"
	"github.com/k-wlosek/image-watch/internal/notify/ntfy"
	"github.com/k-wlosek/image-watch/internal/notify/stdout"
	"github.com/k-wlosek/image-watch/internal/notify/webhook"
	"github.com/k-wlosek/image-watch/internal/policy"
	"github.com/k-wlosek/image-watch/internal/registry/distribution"
	"github.com/k-wlosek/image-watch/internal/runtime/docker"
	"github.com/k-wlosek/image-watch/internal/state"
)

func TestBuildNotifiers_DefaultsToStdout(t *testing.T) {
	cfg := config.Default()
	cfg.Notifications.Targets = nil
	notifiers := buildNotifiers(cfg)
	if len(notifiers) != 1 {
		t.Fatalf("expected 1 default notifier, got %d", len(notifiers))
	}
	if _, ok := notifiers[0].(*stdout.Notifier); !ok {
		t.Errorf("expected stdout notifier, got %T", notifiers[0])
	}
}

func TestBuildNotifiers_Targets(t *testing.T) {
	t.Setenv("NTFY_USER", "user")
	t.Setenv("NTFY_PASS", "pass")

	cfg := config.Default()
	cfg.Notifications.Targets = []config.NotificationTarget{
		{Type: "stdout"},
		{Type: "webhook", URL: "https://example.com/hook"},
		{Type: "ntfy", ServerURL: "https://ntfy.sh", Topic: "docker-updates",
			UsernameEnv: "NTFY_USER", PasswordEnv: "NTFY_PASS", Priority: "high", Title: "updates"},
	}
	notifiers := buildNotifiers(cfg)
	if len(notifiers) != 3 {
		t.Fatalf("expected 3 notifiers, got %d", len(notifiers))
	}
	if _, ok := notifiers[0].(*stdout.Notifier); !ok {
		t.Errorf("expected stdout, got %T", notifiers[0])
	}
	if _, ok := notifiers[1].(*webhook.Notifier); !ok {
		t.Errorf("expected webhook, got %T", notifiers[1])
	}
	if _, ok := notifiers[2].(*ntfy.Notifier); !ok {
		t.Errorf("expected ntfy, got %T", notifiers[2])
	}
}

func TestBuildNotifiers_SkipsUnknownTypes(t *testing.T) {
	cfg := config.Default()
	cfg.Notifications.Targets = []config.NotificationTarget{
		{Type: "email"},
		{Type: "stdout"},
	}
	notifiers := buildNotifiers(cfg)
	if len(notifiers) != 1 {
		t.Errorf("expected the unknown type to be skipped, got %d notifiers", len(notifiers))
	}
}

func TestResolveEnvCredential(t *testing.T) {
	t.Setenv("IW_TEST_USER", "alice")
	t.Setenv("IW_TEST_PASS", "secret")

	u, p := resolveEnvCredential("IW_TEST_USER", "IW_TEST_PASS")
	if u != "alice" || p != "secret" {
		t.Errorf("got %q/%q, want alice/secret", u, p)
	}
	if u, p := resolveEnvCredential("", "IW_TEST_PASS"); u != "" || p != "secret" {
		t.Errorf("empty username env should resolve to empty, got %q/%q", u, p)
	}
	if u, p := resolveEnvCredential("UNSET_VAR_X", "UNSET_VAR_Y"); u != "" || p != "" {
		t.Errorf("unset env vars should resolve empty, got %q/%q", u, p)
	}
}

func TestCredentialProviderFor(t *testing.T) {
	t.Setenv("IW_REG_USER", "reguser")
	t.Setenv("IW_REG_PASS", "regpass")

	cfg := config.Default()
	cfg.Registries["ghcr.io"] = config.RegistryAuthConfig{
		UsernameEnv: "IW_REG_USER",
		PasswordEnv: "IW_REG_PASS",
	}

	provider := credentialProviderFor(cfg)
	u, p, ok := provider("ghcr.io")
	if !ok || u != "reguser" || p != "regpass" {
		t.Errorf("provider(ghcr.io) = %q/%q/%v, want reguser/regpass/true", u, p, ok)
	}
	if _, _, ok := provider("docker.io"); ok {
		t.Errorf("provider for an unknown host should not resolve credentials")
	}
}

func TestEnabledCategories(t *testing.T) {
	p := policy.Default()
	if got := enabledCategories(p); got != "patch, minor, major, base-advancement, tag-changed, tag-mutated" {
		t.Errorf("default categories = %q", got)
	}
	none := policy.Policy{}
	if got := enabledCategories(none); got != "(none)" {
		t.Errorf("empty policy categories = %q, want (none)", got)
	}
}

func TestHTTPClientForRegistry(t *testing.T) {
	if c, err := httpClientForRegistry(config.RegistryAuthConfig{}); err != nil || c != nil {
		t.Errorf("no ca_file should yield (nil, nil), got (%v, %v)", c, err)
	}

	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.pem")
	if _, err := httpClientForRegistry(config.RegistryAuthConfig{CAFile: missing}); err == nil {
		t.Errorf("expected an error for a missing ca_file")
	}

	badPEM := filepath.Join(dir, "bad.pem")
	os.WriteFile(badPEM, []byte("not a certificate"), 0o600)
	if _, err := httpClientForRegistry(config.RegistryAuthConfig{CAFile: badPEM}); err == nil {
		t.Errorf("expected an error for a ca_file without PEM certificates")
	}

	goodPEM := filepath.Join(dir, "good.pem")
	os.WriteFile(goodPEM, testCertPEM(t), 0o600)
	c, err := httpClientForRegistry(config.RegistryAuthConfig{CAFile: goodPEM})
	if err != nil {
		t.Fatalf("expected a client for a valid ca_file: %v", err)
	}
	if c == nil || c.Transport == nil {
		t.Errorf("expected a client with a custom transport")
	}
}

func testCertPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestRegistryInstrumentation(t *testing.T) {
	m := metrics.New()
	ri := registryInstrumentation{m}
	ri.ObserveRequest("ghcr.io", time.Second, nil) // must not panic
	eo := enrichmentObserver{m}
	eo.ObserveEnrichment(true)
	eo.ObserveEnrichment(false)
}

func TestShutdownHTTPServer(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdownHTTPServer(ctx, srv); err != nil {
		t.Errorf("shutdown error: %v", err)
	}
}

func buildObserverTestConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.State.Path = filepath.Join(t.TempDir(), "state.db")
	cfg.Registries["registry.example.com"] = config.RegistryAuthConfig{Scheme: "http"}
	return cfg
}

func TestBuildObserver_WiresObserver(t *testing.T) {
	cfg := buildObserverTestConfig(t)
	obs, err := buildObserver(cfg, nil)
	if err != nil {
		t.Fatalf("buildObserver error: %v", err)
	}
	if _, ok := obs.Store.(*state.SQLiteStore); !ok {
		t.Errorf("Store = %T, want *state.SQLiteStore", obs.Store)
	}
	if _, ok := obs.Runtime.(*docker.Client); !ok {
		t.Errorf("Runtime = %T, want *docker.Client", obs.Runtime)
	}
	if obs.Metrics != nil {
		t.Errorf("expected nil Metrics field when no metrics instance is passed, got %v", obs.Metrics)
	}

	pre, ok := obs.Registries("registry.example.com").(*distribution.Client)
	if !ok {
		t.Fatalf("pre-built registry client = %T, want *distribution.Client", obs.Registries("registry.example.com"))
	}
	if pre.Scheme != "http" {
		t.Errorf("pre-built client Scheme = %q, want http", pre.Scheme)
	}

	lazy := obs.Registries("lazy.example.com")
	if _, ok := lazy.(*distribution.Client); !ok {
		t.Errorf("lazy-built client = %T, want *distribution.Client", lazy)
	}
	if again := obs.Registries("lazy.example.com"); again != lazy {
		t.Errorf("expected the lazily-built client to be cached")
	}
}

func TestBuildObserver_WithMetrics(t *testing.T) {
	cfg := buildObserverTestConfig(t)
	obs, err := buildObserver(cfg, metrics.New())
	if err != nil {
		t.Fatalf("buildObserver error: %v", err)
	}
	if obs.Metrics == nil {
		t.Error("expected a Metrics field when a metrics instance is provided")
	}
}

func TestBuildObserver_InvalidRuntimeEndpoint(t *testing.T) {
	cfg := buildObserverTestConfig(t)
	cfg.Runtime.Endpoint = "ftp://example.com"
	if _, err := buildObserver(cfg, nil); err == nil {
		t.Fatal("expected buildObserver to fail for an unsupported runtime endpoint")
	}
}

func TestBuildObserver_InvalidStatePath(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := buildObserverTestConfig(t)
	cfg.State.Path = filepath.Join(parent, "state.db")
	if _, err := buildObserver(cfg, nil); err == nil {
		t.Fatal("expected buildObserver to fail when the state directory can't be created")
	}
}

func TestCredentialProviderFor_UnsetEnv(t *testing.T) {
	cfg := config.Default()
	cfg.Registries["ghcr.io"] = config.RegistryAuthConfig{
		UsernameEnv: "IW_DOES_NOT_EXIST",
		PasswordEnv: "IW_ALSO_DOES_NOT_EXIST",
	}
	provider := credentialProviderFor(cfg)
	if u, p, ok := provider("ghcr.io"); ok || u != "" || p != "" {
		t.Errorf("provider = %q/%q/%v, want empty/empty/false when env vars are unset", u, p, ok)
	}
}
