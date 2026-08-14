package version

// ComponentRole identifies the role of a recognized tag component.
type ComponentRole string

const (
	// RoleApplication is the application version component.
	RoleApplication ComponentRole = "application"

	// RoleBase is a recognized base-image or platform component.
	RoleBase ComponentRole = "base"
)

// Confidence indicates how certain the parser is about a component.
type Confidence string

const (
	// ConfidenceHigh means the component is safe to use in comparisons.
	ConfidenceHigh Confidence = "high"

	// ConfidenceLow means the component was extracted but remains uncertain.
	ConfidenceLow Confidence = "low"
)

// VersionComponent is one parsed piece of a tag.
type VersionComponent struct {
	Role       ComponentRole
	Name       string
	Version    SemVer
	Confidence Confidence
	Raw        string
}

// Usable reports whether this component may participate in comparisons.
func (c VersionComponent) Usable() bool {
	return c.Confidence == ConfidenceHigh
}

// TagVersion is the parsed representation of a tag.
type TagVersion struct {
	Raw        string
	Components []VersionComponent
}

// Application returns the application component, if present and usable.
func (t TagVersion) Application() (VersionComponent, bool) {
	for _, c := range t.Components {
		if c.Role == RoleApplication && c.Usable() {
			return c, true
		}
	}
	return VersionComponent{}, false
}

// Base returns the base component, if present and usable.
func (t TagVersion) Base() (VersionComponent, bool) {
	for _, c := range t.Components {
		if c.Role == RoleBase && c.Usable() {
			return c, true
		}
	}
	return VersionComponent{}, false
}

// IsComposite reports whether the tag has both application and base components.
func (t TagVersion) IsComposite() bool {
	hasApp, hasBase := false, false
	for _, c := range t.Components {
		switch c.Role {
		case RoleApplication:
			hasApp = true
		case RoleBase:
			hasBase = true
		}
	}
	return hasApp && hasBase
}

// IsOpaque reports whether no components were recognized.
func (t TagVersion) IsOpaque() bool {
	return len(t.Components) == 0
}
