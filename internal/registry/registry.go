// Package registry defines the registry abstraction used by image-watch.
package registry

import (
	"context"

	"github.com/example/image-watch/internal/image"
)

// Registry queries an OCI or Docker-compatible registry.
type Registry interface {
	// ListTags returns the tags currently published for a repository.
	// Implementations must bound pagination.
	ListTags(ctx context.Context, repository string) ([]string, error)

	// Resolve fetches manifest/index metadata for a repository+reference
	// (tag or digest). It must not pull image content.
	Resolve(ctx context.Context, repository string, reference string) (ManifestObservation, error)

	// ResolveForPlatform resolves a reference for a specific platform.
	ResolveForPlatform(ctx context.Context, repository string, reference string, platform image.Platform) (ManifestObservation, error)
}

// ManifestObservation is the result of resolving a registry reference.
type ManifestObservation struct {
	// IndexDigest is the digest of the top-level OCI image index, if the
	// reference resolved to a multi-platform index. Empty if the
	// reference resolved directly to a single-platform manifest.
	IndexDigest string

	// PlatformManifestDigest is the content digest used for comparisons.
	PlatformManifestDigest string

	// MediaType is the manifest or index media type as reported by the
	// registry.
	MediaType string

	// Platform is the platform this observation applies to, when known.
	Platform *image.Platform

	// AvailablePlatforms lists every platform present in the index.
	AvailablePlatforms []image.Platform
}

// HasPlatform reports whether the given platform is available.
func (m ManifestObservation) HasPlatform(p image.Platform) bool {
	if m.Platform != nil && m.Platform.Equal(p) {
		return true
	}
	for _, avail := range m.AvailablePlatforms {
		if avail.Equal(p) {
			return true
		}
	}
	return false
}
