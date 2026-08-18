package observer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/k-wlosek/image-watch/internal/image"
	"github.com/k-wlosek/image-watch/internal/registry"
	iwruntime "github.com/k-wlosek/image-watch/internal/runtime"
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

// fakeRegistry is a test double for registry.Registry, safe for concurrent
// use (Check runs groups in parallel, so multiple goroutines may resolve
// through the same instance).
type fakeRegistry struct {
	mu sync.Mutex

	tags      map[string][]string          // repository -> tags
	manifests map[string]map[string]string // repository -> tag -> platform manifest digest
	// indexDigests maps repository -> tag -> index (multi-arch) digest.
	// Distinct from the platform manifest digest to model multi-arch images.
	indexDigests map[string]map[string]string
	// platforms restricts which platforms a given repository+tag has a manifest for.
	platformsFor map[string]map[string][]image.Platform
	listErr      error
	resolveErr   map[string]error // repository+"/"+tag -> error

	// resolveCalls counts ResolveForPlatform invocations per
	// "repository/tag", so tests can assert on call counts (e.g. to
	// verify the observer's per-cycle candidate-resolve memoization
	// actually avoids redundant calls).
	resolveCalls map[string]int

	// resolveOrder logs each ResolveForPlatform invocation as
	// "repository/tag" in completion order. With concurrent resolution
	// the order is only meaningful for single-group, small-window tests.
	resolveOrder []string

	// listCalls counts ListTags invocations per repository, so tests can
	// assert shared tag lists are fetched once per group.
	listCalls map[string]int

	// delays adds an artificial latency per "repository/tag" resolve, so
	// tests can exercise completion-order races deterministically.
	delays map[string]time.Duration

	// inFlight/maxInFlight track how many resolves are outstanding at
	// once, so tests can prove parallel execution actually overlaps.
	inFlight    int
	maxInFlight int
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{
		tags:         make(map[string][]string),
		manifests:    make(map[string]map[string]string),
		indexDigests: make(map[string]map[string]string),
		platformsFor: make(map[string]map[string][]image.Platform),
		resolveErr:   make(map[string]error),
		resolveCalls: make(map[string]int),
		delays:       make(map[string]time.Duration),
		listCalls:    make(map[string]int),
	}
}

func (f *fakeRegistry) setTags(repository string, tags []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tags[repository] = tags
}

func (f *fakeRegistry) setDigest(repository, tag, digest string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.manifests[repository] == nil {
		f.manifests[repository] = make(map[string]string)
	}
	f.manifests[repository][tag] = digest
}

// setIndexDigest attaches a distinct multi-arch index digest to a tag.
func (f *fakeRegistry) setIndexDigest(repository, tag, digest string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.indexDigests[repository] == nil {
		f.indexDigests[repository] = make(map[string]string)
	}
	f.indexDigests[repository][tag] = digest
}

// setPlatforms restricts which platforms a repository+tag's manifest is
// available for.
func (f *fakeRegistry) setPlatforms(repository, tag string, platforms []image.Platform) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.platformsFor[repository] == nil {
		f.platformsFor[repository] = make(map[string][]image.Platform)
	}
	f.platformsFor[repository][tag] = platforms
}

func (f *fakeRegistry) setResolveError(repository, tag string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolveErr[repository+"/"+tag] = err
}

// setResolveDelay adds an artificial latency to one repository/tag resolve.
func (f *fakeRegistry) setResolveDelay(repository, tag string, d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delays[repository+"/"+tag] = d
}

// resolveCallsCount returns how many times repository/tag was resolved.
func (f *fakeRegistry) resolveCallsCount(repository, tag string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resolveCalls[repository+"/"+tag]
}

func (f *fakeRegistry) ListTags(ctx context.Context, repository string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls[repository]++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.tags[repository], nil
}

// listCallsCount returns how many times a repository's tags were listed.
func (f *fakeRegistry) listCallsCount(repository string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listCalls[repository]
}

func (f *fakeRegistry) Resolve(ctx context.Context, repository, reference string) (registry.ManifestObservation, error) {
	return f.ResolveForPlatform(ctx, repository, reference, image.Platform{})
}

func (f *fakeRegistry) ResolveForPlatform(ctx context.Context, repository, reference string, platform image.Platform) (registry.ManifestObservation, error) {
	f.mu.Lock()
	f.inFlight++
	if f.inFlight > f.maxInFlight {
		f.maxInFlight = f.inFlight
	}
	delay := f.delays[repository+"/"+reference]
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
		}
	}

	f.mu.Lock()
	f.resolveCalls[repository+"/"+reference]++
	f.resolveOrder = append(f.resolveOrder, repository+"/"+reference)
	checkErr := f.resolveErr[repository+"/"+reference]
	digest, have := f.manifests[repository][reference]
	restricted := f.platformsFor[repository][reference]
	indexDigest := f.indexDigests[repository][reference]
	f.mu.Unlock()

	if checkErr != nil {
		return registry.ManifestObservation{}, checkErr
	}
	if !have {
		return registry.ManifestObservation{}, fmt.Errorf("fakeRegistry: no manifest for %s:%s", repository, reference)
	}

	if restricted != nil {
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

	return registry.ManifestObservation{
		PlatformManifestDigest: digest,
		IndexDigest:            indexDigest,
	}, nil
}

var _ registry.Registry = (*fakeRegistry)(nil)
