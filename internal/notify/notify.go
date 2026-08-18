// Package notify defines the notification delivery boundary.
package notify

import (
	"context"
	"time"

	"github.com/k-wlosek/image-watch/internal/event"
)

// Item is one event prepared for delivery.
type Item struct {
	Fingerprint string

	Image    string // "registry/repository", e.g. "ghcr.io/acme/foo"
	Platform string // e.g. "linux/amd64"

	Type event.Type

	CurrentTag   string
	CandidateTag string

	CurrentDigest   string
	CandidateDigest string

	CombinedCandidate string

	ContainerNames []string
}

// Notification is a batch of Items ready for delivery.
type Notification struct {
	Timestamp time.Time
	Items     []Item
}

// Notifier delivers a Notification to one destination.
type Notifier interface {
	Notify(ctx context.Context, n Notification) error
}
