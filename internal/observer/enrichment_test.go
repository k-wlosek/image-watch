package observer

import (
	"context"
	"testing"

	"github.com/example/image-watch/internal/image"
)

func enrichmentKey() groupKey {
	return groupKey{
		Registry:   "ghcr.io",
		Repository: "acme/foo",
		Tag:        "latest",
		Platform:   image.Platform{OS: "linux", Architecture: "amd64"},
	}
}

func TestAttemptEnrichment_NewestMatchFirst(t *testing.T) {
	reg := newFakeRegistry()
	// Deliberately unsorted; the newest release matches the served digest.
	reg.setTags("acme/foo", []string{"1.2.3", "2.0.0", "1.4.0", "1.10.0", "1.2.4"})
	reg.setDigest("acme/foo", "1.2.3", "sha256:a")
	reg.setDigest("acme/foo", "1.4.0", "sha256:b")
	reg.setDigest("acme/foo", "1.10.0", "sha256:c")
	reg.setDigest("acme/foo", "1.2.4", "sha256:d")
	reg.setDigest("acme/foo", "2.0.0", "sha256:match")

	o := newTestObserver(&fakeRuntime{}, reg)
	// Sequential scan (window 1): this test asserts the strict
	// newest-first short-circuit, so only the match should resolve.
	o.ConcurrencyWorkers = 1
	tag, ok := o.attemptEnrichment(context.Background(), reg, enrichmentKey(), "sha256:match", newGroupCache())
	if !ok || tag != "2.0.0" {
		t.Fatalf("enrichment = %q, %v; want 2.0.0, true", tag, ok)
	}
	if len(reg.resolveOrder) != 1 || reg.resolveOrder[0] != "acme/foo/2.0.0" {
		t.Errorf("expected the newest tag to be resolved first and only once, got %v", reg.resolveOrder)
	}
}

func TestAttemptEnrichment_NoMatch(t *testing.T) {
	reg := newFakeRegistry()
	reg.setTags("acme/foo", []string{"1.2.3", "2.0.0"})
	reg.setDigest("acme/foo", "1.2.3", "sha256:a")
	reg.setDigest("acme/foo", "2.0.0", "sha256:b")

	o := newTestObserver(&fakeRuntime{}, reg)
	tag, ok := o.attemptEnrichment(context.Background(), reg, enrichmentKey(), "sha256:nope", newGroupCache())
	if ok {
		t.Fatalf("expected no match, got %q", tag)
	}
	if len(reg.resolveOrder) != 2 {
		t.Errorf("expected both candidates resolved, got %v", reg.resolveOrder)
	}
}

func TestAttemptEnrichment_RespectsMaxTags(t *testing.T) {
	reg := newFakeRegistry()
	reg.setTags("acme/foo", []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0"})
	for _, tag := range []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0"} {
		reg.setDigest("acme/foo", tag, "sha256:"+tag)
	}

	o := newTestObserver(&fakeRuntime{}, reg)
	o.EnrichmentMaxTags = 2
	// Window 1 keeps resolution order deterministic for the ordering
	// assertion below.
	o.ConcurrencyWorkers = 1
	tag, ok := o.attemptEnrichment(context.Background(), reg, enrichmentKey(), "sha256:1.0.0", newGroupCache())
	if ok {
		t.Fatalf("expected the cap to stop before reaching 1.0.0, got %q", tag)
	}
	if len(reg.resolveOrder) != 2 {
		t.Fatalf("expected exactly maxTags(2) resolves, got %v", reg.resolveOrder)
	}
	if reg.resolveOrder[0] != "acme/foo/1.4.0" || reg.resolveOrder[1] != "acme/foo/1.3.0" {
		t.Errorf("expected newest-first resolution order, got %v", reg.resolveOrder)
	}
}

func TestAttemptEnrichment_SkipsCurrentAndNonVersion(t *testing.T) {
	reg := newFakeRegistry()
	reg.setTags("acme/foo", []string{"latest", "dev", "1.2.3", "1.2.4"})
	reg.setDigest("acme/foo", "1.2.3", "sha256:a")
	reg.setDigest("acme/foo", "1.2.4", "sha256:match")

	o := newTestObserver(&fakeRuntime{}, reg)
	// Window 1 keeps the sequential short-circuit semantics this test
	// asserts (only the newest matching tag resolves).
	o.ConcurrencyWorkers = 1
	tag, ok := o.attemptEnrichment(context.Background(), reg, enrichmentKey(), "sha256:match", newGroupCache())
	if !ok || tag != "1.2.4" {
		t.Fatalf("enrichment = %q, %v; want 1.2.4, true", tag, ok)
	}
	// "latest" (the current tag) and "dev" (opaque) must not be resolved.
	if len(reg.resolveOrder) != 1 || reg.resolveOrder[0] != "acme/foo/1.2.4" {
		t.Errorf("expected only the matching version tag resolved, got %v", reg.resolveOrder)
	}
}
