// Package version implements tag interpretation and SemVer parsing.
package version

import (
	"fmt"
	"strconv"
	"strings"
)

// Precision indicates how much of a version a tag specifies.
type Precision int

const (
	PrecisionUnknown Precision = iota
	PrecisionMajor
	PrecisionMinor
	PrecisionPatch
)

func (p Precision) String() string {
	switch p {
	case PrecisionMajor:
		return "major"
	case PrecisionMinor:
		return "minor"
	case PrecisionPatch:
		return "patch"
	default:
		return "unknown"
	}
}

// SemVer is a parsed SemVer-like version.
type SemVer struct {
	Major int
	Minor int
	Patch int

	// Prerelease holds any prerelease/build suffix text found after the numeric core.
	Prerelease string

	Precision Precision

	// Raw is the original parsed substring.
	Raw string
}

// String renders the version at its original precision.
func (v SemVer) String() string {
	switch v.Precision {
	case PrecisionMajor:
		return strconv.Itoa(v.Major)
	case PrecisionMinor:
		return fmt.Sprintf("%d.%d", v.Major, v.Minor)
	case PrecisionPatch:
		return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	default:
		return v.Raw
	}
}

// Compare returns -1, 0, or 1 if v is less than, equal to, or greater than other.
func (v SemVer) Compare(other SemVer) int {
	if v.Major != other.Major {
		return cmp(v.Major, other.Major)
	}
	if v.Minor != other.Minor {
		return cmp(v.Minor, other.Minor)
	}
	if v.Patch != other.Patch {
		return cmp(v.Patch, other.Patch)
	}
	return 0
}

func cmp(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// ParseSemVer parses a numeric version core such as "1", "1.2", or "1.2.3".
func ParseSemVer(raw string) (SemVer, error) {
	s := strings.TrimPrefix(raw, "v")

	// Split off a prerelease/build suffix at the first "-" or "+".
	core := s
	var prerelease string
	if idx := strings.IndexAny(s, "-+"); idx != -1 {
		core = s[:idx]
		prerelease = s[idx+1:]
	}

	parts := strings.Split(core, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return SemVer{}, fmt.Errorf("version: %q is not a recognizable SemVer-like core", raw)
	}

	nums := make([]int, len(parts))
	for i, p := range parts {
		if p == "" {
			return SemVer{}, fmt.Errorf("version: %q has an empty numeric component", raw)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return SemVer{}, fmt.Errorf("version: %q component %q is not a non-negative integer", raw, p)
		}
		nums[i] = n
	}

	v := SemVer{Raw: raw, Prerelease: prerelease}
	switch len(nums) {
	case 1:
		v.Major = nums[0]
		v.Precision = PrecisionMajor
	case 2:
		v.Major, v.Minor = nums[0], nums[1]
		v.Precision = PrecisionMinor
	case 3:
		v.Major, v.Minor, v.Patch = nums[0], nums[1], nums[2]
		v.Precision = PrecisionPatch
	}
	return v, nil
}
