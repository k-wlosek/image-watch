package metrics

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/example/image-watch/internal/event"
)

// scrape renders the current metrics exactly as an HTTP scrape would.
func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handler returned status %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

var (
	unlabeledLineRe = regexp.MustCompile(`(?m)^([a-zA-Z_:][a-zA-Z0-9_:]*)\s+(\S+)\s*$`)
	labeledLineRe   = regexp.MustCompile(`(?m)^([a-zA-Z_:][a-zA-Z0-9_:]*)\{([^}]*)\}\s+(\S+)\s*$`)
)

// scalarValue extracts the value of an unlabeled (plain Counter/Gauge)
// metric from scraped text.
func scalarValue(t *testing.T, body, name string) float64 {
	t.Helper()
	for _, m := range unlabeledLineRe.FindAllStringSubmatch(body, -1) {
		if m[1] == name {
			v, err := strconv.ParseFloat(m[2], 64)
			if err != nil {
				t.Fatalf("failed to parse value %q for %s: %v", m[2], name, err)
			}
			return v
		}
	}
	t.Fatalf("metric %q not found as an unlabeled sample in scraped output:\n%s", name, body)
	return 0
}

// labeledValue extracts the value of a labeled (Vec) metric sample whose
// label set exactly matches want.
func labeledValue(t *testing.T, body, name string, want map[string]string) (value float64, ok bool) {
	t.Helper()
	for _, m := range labeledLineRe.FindAllStringSubmatch(body, -1) {
		if m[1] != name {
			continue
		}
		if !labelsMatch(m[2], want) {
			continue
		}
		v, err := strconv.ParseFloat(m[3], 64)
		if err != nil {
			t.Fatalf("failed to parse value %q for %s{%s}: %v", m[3], name, m[2], err)
		}
		return v, true
	}
	return 0, false
}

