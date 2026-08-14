package image

import "fmt"

// Platform identifies an OCI platform: OS, architecture, and optional variant
// (e.g. linux/amd64, linux/arm64, linux/arm/v7).
type Platform struct {
	OS           string
	Architecture string
	Variant      string
}

// String renders the platform in "os/arch" or "os/arch/variant" form.
func (p Platform) String() string {
	if p.Variant == "" {
		return fmt.Sprintf("%s/%s", p.OS, p.Architecture)
	}
	return fmt.Sprintf("%s/%s/%s", p.OS, p.Architecture, p.Variant)
}

// Equal reports whether two platforms refer to the same OS/arch/variant.
func (p Platform) Equal(other Platform) bool {
	return p.OS == other.OS &&
		p.Architecture == other.Architecture &&
		p.Variant == other.Variant
}

// IsZero reports whether the platform is unset.
func (p Platform) IsZero() bool {
	return p.OS == "" && p.Architecture == ""
}
