package version

import (
	"regexp"
	"strings"
)

// knownBaseFamilies is the list of recognized base-image prefixes.
var knownBaseFamilies = []string{
	"alpine",
	"debian",
	"ubuntu",
	"bookworm",
	"bullseye",
	"jammy",
	"noble",
}

// baseFamilyPattern matches a known family name with an optional version core.
var baseFamilyPattern = regexp.MustCompile(`^(` + joinAlternation(knownBaseFamilies) + `)(\d+(?:\.\d+)*)?$`)

func joinAlternation(names []string) string {
	var out strings.Builder
	for i, n := range names {
		if i > 0 {
			out.WriteString("|")
		}
		out.WriteString(n)
	}
	return out.String()
}

// matchBaseFamily interprets s as a known base-family token.
func matchBaseFamily(s string) (family string, ver SemVer, ok bool) {
	m := baseFamilyPattern.FindStringSubmatch(s)
	if m == nil {
		return "", SemVer{}, false
	}
	family = m[1]
	if m[2] == "" {
		// Recognized family name with no attached version.
		return family, SemVer{}, true
	}
	v, err := ParseSemVer(m[2])
	if err != nil {
		// Matched the family name but not a clean version core.
		return family, SemVer{}, true
	}
	return family, v, true
}

// FamilyAdvancementEligible reports whether family advancement applies.
func (t TagVersion) FamilyAdvancementEligible() bool {
	app, ok := t.Application()
	if !ok {
		return false
	}
	if t.IsComposite() {
		return false
	}
	return app.Version.Precision == PrecisionMajor
}