func labelsMatch(raw string, want map[string]string) bool {
	got := make(map[string]string)
	for part := range strings.SplitSeq(raw, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		got[key] = val
	}
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

func TestNew_UnlabeledMetricsAlwaysPresent(t *testing.T) {
	m := New()
	body := scrape(t, m)

	names := []string{
		"image_watch_checks_total",
		"image_watch_check_errors_total",
		"image_watch_check_duration_seconds",
		"image_watch_containers",
		"image_watch_images",
		"image_watch_notifications_total",
		"image_watch_notification_errors_total",
		"image_watch_enrichment_attempts_total",
		"image_watch_enrichment_success_total",
		"image_watch_enrichment_failures_total",
	}
	for _, name := range names {
		if !strings.Contains(body, "# TYPE "+name+" ") {
			t.Errorf("expected %s to be present in a fresh scrape, got body:\n%s", name, body)
		}
	}
}

func TestVecMetrics_AbsentUntilTouched(t *testing.T) {
	m := New()
	body := scrape(t, m)

	vecNames := []string{
		"image_watch_updates_available",
		"image_watch_observation_stale",
		"image_watch_digest_drift",
		"image_watch_registry_requests_total",
		"image_watch_registry_errors_total",
		"image_watch_registry_request_duration_seconds",
	}
	for _, name := range vecNames {
		if strings.Contains(body, name) {
			t.Errorf("expected %s to be entirely absent from a fresh scrape (no label combinations touched yet), but found it in:\n%s", name, body)
		}
	}
}

func TestRecordCheck(t *testing.T) {
	m := New()
	m.RecordCheck(2*time.Second, nil)
	m.RecordCheck(3*time.Second, nil)
	m.RecordCheck(1*time.Second, errors.New("boom"))

	body := scrape(t, m)

	if got := scalarValue(t, body, "image_watch_checks_total"); got != 3 {
		t.Errorf("checks_total = %v, want 3", got)
	}
	if got := scalarValue(t, body, "image_watch_check_errors_total"); got != 1 {
		t.Errorf("check_errors_total = %v, want 1", got)
	}
	if got := scalarValue(t, body, "image_watch_check_duration_seconds_count"); got != 3 {
		t.Errorf("check_duration_seconds_count = %v, want 3 (one sample per check)", got)
	}
	if got := scalarValue(t, body, "image_watch_check_duration_seconds_sum"); got != 6 {
		t.Errorf("check_duration_seconds_sum = %v, want 6 (2+3+1 seconds)", got)
	}
}

func TestSetContainersAndImages(t *testing.T) {
	m := New()
	m.SetContainers(12)
	m.SetImages(5)

	body := scrape(t, m)
	if got := scalarValue(t, body, "image_watch_containers"); got != 12 {
		t.Errorf("containers = %v, want 12", got)
	}
	if got := scalarValue(t, body, "image_watch_images"); got != 5 {
		t.Errorf("images = %v, want 5", got)
	}
}

func TestUpdateAvailability_FreshCheckSetsExactState(t *testing.T) {
	m := New()
	m.UpdateAvailability("docker.io/library/foo", "1.4", "linux/amd64", true, map[event.Type]bool{
		event.PatchAvailable: true,
	})

	body := scrape(t, m)

	patchVal, ok := labeledValue(t, body, "image_watch_updates_available", map[string]string{
		"image": "docker.io/library/foo", "tag": "1.4", "platform": "linux/amd64", "type": string(event.PatchAvailable),
	})
	if !ok || patchVal != 1 {
		t.Errorf("expected updates_available for PATCH_AVAILABLE = 1, got ok=%v val=%v", ok, patchVal)
	}

	minorVal, ok := labeledValue(t, body, "image_watch_updates_available", map[string]string{
		"image": "docker.io/library/foo", "tag": "1.4", "platform": "linux/amd64", "type": string(event.MinorAvailable),
	})
	if !ok || minorVal != 0 {
		t.Errorf("expected updates_available for MINOR_AVAILABLE (not present this cycle) = 0, got ok=%v val=%v", ok, minorVal)
	}

	staleVal, ok := labeledValue(t, body, "image_watch_observation_stale", map[string]string{"image": "docker.io/library/foo", "tag": "1.4", "platform": "linux/amd64"})
	if !ok || staleVal != 0 {
		t.Errorf("expected observation_stale = 0 for a fresh successful check, got ok=%v val=%v", ok, staleVal)
	}
}

func TestUpdateAvailability_StaleCheckRetainsLastKnownValue(t *testing.T) {
	m := New()
	image := "docker.io/library/foo"

	m.UpdateAvailability(image, "1.4", "linux/amd64", true, map[event.Type]bool{event.PatchAvailable: true})
	m.UpdateAvailability(image, "1.4", "linux/amd64", false, nil)

	body := scrape(t, m)

	patchVal, ok := labeledValue(t, body, "image_watch_updates_available", map[string]string{
		"image": image, "tag": "1.4", "platform": "linux/amd64", "type": string(event.PatchAvailable),
	})
	if !ok || patchVal != 1 {
		t.Errorf("expected updates_available for PATCH_AVAILABLE to remain 1 after a failed check, got ok=%v val=%v", ok, patchVal)
	}

	staleVal, ok := labeledValue(t, body, "image_watch_observation_stale", map[string]string{"image": image, "tag": "1.4", "platform": "linux/amd64"})
	if !ok || staleVal != 1 {
		t.Errorf("expected observation_stale = 1 after a failed check, got ok=%v val=%v", ok, staleVal)
	}
}

func TestUpdateAvailability_FreshCheckClearsResolvedUpdate(t *testing.T) {
	m := New()
	image := "docker.io/library/foo"

	m.UpdateAvailability(image, "1.4", "linux/amd64", true, map[event.Type]bool{event.PatchAvailable: true})
	m.UpdateAvailability(image, "1.4", "linux/amd64", true, map[event.Type]bool{}) // resolved; nothing present now

	body := scrape(t, m)
	patchVal, ok := labeledValue(t, body, "image_watch_updates_available", map[string]string{
		"image": image, "tag": "1.4", "platform": "linux/amd64", "type": string(event.PatchAvailable),
	})
	if !ok || patchVal != 0 {
		t.Errorf("expected a resolved update to drop to 0 on a successful check, got ok=%v val=%v", ok, patchVal)
	}
}

func TestRecordNotification(t *testing.T) {
	m := New()
	m.RecordNotification(nil)
	m.RecordNotification(nil)
	m.RecordNotification(errors.New("delivery failed"))

	body := scrape(t, m)
	if got := scalarValue(t, body, "image_watch_notifications_total"); got != 3 {
		t.Errorf("notifications_total = %v, want 3", got)
	}
	if got := scalarValue(t, body, "image_watch_notification_errors_total"); got != 1 {
		t.Errorf("notification_errors_total = %v, want 1", got)
	}
}

func TestRecordRegistryRequest(t *testing.T) {
	m := New()
	m.RecordRegistryRequest("ghcr.io", 250*time.Millisecond, nil)
	m.RecordRegistryRequest("ghcr.io", 100*time.Millisecond, errors.New("401"))
	m.RecordRegistryRequest("docker.io", 50*time.Millisecond, nil)

	body := scrape(t, m)

	if got, ok := labeledValue(t, body, "image_watch_registry_requests_total", map[string]string{"registry": "ghcr.io"}); !ok || got != 2 {
		t.Errorf("ghcr.io requests_total = %v (ok=%v), want 2", got, ok)
	}
	if got, ok := labeledValue(t, body, "image_watch_registry_errors_total", map[string]string{"registry": "ghcr.io"}); !ok || got != 1 {
		t.Errorf("ghcr.io errors_total = %v (ok=%v), want 1", got, ok)
	}
	if got, ok := labeledValue(t, body, "image_watch_registry_requests_total", map[string]string{"registry": "docker.io"}); !ok || got != 1 {
		t.Errorf("docker.io requests_total = %v (ok=%v), want 1", got, ok)
	}
	if _, ok := labeledValue(t, body, "image_watch_registry_errors_total", map[string]string{"registry": "docker.io"}); ok {
		t.Errorf("expected no errors_total sample for docker.io (never errored) -- CounterVec should not fabricate an untouched series")
	}
	// Duration is a histogram: every request is observed.
	if got, ok := labeledValue(t, body, "image_watch_registry_request_duration_seconds_count", map[string]string{"registry": "ghcr.io"}); !ok || got != 2 {
		t.Errorf("ghcr.io request_duration_seconds_count = %v (ok=%v), want 2", got, ok)
	}
	if got, ok := labeledValue(t, body, "image_watch_registry_request_duration_seconds_sum", map[string]string{"registry": "ghcr.io"}); !ok || got != 0.35 {
		t.Errorf("ghcr.io request_duration_seconds_sum = %v (ok=%v), want 0.35 (0.25+0.1)", got, ok)
	}
}

func TestDigestDrift(t *testing.T) {
	m := New()
	m.SetDigestDrift("docker.io/library/pg", "15", "linux/arm64", true)

	body := scrape(t, m)
	if v, ok := labeledValue(t, body, "image_watch_digest_drift", map[string]string{"image": "docker.io/library/pg", "tag": "15", "platform": "linux/arm64"}); !ok || v != 1 {
		t.Errorf("expected digest_drift = 1 while running a different digest, got ok=%v val=%v", ok, v)
	}

	m.SetDigestDrift("docker.io/library/pg", "15", "linux/arm64", false)
	body = scrape(t, m)
	if v, ok := labeledValue(t, body, "image_watch_digest_drift", map[string]string{"image": "docker.io/library/pg", "tag": "15", "platform": "linux/arm64"}); !ok || v != 0 {
		t.Errorf("expected digest_drift = 0 once the running digest matches the registry, got ok=%v val=%v", ok, v)
	}
}

func TestRecordEnrichment(t *testing.T) {
	m := New()
	m.RecordEnrichment(true)
	m.RecordEnrichment(true)
	m.RecordEnrichment(false)

	body := scrape(t, m)
	if got := scalarValue(t, body, "image_watch_enrichment_attempts_total"); got != 3 {
		t.Errorf("enrichment_attempts_total = %v, want 3", got)
	}
	if got := scalarValue(t, body, "image_watch_enrichment_success_total"); got != 2 {
		t.Errorf("enrichment_success_total = %v, want 2", got)
	}
	if got := scalarValue(t, body, "image_watch_enrichment_failures_total"); got != 1 {
		t.Errorf("enrichment_failures_total = %v, want 1", got)
	}
}

func TestHandler_ContentType(t *testing.T) {
	m := New()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want a text/plain prefix", ct)
	}
}

