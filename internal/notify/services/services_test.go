package services

import (
	"context"
	"strings"
	"testing"

	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/notify"
	nlib "github.com/nikoksr/notify"
)

func TestNotify_EmptyItemsReturnsNil(t *testing.T) {
	n := New(nlib.New())
	if err := n.Notify(context.Background(), notify.Notification{}); err != nil {
		t.Fatalf("expected nil for empty items, got %v", err)
	}
}

func TestFormatSubject_NoHostname(t *testing.T) {
	note := notify.Notification{Items: []notify.Item{{}, {}}}
	got := formatSubject(note)
	if got != "Image Watch - 2 updates" {
		t.Errorf("got %q, want 'Image Watch - 2 updates'", got)
	}
}

func TestFormatSubject_WithHostname(t *testing.T) {
	note := notify.Notification{
		Hostname: "web-01",
		Items:    []notify.Item{{}},
	}
	got := formatSubject(note)
	if got != "Image Watch (web-01) - 1 update" {
		t.Errorf("got %q, want 'Image Watch (web-01) - 1 update'", got)
	}
}

func TestFormatSubject_SingleItemSingular(t *testing.T) {
	note := notify.Notification{Items: []notify.Item{{}}}
	got := formatSubject(note)
	if !strings.HasSuffix(got, "1 update") {
		t.Errorf("expected singular 'update', got %q", got)
	}
}

func TestFormatBody_ContainsImageAndCategory(t *testing.T) {
	note := notify.Notification{Items: []notify.Item{
		{Image: "docker.io/lib/nginx", Type: event.PatchAvailable, CurrentTag: "1", CandidateTag: "2"},
	}}
	got := formatBody(note)
	if !strings.Contains(got, "nginx") {
		t.Errorf("expected body to contain image name, got:\n%s", got)
	}
	if !strings.Contains(got, "PATCH") {
		t.Errorf("expected body to contain PATCH category, got:\n%s", got)
	}
}
