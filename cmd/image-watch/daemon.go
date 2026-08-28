package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/k-wlosek/image-watch/internal/config"
	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/metrics"
	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/observer"
)

// Daemon runs the scheduler loop.
type Daemon struct {
	Config    config.Config
	Observer  *observer.Observer
	Notifiers []notify.Notifier

	// Metrics is optional; nil disables metric recording.
	Metrics *metrics.Metrics

	// RegistryOutages is optional.
	RegistryOutages *RegistryOutageTracker
}

// Run performs one cycle immediately, then repeats until ctx is canceled.
func (d *Daemon) Run(ctx context.Context) error {
	d.runCycle(ctx)

	ticker := time.NewTicker(d.Config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			d.runCycle(ctx)
		}
	}
}

// runCycle performs one check-detect-notify cycle.
func (d *Daemon) runCycle(ctx context.Context) {
	start := time.Now()
	slog.Debug("runCycle: started")

	results, err := d.Observer.Check(ctx)
	if err != nil {
		slog.Error("check cycle failed", "error", err)
		if d.Metrics != nil {
			d.Metrics.RecordCheck(time.Since(start), err)
		}
		return
	}

	failedImages := 0
	totalContainers := 0
	for _, r := range results {
		if r.Err != nil {
			failedImages++
		}
		totalContainers += len(r.ContainerNames)

		drift := computeDigestDrift(r)

		if d.Metrics != nil {
			imageName := r.Image.Registry + "/" + r.Image.Repository
			present := make(map[event.Type]bool, len(r.Events))
			for _, e := range r.Events {
				if r.EffectivePolicy.Allows(e.Type) {
					present[e.Type] = true
				}
			}
			fresh := r.Err == nil && !r.Partial
			d.Metrics.UpdateAvailability(imageName, r.Image.TagOrEmpty(), r.Platform.String(), fresh, present)
			if fresh {
				d.Metrics.SetDigestDrift(imageName, r.Image.TagOrEmpty(), r.Platform.String(), drift != nil)
			}
		}

		if drift != nil {
			for _, item := range drift.items {
				slog.Warn("digest drift",
					"image", drift.image,
					"tag", drift.tag,
					"platform", drift.platform,
					"container", item,
					"served", drift.served,
				)
			}
		}
	}
	if d.Metrics != nil {
		d.Metrics.SetContainers(totalContainers)
		d.Metrics.SetImages(len(results))
	}

	note := BuildNotification(ctx, results, d.Observer.Store)
	slog.Debug("runCycle: notification built", "items", len(note.Items))
	if len(note.Items) > 0 {
		if h, err := d.Observer.Runtime.Hostname(ctx); err == nil {
			note.Hostname = h
		} else {
			slog.Debug("runCycle: hostname resolution failed, notification will lack hostname", "error", err)
		}
		if err := DeliverAndMark(ctx, d.Notifiers, note, d.Config.Notifications.Mode, d.Observer.Store); err != nil {
			slog.Error("notification delivery failed", "error", err)
			if d.Metrics != nil {
				d.Metrics.RecordNotification(err)
			}
		} else if d.Metrics != nil {
			d.Metrics.RecordNotification(nil)
		}
	}

	if d.RegistryOutages != nil && d.Config.Notifications.RegistryOutage.Enabled {
		alerts := d.RegistryOutages.DetectOutages(results, d.Config.Notifications.RegistryOutage.ConsecutiveFailures)
		if len(alerts) > 0 {
			outageNote := notify.Notification{Timestamp: time.Now(), Items: alerts}
			if _, err := Deliver(ctx, d.Notifiers, outageNote); err != nil {
				slog.Error("registry outage notification failed", "error", err)
			}
		}
	}

	if d.Metrics != nil {
		d.Metrics.RecordCheck(time.Since(start), nil)
	}

	slog.Info("check complete",
		"images", len(results),
		"failed", failedImages,
		"events", countEvents(results),
		"notifications", len(note.Items),
		"duration", time.Since(start).Round(time.Millisecond),
	)
}

func countEvents(results []observer.Result) int {
	n := 0
	for _, r := range results {
		n += len(r.Events)
	}
	return n
}

// digestDrift describes the running containers whose digest differs from
// what the registry currently serves for their tag.
type digestDrift struct {
	image    string
	tag      string
	platform string
	served   string
	items    []string // "name=sha256:ab12cd34ef56"
}

// computeDigestDrift reports the containers running a different digest than
// the registry serves for their tag, or nil when there is none. Containers
// whose running digest is unknown don't count as drift.
func computeDigestDrift(r observer.Result) *digestDrift {
	if r.ServedDigest == "" {
		return nil
	}
	d := &digestDrift{
		image:    r.Image.Registry + "/" + r.Image.Repository,
		tag:      r.Image.TagOrEmpty(),
		platform: r.Platform.String(),
		served:   shortDigest(r.ServedDigest),
	}
	for i, name := range r.ContainerNames {
		dig := ""
		if i < len(r.ContainerDigests) {
			dig = r.ContainerDigests[i]
		}
		if dig == "" || dig == r.ServedDigest {
			continue
		}
		d.items = append(d.items, fmt.Sprintf("%s=%s", name, shortDigest(dig)))
	}
	if len(d.items) == 0 {
		return nil
	}
	return d
}

// shortDigest abbreviates a content digest for log output.
func shortDigest(s string) string {
	const prefix = "sha256:"
	if strings.HasPrefix(s, prefix) && len(s) > len(prefix)+12 {
		return s[:len(prefix)+12]
	}
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
