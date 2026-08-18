package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/example/image-watch/internal/config"
	"github.com/example/image-watch/internal/event"
	"github.com/example/image-watch/internal/metrics"
	"github.com/example/image-watch/internal/notify"
	"github.com/example/image-watch/internal/observer"
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

	// Logf receives operational log lines.
	Logf func(format string, args ...any)
}

func (d *Daemon) logf(format string, args ...any) {
	if d.Logf != nil {
		d.Logf(format, args...)
		return
	}
	fmt.Printf(format+"\n", args...)
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

	results, err := d.Observer.Check(ctx)
	if err != nil {
		d.logf("check cycle failed: %v", err)
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
				d.logf("drift: %s:%s (%s) container %s running, registry serves %s", drift.image, drift.tag, drift.platform, item, drift.served)
			}
		}
	}
	if d.Metrics != nil {
		d.Metrics.SetContainers(totalContainers)
		d.Metrics.SetImages(len(results))
	}

	note := BuildNotification(ctx, results, d.Observer.Store)
	if len(note.Items) > 0 {
		if err := DeliverAndMark(ctx, d.Notifiers, note, d.Config.Notifications.Mode, d.Observer.Store); err != nil {
			d.logf("notification delivery had errors: %v", err)
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
				d.logf("registry outage notification failed: %v", err)
			}
		}
	}

	if d.Metrics != nil {
		d.Metrics.RecordCheck(time.Since(start), nil)
	}

	d.logf(
		"check complete: %d image(s) checked, %d failed, %d event(s) detected, %d notification(s) sent, took %s",
		len(results), failedImages, countEvents(results), len(note.Items), time.Since(start).Round(time.Millisecond),
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
