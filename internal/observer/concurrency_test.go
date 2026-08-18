package observer

import (
	"context"
	"testing"
	"time"

	"github.com/example/image-watch/internal/event"
	"github.com/example/image-watch/internal/image"
	iwruntime "github.com/example/image-watch/internal/runtime"
)

func concurrencyPlat() image.Platform {
	return image.Platform{OS: "linux", Architecture: "amd64"}
}

// opaqueCluster builds n containers on distinct opaque-tagged repos, each
// running the digest their registry serves (so only the current-tag resolve
// happens, and every group issues one registry request).
func opaqueCluster(n int) (*fakeRuntime, *fakeRegistry) {
	plat := concurrencyPlat()
	var containers []iwruntime.ContainerObservation
	reg := newFakeRegistry()
	for i := 0; i < n; i++ {
		repo := "acme/app" + string(rune('a'+i))
		digest := "sha256:" + string(rune('a'+i)) + "000"
		reg.setDigest(repo, "latest", digest)
		reg.setResolveDelay(repo, "latest", 5*time.Millisecond)
		containers = append(containers, container("c"+string(rune('a'+i)), "ghcr.io/"+repo+":latest", digest, plat))
	}
	return &fakeRuntime{containers: containers}, reg
}

func TestCheck_RunsGroupsConcurrently(t *testing.T) {
	rt, reg := opaqueCluster(8)
	o := newTestObserver(rt, reg)
	o.ConcurrencyWorkers = 4

	if _, err := o.Check(context.Background()); err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if reg.maxInFlight < 2 {
		t.Errorf("expected groups to be resolved in parallel (peak in-flight resolves %d), want >= 2", reg.maxInFlight)
	}
	if reg.maxInFlight > 4 {
		t.Errorf("expected at most 4 concurrent resolves (configured workers), got %d", reg.maxInFlight)
	}
}

func TestCheck_WorkersOneIsSequential(t *testing.T) {
	rt, reg := opaqueCluster(8)
	o := newTestObserver(rt, reg)
	o.ConcurrencyWorkers = 1

	if _, err := o.Check(context.Background()); err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if reg.maxInFlight != 1 {
		t.Errorf("expected fully sequential resolves with workers=1, got peak %d", reg.maxInFlight)
	}
}

func TestCheck_WorkersCappedByGroupCount(t *testing.T) {
	rt, reg := opaqueCluster(2)
	o := newTestObserver(rt, reg)
	o.ConcurrencyWorkers = 8

	if _, err := o.Check(context.Background()); err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if reg.maxInFlight > 2 {
		t.Errorf("expected the pool to cap at the number of groups (2), got peak %d", reg.maxInFlight)
	}
}

func TestCheck_ResultsInDeterministicOrder(t *testing.T) {
	plat := concurrencyPlat()
	reg := newFakeRegistry()
	reg.setDigest("library/alpine", "3.18", "sha256:318")
	reg.setDigest("library/alpine", "latest", "sha256:al")
	reg.setDigest("acme/bar", "latest", "sha256:bar")
	reg.setDigest("acme/foo", "latest", "sha256:foo")

	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("al1", "docker.io/library/alpine:3.18", "sha256:318", plat),
		container("al2", "docker.io/library/alpine:latest", "sha256:al", plat),
		container("foo", "ghcr.io/acme/foo:latest", "sha256:foo", plat),
		container("bar", "ghcr.io/acme/bar:latest", "sha256:bar", plat),
	}}
	o := newTestObserver(rt, reg)
	o.ConcurrencyWorkers = 4

	results, err := o.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}

	want := []string{
		"docker.io/library/alpine:3.18",
		"docker.io/library/alpine:latest",
		"ghcr.io/acme/bar:latest",
		"ghcr.io/acme/foo:latest",
	}
	if len(results) != len(want) {
		t.Fatalf("got %d results, want %d", len(results), len(want))
	}
	for i, w := range want {
		got := results[i].Image.Registry + "/" + results[i].Image.Repository + ":" + results[i].Image.TagOrEmpty()
		if got != w {
			t.Errorf("results[%d] = %s, want %s", i, got, w)
		}
	}
}

