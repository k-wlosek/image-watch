// Package policy defines which detected events should be notified.
package policy

import (
	"strings"

	"github.com/k-wlosek/image-watch/internal/event"
)

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

// MergeAll folds a slice of policies into one.
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

// labelPrefix is the container-label namespace for policy overrides.
const labelPrefix = "image-watch.policy."

// ApplyLabels overlays image-watch.policy.* container label overrides on top of a base policy.
func ApplyLabels(base Policy, labels map[string]string) Policy {
	result := base
	for key, value := range labels {
		if !strings.HasPrefix(key, labelPrefix) {
			continue
		}
		b, ok := parseBool(value)
		if !ok {
			continue
		}
		switch strings.TrimPrefix(key, labelPrefix) {
		case "patch":
			result.Patch = b
		case "minor":
			result.Minor = b
		case "major":
			result.Major = b
		case "family-advancement":
			result.FamilyAdvancement = b
		case "base-advancement":
			result.BaseAdvancement = b
		case "tag-changed":
			result.TagChanged = b
		case "tag-mutation":
			result.TagMutated = b
		case "other-platform":
			result.OtherPlatform = b
		}
	}
	return result
}

func parseBool(s string) (bool, bool) {
	switch s {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}
