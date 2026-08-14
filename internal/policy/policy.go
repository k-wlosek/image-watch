// Package policy defines which detected events should be notified.
package policy

import "github.com/example/image-watch/internal/event"

// Policy controls which detected event categories are eligible for notification.
type Policy struct {
	Patch             bool
	Minor             bool
	Major             bool
	FamilyAdvancement bool // default false
	BaseAdvancement   bool
	TagChanged        bool
	TagMutated        bool
	OtherPlatform     bool // default false
}

// Default returns the built-in default policy.
func Default() Policy {
	return Policy{
		Patch:             true,
		Minor:             true,
		Major:             true,
		FamilyAdvancement: false,
		BaseAdvancement:   true,
		TagChanged:        true,
		TagMutated:        true,
		OtherPlatform:     false,
	}
}

// Allows reports whether the given event type is enabled.
func (p Policy) Allows(t event.Type) bool {
	switch t {
	case event.PatchAvailable, event.ApplicationPatchAvailable:
		return p.Patch
	case event.MinorAvailable, event.ApplicationMinorAvailable:
		return p.Minor
	case event.MajorAvailable, event.ApplicationMajorAvailable:
		return p.Major
	case event.FamilyAdvancementAvailable:
		return p.FamilyAdvancement
	case event.BaseAdvancementAvailable:
		return p.BaseAdvancement
	case event.TagChanged:
		return p.TagChanged
	case event.TagMutated:
		return p.TagMutated
	case event.OtherPlatformUpdate:
		return p.OtherPlatform
	default:
		return false
	}
}

// Merge combines this policy with another using per-field OR logic.
func (p Policy) Merge(other Policy) Policy {
	return Policy{
		Patch:             p.Patch || other.Patch,
		Minor:             p.Minor || other.Minor,
		Major:             p.Major || other.Major,
		FamilyAdvancement: p.FamilyAdvancement || other.FamilyAdvancement,
		BaseAdvancement:   p.BaseAdvancement || other.BaseAdvancement,
		TagChanged:        p.TagChanged || other.TagChanged,
		TagMutated:        p.TagMutated || other.TagMutated,
		OtherPlatform:     p.OtherPlatform || other.OtherPlatform,
	}
}

// MergeAll folds a slice of policies into one effective policy.
func MergeAll(policies []Policy) Policy {
	if len(policies) == 0 {
		return Default()
	}
	result := policies[0]
	for _, p := range policies[1:] {
		result = result.Merge(p)
	}
	return result
}
