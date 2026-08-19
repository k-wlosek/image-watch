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

func TestNew_WritesToStdoutByDefault(t *testing.T) {
	n := New()
	if n.Writer == nil {
		t.Errorf("expected Writer to default to os.Stdout")
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

func TestNotify_SuppressedContainersListed(t *testing.T) {
	var b strings.Builder
	n := &Notifier{Writer: &b}
	note := notify.Notification{Items: []notify.Item{
		{Image: "docker.io/library/nginx", Type: event.PatchAvailable, CurrentTag: "1", CandidateTag: "2",
			ContainerNames: []string{"web1"}, Suppressed: []string{"web2"}},
	}}
	n.Notify(context.Background(), note)
	out := b.String()
	if !strings.Contains(out, "suppressed: [web2]") {
		t.Errorf("expected suppressed container line, got:\n%s", out)
	}
}

func TestNotify_DefaultBranchShowsCombinedCandidate(t *testing.T) {
	var b strings.Builder
	n := &Notifier{Writer: &b}
	note := notify.Notification{Items: []notify.Item{
		{Image: "docker.io/library/foo", Type: event.ApplicationPatchAvailable, CurrentTag: "1.2.3", CandidateTag: "1.3.0", CombinedCandidate: "1.2.3+upstream"},
	}}
	n.Notify(context.Background(), note)
	out := b.String()
	if !strings.Contains(out, "1.2.3 -> 1.3.0") || !strings.Contains(out, "combined: 1.2.3+upstream") {
		t.Errorf("expected combined candidate rendering, got:\n%s", out)
	}
}

func TestNotify_NilWriterDefaultsToStdout(t *testing.T) {
	n := &Notifier{Writer: nil}
	note := notify.Notification{Items: []notify.Item{
		{Image: "docker.io/library/foo", Type: event.PatchAvailable, CurrentTag: "1", CandidateTag: "2"},
	}}
	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify with nil writer error: %v", err)
	}
}

func TestCategoryLabel_AllTypes(t *testing.T) {
	cases := map[event.Type]string{
		event.PatchAvailable:             "PATCH",
		event.ApplicationPatchAvailable:  "PATCH",
		event.MinorAvailable:             "MINOR",
		event.ApplicationMinorAvailable:  "MINOR",
		event.MajorAvailable:             "MAJOR",
		event.ApplicationMajorAvailable:  "MAJOR",
		event.FamilyAdvancementAvailable: "FAMILY ADVANCEMENT",
		event.BaseAdvancementAvailable:   "BASE ADVANCEMENT",
		event.TagChanged:                 "TAG CHANGED",
		event.TagMutated:                 "TAG MUTATED",
		event.OtherPlatformUpdate:        "OTHER PLATFORM UPDATE",
	}
	for typ, want := range cases {
		if got := categoryLabel(typ); got != want {
			t.Errorf("categoryLabel(%s) = %q, want %q", typ, got, want)
		}
	}
	if got := categoryLabel("UNKNOWN_EVENT"); got != "UNKNOWN_EVENT" {
		t.Errorf("categoryLabel(unknown) = %q, want the raw string", got)
	}
}
