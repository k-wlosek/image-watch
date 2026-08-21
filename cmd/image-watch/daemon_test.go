package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/k-wlosek/image-watch/internal/config"
	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/image"
	"github.com/k-wlosek/image-watch/internal/metrics"
	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/observer"
	"github.com/k-wlosek/image-watch/internal/policy"
	"github.com/k-wlosek/image-watch/internal/registry"
	iwruntime "github.com/k-wlosek/image-watch/internal/runtime"
	"github.com/k-wlosek/image-watch/internal/state"
)

// fakeRuntimeList is a test double for iwruntime.Runtime.
type fakeRuntimeList struct {
	containers []iwruntime.ContainerObservation
	err        error
}

func (f *fakeRuntimeList) Name() string { return "fake" }

func (f *fakeRuntimeList) Hostname(context.Context) (string, error) { return "fake", nil }

func (f *fakeRuntimeList) ListContainers(context.Context) ([]iwruntime.ContainerObservation, error) {
	return f.containers, f.err
}

// fakeRegistryList is a test double for registry.Registry.
type fakeRegistryList struct {
	tags       []string
	digest     string
	resolveErr error
}

func (f *fakeRegistryList) ListTags(_ context.Context, _ string) ([]string, error) {
	return f.tags, nil
}

func (f *fakeRegistryList) Resolve(ctx context.Context, repository, reference string) (registry.ManifestObservation, error) {
	return f.ResolveForPlatform(ctx, repository, reference, image.Platform{})
}

func (f *fakeRegistryList) ResolveForPlatform(_ context.Context, _ string, _ string, _ image.Platform) (registry.ManifestObservation, error) {
	if f.resolveErr != nil {
		return registry.ManifestObservation{}, f.resolveErr
	}
	return registry.ManifestObservation{
		PlatformManifestDigest: f.digest,
		IndexDigest:            f.digest,
	}, nil
}

func testObservation(name, ref, digest string) iwruntime.ContainerObservation {
	r, _ := image.ParseReference(ref)
	return iwruntime.ContainerObservation{
		Runtime:  "fake",
		Name:     name,
		Image:    r,
		Digest:   digest,
		Platform: image.Platform{OS: "linux", Architecture: "amd64"},
	}
}

func newTestObserver(rt iwruntime.Runtime, reg registry.Registry) *observer.Observer {
	return &observer.Observer{
		Runtime:            rt,
		Registries:         func(string) registry.Registry { return reg },
		Store:              state.NewMemoryStore(),
		DefaultPolicy:      policy.Default(),
		ConcurrencyWorkers: 4,
	}
}

// newTestDaemon wires a Daemon around a fake runtime and fake registry.
func newTestDaemon(rt iwruntime.Runtime, reg registry.Registry, m *metrics.Metrics, notifiers []notify.Notifier) *Daemon {
	return &Daemon{
		Config:          config.Default(),
		Observer:        newTestObserver(rt, reg),
		Notifiers:       notifiers,
		Metrics:         m,
		RegistryOutages: NewRegistryOutageTracker(),
	}
}

// recordingNotifier captures notifications thread-safely.
type recordingNotifier struct {
	mu        sync.Mutex
	notified  []notify.Notification
	failAfter int
}

func (r *recordingNotifier) Notify(_ context.Context, n notify.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.notified) > r.failAfter {
		return errors.New("simulated notifier failure")
	}
	r.notified = append(r.notified, n)
	return nil
}

func (r *recordingNotifier) items() []notify.Item {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []notify.Item
	for _, n := range r.notified {
		out = append(out, n.Items...)
	}
	return out
}

func TestLogf_UsesOverride(t *testing.T) {
	var got []string
	d := &Daemon{Logf: func(format string, args ...any) { got = append(got, fmt.Sprintf(format, args...)) }}
	d.logf("hello %s", "world")
	if len(got) != 1 || got[0] != "hello world" {
		t.Errorf("expected override to receive the formatted line, got %v", got)
	}
}

func TestLogf_DefaultsToStdout(t *testing.T) {
	d := &Daemon{}
	d.logf("no override", 1) // must not panic
}

func TestCountEvents(t *testing.T) {
	results := []observer.Result{
		{Events: make([]event.Event, 2)},
		{Events: make([]event.Event, 3)},
		{Events: make([]event.Event, 0)},
	}
	if got := countEvents(results); got != 5 {
		t.Errorf("countEvents = %d, want 5", got)
	}
}

