package observer

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/example/image-watch/internal/event"
	"github.com/example/image-watch/internal/image"
	"github.com/example/image-watch/internal/policy"
	"github.com/example/image-watch/internal/registry"
	iwruntime "github.com/example/image-watch/internal/runtime"
	"github.com/example/image-watch/internal/state"
)

func container(name, imageRef, imageID string, platform image.Platform) iwruntime.ContainerObservation {
	ref, err := image.ParseReference(imageRef)
	if err != nil {
		panic(err)
	}
	return iwruntime.ContainerObservation{
		Runtime:   "docker",
		ID:        imageID + "-" + name,
		Name:      name,
		Image:     ref,
		Digest:    imageID,
		Platform:  platform,
		CreatedAt: time.Unix(0, 0),
	}
}

func newTestObserver(rt *fakeRuntime, reg *fakeRegistry) *Observer {
	fixedNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	return &Observer{
		Runtime:       rt,
		Registries:    func(string) registry.Registry { return reg },
		Store:         state.NewMemoryStore(),
		DefaultPolicy: policy.Default(),
		Now:           func() time.Time { return fixedNow },
	}
}

func findEvent(events []event.Event, t event.Type) *event.Event {
	for i := range events {
		if events[i].Type == t {
			return &events[i]
		}
	}
	return nil
}

func TestScenario1_StandardSemVer(t *testing.T) {
	plat := image.Platform{OS: "linux", Architecture: "amd64"}
	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("foo1", "ghcr.io/acme/foo:1.2.3", "sha256:current", plat),
	}}
	reg := newFakeRegistry()
	reg.setDigest("acme/foo", "1.2.3", "sha256:current")
	reg.setTags("acme/foo", []string{"1.2.4", "1.2.5", "1.3.0", "2.0.0"})

	o := newTestObserver(rt, reg)
	results, err := o.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	events := results[0].Events

	if p := findEvent(events, event.PatchAvailable); p == nil || p.CandidateTag != "1.2.5" {
		t.Errorf("expected PATCH_AVAILABLE -> 1.2.5, got %+v", p)
	}
	if m := findEvent(events, event.MajorAvailable); m == nil || m.CandidateTag != "2.0.0" {
		t.Errorf("expected MAJOR_AVAILABLE -> 2.0.0, got %+v", m)
	}
}

func TestScenario3_MutableLatest(t *testing.T) {
	plat := image.Platform{OS: "linux", Architecture: "amd64"}
	reg := newFakeRegistry()
	reg.setTags("acme/foo", []string{"1.2.3", "1.2.4"})
	reg.setDigest("acme/foo", "1.2.3", "sha256:AAAA")
	reg.setDigest("acme/foo", "1.2.4", "sha256:BBBB")
	reg.setDigest("acme/foo", "latest", "sha256:AAAA") // starts matching 1.2.3

	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("foo1", "ghcr.io/acme/foo:latest", "sha256:AAAA", plat),
	}}
	o := newTestObserver(rt, reg)

	results, err := o.Check(context.Background())
	if err != nil {
		t.Fatalf("first Check error: %v", err)
	}
	if e := findEvent(results[0].Events, event.TagChanged); e != nil {
		t.Fatalf("did not expect TAG_CHANGED on baseline check, got %+v", e)
	}

	reg.setDigest("acme/foo", "latest", "sha256:BBBB")
	results, err = o.Check(context.Background())
	if err != nil {
		t.Fatalf("second Check error: %v", err)
	}
	ev := findEvent(results[0].Events, event.TagChanged)
	if ev == nil {
		t.Fatalf("expected TAG_CHANGED on second check")
	}
	if ev.CurrentDigest != "sha256:AAAA" || ev.CandidateDigest != "sha256:BBBB" {
		t.Errorf("digest transition = %s -> %s, want sha256:AAAA -> sha256:BBBB", ev.CurrentDigest, ev.CandidateDigest)
	}
	if ev.CandidateTag != "1.2.4" {
		t.Errorf("expected enrichment to infer candidate tag 1.2.4, got %q", ev.CandidateTag)
	}
}

