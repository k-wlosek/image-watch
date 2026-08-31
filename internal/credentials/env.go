package credentials

import (
	"context"

	"github.com/k-wlosek/image-watch/internal/secret"
)

// RegistryAuth names the files holding credentials for one host.
type RegistryAuth struct {
	UsernameFile string
	PasswordFile string
}

// FileSource resolves credentials from secret files per host.
type FileSource struct {
	Registries map[string]RegistryAuth
}

func (e FileSource) Lookup(_ context.Context, host string) (string, string, bool) {
	auth, ok := e.Registries[host]
	if !ok {
		return "", "", false
	}
	u, uErr := secret.ReadFile(auth.UsernameFile)
	p, pErr := secret.ReadFile(auth.PasswordFile)
	if uErr != nil && pErr != nil {
		return "", "", false
	}
	if u == "" && p == "" {
		return "", "", false
	}
	return u, p, true
}
