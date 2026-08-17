package observer

import (
	"context"
	"fmt"

	"github.com/example/image-watch/internal/image"
	"github.com/example/image-watch/internal/registry"
	iwruntime "github.com/example/image-watch/internal/runtime"
)

// fakeRuntime is a test double for runtime.Runtime.
type fakeRuntime struct {
	containers []iwruntime.ContainerObservation
	err        error
}

func (f *fakeRuntime) Name() string { return "fake" }

func (f *fakeRuntime) ListContainers(ctx context.Context) ([]iwruntime.ContainerObservation, error) {
	return f.containers, f.err
}

// fakeRegistry is a test double for registry.Registry.
type fakeRegistry struct {
	tags      map[string][]string          // repository -> tags
	manifests map[string]map[string]string // repository -> tag -> platform manifest digest
	// platforms restricts which platforms a given repository+tag has a manifest for.
	platformsFor map[string]map[string][]image.Platform
	listErr      error
	resolveErr   map[string]error // repository+"/"+tag -> error

	// resolveCalls counts ResolveForPlatform invocations per
	// "repository/tag", so tests can assert on call counts (e.g. to
	// verify the observer's per-cycle candidate-resolve memoization
	// actually avoids redundant calls).
	resolveCalls map[string]int
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{
		tags:         make(map[string][]string),
		manifests:    make(map[string]map[string]string),
		platformsFor: make(map[string]map[string][]image.Platform),
		resolveErr:   make(map[string]error),
		resolveCalls: make(map[string]int),
	}
}

func (f *fakeRegistry) setTags(repository string, tags []string) {
	f.tags[repository] = tags
}

func (f *fakeRegistry) setDigest(repository, tag, digest string) {
	if f.manifests[repository] == nil {
		f.manifests[repository] = make(map[string]string)
	}
	f.manifests[repository][tag] = digest
}

// setPlatforms restricts which platforms a repository+tag's manifest is
// available for.
func (f *fakeRegistry) setPlatforms(repository, tag string, platforms []image.Platform) {
	if f.platformsFor[repository] == nil {
		f.platformsFor[repository] = make(map[string][]image.Platform)
	}
	f.platformsFor[repository][tag] = platforms
}

func (f *fakeRegistry) setResolveError(repository, tag string, err error) {
	f.resolveErr[repository+"/"+tag] = err
}

func (f *fakeRegistry) ListTags(ctx context.Context, repository string) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.tags[repository], nil
}

func (f *fakeRegistry) Resolve(ctx context.Context, repository, reference string) (registry.ManifestObservation, error) {
	return f.ResolveForPlatform(ctx, repository, reference, image.Platform{})
}

func (f *fakeRegistry) ResolveForPlatform(ctx context.Context, repository, reference string, platform image.Platform) (registry.ManifestObservation, error) {
	f.resolveCalls[repository+"/"+reference]++

	if err, ok := f.resolveErr[repository+"/"+reference]; ok {
		return registry.ManifestObservation{}, err
	}
	digest, ok := f.manifests[repository][reference]
	if !ok {
		return registry.ManifestObservation{}, fmt.Errorf("fakeRegistry: no manifest for %s:%s", repository, reference)
	}

	if restricted, ok := f.platformsFor[repository][reference]; ok {
		matched := false
		for _, p := range restricted {
			if p.Equal(platform) {
				matched = true
				break
			}
		}
		if !matched {
			return registry.ManifestObservation{AvailablePlatforms: restricted}, nil
		}
	}

	return registry.ManifestObservation{PlatformManifestDigest: digest}, nil
}

var _ registry.Registry = (*fakeRegistry)(nil)