func TestScenario4_VersionTagMutation(t *testing.T) {
	plat := image.Platform{OS: "linux", Architecture: "amd64"}
	reg := newFakeRegistry()
	reg.setTags("acme/foo", []string{"1.2.4"})
	reg.setDigest("acme/foo", "1.2.3", "sha256:AAAA")
	reg.setDigest("acme/foo", "1.2.4", "sha256:CCCC")

	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("foo1", "ghcr.io/acme/foo:1.2.3", "sha256:AAAA", plat),
	}}
	o := newTestObserver(rt, reg)

	if _, err := o.Check(context.Background()); err != nil {
		t.Fatalf("baseline check error: %v", err)
	}

	reg.setDigest("acme/foo", "1.2.3", "sha256:BBBB")
	results, err := o.Check(context.Background())
	if err != nil {
		t.Fatalf("second check error: %v", err)
	}

	mutated := findEvent(results[0].Events, event.TagMutated)
	if mutated == nil {
		t.Fatalf("expected TAG_MUTATED")
	}
	if mutated.CurrentDigest != "sha256:AAAA" || mutated.CandidateDigest != "sha256:BBBB" {
		t.Errorf("mutation digests = %s -> %s, want sha256:AAAA -> sha256:BBBB", mutated.CurrentDigest, mutated.CandidateDigest)
	}

	patch := findEvent(results[0].Events, event.PatchAvailable)
	if patch == nil || patch.CandidateTag != "1.2.4" {
		t.Errorf("expected PATCH_AVAILABLE -> 1.2.4 to co-occur with TAG_MUTATED, got %+v", patch)
	}
}

func TestScenario7_Deduplication(t *testing.T) {
	plat := image.Platform{OS: "linux", Architecture: "amd64"}
	var containers []iwruntime.ContainerObservation
	for i := 0; i < 20; i++ {
		containers = append(containers, container(
			fmt.Sprintf("foo%d", i), "ghcr.io/acme/foo:1.2.3", "sha256:current", plat,
		))
	}
	reg := newFakeRegistry()
	reg.setDigest("acme/foo", "1.2.3", "sha256:current")
	reg.setTags("acme/foo", nil)

	rt := &fakeRuntime{containers: containers}
	o := newTestObserver(rt, reg)

	results, err := o.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results for 20 containers on the same image, want 1", len(results))
	}
	if len(results[0].ContainerNames) != 20 {
		t.Errorf("got %d container names, want 20", len(results[0].ContainerNames))
	}
}

func TestScenario9_ContainerRecreation(t *testing.T) {
	plat := image.Platform{OS: "linux", Architecture: "amd64"}
	reg := newFakeRegistry()
	reg.setDigest("acme/foo", "1.2.3", "sha256:current")
	reg.setTags("acme/foo", nil)

	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("original", "ghcr.io/acme/foo:1.2.3", "sha256:current", plat),
	}}
	o := newTestObserver(rt, reg)
	if _, err := o.Check(context.Background()); err != nil {
		t.Fatalf("baseline check error: %v", err)
	}

	rt.containers = []iwruntime.ContainerObservation{
		container("recreated", "ghcr.io/acme/foo:1.2.3", "sha256:current", plat),
	}
	results, err := o.Check(context.Background())
	if err != nil {
		t.Fatalf("second check error: %v", err)
	}
	if e := findEvent(results[0].Events, event.TagMutated); e != nil {
		t.Errorf("container recreation must not produce TAG_MUTATED, got %+v", e)
	}
}