func TestShortDigest(t *testing.T) {
	long := "sha256:" + strings.Repeat("ab", 20) // 7 + 40 chars
	if got := shortDigest(long); got != "sha256:abababababab" {
		t.Errorf("long sha256 digest = %q, want sha256:abababababab", got)
	}
	if got := shortDigest("myregistry.io/" + strings.Repeat("x", 30)); got != "myregistry.i" {
		t.Errorf("non-prefixed long digest = %q, want first 12 chars", got)
	}
	if got := shortDigest("abc"); got != "abc" {
		t.Errorf("short string = %q, want unchanged", got)
	}
	if got := shortDigest("sha256:abc"); got != "sha256:abc" {
		t.Errorf("short sha256 = %q, want unchanged", got)
	}
	if got := shortDigest(""); got != "" {
		t.Errorf("empty = %q, want unchanged", got)
	}
}

func TestComputeDigestDrift(t *testing.T) {
	tag := "1.2.3"
	base := observer.Result{
		Image:            image.Reference{Registry: "ghcr.io", Repository: "acme/foo", Tag: &tag},
		Platform:         image.Platform{OS: "linux", Architecture: "amd64"},
		ServedDigest:     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ContainerNames:   []string{"foo1", "foo2", "foo3", "foo4"},
		ContainerDigests: []string{"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}

	got := computeDigestDrift(base)
	if got == nil {
		t.Fatalf("expected drift to be detected")
	}
	if got.served != "sha256:aaaaaaaaaaaa" {
		t.Errorf("served = %q, want shortened digest", got.served)
	}
	if len(got.items) != 1 || got.items[0] != "foo1=sha256:bbbbbbbbbbbb" {
		t.Errorf("items = %v, want the single drifted container", got.items)
	}

	noServed := base
	noServed.ServedDigest = ""
	if got := computeDigestDrift(noServed); got != nil {
		t.Errorf("expected nil drift when served digest is unknown")
	}

	allMatch := base
	allMatch.ContainerDigests = []string{"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if got := computeDigestDrift(allMatch); got != nil {
		t.Errorf("expected nil drift when every container matches")
	}
}

func TestRunCycle_SuccessfulCycle(t *testing.T) {
	reg := &fakeRegistryList{tags: []string{"1.2.4"}, digest: "sha256:current"}
	rt := &fakeRuntimeList{containers: []iwruntime.ContainerObservation{
		testObservation("foo1", "ghcr.io/acme/foo:1.2.3", "sha256:current"),
	}}
	n := &recordingNotifier{}
	var logs []string
	d := newTestDaemon(rt, reg, metrics.New(), []notify.Notifier{n})
	d.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }

	d.runCycle(context.Background())

	if len(logs) == 0 || !strings.Contains(strings.Join(logs, " "), "check complete: 1 image(s) checked, 0 failed") {
		t.Errorf("expected completion log, got %v", logs)
	}
	items := n.items()
	if len(items) != 1 || items[0].Type != event.PatchAvailable {
		t.Fatalf("expected one PATCH_AVAILABLE notification, got %v", items)
	}
	if items[0].ContainerNames[0] != "foo1" {
		t.Errorf("ContainerNames = %v, want [foo1]", items[0].ContainerNames)
	}
}

func TestRunCycle_CheckFailure(t *testing.T) {
	rt := &fakeRuntimeList{err: errors.New("runtime down")}
	n := &recordingNotifier{}
	var logs []string
	d := newTestDaemon(rt, &fakeRegistryList{}, metrics.New(), []notify.Notifier{n})
	d.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }

	d.runCycle(context.Background())

	joined := strings.Join(logs, " ")
	if !strings.Contains(joined, "check cycle failed") || !strings.Contains(joined, "runtime down") {
		t.Errorf("expected failure log, got %v", logs)
	}
}

func TestRunCycle_DriftLogged(t *testing.T) {
	reg := &fakeRegistryList{tags: []string{"1.2.4"}, digest: "sha256:current"}
	rt := &fakeRuntimeList{containers: []iwruntime.ContainerObservation{
		testObservation("stale1", "ghcr.io/acme/foo:1.2.3", "sha256:old"),
	}}
	n := &recordingNotifier{}
	var logs []string
	d := newTestDaemon(rt, reg, metrics.New(), []notify.Notifier{n})
	d.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }

	d.runCycle(context.Background())

	joined := strings.Join(logs, " ")
	if !strings.Contains(joined, "drift:") || !strings.Contains(joined, "stale1=sha256:old") {
		t.Errorf("expected drift entry in logs, got %v", logs)
	}
}

