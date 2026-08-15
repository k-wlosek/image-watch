package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/image-watch/internal/image"
)

func TestSQLiteStore_PutAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	defer s.Close()

	key := Key{Registry: "docker.io", Repository: "library/nginx", Tag: "1.25", Platform: image.Platform{OS: "linux", Architecture: "amd64"}}
	obs := Observation{
		Key:                    key,
		PlatformManifestDigest: "sha256:aaaa",
		IndexDigest:            "sha256:index0000",
		LastSuccess:            time.Now().UTC().Truncate(time.Second),
		Status:                 StatusFresh,
	}

	if err := s.PutObservation(context.Background(), obs); err != nil {
		t.Fatalf("PutObservation error: %v", err)
	}

	got, ok, err := s.GetObservation(context.Background(), key)
	if err != nil {
		t.Fatalf("GetObservation error: %v", err)
	}
	if !ok {
		t.Fatalf("expected observation to be found")
	}
	if got.PlatformManifestDigest != "sha256:aaaa" {
		t.Errorf("PlatformManifestDigest = %q, want sha256:aaaa", got.PlatformManifestDigest)
	}
	if got.IndexDigest != "sha256:index0000" {
		t.Errorf("IndexDigest = %q, want sha256:index0000", got.IndexDigest)
	}
	if !got.LastSuccess.Equal(obs.LastSuccess) {
		t.Errorf("LastSuccess = %v, want %v", got.LastSuccess, obs.LastSuccess)
	}
	if got.Status != StatusFresh {
		t.Errorf("Status = %q, want fresh", got.Status)
	}
}

func TestSQLiteStore_Update(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	defer s.Close()

	key := Key{Registry: "ghcr.io", Repository: "acme/foo", Tag: "latest", Platform: image.Platform{OS: "linux", Architecture: "amd64"}}

	if err := s.PutObservation(context.Background(), Observation{Key: key, PlatformManifestDigest: "sha256:AAAA", Status: StatusFresh}); err != nil {
		t.Fatalf("first PutObservation error: %v", err)
	}
	if err := s.PutObservation(context.Background(), Observation{Key: key, PlatformManifestDigest: "sha256:BBBB", Status: StatusFresh}); err != nil {
		t.Fatalf("second PutObservation (update) error: %v", err)
	}

	got, ok, err := s.GetObservation(context.Background(), key)
	if err != nil {
		t.Fatalf("GetObservation error: %v", err)
	}
	if !ok {
		t.Fatalf("expected observation to be found")
	}
	if got.PlatformManifestDigest != "sha256:BBBB" {
		t.Errorf("expected the second write to overwrite the first (upsert), got %q", got.PlatformManifestDigest)
	}
}

func TestSQLiteStore_DurableAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	key := Key{Registry: "ghcr.io", Repository: "acme/foo", Tag: "1.2.3", Platform: image.Platform{OS: "linux", Architecture: "arm64"}}
	obs := Observation{Key: key, PlatformManifestDigest: "sha256:cccc", Status: StatusFresh}

	// "Process 1": write and close.
	s1, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore (process 1) error: %v", err)
	}
	if err := s1.PutObservation(context.Background(), obs); err != nil {
		t.Fatalf("PutObservation error: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// "Process 2": fresh handle to the same file.
	s2, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore (process 2) error: %v", err)
	}
	defer s2.Close()

	got, ok, err := s2.GetObservation(context.Background(), key)
	if err != nil {
		t.Fatalf("GetObservation error: %v", err)
	}
	if !ok {
		t.Fatalf("expected observation to survive a daemon restart, but it was not found")
	}
	if got.PlatformManifestDigest != "sha256:cccc" {
		t.Errorf("got %q, want sha256:cccc", got.PlatformManifestDigest)
	}
}

func TestSQLiteStore_NotificationDedup(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	defer s.Close()

	fp := "abc123fingerprint"

	notified, err := s.HasNotified(context.Background(), fp)
	if err != nil {
		t.Fatalf("HasNotified error: %v", err)
	}
	if notified {
		t.Fatalf("expected fingerprint to be unnotified before MarkNotified")
	}

	if err := s.MarkNotified(context.Background(), fp); err != nil {
		t.Fatalf("MarkNotified error: %v", err)
	}

	notified, err = s.HasNotified(context.Background(), fp)
	if err != nil {
		t.Fatalf("HasNotified error: %v", err)
	}
	if !notified {
		t.Fatalf("expected fingerprint to be notified after MarkNotified")
	}
}

func TestSQLiteStore_NotificationDedupSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	fp := "restart-fingerprint"

	s1, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore (process 1) error: %v", err)
	}
	if err := s1.MarkNotified(context.Background(), fp); err != nil {
		t.Fatalf("MarkNotified error: %v", err)
	}
	s1.Close()

	s2, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore (process 2) error: %v", err)
	}
	defer s2.Close()

	notified, err := s2.HasNotified(context.Background(), fp)
	if err != nil {
		t.Fatalf("HasNotified error: %v", err)
	}
	if !notified {
		t.Fatalf("expected notification dedup state to survive a restart")
	}
}

func TestSQLiteStore_CreatesParentDirectory(t *testing.T) {
	dir := t.TempDir()
	// NewSQLiteStore should create missing parent directories.
	path := filepath.Join(dir, "nested", "subdir", "state.db")
	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore should create missing parent directories: %v", err)
	}
	defer s.Close()

	if err := s.PutObservation(context.Background(), Observation{
		Key: Key{Registry: "x", Repository: "y", Tag: "z"},
	}); err != nil {
		t.Fatalf("PutObservation on a freshly created store should succeed: %v", err)
	}
}

func TestSQLiteStore_MissingKeyReturnsNotFoundNotError(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	defer s.Close()

	_, ok, err := s.GetObservation(context.Background(), Key{Registry: "docker.io", Repository: "nope", Tag: "1.0"})
	if err != nil {
		t.Fatalf("expected no error for a missing key, got %v", err)
	}
	if ok {
		t.Errorf("expected ok=false for a missing key")
	}
}

func TestSQLiteStore_DistinctPlatformsAreDistinctRows(t *testing.T) {
	// The same tag on different platforms must be tracked independently.
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	defer s.Close()

	amd64Key := Key{Registry: "docker.io", Repository: "library/foo", Tag: "latest", Platform: image.Platform{OS: "linux", Architecture: "amd64"}}
	arm64Key := Key{Registry: "docker.io", Repository: "library/foo", Tag: "latest", Platform: image.Platform{OS: "linux", Architecture: "arm64"}}

	s.PutObservation(context.Background(), Observation{Key: amd64Key, PlatformManifestDigest: "sha256:amd64digest"})
	s.PutObservation(context.Background(), Observation{Key: arm64Key, PlatformManifestDigest: "sha256:arm64digest"})

	gotAmd64, _, _ := s.GetObservation(context.Background(), amd64Key)
	gotArm64, _, _ := s.GetObservation(context.Background(), arm64Key)

	if gotAmd64.PlatformManifestDigest != "sha256:amd64digest" {
		t.Errorf("amd64 digest = %q, want sha256:amd64digest", gotAmd64.PlatformManifestDigest)
	}
	if gotArm64.PlatformManifestDigest != "sha256:arm64digest" {
		t.Errorf("arm64 digest = %q, want sha256:arm64digest", gotArm64.PlatformManifestDigest)
	}
}
