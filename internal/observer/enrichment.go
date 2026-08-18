package observer

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/k-wlosek/image-watch/internal/registry"
	"github.com/k-wlosek/image-watch/internal/version"
)

// EnrichmentObserver records enrichment telemetry.
type EnrichmentObserver interface {
	ObserveEnrichment(success bool)
}

// Default enrichment limits.
const (
	defaultEnrichmentMaxTags = 100
	defaultEnrichmentTimeout = 30 * time.Second
)

// groupCache memoizes inputs shared across one group's enrichment attempts.
type groupCache struct {
	mu   sync.Mutex
	tags map[string][]string // repository -> tag list
	errs map[string]error    // repository -> list error
}

func newGroupCache() *groupCache {
	return &groupCache{
		tags: make(map[string][]string),
		errs: make(map[string]error),
	}
}

func (c *groupCache) list(ctx context.Context, reg registry.Registry, repository string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tags, ok := c.tags[repository]; ok {
		return tags, c.errs[repository]
	}
	tags, err := reg.ListTags(ctx, repository)
	c.tags[repository] = tags
	c.errs[repository] = err
	return tags, err
}

// attemptEnrichment tries to identify a tag for a changed digest.
func (o *Observer) attemptEnrichment(ctx context.Context, reg registry.Registry, key groupKey, newDigest string, cache *groupCache) (tag string, ok bool) {
	maxTags := o.EnrichmentMaxTags
	if maxTags <= 0 {
		maxTags = defaultEnrichmentMaxTags
	}
	timeout := o.EnrichmentTimeout
	if timeout <= 0 {
		timeout = defaultEnrichmentTimeout
	}

	enrichCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tags, err := cache.list(enrichCtx, reg, key.Repository)
	if err != nil {
		o.observeEnrichment(false)
		return "", false
	}

	// Sort the versionable tags newest-first.
	type candidate struct {
		raw string
		ver version.SemVer
	}
	var candidates []candidate
	for _, raw := range tags {
		if raw == key.Tag {
			continue
		}
		tv := version.ParseTag(raw)
		if v, hasApp := tv.Application(); hasApp {
			candidates = append(candidates, candidate{raw: raw, ver: v.Version})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ver.Compare(candidates[j].ver) > 0
	})
	if len(candidates) > maxTags {
		candidates = candidates[:maxTags]
	}
	if len(candidates) == 0 {
		o.observeEnrichment(false)
		return "", false
	}

	// Resolve candidate manifests newest-first, window at a time. Within
	// one batch the fetches run in parallel (completion order doesn't
	// matter); batches are evaluated in order after the whole batch has
	// resolved, so the first match found is the newest match
	window := min(o.workerLimit(), len(candidates))

	obs := make([]registry.ManifestObservation, len(candidates))
	errs := make([]error, len(candidates))
	for start := 0; start < len(candidates); start += window {
		end := min(start+window, len(candidates))
		if enrichCtx.Err() != nil {
			break
		}

		var wg sync.WaitGroup
		for i := start; i < end; i++ {
			wg.Go(func() {
				obs[i], errs[i] = reg.ResolveForPlatform(enrichCtx, key.Repository, candidates[i].raw, key.Platform)
			})
		}
		wg.Wait()
		if enrichCtx.Err() != nil {
			break
		}

		for i := start; i < end; i++ {
			if errs[i] != nil {
				continue
			}
			if obs[i].PlatformManifestDigest == newDigest {
				o.observeEnrichment(true)
				return candidates[i].raw, true
			}
		}
	}

	o.observeEnrichment(false)
	return "", false
}

func (o *Observer) observeEnrichment(success bool) {
	if o.Metrics != nil {
		o.Metrics.ObserveEnrichment(success)
	}
}