func TestRunCycle_WithoutMetrics(t *testing.T) {
	reg := &fakeRegistryList{tags: []string{"1.2.4"}, digest: "sha256:current"}
	rt := &fakeRuntimeList{containers: []iwruntime.ContainerObservation{
		testObservation("foo1", "ghcr.io/acme/foo:1.2.3", "sha256:current"),
	}}
	n := &recordingNotifier{}
	d := newTestDaemon(rt, reg, nil, []notify.Notifier{n})

	d.runCycle(context.Background())

	if items := n.items(); len(items) != 1 {
		t.Errorf("expected notification without metrics, got %v", items)
	}
}

func TestRunCycle_NotificationFailure(t *testing.T) {
	reg := &fakeRegistryList{tags: []string{"1.2.4"}, digest: "sha256:current"}
	rt := &fakeRuntimeList{containers: []iwruntime.ContainerObservation{
		testObservation("foo1", "ghcr.io/acme/foo:1.2.3", "sha256:current"),
	}}
	n := &recordingNotifier{failAfter: -1}
	var logs []string
	d := newTestDaemon(rt, reg, metrics.New(), []notify.Notifier{n})
	d.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }

	d.runCycle(context.Background())

	if !strings.Contains(strings.Join(logs, " "), "notification delivery had errors") {
		t.Errorf("expected delivery error log, got %v", logs)
	}
}

func TestRunCycle_RegistryOutageAlert(t *testing.T) {
	reg := &fakeRegistryList{resolveErr: errors.New("401 unauthorized")}
	rt := &fakeRuntimeList{containers: []iwruntime.ContainerObservation{
		testObservation("foo1", "ghcr.io/acme/foo:1.2.3", "sha256:current"),
	}}
	n := &recordingNotifier{}
	d := newTestDaemon(rt, reg, nil, []notify.Notifier{n})
	d.Config.Notifications.RegistryOutage.Enabled = true
	d.Config.Notifications.RegistryOutage.ConsecutiveFailures = 1

	d.runCycle(context.Background())

	items := n.items()
	if len(items) != 1 || items[0].Type != registryOutageEventType {
		t.Fatalf("expected a registry outage alert, got %v", items)
	}
	if items[0].Image != "ghcr.io" {
		t.Errorf("alert Image = %q, want ghcr.io", items[0].Image)
	}
}

func TestRun_CancellationReturnsContextError(t *testing.T) {
	reg := &fakeRegistryList{tags: []string{"1.2.4"}, digest: "sha256:current"}
	rt := &fakeRuntimeList{containers: []iwruntime.ContainerObservation{
		testObservation("foo1", "ghcr.io/acme/foo:1.2.3", "sha256:current"),
	}}
	n := &recordingNotifier{}
	d := newTestDaemon(rt, reg, nil, []notify.Notifier{n})
	d.Config.CheckInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := d.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run error = %v, want context.Canceled", err)
	}
	if items := n.items(); len(items) != 1 {
		t.Errorf("expected the immediate cycle to have run and notified, got %v", items)
	}
}

func TestRun_TickerTriggersSubsequentCycles(t *testing.T) {
	reg := &fakeRegistryList{tags: []string{"1.2.4"}, digest: "sha256:current"}
	rt := &fakeRuntimeList{containers: []iwruntime.ContainerObservation{
		testObservation("foo1", "ghcr.io/acme/foo:1.2.3", "sha256:current"),
	}}
	n := &recordingNotifier{}
	var logs []string
	d := newTestDaemon(rt, reg, nil, []notify.Notifier{n})
	d.Config.CheckInterval = 5 * time.Millisecond
	d.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()

	if err := d.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}

	cycles := 0
	for _, l := range logs {
		if strings.Contains(l, "check complete") {
			cycles++
		}
	}
	if cycles < 2 {
		t.Errorf("expected at least 2 cycles (immediate + ticker), got %d: %v", cycles, logs)
	}
}

func TestRunCycle_OutageDeliveryFailure(t *testing.T) {
	reg := &fakeRegistryList{resolveErr: errors.New("401 unauthorized")}
	rt := &fakeRuntimeList{containers: []iwruntime.ContainerObservation{
		testObservation("foo1", "ghcr.io/acme/foo:1.2.3", "sha256:current"),
	}}
	n := &recordingNotifier{failAfter: -1}
	var logs []string
	d := newTestDaemon(rt, reg, nil, []notify.Notifier{n})
	d.Config.Notifications.RegistryOutage.Enabled = true
	d.Config.Notifications.RegistryOutage.ConsecutiveFailures = 1
	d.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }

	d.runCycle(context.Background())

	joined := strings.Join(logs, " ")
	if !strings.Contains(joined, "registry outage notification failed") {
		t.Errorf("expected outage delivery failure log, got %v", logs)
	}
}
