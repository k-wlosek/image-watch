package main

import (
	"context"
	"errors"
	"testing"

	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/image"
	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/observer"
	"github.com/k-wlosek/image-watch/internal/policy"
	"github.com/k-wlosek/image-watch/internal/state"
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

func TestDeliverAndMark_BatchModeSendsOneNotification(t *testing.T) {
	n := &fakeNotifier{}
	store := state.NewMemoryStore()
	note := notify.Notification{Items: []notify.Item{
		{Fingerprint: "fp1", Type: event.PatchAvailable},
		{Fingerprint: "fp2", Type: event.MinorAvailable},
	}}

	if err := DeliverAndMark(context.Background(), []notify.Notifier{n}, note, "batch", store); err != nil {
		t.Fatalf("DeliverAndMark error: %v", err)
	}
	if len(n.notified) != 1 {
		t.Fatalf("expected exactly 1 Notify call in batch mode, got %d", len(n.notified))
	}
	if len(n.notified[0].Items) != 2 {
		t.Errorf("expected the single batch call to contain both items, got %d", len(n.notified[0].Items))
	}
	for _, fp := range []string{"fp1", "fp2"} {
		notified, _ := store.HasNotified(context.Background(), fp)
		if !notified {
			t.Errorf("expected %s to be marked notified", fp)
		}
	}
}

func TestDeliverAndMark_IndividualModeSendsPerItem(t *testing.T) {
	n := &fakeNotifier{}
	store := state.NewMemoryStore()
	note := notify.Notification{Items: []notify.Item{
		{Fingerprint: "fp1", Type: event.PatchAvailable},
		{Fingerprint: "fp2", Type: event.MinorAvailable},
	}}

	if err := DeliverAndMark(context.Background(), []notify.Notifier{n}, note, "individual", store); err != nil {
		t.Fatalf("DeliverAndMark error: %v", err)
	}
	if len(n.notified) != 2 {
		t.Fatalf("expected 2 separate Notify calls in individual mode, got %d", len(n.notified))
	}
	for _, call := range n.notified {
		if len(call.Items) != 1 {
			t.Errorf("expected each individual-mode call to carry exactly 1 item, got %d", len(call.Items))
		}
	}
}

func TestDeliverAndMark_IndividualModeOneFailureDoesNotBlockOthers(t *testing.T) {
	// Fails the first item and succeeds on the second.
	n := &recordingFailFirstNotifier{}
	store := state.NewMemoryStore()
	note := notify.Notification{Items: []notify.Item{
		{Fingerprint: "fails", Type: event.PatchAvailable},
		{Fingerprint: "succeeds", Type: event.MinorAvailable},
	}}
	err := DeliverAndMark(context.Background(), []notify.Notifier{n}, note, "individual", store)
	if err == nil {
		t.Fatalf("expected an error reflecting the one failed item")
	}

	failedNotified, _ := store.HasNotified(context.Background(), "fails")
	if failedNotified {
		t.Errorf("the failed item must not be marked notified")
	}
	succeededNotified, _ := store.HasNotified(context.Background(), "succeeds")
	if !succeededNotified {
		t.Errorf("the succeeding item must still be marked notified despite the other item's failure")
	}
}

// recordingFailFirstNotifier fails delivery for any Notification whose
// single item has Fingerprint == "fails", and succeeds otherwise.
type recordingFailFirstNotifier struct{}

func (r *recordingFailFirstNotifier) Notify(_ context.Context, n notify.Notification) error {
	for _, item := range n.Items {
		if item.Fingerprint == "fails" {
			return errors.New("simulated failure")
		}
	}
	return nil
}

func failedResult(host, repo string) observer.Result {
	tag := "1.0"
	return observer.Result{
		Image: image.Reference{Registry: host, Repository: repo, Tag: &tag},
		Err:   errors.New("registry unavailable"),
		Stale: true,
	}
}

func succeededResult(host, repo string) observer.Result {
	tag := "1.0"
	return observer.Result{
		Image: image.Reference{Registry: host, Repository: repo, Tag: &tag},
	}
}

func TestRegistryOutageTracker_FiresAfterThreshold(t *testing.T) {
	tracker := NewRegistryOutageTracker()
	results := []observer.Result{failedResult("ghcr.io", "acme/foo"), failedResult("ghcr.io", "acme/bar")}

	if alerts := tracker.DetectOutages(results, 3); len(alerts) != 0 {
		t.Fatalf("cycle 1: expected no alert, got %v", alerts)
	}
	if alerts := tracker.DetectOutages(results, 3); len(alerts) != 0 {
		t.Fatalf("cycle 2: expected no alert, got %v", alerts)
	}
	alerts := tracker.DetectOutages(results, 3)
	if len(alerts) != 1 {
		t.Fatalf("cycle 3: expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Image != "ghcr.io" {
		t.Errorf("alert Image = %q, want ghcr.io", alerts[0].Image)
	}
}

func TestRegistryOutageTracker_OnlyFiresOncePerEpisode(t *testing.T) {
	tracker := NewRegistryOutageTracker()
	results := []observer.Result{failedResult("ghcr.io", "acme/foo")}

	tracker.DetectOutages(results, 2)
	first := tracker.DetectOutages(results, 2)
	if len(first) != 1 {
		t.Fatalf("expected the threshold-crossing cycle to alert, got %d", len(first))
	}

	second := tracker.DetectOutages(results, 2)
	third := tracker.DetectOutages(results, 2)
	if len(second) != 0 || len(third) != 0 {
		t.Errorf("expected no repeat alerts while the outage continues, got %d and %d", len(second), len(third))
	}
}

func TestRegistryOutageTracker_RecoveryResetsEpisode(t *testing.T) {
	tracker := NewRegistryOutageTracker()
	failing := []observer.Result{failedResult("ghcr.io", "acme/foo")}
	recovered := []observer.Result{succeededResult("ghcr.io", "acme/foo")}

	tracker.DetectOutages(failing, 2)
	first := tracker.DetectOutages(failing, 2)
	if len(first) != 1 {
		t.Fatalf("expected first episode to alert, got %d", len(first))
	}

	if alerts := tracker.DetectOutages(recovered, 2); len(alerts) != 0 {
		t.Errorf("recovery cycle should not itself alert, got %v", alerts)
	}

	tracker.DetectOutages(failing, 2)
	second := tracker.DetectOutages(failing, 2)
	if len(second) != 1 {
		t.Fatalf("expected a new outage episode to alert again, got %d", len(second))
	}
}

func TestRegistryOutageTracker_PartialImageFailureDoesNotCountAsOutage(t *testing.T) {
	tracker := NewRegistryOutageTracker()
	mixed := []observer.Result{failedResult("ghcr.io", "acme/foo"), succeededResult("ghcr.io", "acme/bar")}

	for i := range 5 {
		if alerts := tracker.DetectOutages(mixed, 2); len(alerts) != 0 {
			t.Fatalf("cycle %d: expected no outage alert for a partial failure, got %v", i, alerts)
		}
	}
}

func TestRegistryOutageTracker_IndependentHosts(t *testing.T) {
	tracker := NewRegistryOutageTracker()
	results := []observer.Result{
		failedResult("ghcr.io", "acme/foo"),
		succeededResult("docker.io", "library/nginx"),
	}

	tracker.DetectOutages(results, 2)
	alerts := tracker.DetectOutages(results, 2)
	if len(alerts) != 1 {
		t.Fatalf("expected exactly 1 alert (ghcr.io only), got %d", len(alerts))
	}
	if alerts[0].Image != "ghcr.io" {
		t.Errorf("alert should be for ghcr.io, got %q", alerts[0].Image)
	}
}