// TestEnrichment_NewestMatchWinsEvenWhenOlderCompletesFirst exercises the
// windowed enrichment scan: an older matching candidate resolves instantly
// while the newer match is delayed, and the result must still be the newest
// match, not the first to finish.
func TestEnrichment_NewestMatchWinsEvenWhenOlderCompletesFirst(t *testing.T) {
	reg := newFakeRegistry()
	reg.setTags("acme/foo", []string{"1.0.0", "2.0.0", "2.1.0"})
	reg.setDigest("acme/foo", "1.0.0", "sha256:match") // older match, resolves instantly
	reg.setDigest("acme/foo", "2.0.0", "sha256:match") // newest match, deliberately slow
	reg.setDigest("acme/foo", "2.1.0", "sha256:other")
	reg.setResolveDelay("acme/foo", "2.0.0", 30*time.Millisecond)
	reg.setResolveDelay("acme/foo", "2.1.0", 30*time.Millisecond)

	o := newTestObserver(&fakeRuntime{}, reg)

	tag, ok := o.attemptEnrichment(context.Background(), reg, enrichmentKey(), "sha256:match", newGroupCache())
	if !ok || tag != "2.0.0" {
		t.Fatalf("enrichment = %q, %v; want 2.0.0, true (newest match must win over first-finished)", tag, ok)
	}
	if reg.maxInFlight < 2 {
		t.Errorf("expected the enrichment scan to run candidates in parallel, peak in-flight %d", reg.maxInFlight)
	}
}

// TestEnrichment_SharedTagListFetchedOnce proves multiple drifted running
// digests in one group share a single ListTags call through the group cache.
func TestEnrichment_SharedTagListFetchedOnce(t *testing.T) {
	plat := concurrencyPlat()
	reg := newFakeRegistry()
	reg.setTags("acme/foo", []string{"1.0.0", "1.1.0"})
	reg.setDigest("acme/foo", "latest", "sha256:new")
	reg.setDigest("acme/foo", "1.0.0", "sha256:old")
	reg.setDigest("acme/foo", "1.1.0", "sha256:new")

	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("one", "ghcr.io/acme/foo:latest", "sha256:old1", plat),
		container("two", "ghcr.io/acme/foo:latest", "sha256:old2", plat),
	}}
	o := newTestObserver(rt, reg)

	results, err := o.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	var changed int
	for _, e := range results[0].Events {
		if e.Type == event.TagChanged {
			changed++
			if e.CandidateTag != "1.1.0" {
				t.Errorf("expected enriched drift candidate 1.1.0, got %q", e.CandidateTag)
			}
		}
	}
	if changed != 2 {
		t.Errorf("expected 2 drift events (one per running digest), got %d", changed)
	}
	if calls := reg.listCallsCount("acme/foo"); calls != 1 {
		t.Errorf("expected the group to list tags once for both enrichment attempts, got %d", calls)
	}
}

// TestCandidateResolve_RunsConcurrentlyAndDeterministically verifies the
// intra-group candidate resolution runs in parallel while events keep their
// canonical (patch, minor, major) order.
func TestCandidateResolve_RunsConcurrentlyAndDeterministically(t *testing.T) {
	plat := concurrencyPlat()
	reg := newFakeRegistry()
	reg.setDigest("acme/foo", "1.2.3", "sha256:current")
	reg.setTags("acme/foo", []string{"1.2.4", "1.3.0", "2.0.0"})
	reg.setDigest("acme/foo", "1.2.4", "sha256:v124")
	reg.setDigest("acme/foo", "1.3.0", "sha256:v130")
	reg.setDigest("acme/foo", "2.0.0", "sha256:v200")
	reg.setResolveDelay("acme/foo", "1.2.4", 10*time.Millisecond)
	reg.setResolveDelay("acme/foo", "1.3.0", 10*time.Millisecond)
	reg.setResolveDelay("acme/foo", "2.0.0", 10*time.Millisecond)

	rt := &fakeRuntime{containers: []iwruntime.ContainerObservation{
		container("foo", "ghcr.io/acme/foo:1.2.3", "sha256:current", plat),
	}}
	o := newTestObserver(rt, reg)

	results, err := o.Check(context.Background())
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}

	var order []event.Type
	for _, e := range results[0].Events {
		order = append(order, e.Type)
	}
	want := []event.Type{event.PatchAvailable, event.MinorAvailable, event.MajorAvailable}
	if len(order) != len(want) {
		t.Fatalf("event types = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("event[%d] = %s, want %s (canonical order must survive concurrent resolution)", i, order[i], want[i])
		}
	}
	if reg.maxInFlight < 2 {
		t.Errorf("expected candidate resolution to overlap, peak in-flight %d", reg.maxInFlight)
	}
}
