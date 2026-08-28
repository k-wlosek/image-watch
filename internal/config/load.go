package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// DefaultConfigPath is used when Load is called without an explicit path.
const DefaultConfigPath = "/etc/image-watch/config.yaml"

const envConfigPathVar = "IMAGE_WATCH_CONFIG_PATH"

// Load resolves defaults, YAML, and environment overrides.
func Load(path string) (Config, error) {
	cfg := Default()

	resolvedPath := path
	explicit := path != ""
	if resolvedPath == "" {
		if envPath := os.Getenv(envConfigPathVar); envPath != "" {
			resolvedPath = envPath
			explicit = true
		} else {
			resolvedPath = DefaultConfigPath
		}
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			// No file at the conventional default location, use defaults.
			cfg, err = applyEnvOverrides(cfg)
			if err != nil {
				return Config{}, fmt.Errorf("config: %w", err)
			}
			if err := validate(cfg); err != nil {
				return Config{}, fmt.Errorf("config: %w", err)
			}
			return cfg, nil
		}
		if os.IsNotExist(err) {
			return Config{}, fmt.Errorf("config: no such file %s", resolvedPath)
		}
		return Config{}, fmt.Errorf("config: failed to read %s: %w", resolvedPath, err)
	}

	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("config: failed to parse %s: %w", resolvedPath, err)
	}

	cfg, err = mergeRaw(cfg, raw)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid value in %s: %w", resolvedPath, err)
	}

	migrateNotificationTargets(&cfg, resolvedPath)

	cfg, err = applyEnvOverrides(cfg)
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	if err := validate(cfg); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	return cfg, nil
}

// rawNotificationTarget unmarshals a notification target from YAML in either
// the legacy flat format or the new Params-based format.
type rawNotificationTarget struct {
	Type   string            `yaml:"type"`
	Params map[string]string `yaml:"params"`

	// Legacy flat fields, populated when YAML has no "params" key.
	ServerURL   string `yaml:"server_url"`
	Topic       string `yaml:"topic"`
	UsernameEnv string `yaml:"username_env"`
	PasswordEnv string `yaml:"password_env"`
	Priority    string `yaml:"priority"`
	Title       string `yaml:"title"`
	URL         string `yaml:"url"`
}

// hasLegacyFields reports whether this target uses the old flat-field format.
func (t *rawNotificationTarget) hasLegacyFields() bool {
	return t.Params == nil && (t.ServerURL != "" || t.Topic != "" || t.UsernameEnv != "" ||
		t.PasswordEnv != "" || t.Priority != "" || t.Title != "" || t.URL != "")
}

// toParams converts legacy flat fields into a Params map.
func (t *rawNotificationTarget) toParams() map[string]string {
	m := make(map[string]string)
	if t.Topic != "" {
		m["topic"] = t.Topic
	}
	if t.ServerURL != "" {
		m["server_url"] = t.ServerURL
	}
	if t.UsernameEnv != "" {
		m["username_env"] = t.UsernameEnv
	}
	if t.PasswordEnv != "" {
		m["password_env"] = t.PasswordEnv
	}
	if t.Priority != "" {
		m["priority"] = t.Priority
	}
	if t.Title != "" {
		m["title"] = t.Title
	}
	if t.URL != "" {
		m["url"] = t.URL
	}
	return m
}

