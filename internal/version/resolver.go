package version

import "regexp"

// versionTokenPattern matches SemVer-like numeric cores with an optional v prefix.
var versionTokenPattern = regexp.MustCompile(`v?\d+(?:\.\d+){0,2}`)

// ParseTag interprets a raw tag string into a TagVersion.
func ParseTag(raw string) TagVersion {
	if raw == "" {
		return TagVersion{Raw: raw}
	}

	// Case 1: bare SemVer-like core.
	if versionTokenPattern.FindString(raw) == raw {
		if v, err := ParseSemVer(raw); err == nil {
			return TagVersion{
				Raw: raw,
				Components: []VersionComponent{
					{
						Role:       RoleApplication,
						Version:    v,
						Confidence: ConfidenceHigh,
						Raw:        raw,
					},
				},
			}
		}
	}

	// Case 2: dash-separated form.
	segments := splitOnDash(raw)
	if len(segments) >= 2 {
		return parseSegments(raw, segments)
	}

	// Nothing recognized.
	return TagVersion{Raw: raw}
}

func splitOnDash(raw string) []string {
	var segs []string
	start := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == '-' {
			segs = append(segs, raw[start:i])
			start = i + 1
		}
	}
	segs = append(segs, raw[start:])
	return segs
}

// parseSegments handles dash-separated tags.
func parseSegments(raw string, segments []string) TagVersion {
	var components []VersionComponent
	var bestApp *VersionComponent
	var bestAppIndex = -1

	for i, seg := range segments {
		if seg == "" {
			continue
		}

		// Try base-family recognition first.
		if family, ver, ok := matchBaseFamily(seg); ok {
			components = append(components, VersionComponent{
				Role:       RoleBase,
				Name:       family,
				Version:    ver,
				Confidence: ConfidenceHigh,
				Raw:        seg,
			})
			continue
		}

		// Try a bare application-version token.
		if versionTokenPattern.FindString(seg) == seg {
			if v, err := ParseSemVer(seg); err == nil {
				candidate := VersionComponent{
					Role:       RoleApplication,
					Version:    v,
					Confidence: ConfidenceHigh,
					Raw:        seg,
				}
				// Prefer higher precision; break ties by earliest position.
				if bestApp == nil || v.Precision > bestApp.Version.Precision {
					bestApp = &candidate
					bestAppIndex = i
				}
				continue
			}
		}

		// Unrecognized segment. Record it as low confidence.
		components = append(components, VersionComponent{
			Role:       RoleBase,
			Confidence: ConfidenceLow,
			Raw:        seg,
		})
	}

	if bestApp != nil {
		// Put the application component first for readability.
		_ = bestAppIndex
		components = append([]VersionComponent{*bestApp}, components...)
	}

	if len(components) == 0 {
		return TagVersion{Raw: raw}
	}
	return TagVersion{Raw: raw, Components: components}
}
