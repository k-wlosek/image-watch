package observer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/k-wlosek/image-watch/internal/image"
	"github.com/k-wlosek/image-watch/internal/policy"
	"github.com/k-wlosek/image-watch/internal/registry"
	iwruntime "github.com/k-wlosek/image-watch/internal/runtime"
	"github.com/k-wlosek/image-watch/internal/state"
)

// fakeStore is a state.Store test double with injectable failures.
type fakeStore struct {
	failGet bool
	failPut bool
	obs     map[state.Key]state.Observation
}

func newFakeStore() *fakeStore { return &fakeStore{obs: make(map[state.Key]state.Observation)} }

func (f *fakeStore) GetObservation(_ context.Context, key state.Key) (state.Observation, bool, error) {
	if f.failGet {
		return state.Observation{}, false, errors.New("store: get failed")
	}
	obs, ok := f.obs[key]
	return obs, ok, nil
}

func (f *fakeStore) PutObservation(_ context.Context, obs state.Observation) error {
	if f.failPut {
		return errors.New("store: put failed")
	}
	f.obs[obs.Key] = obs
	return nil
}

func (f *fakeStore) HasNotified(context.Context, string) (bool, error) { return false, nil }

func (f *fakeStore) MarkNotified(context.Context, string) error { return nil }

var _ state.Store = (*fakeStore)(nil)

func TestGroupKeyLess_PlatformTieBreaks(t *testing.T) {
	base := groupKey{Registry: "ghcr.io", Repository: "acme/foo", Tag: "1.2.3", Platform: image.Platform{OS: "linux", Architecture: "amd64"}}

	byOS := base
	byOS.Platform.OS = "windows"
	byArch := base
	byArch.Platform.Architecture = "arm64"
	byVariant := base
	byVariant.Platform.Architecture = "arm"
	byVariant.Platform.Variant = "v7"

	if !base.less(byVariant) || !byVariant.less(byArch) || !byArch.less(byOS) {
		t.Errorf("expected deterministic platform tie-breaks to order base < byVariant < byArch < byOS")
	}
	if base.less(base) {
		t.Errorf("a key must not be less than itself")
	}
	if byOS.less(base) {
		t.Errorf("ordering must be strict")
	}
}

func TestCheck_RuntimeError(t *testing.T) {
	rt := &fakeRuntime{err: errors.New("daemon down")}
	reg := newFakeRegistry()
	o := newTestObserver(rt, reg)

	_, err := o.Check(context.Background())
	if err == nil {
		t.Fatal("expected the runtime error to propagate")
	}
}

func TestCheck_NoGroups(t *testing.T) {
	o := newTestObserver(&fakeRuntime{}, newFakeRegistry())
	results, err := o.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for an empty container set, got %v", results)
	}
}

func TestObserverNow_Default(t *testing.T) {
	now := (&Observer{}).now()
	if now.After(time.Now().Add(time.Minute)) || now.Before(time.Now().Add(-time.Minute)) {
		t.Errorf("default now() unexpectedly off: %v", now)
	}
}

func TestCheckGroup_UnresolvedRegistry(t *testing.T) {
	plat := image.Platform{OS: "linux", Architecture: "amd64"}
	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("web1", "ghcr.io/acme/foo:1.2.3", "sha256:local", plat),
	}}
	o := &Observer{
		Runtime:       rt,
		Registries:    func(string) registry.Registry { return nil },
		Store:         state.NewMemoryStore(),
		DefaultPolicy: policy.Default(),
		Now:           func() time.Time { return time.Unix(0, 0) },
	}

	results, err := o.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	r := results[0]
	ure, ok := r.Err.(*UnresolvedRegistryError)
	if !ok {
		t.Fatalf("Err = %T, want *UnresolvedRegistryError", r.Err)
	}
	if ure.Registry != "ghcr.io" {
		t.Errorf("Registry = %q", ure.Registry)
	}
	if want := "observer: no registry client configured for host ghcr.io"; ure.Error() != want {
		t.Errorf("Error() = %q, want %q", ure.Error(), want)
	}
}

