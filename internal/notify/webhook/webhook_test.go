package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/notify"
)

func TestNotify_SendsExpectedPayload(t *testing.T) {
	var received []payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", r.Header.Get("Content-Type"))
		}
		var p payload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		received = append(received, p)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(Config{URL: srv.URL}, srv.Client())
	note := notify.Notification{Items: []notify.Item{
		{
			Image: "ghcr.io/acme/foo", Type: event.PatchAvailable,
			CurrentTag: "1.2.3", CandidateTag: "1.2.4",
			Platform: "linux/amd64",
		},
	}}

	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if len(received) != 1 {
		t.Fatalf("got %d requests, want 1", len(received))
	}
	p := received[0]
	if p.Event != string(event.PatchAvailable) {
		t.Errorf("Event = %q, want %q", p.Event, event.PatchAvailable)
	}
	if p.Image != "ghcr.io/acme/foo" || p.Current != "1.2.3" || p.Candidate != "1.2.4" {
		t.Errorf("unexpected payload: %+v", p)
	}
	if p.Platform == nil || p.Platform.OS != "linux" || p.Platform.Architecture != "amd64" {
		t.Errorf("expected platform linux/amd64, got %+v", p.Platform)
	}
	if p.Containers != nil {
		t.Errorf("expected no containers when unset, got %v", p.Containers)
	}
}

func TestNotify_PayloadIncludesContainersAndSuppressed(t *testing.T) {
	var received payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(Config{URL: srv.URL}, srv.Client())
	note := notify.Notification{Items: []notify.Item{
		{
			Image: "ghcr.io/acme/foo", Type: event.PatchAvailable,
			CurrentTag: "1.2.3", CandidateTag: "1.2.4",
			ContainerNames: []string{"web1"}, Suppressed: []string{"web2"},
		},
	}}
	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if len(received.Containers) != 1 || received.Containers[0] != "web1" {
		t.Errorf("Containers = %v, want [web1]", received.Containers)
	}
	if len(received.Suppressed) != 1 || received.Suppressed[0] != "web2" {
		t.Errorf("Suppressed = %v, want [web2]", received.Suppressed)
	}
}

func TestNotify_OneItemFailureDoesNotPreventOthers(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var p payload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		if p.Candidate == "fails" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(Config{URL: srv.URL}, srv.Client())
	note := notify.Notification{Items: []notify.Item{
		{Image: "a", Type: event.PatchAvailable, CandidateTag: "fails"},
		{Image: "b", Type: event.PatchAvailable, CandidateTag: "succeeds"},
	}}

	err := n.Notify(context.Background(), note)
	if err == nil {
		t.Fatalf("expected an error since one item failed")
	}
	if callCount != 2 {
		t.Errorf("expected both items to be attempted despite the first failing, got %d calls", callCount)
	}
}

func TestNotify_NoURLConfigured(t *testing.T) {
	n := New(Config{}, nil)
	err := n.Notify(context.Background(), notify.Notification{Items: []notify.Item{{}}})
	if err == nil {
		t.Fatal("expected an error when no URL is configured")
	}
}

func TestNotify_DoesNotSendWhenRequestBuildFails(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(Config{URL: "http://exa mple" /* invalid per http.NewRequest */}, srv.Client())
	err := n.Notify(context.Background(), notify.Notification{Items: []notify.Item{{
		Image: "a", Type: event.PatchAvailable, CurrentTag: "1", CandidateTag: "2",
	}}})
	if err == nil {
		t.Fatal("expected an error building the request")
	}
	if called {
		t.Error("request must not be sent when the URL is invalid")
	}
}

func TestNotify_NonSuccessStatusIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
		w.Write([]byte("upstream timeout"))
	}))
	defer srv.Close()

	n := New(Config{URL: srv.URL}, srv.Client())
	err := n.Notify(context.Background(), notify.Notification{Items: []notify.Item{{
		Image: "a", Type: event.PatchAvailable, CurrentTag: "1", CandidateTag: "2",
	}}})
	if err == nil || !strings.Contains(err.Error(), "504") {
		t.Fatalf("expected a 504 error, got %v", err)
	}
}

func TestNotify_HostnameInPayload(t *testing.T) {
	var received payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(Config{URL: srv.URL}, srv.Client())
	note := notify.Notification{
		Hostname: "db-02",
		Items:    []notify.Item{{Image: "a", Type: event.PatchAvailable, CurrentTag: "1", CandidateTag: "2"}},
	}
	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if received.Hostname != "db-02" {
		t.Errorf("Hostname = %q, want %q", received.Hostname, "db-02")
	}
}

func TestNotify_EmptyHostnameOmitted(t *testing.T) {
	var raw map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&raw)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(Config{URL: srv.URL}, srv.Client())
	note := notify.Notification{Items: []notify.Item{
		{Image: "a", Type: event.PatchAvailable, CurrentTag: "1", CandidateTag: "2"},
	}}
	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if _, ok := raw["hostname"]; ok {
		t.Errorf("expected hostname to be omitted when empty, but it was present")
	}
}

func TestNotify_EmptyItemsSendsNothing(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(Config{URL: srv.URL}, srv.Client())
	if err := n.Notify(context.Background(), notify.Notification{}); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if called {
		t.Error("expected no HTTP request for an empty notification")
	}
}
