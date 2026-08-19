package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/image"
	"github.com/k-wlosek/image-watch/internal/observer"
	"github.com/k-wlosek/image-watch/internal/policy"
)

func writeConfigForMain(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMAGE_WATCH_CONFIG_PATH", path)
	return path
}

func TestRunHealthcheck_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	listen := strings.TrimPrefix(srv.URL, "http://")
	writeConfigForMain(t, "metrics:\n  listen: "+listen+"\n")

	if code := runHealthcheck(); code != 0 {
		t.Errorf("runHealthcheck = %d, want 0", code)
	}
}

func TestRunHealthcheck_Unhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	listen := strings.TrimPrefix(srv.URL, "http://")
	writeConfigForMain(t, "metrics:\n  listen: "+listen+"\n")

	if code := runHealthcheck(); code != 1 {
		t.Errorf("runHealthcheck = %d, want 1", code)
	}
}

func TestRunHealthcheck_RequestFailure(t *testing.T) {
	writeConfigForMain(t, "metrics:\n  listen: 127.0.0.1:1\n")
	if code := runHealthcheck(); code != 1 {
		t.Errorf("runHealthcheck = %d, want 1", code)
	}
}

func TestRunHealthcheck_ConfigError(t *testing.T) {
	writeConfigForMain(t, ": : :\n  invalid\n")
	if code := runHealthcheck(); code != 1 {
		t.Errorf("runHealthcheck = %d, want 1 (config error)", code)
	}
}

func TestPrintResultAndPrintEvent(t *testing.T) {
	tag := "1.2.3"
	digest := "sha256:aaa"
	candidate := "1.3.0"

	ok := observer.Result{
		Image:           image.Reference{Registry: "ghcr.io", Repository: "acme/foo", Tag: &tag},
		Platform:        image.Platform{OS: "linux", Architecture: "amd64"},
		EffectivePolicy: policy.Policy{},
		ContainerNames:  []string{"c1"},
		Events: []event.Event{
			{Type: event.TagChanged, CurrentTag: "latest", CandidateTag: "1.2.4", CurrentDigest: "sha256:aaa", CandidateDigest: "sha256:bbb"},
			{Type: event.TagMutated, CurrentTag: "latest", CandidateTag: "", CurrentDigest: "sha256:aaa", CandidateDigest: "sha256:ccc"},
			{Type: event.PatchAvailable, CurrentTag: "1.2.3", CandidateTag: "1.2.4", CombinedCandidate: "1.2.3+upstream"},
			{Type: event.MajorAvailable, CurrentTag: "1.2.3", CandidateTag: "2.0.0"},
		},
	}
	printResult(ok)

	withErr := ok
	withErr.Err = errors.New("registry down")
	printResult(withErr)

	stale := ok
	stale.Err = errors.New("registry down")
	stale.Stale = true
	printResult(stale)

	partial := ok
	partial.Err = nil
	partial.Partial = true
	partial.Events = nil
	printResult(partial)

	noEvents := ok
	noEvents.Err = nil
	noEvents.Events = nil
	printResult(noEvents)

	_ = digest
	_ = candidate
}
