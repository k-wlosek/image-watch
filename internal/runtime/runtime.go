// Package runtime defines the container runtime abstraction.
package runtime

import (
	"context"
	"time"

	"github.com/k-wlosek/image-watch/internal/image"
)

// Runtime discovers running containers and reports the images they use.
type Runtime interface {
	// Name identifies the runtime implementation.
	Name() string

	// Hostname returns the hostname of the node the runtime is running on.
	Hostname(ctx context.Context) (string, error)

	// ListContainers returns an observation for every running container.
	ListContainers(ctx context.Context) ([]ContainerObservation, error)
}

// ContainerObservation describes one running container.
type ContainerObservation struct {
	// Runtime is the name of the runtime that produced this observation.
	Runtime string

	// ID is the runtime-specific container ID.
	ID string

	// Name is the human-readable container name.
	Name string

	// Image is the normalized reference the container was started from.
	Image image.Reference

	// Digest is the content digest of the image, if known.
	Digest string

	// Platform is the platform the container is running under.
	Platform image.Platform

	// CreatedAt is when the container was created.
	CreatedAt time.Time

	// Labels holds the container's labels.
	Labels map[string]string
}
