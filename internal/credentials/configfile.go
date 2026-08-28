package credentials

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
)

// ConfigFileSource resolves credentials from Docker- or Podman-style
// auth config files (auths, credHelpers, credsStore), tried in order.
type ConfigFileSource struct {
	Paths []string
}

func (c ConfigFileSource) Lookup(ctx context.Context, host string) (string, string, bool) {
	for _, path := range c.Paths {
		cfg, err := readConfigFile(path)
		if err != nil {
			slog.Debug("skipping unreadable config", "path", path, "error", err)
			continue
		}
		if u, p, ok := cfg.lookup(ctx, host); ok {
			return u, p, true
		}
	}
	return "", "", false
}

type configFile struct {
	Auths       map[string]authEntry `json:"auths"`
	CredHelpers map[string]string    `json:"credHelpers"`
	CredsStore  string               `json:"credsStore"`
}

type authEntry struct {
	Auth          string `json:"auth"`
	IdentityToken string `json:"identitytoken"`
}

func readConfigFile(path string) (configFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return configFile{}, err
	}
	var cf configFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return configFile{}, err
	}
	return cf, nil
}

// lookup order matches docker/podman: per-host helper, then the global
// helper, then a static entry.
func (cf configFile) lookup(ctx context.Context, host string) (string, string, bool) {
	if helper, ok := cf.CredHelpers[host]; ok {
		if u, p, ok := runHelper(ctx, helper, host); ok {
			return u, p, true
		}
		slog.Debug("credHelper failed, falling through", "helper", helper, "host", host)
	}
	if cf.CredsStore != "" {
		if u, p, ok := runHelper(ctx, cf.CredsStore, host); ok {
			return u, p, true
		}
		slog.Debug("credsStore failed, falling through", "store", cf.CredsStore, "host", host)
	}
	if entry, ok := cf.Auths[host]; ok {
		if entry.Auth != "" {
			return decodeBasicAuth(entry.Auth)
		}
		// identitytoken-only entries aren't supported yet.
	}
	return "", "", false
}
