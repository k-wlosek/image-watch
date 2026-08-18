package ntfy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/notify"
)

func TestNotify_SendsToConfiguredTopic(t *testing.T) {
	var gotPath, gotTitle, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTitle = r.Header.Get("Title")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(Config{ServerURL: srv.URL, Topic: "docker-updates"}, srv.Client())
	note := notify.Notification{Items: []notify.Item{
		{Image: "docker.io/library/nginx", Type: event.PatchAvailable, CurrentTag: "1", CandidateTag: "2"},
	}}

	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if gotPath != "/docker-updates" {
		t.Errorf("path = %q, want /docker-updates", gotPath)
	}
	if gotTitle != "Image Watch" {
		t.Errorf("title = %q, want default 'Image Watch'", gotTitle)
	}
	if !strings.Contains(gotBody, "PATCH") {
		t.Errorf("expected batch summary body to mention PATCH, got:\n%s", gotBody)
	}
}

func TestNotify_CustomTitleAndPriority(t *testing.T) {
	var gotTitle, gotPriority string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle = r.Header.Get("Title")
		gotPriority = r.Header.Get("Priority")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(Config{ServerURL: srv.URL, Topic: "t", Title: "Custom", Priority: "high"}, srv.Client())
	n.Notify(context.Background(), notify.Notification{Items: []notify.Item{{Type: event.PatchAvailable}}})

	if gotTitle != "Custom" {
		t.Errorf("title = %q, want Custom", gotTitle)
	}
	if gotPriority != "high" {
		t.Errorf("priority = %q, want high", gotPriority)
	}
}

func TestNotify_NoTopicConfigured(t *testing.T) {
	n := New(Config{}, nil)
	err := n.Notify(context.Background(), notify.Notification{Items: []notify.Item{{}}})
	if err == nil {
		t.Fatal("expected an error when no topic is configured")
	}
}

func TestNotify_EmptyNotificationSendsNothing(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	n := New(Config{ServerURL: srv.URL, Topic: "t"}, srv.Client())
	if err := n.Notify(context.Background(), notify.Notification{}); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if called {
		t.Errorf("expected no HTTP request for an empty notification")
	}
}
