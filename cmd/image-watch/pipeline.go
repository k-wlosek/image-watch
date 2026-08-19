package main

import (
	"context"
	"fmt"
	"time"

	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/observer"
	"github.com/k-wlosek/image-watch/internal/policy"
	"github.com/k-wlosek/image-watch/internal/state"
)

// BuildNotification turns detection results into a deliverable notification.
func BuildNotification(ctx context.Context, results []observer.Result, store state.Store) notify.Notification {
	note := notify.Notification{Timestamp: time.Now()}

	for _, r := range results {
		if r.Err != nil {
			continue
		}
		imageName := r.Image.Registry + "/" + r.Image.Repository

		for _, e := range r.Events {
			allowed, suppressed := allowContainers(r, e.Type)
			if len(allowed) == 0 {
				continue
			}

			fp := event.Fingerprint(e)
			notified, err := store.HasNotified(ctx, fp)
			if err != nil {
				fmt.Printf("warning: dedup lookup failed for %s (%s): %v; including anyway\n", imageName, e.Type, err)
			} else if notified {
				continue
			}

			note.Items = append(note.Items, notify.Item{
				Fingerprint:       fp,
				Image:             imageName,
				Platform:          r.Platform.String(),
				Type:              e.Type,
				CurrentTag:        e.CurrentTag,
				CandidateTag:      e.CandidateTag,
				CurrentDigest:     e.CurrentDigest,
				CandidateDigest:   e.CandidateDigest,
				CombinedCandidate: e.CombinedCandidate,
				ContainerNames:    allowed,
				Suppressed:        suppressed,
			})
		}
	}

	return note
}

// allowContainers splits a result's members by whether their own policy allows an event.
func allowContainers(r observer.Result, t event.Type) (allowed, suppressed []string) {
	for i, name := range r.ContainerNames {
		p := policy.Default()
		if i < len(r.ContainerPolicies) {
			p = r.ContainerPolicies[i]
		}
		if p.Allows(t) {
			allowed = append(allowed, name)
		} else {
			suppressed = append(suppressed, name)
		}
	}
	return allowed, suppressed
}

// Deliver sends the notification through every configured notifier.
func Deliver(ctx context.Context, notifiers []notify.Notifier, note notify.Notification) (delivered bool, err error) {
	if len(note.Items) == 0 {
		return true, nil
	}

	var errs []error
	for _, n := range notifiers {
		if nerr := n.Notify(ctx, note); nerr != nil {
			errs = append(errs, nerr)
		} else {
			delivered = true
		}
	}
	if len(errs) > 0 {
		err = fmt.Errorf("%d notifier(s) failed: %v", len(errs), errs)
	}
	return delivered, err
}

// MarkDelivered records every item's fingerprint as notified.
func MarkDelivered(ctx context.Context, note notify.Notification, store state.Store) {
	for _, item := range note.Items {
		if err := store.MarkNotified(ctx, item.Fingerprint); err != nil {
			fmt.Printf("warning: failed to record notification dedup state for %s (%s): %v\n", item.Image, item.Type, err)
		}
	}
}

// DeliverAndMark handles batch and individual notification delivery.
func DeliverAndMark(ctx context.Context, notifiers []notify.Notifier, note notify.Notification, mode string, store state.Store) error {
	if len(note.Items) == 0 {
		return nil
	}

	if mode != "individual" {
		delivered, err := Deliver(ctx, notifiers, note)
		if delivered {
			MarkDelivered(ctx, note, store)
		}
		return err
	}

	var errs []error
	for _, item := range note.Items {
		single := notify.Notification{Timestamp: note.Timestamp, Items: []notify.Item{item}}
		delivered, err := Deliver(ctx, notifiers, single)
		if delivered {
			MarkDelivered(ctx, single, store)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d item(s) had delivery errors: %v", len(errs), errs)
	}
	return nil
}

// RegistryOutageTracker tracks consecutive registry failures.
type RegistryOutageTracker struct {
	consecutiveFailures map[string]int
	alreadyNotified     map[string]bool
}

// NewRegistryOutageTracker constructs an empty tracker.
func NewRegistryOutageTracker() *RegistryOutageTracker {
	return &RegistryOutageTracker{
		consecutiveFailures: make(map[string]int),
		alreadyNotified:     make(map[string]bool),
	}
}

// registryFailedThisCycle reports whether every image on a host failed.
func registryFailedThisCycle(group []observer.Result) bool {
	if len(group) == 0 {
		return false
	}
	for _, r := range group {
		if r.Err == nil {
			return false
		}
	}
	return true
}

// DetectOutages updates the tracker and returns any new outage alerts.
func (t *RegistryOutageTracker) DetectOutages(results []observer.Result, threshold int) []notify.Item {
	if threshold <= 0 {
		threshold = 3
	}

	byHost := make(map[string][]observer.Result)
	for _, r := range results {
		byHost[r.Image.Registry] = append(byHost[r.Image.Registry], r)
	}

	var alerts []notify.Item
	for host, group := range byHost {
		if registryFailedThisCycle(group) {
			t.consecutiveFailures[host]++
			if t.consecutiveFailures[host] >= threshold && !t.alreadyNotified[host] {
				t.alreadyNotified[host] = true
				alerts = append(alerts, notify.Item{
					Image:        host,
					Type:         registryOutageEventType,
					CurrentTag:   fmt.Sprintf("%d monitored image(s) affected", len(group)),
					CandidateTag: fmt.Sprintf("%d consecutive failed checks", t.consecutiveFailures[host]),
				})
			}
		} else {
			t.consecutiveFailures[host] = 0
			t.alreadyNotified[host] = false
		}
	}
	return alerts
}

const registryOutageEventType event.Type = "REGISTRY_OUTAGE"
