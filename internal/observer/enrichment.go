package observer

import (
	"context"
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
	defaultEnrichmentTimeout = 5 * time.Second
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

	checked := 0
	for _, raw := range tags {
		if checked >= maxTags {
			break
		}
		if enrichCtx.Err() != nil {
			break
		}
		if raw == key.Tag {
			continue
		}

		tv := version.ParseTag(raw)
		if _, hasApp := tv.Application(); !hasApp {
			continue
		}

		checked++
		obs, err := reg.ResolveForPlatform(enrichCtx, key.Repository, raw, key.Platform)
		if err != nil {
			continue
		}
		if obs.PlatformManifestDigest == newDigest {
			o.observeEnrichment(true)
			return raw, true
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