func TestScenario10_RegistryFailureIsolation(t *testing.T) {
	plat := image.Platform{OS: "linux", Architecture: "amd64"}
	reg := newFakeRegistry()
	reg.setDigest("acme/good", "1.0.0", "sha256:good")
	reg.setTags("acme/good", nil)
	reg.setResolveError("acme/bad", "1.0.0", errors.New("401 unauthorized"))

	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("good1", "ghcr.io/acme/good:1.0.0", "sha256:good", plat),
		container("bad1", "ghcr.io/acme/bad:1.0.0", "sha256:bad", plat),
	}}
	o := newTestObserver(rt, reg)

	results, err := o.Check(context.Background())
	if err != nil {
		t.Fatalf("Check should not fail wholesale on one image's registry error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (one failing, one succeeding)", len(results))
	}

	var sawGoodSuccess, sawBadFailure bool
	for _, r := range results {
		if r.Image.Repository == "acme/good" && r.Err == nil {
			sawGoodSuccess = true
		}
		if r.Image.Repository == "acme/bad" && r.Err != nil && r.Stale {
			sawBadFailure = true
		}
	}
	if !sawGoodSuccess {
		t.Errorf("expected acme/good to succeed despite acme/bad failing")
	}
	if !sawBadFailure {
		t.Errorf("expected acme/bad to report a stale, non-nil error result")
	}
}

type fakeEnrichmentObserver struct {
	calls []bool // each entry is the success value passed to ObserveEnrichment
}

func (f *fakeEnrichmentObserver) ObserveEnrichment(success bool) {
	f.calls = append(f.calls, success)
}

func TestEnrichmentObserver_RecordsSuccessAndFailure(t *testing.T) {
	plat := image.Platform{OS: "linux", Architecture: "amd64"}
	reg := newFakeRegistry()
	reg.setTags("acme/foo", []string{"1.2.3", "1.2.4"})
	reg.setDigest("acme/foo", "1.2.3", "sha256:AAAA")
	reg.setDigest("acme/foo", "1.2.4", "sha256:BBBB")
	reg.setDigest("acme/foo", "latest", "sha256:AAAA")

	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("foo1", "ghcr.io/acme/foo:latest", "sha256:AAAA", plat),
	}}
	o := newTestObserver(rt, reg)
	obs := &fakeEnrichmentObserver{}
	o.Metrics = obs

	// Baseline check: no digest change yet, so enrichment shouldn't run
	// at all.
	if _, err := o.Check(context.Background()); err != nil {
		t.Fatalf("baseline check error: %v", err)
	}
	if len(obs.calls) != 0 {
		t.Fatalf("expected no enrichment calls before any digest change, got %d", len(obs.calls))
	}

	// latest moves to a digest matching 1.2.4 -> enrichment should
	// succeed and be recorded.
	reg.setDigest("acme/foo", "latest", "sha256:BBBB")
	if _, err := o.Check(context.Background()); err != nil {
		t.Fatalf("second check error: %v", err)
	}
	if len(obs.calls) != 1 || !obs.calls[0] {
		t.Errorf("expected exactly one successful enrichment call, got %v", obs.calls)
	}

	// latest moves again, this time to a digest with no matching tag ->
	// enrichment should be attempted but recorded as unsuccessful.
	reg.setDigest("acme/foo", "latest", "sha256:CCCC")
	if _, err := o.Check(context.Background()); err != nil {
		t.Fatalf("third check error: %v", err)
	}
	if len(obs.calls) != 2 || obs.calls[1] {
		t.Errorf("expected a second, unsuccessful enrichment call, got %v", obs.calls)
	}
}

func TestEnrichmentObserver_NilIsSafe(t *testing.T) {
	plat := image.Platform{OS: "linux", Architecture: "amd64"}
	reg := newFakeRegistry()
	reg.setTags("acme/foo", nil)
	reg.setDigest("acme/foo", "latest", "sha256:AAAA")

	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("foo1", "ghcr.io/acme/foo:latest", "sha256:AAAA", plat),
	}}
	o := newTestObserver(rt, reg)
	// o.Metrics is nil by default -- must not panic even when enrichment
	// actually runs.
	if _, err := o.Check(context.Background()); err != nil {
		t.Fatalf("baseline check error: %v", err)
	}
	reg.setDigest("acme/foo", "latest", "sha256:BBBB")
	if _, err := o.Check(context.Background()); err != nil {
		t.Fatalf("second check error: %v", err)
	}
}
