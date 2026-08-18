package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
}

func TestNotify_OneItemFailureDoesNotPreventOthers(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var p payload
		json.NewDecoder(r.Body).Decode(&p)
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