func TestUpdateAvailability_DistinctStreamsDoNotClobber(t *testing.T) {
	m := New()
	// Two tag streams of the same repository: the exact postgres:15 +
	// postgres:15-alpine shape that used to clobber (last writer wins).
	m.UpdateAvailability("docker.io/library/pg", "15", "linux/arm64", true, map[event.Type]bool{
		event.MajorAvailable: true,
	})
	m.UpdateAvailability("docker.io/library/pg", "15-alpine", "linux/arm64", true, map[event.Type]bool{
		event.ApplicationMajorAvailable: true,
	})

	body := scrape(t, m)

	majorVal, ok := labeledValue(t, body, "image_watch_updates_available", map[string]string{
		"image": "docker.io/library/pg", "tag": "15", "platform": "linux/arm64", "type": string(event.MajorAvailable),
	})
	if !ok || majorVal != 1 {
		t.Errorf("tag 15 MAJOR_AVAILABLE = %v (ok=%v), want 1", majorVal, ok)
	}

	appMajorVal, ok := labeledValue(t, body, "image_watch_updates_available", map[string]string{
		"image": "docker.io/library/pg", "tag": "15-alpine", "platform": "linux/arm64", "type": string(event.ApplicationMajorAvailable),
	})
	if !ok || appMajorVal != 1 {
		t.Errorf("tag 15-alpine APPLICATION_MAJOR_AVAILABLE = %v (ok=%v), want 1 -- must not be clobbered by the plain stream", appMajorVal, ok)
	}

	// The two streams must not leak into each other's series.
	if val, ok := labeledValue(t, body, "image_watch_updates_available", map[string]string{
		"image": "docker.io/library/pg", "tag": "15-alpine", "platform": "linux/arm64", "type": string(event.MajorAvailable),
	}); ok && val == 1 {
		t.Errorf("tag 15-alpine must not report plain MAJOR_AVAILABLE (application-only stream)")
	}
	if val, ok := labeledValue(t, body, "image_watch_updates_available", map[string]string{
		"image": "docker.io/library/pg", "tag": "15", "platform": "linux/arm64", "type": string(event.ApplicationMajorAvailable),
	}); ok && val == 1 {
		t.Errorf("tag 15 must not report APPLICATION_MAJOR_AVAILABLE (plain-only stream)")
	}
}

func TestNoCandidateOrDigestValuesInLabels(t *testing.T) {
	m := New()
	m.RecordRegistryRequest("ghcr.io", time.Second, nil)
	m.UpdateAvailability("docker.io/library/foo", "15-alpine", "linux/arm64", true, map[event.Type]bool{
		event.ApplicationMajorAvailable: true,
	})

	body := scrape(t, m)

	// The running tag is a bounded label (it mirrors observer.groupKey, so it
	// is limited to tags actually being monitored) and is expected in output.
	if !strings.Contains(body, `tag="15-alpine"`) {
		t.Errorf("expected the running tag to appear as a bounded label:\n%s", body)
	}
	// Digest and candidate values are unbounded and must never appear as labels.
	for _, s := range []string{"sha256:", "18.6-alpine", "v18.6", "2.0.0"} {
		if strings.Contains(body, s) {
			t.Errorf("scraped output unexpectedly contains %q -- digest/candidate values must never be labels:\n%s", s, body)
		}
	}
}
