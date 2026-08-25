// Package credentials resolves registry credentials from multiple
// sources: static config, Docker/Podman config files, credential
// helpers.
package credentials

import "context"

// Source resolves credentials for a registry host.
type Source interface {
	Lookup(ctx context.Context, host string) (username, password string, ok bool)
}

// Chain tries each Source in order, returning the first match.
type Chain []Source

func (c Chain) Lookup(ctx context.Context, host string) (username, password string, ok bool) {
	for _, s := range c {
		if u, p, found := s.Lookup(ctx, host); found {
			return u, p, true
		}
	}
	return "", "", false
}
