package event

import (
	"testing"
	"time"

	"github.com/example/image-watch/internal/image"
)

func baseEvent() Event {
	return Event{
		Image:        image.Reference{Registry: "docker.io", Repository: "library/nginx"},
		Type:         PatchAvailable,
		CurrentTag:   "1.25.3",
		CandidateTag: "1.25.4",
		Platform:     image.Platform{OS: "linux", Architecture: "amd64"},
	}
}

func TestFingerprint_IdenticalEventsMatch(t *testing.T) {
	a := baseEvent()
	b := baseEvent()
	if Fingerprint(a) != Fingerprint(b) {
		t.Errorf("expected identical events to produce the same fingerprint")
	}
}

func TestFingerprint_DifferentCandidateTagDiffers(t *testing.T) {
	a := baseEvent()
	b := baseEvent()
	b.CandidateTag = "1.25.5"
	if Fingerprint(a) == Fingerprint(b) {
		t.Errorf("expected different candidate tags to produce different fingerprints")
	}
}

func TestFingerprint_DifferentDigestDiffers(t *testing.T) {
	a := baseEvent()
	a.Type = TagMutated
	a.CandidateDigest = "sha256:AAAA"
	b := a
	b.CandidateDigest = "sha256:BBBB"
	if Fingerprint(a) == Fingerprint(b) {
		t.Errorf("expected different candidate digests to produce different fingerprints")
	}
}

func TestFingerprint_DifferentCurrentDigestDiffers(t *testing.T) {
	a := baseEvent()
	a.Type = TagMutated
	a.CandidateDigest = "sha256:YYYY"
	b := a
	a.CurrentDigest = "sha256:XXXX"
	b.CurrentDigest = "sha256:ZZZZ"
	if Fingerprint(a) == Fingerprint(b) {
		t.Errorf("expected different current digests to produce different fingerprints (drift identity)")
	}
}

func TestFingerprint_DifferentPlatformDiffers(t *testing.T) {
	a := baseEvent()
	b := baseEvent()
	b.Platform = image.Platform{OS: "linux", Architecture: "arm64"}
	if Fingerprint(a) == Fingerprint(b) {
		t.Errorf("expected different platforms to produce different fingerprints (multi-platform isolation)")
	}
}

func TestFingerprint_DifferentEventTypeDiffers(t *testing.T) {
	a := baseEvent()
	b := baseEvent()
	b.Type = MinorAvailable
	if Fingerprint(a) == Fingerprint(b) {
		t.Errorf("expected different event types to produce different fingerprints")
	}
}

func TestFingerprint_TimestampDoesNotAffectFingerprint(t *testing.T) {
	a := baseEvent()
	b := baseEvent()
	a.Timestamp = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b.Timestamp = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if Fingerprint(a) != Fingerprint(b) {
		t.Errorf("expected timestamp to not affect the fingerprint")
	}
}
