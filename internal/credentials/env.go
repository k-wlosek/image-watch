package credentials

import (
	"context"
	"os"
)

// RegistryAuth names the env vars holding credentials for one host.
type RegistryAuth struct {
	UsernameEnv string
	PasswordEnv string
}

// EnvSource resolves credentials from environment variables named per
// host.
type EnvSource struct {
	Registries map[string]RegistryAuth
}

func (e EnvSource) Lookup(_ context.Context, host string) (string, string, bool) {
	auth, ok := e.Registries[host]
	if !ok {
		return "", "", false
	}
	u := os.Getenv(auth.UsernameEnv)
	p := os.Getenv(auth.PasswordEnv)
	if u == "" && p == "" {
		return "", "", false
	}
	return u, p, true
}
