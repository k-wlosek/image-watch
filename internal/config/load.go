package config

import (
	"fmt"
	"os"
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

	cfg, err = applyEnvOverrides(cfg)
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	if err := validate(cfg); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	return cfg, nil
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
		Mode    string `yaml:"mode"`
		Targets []struct {
			Type        string `yaml:"type"`
			ServerURL   string `yaml:"server_url"`
			Topic       string `yaml:"topic"`
			UsernameEnv string `yaml:"username_env"`
			PasswordEnv string `yaml:"password_env"`
			Priority    string `yaml:"priority"`
			Title       string `yaml:"title"`
			URL         string `yaml:"url"`
		} `yaml:"targets"`
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

	Registries map[string]struct {
		UsernameEnv string `yaml:"username_env"`
		PasswordEnv string `yaml:"password_env"`
		Scheme      string `yaml:"scheme"`
		CAFile      string `yaml:"ca_file"`
	} `yaml:"registries"`
}

func mergeRaw(cfg Config, raw rawConfig) (Config, error) {
	if raw.Runtime != nil {
		if raw.Runtime.Type != "" {
			cfg.Runtime.Type = raw.Runtime.Type
		}
		if raw.Runtime.Endpoint != "" {
			cfg.Runtime.Endpoint = raw.Runtime.Endpoint
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
				cfg.Notifications.Targets = append(cfg.Notifications.Targets, NotificationTarget{
					Type:        t.Type,
					ServerURL:   t.ServerURL,
					Topic:       t.Topic,
					UsernameEnv: t.UsernameEnv,
					PasswordEnv: t.PasswordEnv,
					Priority:    t.Priority,
					Title:       t.Title,
					URL:         t.URL,
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

	return cfg, nil
}

func setBool(dst *bool, src *bool) {
	if src != nil {
		*dst = *src
	}
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
	}
	if v := os.Getenv("IMAGE_WATCH_RUNTIME_ENDPOINT"); v != "" {
		cfg.Runtime.Endpoint = v
	}
	if v := os.Getenv("IMAGE_WATCH_METRICS_LISTEN"); v != "" {
		cfg.Metrics.Listen = v
	}
	return cfg, nil
}

// validate performs minimal sanity checks.
func validate(cfg Config) error {
	if cfg.Runtime.Type != "docker" {
		return fmt.Errorf("unsupported runtime.type %q (v1 supports only \"docker\")", cfg.Runtime.Type)
	}
	if cfg.CheckInterval <= 0 {
		return fmt.Errorf("check_interval must be positive, got %s", cfg.CheckInterval)
	}
	if cfg.Notifications.Mode != "batch" && cfg.Notifications.Mode != "individual" {
		return fmt.Errorf("notifications.mode must be \"batch\" or \"individual\", got %q", cfg.Notifications.Mode)
	}
	for _, t := range cfg.Notifications.Targets {
		switch t.Type {
		case "stdout", "ntfy", "webhook":
		default:
			return fmt.Errorf("unrecognized notification target type %q", t.Type)
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
	return nil
}
