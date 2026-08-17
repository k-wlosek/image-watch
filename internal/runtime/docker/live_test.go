//go:build live

package docker_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/example/image-watch/internal/event"
	"github.com/example/image-watch/internal/observer"
	"github.com/example/image-watch/internal/registry"
	"github.com/example/image-watch/internal/registry/distribution"
	docker "github.com/example/image-watch/internal/runtime/docker"
	"github.com/example/image-watch/internal/state"
	"github.com/example/image-watch/internal/version"
)

// Live suite against a real Docker daemon. The fixture containers are
// provisioned (and cleaned up) by `make test-live`

const (
	liveFixtureLabel = "image-watch.live-fixture"
	liveFixtureValue = "true"
)

// fixture describes one container the Makefile provisions and what the
// detection pipeline is expected to report for it.
type fixture struct {
	name   string // container name
	repo   string // expected repository, e.g. library/nginx
	tag    string // expected tag
	expect eventKind
}

type eventKind int

const (
	kindOpaque    eventKind = iota // opaque tag: no version events
	kindVersioned                  // precise version: >=1 patch/minor/major candidate
	kindBoth                       // imprecise major with both a newer major and same-major point releases
	kindFamily                     // imprecise major with same-major point releases and no newer major
	kindComposite                  // composite tag: >=1 APPLICATION_* candidate
)

var liveFixtures = []fixture{
	{name: "iw-live-latest", repo: "library/alpine", tag: "latest", expect: kindOpaque},
	{name: "iw-live-pinned", repo: "library/nginx", tag: "1.28.2", expect: kindVersioned},
	// postgres:15 sits on a published major line (15.x point releases) while
	// newer majors exist, so it must surface both a FAMILY_ADVANCEMENT and a
	// MAJOR candidate -- the realistic postgres:16 case from Scenario 5.
	{name: "iw-live-family-pg", repo: "library/postgres", tag: "15", expect: kindBoth},
	// nginx:1 is a perpetual 1.x line with point releases but no newer major,
	// so it must surface FAMILY_ADVANCEMENT and no MAJOR.
	{name: "iw-live-family", repo: "library/nginx", tag: "1", expect: kindFamily},
	{name: "iw-live-composite", repo: "library/postgres", tag: "15-alpine", expect: kindComposite},
}

func newLiveRuntime(t *testing.T) *docker.Client {
	t.Helper()
	c, err := docker.New("") // default unix socket
	if err != nil {
		t.Fatalf("docker.New: %v", err)
	}
	return c
}

// hostArch returns the architecture the fixture containers should run under,
// matching the Docker daemon VM to the Go toolchain.
func hostArch() string {
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return runtime.GOARCH
	default:
		return ""
	}
}

func TestLive_ListContainers_DaemonReachable(t *testing.T) {
	c := newLiveRuntime(t)
	if _, err := c.ListContainers(context.Background()); err != nil {
		t.Fatalf("ListContainers against the default socket failed: %v", err)
	}
}

// TestLive_FixtureObservations checks the adapter-level plumbing for every
// provisioned fixture: labels, parsed reference, digest, and platform.
func TestLive_FixtureObservations(t *testing.T) {
	c := newLiveRuntime(t)
	observations, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}

	seen := make(map[string]bool)
	for _, obs := range observations {
		if obs.Labels[liveFixtureLabel] != liveFixtureValue {
			continue
		}
		seen[obs.Name] = true

		if obs.Image.Registry != "docker.io" {
			t.Errorf("%s: registry = %q, want docker.io", obs.Name, obs.Image.Registry)
		}
		if obs.Image.TagOrEmpty() == "" {
			t.Errorf("%s: expected a tag, got reference %q", obs.Name, obs.Image.String())
		}
		if !strings.HasPrefix(obs.Digest, "sha256:") {
			t.Errorf("%s: digest = %q, want a sha256: digest (RepoDigest match)", obs.Name, obs.Digest)
		}
		if obs.Platform.OS != "linux" {
			t.Errorf("%s: platform OS = %q, want linux", obs.Name, obs.Platform.OS)
		}
		if wa := hostArch(); wa != "" && obs.Platform.Architecture != wa {
			t.Errorf("%s: platform arch = %q, want %q", obs.Name, obs.Platform.Architecture, wa)
		}
	}

	for _, f := range liveFixtures {
		if !seen[f.name] {
			t.Errorf("fixture %s (%s:%s) was not observed on the daemon", f.name, f.repo, f.tag)
		}
	}
}