func TestCheckGroup_StoreGetErrorMarksPartial(t *testing.T) {
	plat := image.Platform{OS: "linux", Architecture: "amd64"}
	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("web1", "ghcr.io/acme/foo:1.2.3", "sha256:local", plat),
	}}
	reg := newFakeRegistry()
	reg.setDigest("acme/foo", "1.2.3", "sha256:served")

	store := newFakeStore()
	store.failGet = true
	o := &Observer{
		Runtime:       rt,
		Registries:    func(string) registry.Registry { return reg },
		Store:         store,
		DefaultPolicy: policy.Default(),
		Now:           func() time.Time { return time.Unix(0, 0) },
	}

	results, err := o.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !results[0].Partial {
		t.Error("expected Partial=true when the store read fails")
	}
}

func TestCheckGroup_StorePutErrorMarksPartial(t *testing.T) {
	plat := image.Platform{OS: "linux", Architecture: "amd64"}
	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("web1", "ghcr.io/acme/foo:1.2.3", "sha256:local", plat),
	}}
	reg := newFakeRegistry()
	reg.setDigest("acme/foo", "1.2.3", "sha256:served")

	store := newFakeStore()
	store.failPut = true
	o := &Observer{
		Runtime:       rt,
		Registries:    func(string) registry.Registry { return reg },
		Store:         store,
		DefaultPolicy: policy.Default(),
		Now:           func() time.Time { return time.Unix(0, 0) },
	}

	results, err := o.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !results[0].Partial {
		t.Error("expected Partial=true when the store write fails")
	}
}

func TestMarkStale_RecordsStaleStatusWhenPreviousExists(t *testing.T) {
	plat := image.Platform{OS: "linux", Architecture: "amd64"}
	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("web1", "ghcr.io/acme/foo:1.2.3", "sha256:local", plat),
	}}
	reg := newFakeRegistry()
	reg.setDigest("acme/foo", "1.2.3", "sha256:served")
	reg.setResolveError("acme/foo", "1.2.3", errors.New("registry outage"))

	store := state.NewMemoryStore()
	key := state.Key{
		Registry:   "ghcr.io",
		Repository: "acme/foo",
		Tag:        "1.2.3",
		Platform:   plat,
	}
	if err := store.PutObservation(context.Background(), state.Observation{
		Key:                    key,
		PlatformManifestDigest: "sha256:old",
		Status:                 state.StatusFresh,
	}); err != nil {
		t.Fatal(err)
	}

	o := &Observer{
		Runtime:       rt,
		Registries:    func(string) registry.Registry { return reg },
		Store:         store,
		DefaultPolicy: policy.Default(),
		Now:           func() time.Time { return time.Unix(0, 0) },
	}

	results, err := o.Check(context.Background())
	if err != nil || !results[0].Stale {
		t.Fatalf("expected a stale result, got %+v (err=%v)", results, err)
	}

	got, found, err := store.GetObservation(context.Background(), key)
	if err != nil || !found {
		t.Fatalf("observation lost from store: found=%v err=%v", found, err)
	}
	if got.Status != state.StatusStale {
		t.Errorf("Status = %q, want stale", got.Status)
	}
	if got.LastError == "" {
		t.Errorf("expected LastError to be recorded, got %q", got.LastError)
	}
}

func TestDetectVersionCandidateEvents_Guards(t *testing.T) {
	ctx := context.Background()
	o := &Observer{}

	evs, partial := o.detectVersionCandidateEvents(ctx, nil, groupKey{Tag: "myapp"})
	if evs != nil || partial {
		t.Errorf("opaque tag: evs=%v partial=%v, want nil/false", evs, partial)
	}

	reg := newFakeRegistry()
	reg.listErr = errors.New("tags unavailable")
	evs, partial = o.detectVersionCandidateEvents(ctx, reg, groupKey{Tag: "1.2.3"})
	if evs != nil || !partial {
		t.Errorf("list failure: evs=%v partial=%v, want nil/true", evs, partial)
	}
}
