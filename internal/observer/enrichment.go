package observer

import (
	"context"
	"sort"
	"time"

	"github.com/example/image-watch/internal/registry"
	"github.com/example/image-watch/internal/version"
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

// attemptEnrichment tries to identify a tag for a changed digest.
func (o *Observer) attemptEnrichment(ctx context.Context, reg registry.Registry, key groupKey, newDigest string) (tag string, ok bool) {
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

	tags, err := reg.ListTags(enrichCtx, key.Repository)
	if err != nil {
		o.observeEnrichment(false)
		return "", false
	}

	// Try the newest release first
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

	checked := 0
	for _, c := range candidates {
		if checked >= maxTags {
			break
		}
		if enrichCtx.Err() != nil {
			break
		}

		checked++
		obs, err := reg.ResolveForPlatform(enrichCtx, key.Repository, c.raw, key.Platform)
		if err != nil {
			continue
		}
		if obs.PlatformManifestDigest == newDigest {
			o.observeEnrichment(true)
			return c.raw, true
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