// TestLive_ObserverScenarios runs the full detection pipeline against the
// live daemon and the live registry and checks the per-fixture event
// expectations.
func TestLive_ObserverScenarios(t *testing.T) {
	c := newLiveRuntime(t)
	obs := &observer.Observer{
		Runtime: c,
		Registries: func(host string) registry.Registry {
			return distribution.New(host, nil, distribution.NoCredentials)
		},
		Store: state.NewMemoryStore(),
	}

	results, err := obs.Check(context.Background())
	if err != nil {
		t.Fatalf("Observer.Check: %v", err)
	}

	byName := make(map[string]observer.Result)
	for _, r := range results {
		for _, name := range r.ContainerNames {
			if strings.HasPrefix(name, "iw-live-") {
				byName[name] = r
			}
		}
	}

	for _, f := range liveFixtures {
		r, ok := byName[f.name]
		if !ok {
			t.Errorf("no observer result for fixture %s (%s:%s)", f.name, f.repo, f.tag)
			continue
		}
		if r.Err != nil {
			t.Errorf("fixture %s: unexpected result error: %v", f.name, r.Err)
			continue
		}
		if r.Image.Repository != f.repo || r.Image.TagOrEmpty() != f.tag {
			t.Errorf("fixture %s: result image = %s:%s, want %s:%s",
				f.name, r.Image.Repository, r.Image.TagOrEmpty(), f.repo, f.tag)
		}
		if r.Platform.OS != "linux" {
			t.Errorf("fixture %s: result platform OS = %q, want linux", f.name, r.Platform.OS)
		}
		if wa := hostArch(); wa != "" && r.Platform.Architecture != wa {
			t.Errorf("fixture %s: result platform arch = %q, want %q", f.name, r.Platform.Architecture, wa)
		}

		for _, e := range r.Events {
			switch e.Type {
			case event.PatchAvailable, event.MinorAvailable, event.MajorAvailable,
				event.FamilyAdvancementAvailable,
				event.ApplicationPatchAvailable, event.ApplicationMinorAvailable, event.ApplicationMajorAvailable,
				event.BaseAdvancementAvailable:
				if e.CandidateTag == "" || e.CandidateTag == f.tag {
					t.Errorf("fixture %s: event %s has an empty or unchanged candidate tag %q",
						f.name, e.Type, e.CandidateTag)
				}
			}
		}

		present := make(map[event.Type]bool)
		for _, e := range r.Events {
			present[e.Type] = true
		}

		switch f.expect {
		case kindOpaque:
			if len(r.Events) != 0 {
				t.Errorf("fixture %s: opaque tag %s must not produce events, got %v", f.name, f.tag, eventTypes(r.Events))
			}
		case kindVersioned:
			if !present[event.PatchAvailable] && !present[event.MinorAvailable] && !present[event.MajorAvailable] {
				t.Errorf("fixture %s: %s should expose a patch/minor/major candidate, got %v",
					f.name, f.tag, eventTypes(r.Events))
			}
		case kindBoth:
			if !present[event.MajorAvailable] {
				t.Errorf("fixture %s: %s should expose MAJOR_AVAILABLE (newer major exists), got %v",
					f.name, f.tag, eventTypes(r.Events))
			}
			if !present[event.FamilyAdvancementAvailable] {
				t.Errorf("fixture %s: %s should expose FAMILY_ADVANCEMENT_AVAILABLE (same-major point releases exist), got %v",
					f.name, f.tag, eventTypes(r.Events))
			}
			if ev := candidateOf(r.Events, event.MajorAvailable); ev != nil {
				if v, err := version.ParseSemVer(ev.CandidateTag); err == nil && v.Major <= 15 {
					t.Errorf("fixture %s: MAJOR_AVAILABLE candidate %q does not advance past major 15",
						f.name, ev.CandidateTag)
				}
			}
			if ev := candidateOf(r.Events, event.FamilyAdvancementAvailable); ev != nil {
				if v, err := version.ParseSemVer(ev.CandidateTag); err == nil && v.Major != 15 {
					t.Errorf("fixture %s: FAMILY_ADVANCEMENT candidate %q must stay on major 15",
						f.name, ev.CandidateTag)
				}
			}
		case kindFamily:
			if !present[event.FamilyAdvancementAvailable] {
				t.Errorf("fixture %s: %s should expose FAMILY_ADVANCEMENT_AVAILABLE, got %v",
					f.name, f.tag, eventTypes(r.Events))
			}
			if present[event.MajorAvailable] {
				t.Errorf("fixture %s: %s must not expose MAJOR_AVAILABLE (no newer major on nginx's perpetual 1.x line), got %v",
					f.name, f.tag, eventTypes(r.Events))
			}
		case kindComposite:
			if !present[event.ApplicationPatchAvailable] && !present[event.ApplicationMinorAvailable] && !present[event.ApplicationMajorAvailable] {
				t.Errorf("fixture %s: %s should expose an APPLICATION_* candidate, got %v",
					f.name, f.tag, eventTypes(r.Events))
			}
		}
	}
}

func eventTypes(events []event.Event) []event.Type {
	out := make([]event.Type, 0, len(events))
	for _, e := range events {
		out = append(out, e.Type)
	}
	return out
}

func candidateOf(events []event.Event, t event.Type) *event.Event {
	for i := range events {
		if events[i].Type == t {
			return &events[i]
		}
	}
	return nil
}
