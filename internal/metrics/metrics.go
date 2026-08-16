// Package metrics implements the Prometheus /metrics endpoint.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/example/image-watch/internal/event"
)

// trackedEventTypes is the fixed set of event types tracked per image.
var trackedEventTypes = []event.Type{
	event.PatchAvailable,
	event.MinorAvailable,
	event.MajorAvailable,
	event.FamilyAdvancementAvailable,
	event.ApplicationPatchAvailable,
	event.ApplicationMinorAvailable,
	event.ApplicationMajorAvailable,
	event.BaseAdvancementAvailable,
	event.TagChanged,
	event.TagMutated,
	event.OtherPlatformUpdate,
}

// Metrics holds the process metrics.
type Metrics struct {
	Registry *prometheus.Registry

	ChecksTotal      prometheus.Counter
	CheckErrorsTotal prometheus.Counter
	CheckDuration    prometheus.Gauge // seconds; most recent cycle

	Containers prometheus.Gauge
	Images     prometheus.Gauge

	// UpdatesAvailable and ObservationStale track stale observations.
	UpdatesAvailable *prometheus.GaugeVec // labels: image, type
	ObservationStale *prometheus.GaugeVec // labels: image

	NotificationsTotal      prometheus.Counter
	NotificationErrorsTotal prometheus.Counter

	RegistryRequestsTotal          *prometheus.CounterVec // labels: registry
	RegistryErrorsTotal            *prometheus.CounterVec // labels: registry
	RegistryRequestDurationSeconds *prometheus.GaugeVec   // labels: registry; most recent request

	// Enrichment counters.
	EnrichmentAttemptsTotal prometheus.Counter
	EnrichmentSuccessTotal  prometheus.Counter
	EnrichmentFailuresTotal prometheus.Counter
}

// New constructs a Metrics with its own registry.
func New() *Metrics {
	m := &Metrics{
		Registry: prometheus.NewRegistry(),

		ChecksTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "image_watch_checks_total",
			Help: "Total number of check cycles performed.",
		}),
		CheckErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "image_watch_check_errors_total",
			Help: "Total number of check cycles that failed entirely (e.g. the container runtime was unavailable).",
		}),
		CheckDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "image_watch_check_duration_seconds",
			Help: "Duration of the most recent check cycle, in seconds.",
		}),

		Containers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "image_watch_containers",
			Help: "Number of running containers observed in the most recent check.",
		}),
		Images: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "image_watch_images",
			Help: "Number of unique monitored images in the most recent check.",
		}),

		UpdatesAvailable: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "image_watch_updates_available",
			Help: "Whether an update is currently known to be available (1) or not (0), per image and event type. Retains its last-known value during a registry outage rather than dropping to 0 -- see image_watch_observation_stale.",
		}, []string{"image", "type"}),
		ObservationStale: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "image_watch_observation_stale",
			Help: "Whether the most recent check for this image failed (1) or succeeded (0).",
		}, []string{"image"}),

		NotificationsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "image_watch_notifications_total",
			Help: "Total number of notifications delivered.",
		}),
		NotificationErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "image_watch_notification_errors_total",
			Help: "Total number of notification delivery failures.",
		}),

		RegistryRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "image_watch_registry_requests_total",
			Help: "Total number of requests made to each registry host.",
		}, []string{"registry"}),
		RegistryErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "image_watch_registry_errors_total",
			Help: "Total number of failed requests to each registry host.",
		}, []string{"registry"}),
		RegistryRequestDurationSeconds: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "image_watch_registry_request_duration_seconds",
			Help: "Duration of the most recent request to each registry host, in seconds.",
		}, []string{"registry"}),

		EnrichmentAttemptsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "image_watch_enrichment_attempts_total",
			Help: "Total number of opportunistic latest-tag enrichment attempts.",
		}),
		EnrichmentSuccessTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "image_watch_enrichment_success_total",
			Help: "Total number of enrichment attempts that found a matching version.",
		}),
		EnrichmentFailuresTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "image_watch_enrichment_failures_total",
			Help: "Total number of enrichment attempts that did not find a matching version within budget.",
		}),
	}

	m.Registry.MustRegister(
		m.ChecksTotal,
		m.CheckErrorsTotal,
		m.CheckDuration,
		m.Containers,
		m.Images,
		m.UpdatesAvailable,
		m.ObservationStale,
		m.NotificationsTotal,
		m.NotificationErrorsTotal,
		m.RegistryRequestsTotal,
		m.RegistryErrorsTotal,
		m.RegistryRequestDurationSeconds,
		m.EnrichmentAttemptsTotal,
		m.EnrichmentSuccessTotal,
		m.EnrichmentFailuresTotal,
	)

	return m
}

// Handler returns an http.Handler serving metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

// RecordCheck records the outcome of one observer.Check call.
func (m *Metrics) RecordCheck(duration time.Duration, err error) {
	m.ChecksTotal.Inc()
	if err != nil {
		m.CheckErrorsTotal.Inc()
	}
	m.CheckDuration.Set(duration.Seconds())
}

// SetContainers sets the containers gauge.
func (m *Metrics) SetContainers(n int) {
	m.Containers.Set(float64(n))
}

// SetImages sets the images gauge.
func (m *Metrics) SetImages(n int) {
	m.Images.Set(float64(n))
}

// UpdateAvailability records which tracked event types were present.
func (m *Metrics) UpdateAvailability(image string, fresh bool, present map[event.Type]bool) {
	if !fresh {
		m.ObservationStale.WithLabelValues(image).Set(1)
		return
	}
	m.ObservationStale.WithLabelValues(image).Set(0)
	for _, t := range trackedEventTypes {
		v := 0.0
		if present[t] {
			v = 1
		}
		m.UpdatesAvailable.WithLabelValues(image, string(t)).Set(v)
	}
}

// RecordNotification records one notification delivery attempt.
func (m *Metrics) RecordNotification(err error) {
	m.NotificationsTotal.Inc()
	if err != nil {
		m.NotificationErrorsTotal.Inc()
	}
}

// RecordRegistryRequest records one registry HTTP request.
func (m *Metrics) RecordRegistryRequest(host string, duration time.Duration, err error) {
	m.RegistryRequestsTotal.WithLabelValues(host).Inc()
	if err != nil {
		m.RegistryErrorsTotal.WithLabelValues(host).Inc()
	}
	m.RegistryRequestDurationSeconds.WithLabelValues(host).Set(duration.Seconds())
}

// RecordEnrichment records one enrichment attempt.
func (m *Metrics) RecordEnrichment(success bool) {
	m.EnrichmentAttemptsTotal.Inc()
	if success {
		m.EnrichmentSuccessTotal.Inc()
	} else {
		m.EnrichmentFailuresTotal.Inc()
	}
}
