package stdout

import (
	"context"
	"strings"
	"testing"

	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/notify"
)

func TestNotify_EmptyNotificationWritesNothing(t *testing.T) {
	var b strings.Builder
	n := &Notifier{Writer: &b}
	if err := n.Notify(context.Background(), notify.Notification{}); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if b.Len() != 0 {
		t.Errorf("expected no output for an empty notification, got %q", b.String())
	}
}

func TestNotify_PatchAvailable(t *testing.T) {
	var b strings.Builder
	n := &Notifier{Writer: &b}
	note := notify.Notification{Items: []notify.Item{
		{Image: "docker.io/library/nginx", Type: event.PatchAvailable, CurrentTag: "1.25.3", CandidateTag: "1.25.4"},
	}}
	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "PATCH") {
		t.Errorf("expected PATCH category label, got:\n%s", out)
	}
	if !strings.Contains(out, "1.25.3 -> 1.25.4") {
		t.Errorf("expected current->candidate tag transition, got:\n%s", out)
	}
}

func TestNotify_TagChangedShowsDigests(t *testing.T) {
	var b strings.Builder
	n := &Notifier{Writer: &b}
	note := notify.Notification{Items: []notify.Item{
		{
			Image: "ghcr.io/acme/foo", Type: event.TagChanged, CurrentTag: "latest",
			CurrentDigest: "sha256:AAAA", CandidateDigest: "sha256:BBBB", CandidateTag: "1.2.4",
		},
	}}
	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "sha256:AAAA -> sha256:BBBB") {
		t.Errorf("expected digest transition, got:\n%s", out)
	}
	if !strings.Contains(out, "inferred version: 1.2.4") {
		t.Errorf("expected inferred version annotation, got:\n%s", out)
	}
}

func TestNotify_ContainerNamesIncluded(t *testing.T) {
	var b strings.Builder
	n := &Notifier{Writer: &b}
	note := notify.Notification{Items: []notify.Item{
		{Image: "docker.io/library/nginx", Type: event.PatchAvailable, CurrentTag: "1", CandidateTag: "2", ContainerNames: []string{"web1", "web2"}},
	}}
	n.Notify(context.Background(), note)
	out := b.String()
	if !strings.Contains(out, "web1") || !strings.Contains(out, "web2") {
		t.Errorf("expected container names in output, got:\n%s", out)
	}
}
