// Package stdout implements notify.Notifier by writing a human-readable batch summary.
package stdout

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/notify"
)

// Notifier writes notifications to Writer (defaults to os.Stdout).
type Notifier struct {
	Writer io.Writer
}

// New constructs a stdout Notifier writing to os.Stdout.
func New() *Notifier {
	return &Notifier{Writer: os.Stdout}
}

var _ notify.Notifier = (*Notifier)(nil)

// Notify writes a batch summary to the configured writer.
func (n *Notifier) Notify(_ context.Context, note notify.Notification) error {
	w := n.Writer
	if w == nil {
		w = os.Stdout
	}

	if len(note.Items) == 0 {
		return nil
	}

	fmt.Fprintf(w, "Image Watch - %d update(s)\n\n", len(note.Items))
	for _, item := range note.Items {
		fmt.Fprintln(w, categoryLabel(item.Type))
		switch item.Type {
		case event.TagChanged, event.TagMutated:
			fmt.Fprintf(w, "  %s:%s\n", item.Image, item.CurrentTag)
			fmt.Fprintf(w, "  %s -> %s", item.CurrentDigest, item.CandidateDigest)
			if item.CandidateTag != "" {
				fmt.Fprintf(w, " (inferred version: %s)", item.CandidateTag)
			}
			fmt.Fprintln(w)
		default:
			fmt.Fprintf(w, "  %s:%s -> %s\n", item.Image, item.CurrentTag, item.CandidateTag)
			if item.CombinedCandidate != "" {
				fmt.Fprintf(w, "  combined: %s\n", item.CombinedCandidate)
			}
		}
		if len(item.ContainerNames) > 0 {
			fmt.Fprintf(w, "  containers: %v\n", item.ContainerNames)
		}
		fmt.Fprintln(w)
	}
	return nil
}

func categoryLabel(t event.Type) string {
	switch t {
	case event.PatchAvailable, event.ApplicationPatchAvailable:
		return "PATCH"
	case event.MinorAvailable, event.ApplicationMinorAvailable:
		return "MINOR"
	case event.MajorAvailable, event.ApplicationMajorAvailable:
		return "MAJOR"
	case event.FamilyAdvancementAvailable:
		return "FAMILY ADVANCEMENT"
	case event.BaseAdvancementAvailable:
		return "BASE ADVANCEMENT"
	case event.TagChanged:
		return "TAG CHANGED"
	case event.TagMutated:
		return "TAG MUTATED"
	case event.OtherPlatformUpdate:
		return "OTHER PLATFORM UPDATE"
	default:
		return string(t)
	}
}
