package state

import (
	"context"
	"testing"

	"github.com/k-wlosek/image-watch/internal/image"
)

func testKey() Key {
	return Key{
		Registry:   "ghcr.io",
		Repository: "acme/foo",
		Tag:        "1.2.3",
		Platform:   image.Platform{OS: "linux", Architecture: "amd64"},
	}
}

func TestMemoryStore_ObservationRoundTrip(t *testing.T) {
	s := NewMemoryStore()
	key := testKey()

	if _, found, err := s.GetObservation(context.Background(), key); err != nil || found {
		t.Errorf("expected no observation yet, found=%v err=%v", found, err)
	}

	obs := Observation{Key: key, PlatformManifestDigest: "sha256:AAAA", IndexDigest: "sha256:BBBB", Status: StatusFresh}
	if err := s.PutObservation(context.Background(), obs); err != nil {
		t.Fatalf("PutObservation error: %v", err)
	}

	got, found, err := s.GetObservation(context.Background(), key)
	if err != nil || !found {
		t.Fatalf("expected observation after put, found=%v err=%v", found, err)
	}
	if got.PlatformManifestDigest != obs.PlatformManifestDigest || got.IndexDigest != obs.IndexDigest {
		t.Errorf("round-trip mismatch: %+v vs %+v", got, obs)
	}
}

func TestMemoryStore_PutOverwrites(t *testing.T) {
	s := NewMemoryStore()
	key := testKey()
	s.PutObservation(context.Background(), Observation{Key: key, PlatformManifestDigest: "sha256:AAA"})
	s.PutObservation(context.Background(), Observation{Key: key, PlatformManifestDigest: "sha256:BBB"})

	got, _, _ := s.GetObservation(context.Background(), key)
	if got.PlatformManifestDigest != "sha256:BBB" {
		t.Errorf("expected the newer observation to win, got %q", got.PlatformManifestDigest)
	}
}

func TestMemoryStore_DistinctKeys(t *testing.T) {
	s := NewMemoryStore()
	k1 := testKey()
	k2 := k1
	k2.Tag = "2.0.0"

	s.PutObservation(context.Background(), Observation{Key: k1, PlatformManifestDigest: "sha256:A"})
	s.PutObservation(context.Background(), Observation{Key: k2, PlatformManifestDigest: "sha256:B"})

	got1, _, _ := s.GetObservation(context.Background(), k1)
	got2, _, _ := s.GetObservation(context.Background(), k2)
	if got1.PlatformManifestDigest != "sha256:A" || got2.PlatformManifestDigest != "sha256:B" {
		t.Errorf("observations leaked across keys: %q vs %q", got1.PlatformManifestDigest, got2.PlatformManifestDigest)
	}
}

func TestMemoryStore_NotificationRoundTrip(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	if notified, err := s.HasNotified(ctx, "fp"); err != nil || notified {
		t.Errorf("expected no notification yet, notified=%v err=%v", notified, err)
	}

	if err := s.MarkNotified(ctx, "fp"); err != nil {
		t.Fatalf("MarkNotified error: %v", err)
	}
	if notified, err := s.HasNotified(ctx, "fp"); err != nil || !notified {
		t.Errorf("expected notification after mark, notified=%v err=%v", notified, err)
	}

	if notified, _ := s.HasNotified(ctx, "other"); notified {
		t.Errorf("unrelated fingerprint must not be notified")
	}
}
