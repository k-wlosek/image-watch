package main

import (
	"context"
	"fmt"
	"time"

	"github.com/example/image-watch/internal/event"
	"github.com/example/image-watch/internal/notify"
	"github.com/example/image-watch/internal/observer"
	"github.com/example/image-watch/internal/state"
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
			if !r.EffectivePolicy.Allows(e.Type) {
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
				ContainerNames:    r.ContainerNames,
			})
		}
	}

	return note
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