// rawConfig mirrors the YAML shape.
type rawConfig struct {
	Runtime *struct {
		Type     string `yaml:"type"`
		Endpoint string `yaml:"endpoint"`
	} `yaml:"runtime"`

	CheckInterval string `yaml:"check_interval"`

	Policy *struct {
		Patch             *bool `yaml:"patch"`
		Minor             *bool `yaml:"minor"`
		Major             *bool `yaml:"major"`
		FamilyAdvancement *bool `yaml:"family_advancement"`
		BaseAdvancement   *bool `yaml:"base_advancement"`
		TagChanged        *bool `yaml:"tag_changed"`
		TagMutated        *bool `yaml:"tag_mutated"`
		OtherPlatform     *bool `yaml:"other_platform"`
	} `yaml:"policy"`

	Notifications *struct {
		Mode           string                  `yaml:"mode"`
		Targets        []rawNotificationTarget `yaml:"targets"`
		RegistryOutage *struct {
			Enabled             *bool `yaml:"enabled"`
			ConsecutiveFailures *int  `yaml:"consecutive_failures"`
		} `yaml:"registry_outage"`
	} `yaml:"notifications"`

	Metrics *struct {
		Enabled *bool  `yaml:"enabled"`
		Listen  string `yaml:"listen"`
	} `yaml:"metrics"`

	State *struct {
		Path string `yaml:"path"`
	} `yaml:"state"`

	Enrichment *struct {
		MaxTags *int   `yaml:"max_tags"`
		Timeout string `yaml:"timeout"`
	} `yaml:"enrichment"`

	Concurrency *struct {
		Workers *int `yaml:"workers"`
	} `yaml:"concurrency"`

	Registries map[string]struct {
		UsernameEnv string `yaml:"username_env"`
		PasswordEnv string `yaml:"password_env"`
		Scheme      string `yaml:"scheme"`
		CAFile      string `yaml:"ca_file"`
	} `yaml:"registries"`

	Log *struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"log"`
}

func mergeRaw(cfg Config, raw rawConfig) (Config, error) {
	if raw.Runtime != nil {
		if raw.Runtime.Type != "" {
			cfg.Runtime.Type = raw.Runtime.Type
		}
		if raw.Runtime.Endpoint != "" {
			cfg.Runtime.Endpoint = raw.Runtime.Endpoint
		} else {
			// No explicit endpoint: align it to the conventional socket
			// for the resolved runtime type.
			cfg.Runtime.Endpoint = DefaultEndpoint(cfg.Runtime.Type)
		}
	}

	if raw.CheckInterval != "" {
		d, err := time.ParseDuration(raw.CheckInterval)
		if err != nil {
			return Config{}, fmt.Errorf("check_interval: %w", err)
		}
		cfg.CheckInterval = d
	}

	if raw.Policy != nil {
		p := raw.Policy
		setBool(&cfg.Policy.Patch, p.Patch)
		setBool(&cfg.Policy.Minor, p.Minor)
		setBool(&cfg.Policy.Major, p.Major)
		setBool(&cfg.Policy.FamilyAdvancement, p.FamilyAdvancement)
		setBool(&cfg.Policy.BaseAdvancement, p.BaseAdvancement)
		setBool(&cfg.Policy.TagChanged, p.TagChanged)
		setBool(&cfg.Policy.TagMutated, p.TagMutated)
		setBool(&cfg.Policy.OtherPlatform, p.OtherPlatform)
	}

	if raw.Notifications != nil {
		if raw.Notifications.Mode != "" {
			cfg.Notifications.Mode = raw.Notifications.Mode
		}
		if raw.Notifications.Targets != nil {
			cfg.Notifications.Targets = nil
			for _, t := range raw.Notifications.Targets {
				params := t.Params
				if params == nil && t.hasLegacyFields() {
					params = t.toParams()
				}
				cfg.Notifications.Targets = append(cfg.Notifications.Targets, NotificationTarget{
					Type:   t.Type,
					Params: params,
				})
			}
		}
		if raw.Notifications.RegistryOutage != nil {
			ro := raw.Notifications.RegistryOutage
			if ro.Enabled != nil {
				cfg.Notifications.RegistryOutage.Enabled = *ro.Enabled
			}
			if ro.ConsecutiveFailures != nil {
				cfg.Notifications.RegistryOutage.ConsecutiveFailures = *ro.ConsecutiveFailures
			}
		}
	}

	if raw.Metrics != nil {
		if raw.Metrics.Enabled != nil {
			cfg.Metrics.Enabled = *raw.Metrics.Enabled
		}
		if raw.Metrics.Listen != "" {
			cfg.Metrics.Listen = raw.Metrics.Listen
		}
	}

	if raw.State != nil && raw.State.Path != "" {
		cfg.State.Path = raw.State.Path
	}

	if raw.Enrichment != nil {
		if raw.Enrichment.MaxTags != nil {
			cfg.Enrichment.MaxTags = *raw.Enrichment.MaxTags
		}
		if raw.Enrichment.Timeout != "" {
			d, err := time.ParseDuration(raw.Enrichment.Timeout)
			if err != nil {
				return Config{}, fmt.Errorf("enrichment.timeout: %w", err)
			}
			cfg.Enrichment.Timeout = d
		}
	}

	if raw.Concurrency != nil && raw.Concurrency.Workers != nil {
		cfg.Concurrency.Workers = *raw.Concurrency.Workers
	}

	if raw.Registries != nil {
		if cfg.Registries == nil {
			cfg.Registries = make(map[string]RegistryAuthConfig)
		}
		for host, auth := range raw.Registries {
			cfg.Registries[host] = RegistryAuthConfig{
				UsernameEnv: auth.UsernameEnv,
				PasswordEnv: auth.PasswordEnv,
				Scheme:      auth.Scheme,
				CAFile:      auth.CAFile,
			}
		}
	}

	if raw.Log != nil {
		if raw.Log.Level != "" {
			cfg.Log.Level = raw.Log.Level
		}
		if raw.Log.Format != "" {
			cfg.Log.Format = raw.Log.Format
		}
	}

	return cfg, nil
}

func setBool(dst *bool, src *bool) {
	if src != nil {
		*dst = *src
	}
}

// migrateNotificationTargets detects old-format targets and converts them to
// the new Params-based format. If the config file is writable, the migrated
// config is saved back. If not (e.g. read-only mount), a warning is logged
// and the migrated notifications section is printed to stdout.
func migrateNotificationTargets(cfg *Config, configPath string) {
	needsMigration := false
	for _, t := range cfg.Notifications.Targets {
		if t.Type == "stdout" {
			continue
		}
		if t.Params == nil {
			needsMigration = true
			break
		}
	}
	if !needsMigration {
		return
	}

	slog.Warn("notification config uses legacy format, migrating to params-based format")

	// Attempt to write back the migrated config.
	if err := writeMigratedConfig(cfg, configPath); err != nil {
		slog.Warn("could not save migrated config",
			"path", configPath, "error", err,
		)
		printMigratedNotifications(cfg)
	}
}

// writeMigratedConfig attempts to write the full config back to the file.
func writeMigratedConfig(cfg *Config, configPath string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal migrated config: %w", err)
	}
	return os.WriteFile(configPath, data, 0o644)
}

// printMigratedNotifications prints the migrated notifications section to stdout.
func printMigratedNotifications(cfg *Config) {
	type targetYAML struct {
		Type   string            `yaml:"type"`
		Params map[string]string `yaml:"params,omitempty"`
	}
	type notificationsYAML struct {
		Mode    string       `yaml:"mode"`
		Targets []targetYAML `yaml:"targets"`
	}

	targets := make([]targetYAML, 0, len(cfg.Notifications.Targets))
	for _, t := range cfg.Notifications.Targets {
		targets = append(targets, targetYAML(t))
	}

	out := notificationsYAML{Mode: cfg.Notifications.Mode, Targets: targets}
	data, err := yaml.Marshal(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "# failed to format migrated config: %v\n", err)
		return
	}

	var b strings.Builder
	b.WriteString("# Notification config has been migrated to the new format.\n")
	b.WriteString("# Add or replace the following section in your config file:\n\n")
	b.WriteString("notifications:\n")
	// Indent the marshaled content by 2 spaces.
	for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	fmt.Fprintln(os.Stderr, b.String())
}

// applyEnvOverrides applies environment variable overrides.
func applyEnvOverrides(cfg Config) (Config, error) {
	if v := os.Getenv("IMAGE_WATCH_CHECK_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("IMAGE_WATCH_CHECK_INTERVAL: invalid duration %q: %w", v, err)
		}
		cfg.CheckInterval = d
	}
	if v := os.Getenv("IMAGE_WATCH_STATE_PATH"); v != "" {
		cfg.State.Path = v
	}
	if v := os.Getenv("IMAGE_WATCH_RUNTIME_TYPE"); v != "" {
		cfg.Runtime.Type = v
		// Without an explicit endpoint override, assume the socket for
		// the selected runtime.
		if os.Getenv("IMAGE_WATCH_RUNTIME_ENDPOINT") == "" {
			cfg.Runtime.Endpoint = DefaultEndpoint(v)
		}
	}
	if v := os.Getenv("IMAGE_WATCH_RUNTIME_ENDPOINT"); v != "" {
		cfg.Runtime.Endpoint = v
	}
	if v := os.Getenv("IMAGE_WATCH_METRICS_LISTEN"); v != "" {
		cfg.Metrics.Listen = v
	}
	if v := os.Getenv("IMAGE_WATCH_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("IMAGE_WATCH_LOG_FORMAT"); v != "" {
		cfg.Log.Format = v
	}
	return cfg, nil
}

// validate performs minimal sanity checks.
func validate(cfg Config) error {
	switch cfg.Runtime.Type {
	case "docker", "podman":
		// Podman and Docker both suppport the API surface we care about,
		// so we can treat them the same way.
	default:
		return fmt.Errorf("unsupported runtime.type %q (v1 supports \"docker\" or \"podman\")", cfg.Runtime.Type)
	}
	if cfg.CheckInterval <= 0 {
		return fmt.Errorf("check_interval must be positive, got %s", cfg.CheckInterval)
	}
	if cfg.Notifications.Mode != "batch" && cfg.Notifications.Mode != "individual" {
		return fmt.Errorf("notifications.mode must be \"batch\" or \"individual\", got %q", cfg.Notifications.Mode)
	}
	for _, t := range cfg.Notifications.Targets {
		if t.Type == "" {
			return fmt.Errorf("notification target missing type")
		}
	}
	for host, auth := range cfg.Registries {
		if auth.Scheme != "" && auth.Scheme != "http" && auth.Scheme != "https" {
			return fmt.Errorf("registries.%s.scheme must be \"http\" or \"https\", got %q", host, auth.Scheme)
		}
	}
	if cfg.Enrichment.MaxTags < 0 {
		return fmt.Errorf("enrichment.max_tags must be >= 0, got %d", cfg.Enrichment.MaxTags)
	}
	if cfg.Enrichment.Timeout <= 0 {
		return fmt.Errorf("enrichment.timeout must be positive, got %s", cfg.Enrichment.Timeout)
	}
	if cfg.Concurrency.Workers < 1 {
		return fmt.Errorf("concurrency.workers must be at least 1, got %d", cfg.Concurrency.Workers)
	}
	switch cfg.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level must be \"debug\", \"info\", \"warn\", or \"error\", got %q", cfg.Log.Level)
	}
	switch cfg.Log.Format {
	case "text", "json":
	default:
		return fmt.Errorf("log.format must be \"text\" or \"json\", got %q", cfg.Log.Format)
	}
	return nil
}
