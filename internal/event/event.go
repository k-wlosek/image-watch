// Package event defines the semantic event model: what a meaningful
// transition between observations looks like, independent of how it gets
// detected (registry+version) or acted on (policy+notify).
package event

import (
	"time"

	"github.com/k-wlosek/image-watch/internal/image"
)

// Type identifies an update event category.
type Type string

const (
	// PatchAvailable/MinorAvailable/MajorAvailable are SemVer-distance events.
	PatchAvailable Type = "PATCH_AVAILABLE"
	MinorAvailable Type = "MINOR_AVAILABLE"
	MajorAvailable Type = "MAJOR_AVAILABLE"

	// FamilyAdvancementAvailable fires when an imprecise family tag advances.
	FamilyAdvancementAvailable Type = "FAMILY_ADVANCEMENT_AVAILABLE"

	// ApplicationPatchAvailable / ApplicationMinorAvailable /
	// ApplicationMajorAvailable are the composite-tag equivalents.
	ApplicationPatchAvailable Type = "APPLICATION_PATCH_AVAILABLE"
	ApplicationMinorAvailable Type = "APPLICATION_MINOR_AVAILABLE"
	ApplicationMajorAvailable Type = "APPLICATION_MAJOR_AVAILABLE"

	// BaseAdvancementAvailable fires when a composite tag's base component advances.
	BaseAdvancementAvailable Type = "BASE_ADVANCEMENT_AVAILABLE"

	// TagChanged fires when a mutable reference resolves to a different digest.
	TagChanged Type = "TAG_CHANGED"

	// TagMutated fires when a fixed-looking tag resolves to a different digest.
	TagMutated Type = "TAG_MUTATED"

	// OtherPlatformUpdate reports a newer tag for another platform.
	OtherPlatformUpdate Type = "OTHER_PLATFORM_UPDATE"
)

// Event represents one detected transition for a monitored image.
type Event struct {
	Timestamp time.Time

	Image image.Reference

	Type Type

	CurrentTag    string
	CurrentDigest string

	CandidateTag    string
	CandidateDigest string

	Platform image.Platform

	// CombinedCandidate is the full tag after combining application and base candidates.
	CombinedCandidate string
}
