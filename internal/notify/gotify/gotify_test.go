package gotify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/secret"
)

func TestParseConfig_MissingServerURLReturnsError(t *testing.T) {
	_, err := ParseConfig(map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "server_url is required") {
		t.Fatalf("expected server_url error, got %v", err)
	}
}

func TestParseConfig_MissingTokenFileReturnsError(t *testing.T) {
	_, err := ParseConfig(map[string]string{"server_url": "https://gotify.example.com"})
	if err == nil || !strings.Contains(err.Error(), "token_file is required") {
		t.Fatalf("expected token_file error, got %v", err)
	}
}

func TestParseConfig_EmptyTokenFileReturnsError(t *testing.T) {
	f := secret.WriteFile(t, "")
	_, err := ParseConfig(map[string]string{"server_url": "https://gotify.example.com", "token_file": f})
	if err == nil || !strings.Contains(err.Error(), "token_file is empty") {
		t.Fatalf("expected empty token_file error, got %v", err)
	}
}

func TestParseConfig_HappyPath(t *testing.T) {
	f := secret.WriteFile(t, "mytoken")
	cfg, err := ParseConfig(map[string]string{
		"server_url": "https://gotify.example.com",
		"token_file": f,
	})
	if err != nil {
		t.Fatalf("ParseConfig error: %v", err)
	}
	if cfg.ServerURL != "https://gotify.example.com" {
		t.Errorf("ServerURL = %q", cfg.ServerURL)
	}
	if cfg.Token != "mytoken" {
		t.Errorf("Token = %q, want mytoken", cfg.Token)
	}
	if cfg.Priority != "5" {
		t.Errorf("Priority = %q, want 5 (default)", cfg.Priority)
	}
}

func TestParseConfig_CustomPriority(t *testing.T) {
	f := secret.WriteFile(t, "mytoken")
	cfg, err := ParseConfig(map[string]string{
		"server_url": "https://gotify.example.com",
		"token_file": f,
		"priority":   "10",
	})
	if err != nil {
		t.Fatalf("ParseConfig error: %v", err)
	}
	if cfg.Priority != "10" {
		t.Errorf("Priority = %q, want 10", cfg.Priority)
	}
}

func TestParseConfig_StripsTrailingSlash(t *testing.T) {
	f := secret.WriteFile(t, "tok")
	cfg, err := ParseConfig(map[string]string{
		"server_url": "https://gotify.example.com/",
		"token_file": f,
	})
	if err != nil {
		t.Fatalf("ParseConfig error: %v", err)
	}
	if cfg.ServerURL != "https://gotify.example.com" {
		t.Errorf("ServerURL = %q, trailing slash not stripped", cfg.ServerURL)
	}
}

func TestNotify_SendsMessage(t *testing.T) {
	var gotPath, gotKey string
	var gotMsg gotifyMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("X-Gotify-Key")
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &gotMsg); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(Config{ServerURL: srv.URL, Token: "mytoken", Priority: "7"}, srv.Client())
	note := notify.Notification{Items: []notify.Item{
		{Image: "ghcr.io/acme/foo", Type: event.PatchAvailable, CurrentTag: "1", CandidateTag: "2"},
	}}

	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if gotPath != "/message" {
		t.Errorf("path = %q, want /message", gotPath)
	}
	if gotKey != "mytoken" {
		t.Errorf("X-Gotify-Key = %q, want mytoken", gotKey)
	}
	if gotMsg.Priority != 7 {
		t.Errorf("Priority = %d, want 7", gotMsg.Priority)
	}
	if gotMsg.Title == "" {
		t.Errorf("Title should not be empty")
	}
	if gotMsg.Message == "" {
		t.Errorf("Message should not be empty")
	}
}

func TestNotify_HostnameInTitle(t *testing.T) {
	var gotMsg gotifyMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotMsg)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(Config{ServerURL: srv.URL, Token: "tok"}, srv.Client())
	note := notify.Notification{
		Hostname: "db-01",
		Items:    []notify.Item{{Image: "a", Type: event.PatchAvailable}},
	}

	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if gotMsg.Title != "Image Watch (db-01)" {
		t.Errorf("Title = %q, want 'Image Watch (db-01)'", gotMsg.Title)
	}
}

func TestNotify_EmptyItemsSendsNothing(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(Config{ServerURL: srv.URL, Token: "tok"}, srv.Client())
	if err := n.Notify(context.Background(), notify.Notification{}); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if called {
		t.Errorf("expected no HTTP request for empty notification")
	}
}

func TestNotify_NoServerURLConfigured(t *testing.T) {
	n := New(Config{}, nil)
	err := n.Notify(context.Background(), notify.Notification{Items: []notify.Item{{}}})
	if err == nil {
		t.Fatal("expected error when no server URL configured")
	}
}

func TestNotify_NonSuccessStatusIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	n := New(Config{ServerURL: srv.URL, Token: "tok"}, srv.Client())
	err := n.Notify(context.Background(), notify.Notification{Items: []notify.Item{{Image: "a"}}})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected a 403 error, got %v", err)
	}
}

func TestNotify_RequestFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	n := New(Config{ServerURL: url, Token: "tok"}, &http.Client{})
	err := n.Notify(context.Background(), notify.Notification{Items: []notify.Item{{Image: "a"}}})
	if err == nil {
		t.Fatal("expected a request failure against a closed server")
	}
}
