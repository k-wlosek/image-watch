package version

import "regexp"

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
	out := ""
	for i, n := range names {
		if i > 0 {
			out += "|"
		}
		out += n
	}
	return out
}

// matchBaseFamily interprets s as a known base-family token.
func matchBaseFamily(s string) (family string, ver SemVer, ok bool) {
	m := baseFamilyPattern.FindStringSubmatch(s)
	if m == nil {
		return "", SemVer{}, false
	}
	family = m[1]
	if m[2] == "" {
		// Recognized family name with no attached version (e.g. plain
		// "1.2.3-alpine"). Still a valid, high-confidence recognition -
		// just nothing to compare for base-advancement purposes.
		return family, SemVer{}, true
	}
	v, err := ParseSemVer(m[2])
	if err != nil {
		// Matched the family name but the trailing digits weren't a
		// clean version core; treat as recognized family, no version.
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
	return app.Version.Precision == PrecisionMajor || app.Version.Precision == PrecisionMinor
}
