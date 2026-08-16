package main

import (
	"context"
	"errors"
	"testing"

	"github.com/example/image-watch/internal/event"
	"github.com/example/image-watch/internal/image"
	"github.com/example/image-watch/internal/notify"
	"github.com/example/image-watch/internal/observer"
	"github.com/example/image-watch/internal/policy"
	"github.com/example/image-watch/internal/state"
)

type fakeNotifier struct {
	notified []notify.Notification
	err      error
}

func (f *fakeNotifier) Notify(_ context.Context, n notify.Notification) error {
	if f.err != nil {
		return f.err
	}
	f.notified = append(f.notified, n)
	return nil
}

func sampleResult(policyOverride func(p *policy.Policy)) observer.Result {
	p := policy.Default()
	if policyOverride != nil {
		policyOverride(&p)
	}
	tag := "1.2.3"
	return observer.Result{
		Image:           image.Reference{Registry: "docker.io", Repository: "library/foo", Tag: &tag},
		Platform:        image.Platform{OS: "linux", Architecture: "amd64"},
		EffectivePolicy: p,
		ContainerNames:  []string{"foo1"},
		Events: []event.Event{
			{Type: event.PatchAvailable, CurrentTag: "1.2.3", CandidateTag: "1.2.4",
				Image:    image.Reference{Registry: "docker.io", Repository: "library/foo"},
				Platform: image.Platform{OS: "linux", Architecture: "amd64"}},
			{Type: event.OtherPlatformUpdate, CurrentTag: "1.2.3", CandidateTag: "1.2.4",
				Image:    image.Reference{Registry: "docker.io", Repository: "library/foo"},
				Platform: image.Platform{OS: "linux", Architecture: "amd64"}},
		},
	}
}

func TestBuildNotification_PolicyFiltersDisallowedTypes(t *testing.T) {
	results := []observer.Result{sampleResult(nil)}
	store := state.NewMemoryStore()

	note := BuildNotification(context.Background(), results, store)
	if len(note.Items) != 1 {
		t.Fatalf("got %d items, want 1 (policy should filter OTHER_PLATFORM_UPDATE)", len(note.Items))
	}
	if note.Items[0].Type != event.PatchAvailable {
		t.Errorf("got %s, want PATCH_AVAILABLE", note.Items[0].Type)
	}
}

func TestBuildNotification_SkipsFailedResults(t *testing.T) {
	failed := sampleResult(nil)
	failed.Err = errors.New("registry unavailable")
	results := []observer.Result{failed}
	store := state.NewMemoryStore()

	note := BuildNotification(context.Background(), results, store)
	if len(note.Items) != 0 {
		t.Errorf("expected no items from a failed result, got %d", len(note.Items))
	}
}

func TestBuildNotification_DedupSuppressesAlreadyNotified(t *testing.T) {
	results := []observer.Result{sampleResult(nil)}
	store := state.NewMemoryStore()

	first := BuildNotification(context.Background(), results, store)
	if len(first.Items) != 1 {
		t.Fatalf("expected 1 item on first build, got %d", len(first.Items))
	}
	store.MarkNotified(context.Background(), first.Items[0].Fingerprint)

	second := BuildNotification(context.Background(), results, store)
	if len(second.Items) != 0 {
		t.Errorf("expected the already-notified event to be suppressed, got %d items", len(second.Items))
	}
}

func TestBuildNotification_ChangedCandidateIsNotSuppressed(t *testing.T) {
	results := []observer.Result{sampleResult(nil)}
	store := state.NewMemoryStore()

	first := BuildNotification(context.Background(), results, store)
	store.MarkNotified(context.Background(), first.Items[0].Fingerprint)

	results[0].Events[0].CandidateTag = "1.2.5"
	second := BuildNotification(context.Background(), results, store)
	if len(second.Items) != 1 {
		t.Fatalf("expected the new candidate to produce a fresh notification, got %d items", len(second.Items))
	}
}

func TestDeliver_AllNotifiersAttempted(t *testing.T) {
	n1 := &fakeNotifier{}
	n2 := &fakeNotifier{}
	note := notify.Notification{Items: []notify.Item{{Type: event.PatchAvailable}}}

	delivered, err := Deliver(context.Background(), []notify.Notifier{n1, n2}, note)
	if err != nil {
		t.Fatalf("Deliver error: %v", err)
	}
	if !delivered {
		t.Errorf("expected delivered=true")
	}
	if len(n1.notified) != 1 || len(n2.notified) != 1 {
		t.Errorf("expected both notifiers to receive the notification")
	}
}

func TestDeliver_PartialFailureStillReportsDelivered(t *testing.T) {
	good := &fakeNotifier{}
	bad := &fakeNotifier{err: errors.New("boom")}
	note := notify.Notification{Items: []notify.Item{{Type: event.PatchAvailable}}}

	delivered, err := Deliver(context.Background(), []notify.Notifier{good, bad}, note)
	if err == nil {
		t.Fatalf("expected a non-nil error reflecting the failed notifier")
	}
	if !delivered {
		t.Errorf("expected delivered=true since at least one notifier succeeded")
	}
}

func TestDeliver_TotalFailureReportsNotDelivered(t *testing.T) {
	bad1 := &fakeNotifier{err: errors.New("boom1")}
	bad2 := &fakeNotifier{err: errors.New("boom2")}
	note := notify.Notification{Items: []notify.Item{{Type: event.PatchAvailable}}}

	delivered, err := Deliver(context.Background(), []notify.Notifier{bad1, bad2}, note)
	if err == nil {
		t.Fatalf("expected an error")
	}
	if delivered {
		t.Errorf("expected delivered=false when every notifier fails")
	}
}

func TestMarkDelivered_OnlyAfterSuccessfulDelivery(t *testing.T) {
	store := state.NewMemoryStore()
	results := []observer.Result{sampleResult(nil)}

	note := BuildNotification(context.Background(), results, store)
	bad := &fakeNotifier{err: errors.New("down")}
	delivered, _ := Deliver(context.Background(), []notify.Notifier{bad}, note)
	if delivered {
		t.Fatalf("expected delivery to fail")
	}
	notified, _ := store.HasNotified(context.Background(), note.Items[0].Fingerprint)
	if notified {
		t.Errorf("fingerprint must not be marked notified after a failed delivery")
	}

	good := &fakeNotifier{}
	delivered, _ = Deliver(context.Background(), []notify.Notifier{good}, note)
	if !delivered {
		t.Fatalf("expected delivery to succeed")
	}
	MarkDelivered(context.Background(), note, store)
	notified, _ = store.HasNotified(context.Background(), note.Items[0].Fingerprint)
	if !notified {
		t.Errorf("expected fingerprint to be marked notified after successful delivery")
	}
}
