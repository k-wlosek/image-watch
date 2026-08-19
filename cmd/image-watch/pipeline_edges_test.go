package main

import (
	"context"
	"errors"
	"testing"

	"github.com/k-wlosek/image-watch/internal/image"
	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/observer"
	"github.com/k-wlosek/image-watch/internal/state"
)

// errStore is a state.Store with injectable notification-method failures.
type errStore struct {
	failNotified bool
	failMarked   bool
}

func (e *errStore) GetObservation(context.Context, state.Key) (state.Observation, bool, error) {
	return state.Observation{}, false, nil
}

func (e *errStore) PutObservation(context.Context, state.Observation) error { return nil }

func (e *errStore) HasNotified(context.Context, string) (bool, error) {
	if e.failNotified {
		return false, errors.New("dedup lookup broken")
	}
	return false, nil
}

func (e *errStore) MarkNotified(context.Context, string) error {
	if e.failMarked {
		return errors.New("record broken")
	}
	return nil
}

var _ state.Store = (*errStore)(nil)

func TestBuildNotification_DedupLookupErrorWarns(t *testing.T) {
	results := []observer.Result{sampleResult(nil)}
	store := &errStore{failNotified: true}

	note := BuildNotification(context.Background(), results, store)
	if len(note.Items) != 1 {
		t.Fatalf("got %d items, want 1 (dedup lookup failure must not drop the event)", len(note.Items))
	}
}

func TestDeliver_EmptyItemsReturnsSuccess(t *testing.T) {
	delivered, err := Deliver(context.Background(), nil, notify.Notification{})
	if err != nil {
		t.Fatalf("Deliver(empty) error: %v", err)
	}
	if !delivered {
		t.Error("Deliver(empty) must report delivered=true")
	}
}

func TestMarkDelivered_StoreErrorWarns(t *testing.T) {
	note := notify.Notification{Items: []notify.Item{{Image: "docker.io/library/foo", Type: "PATCH_AVAILABLE"}}}
	store := &errStore{failMarked: true}
	MarkDelivered(context.Background(), note, store) // must not panic
}

func TestDeliverAndMark_EmptyItems(t *testing.T) {
	if err := DeliverAndMark(context.Background(), nil, notify.Notification{}, "batch", nil); err != nil {
		t.Fatalf("DeliverAndMark(empty) error: %v", err)
	}
}

func TestRegistryFailedThisCycle_EmptyGroup(t *testing.T) {
	if registryFailedThisCycle(nil) {
		t.Error("empty group must not count as a failed cycle")
	}
}

func TestRegistryOutageTracker_DefaultThreshold(t *testing.T) {
	tracker := NewRegistryOutageTracker()
	mkResult := func() []observer.Result {
		img := image.Reference{Registry: "ghcr.io", Repository: "acme/foo"}
		return []observer.Result{
			{Image: img, Err: errors.New("boom")},
			{Image: image.Reference{Registry: "ghcr.io", Repository: "acme/bar"}, Err: errors.New("boom")},
		}
	}

	for i := range 2 {
		if alerts := tracker.DetectOutages(mkResult(), 0); len(alerts) != 0 {
			t.Fatalf("cycle %d: expected no alert at the default threshold of 3", i+1)
		}
	}
	if alerts := tracker.DetectOutages(mkResult(), 0); len(alerts) != 1 {
		t.Fatalf("expected an alert on the third consecutive failure, got %v", alerts)
	}
}
