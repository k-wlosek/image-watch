package version

import "testing"

func TestPrecisionString(t *testing.T) {
	cases := []struct {
		p    Precision
		want string
	}{
		{PrecisionUnknown, "unknown"},
		{PrecisionMajor, "major"},
		{PrecisionMinor, "minor"},
		{PrecisionPatch, "patch"},
	}
	for _, tc := range cases {
		if got := tc.p.String(); got != tc.want {
			t.Errorf("Precision(%d).String() = %q, want %q", tc.p, got, tc.want)
		}
	}
}

func TestSemVerString(t *testing.T) {
	cases := []struct {
		v    SemVer
		want string
	}{
		{SemVer{Major: 16, Precision: PrecisionMajor}, "16"},
		{SemVer{Major: 1, Minor: 26, Precision: PrecisionMinor}, "1.26"},
		{SemVer{Major: 1, Minor: 2, Patch: 3, Precision: PrecisionPatch}, "1.2.3"},
		{SemVer{Raw: "v2.0.0-rc1", Precision: PrecisionUnknown}, "v2.0.0-rc1"},
	}
	for _, tc := range cases {
		if got := tc.v.String(); got != tc.want {
			t.Errorf("SemVer.String() = %q, want %q", got, tc.want)
		}
	}
}

func TestSemVerCompareEdge(t *testing.T) {
	one := SemVer{Major: 1, Minor: 2, Patch: 0}
	two := SemVer{Major: 1, Minor: 2, Patch: 1}
	big := SemVer{Major: 2}

	if one.Compare(two) != -1 || two.Compare(one) != 1 {
		t.Errorf("major/minor-based comparisons are wrong: %d %d", one.Compare(two), two.Compare(one))
	}
	if one.Compare(big) != -1 || big.Compare(one) != 1 {
		t.Errorf("major-mismatch comparison is wrong")
	}
	if one.Compare(one) != 0 {
		t.Errorf("equal values must compare as 0")
	}
}

func TestParseSemVer_Errors(t *testing.T) {
	for _, raw := range []string{"1.2.3.4", "1.2.", "1.a", "1.-2", ".5"} {
		if _, err := ParseSemVer(raw); err == nil {
			t.Errorf("ParseSemVer(%q): expected an error", raw)
		}
	}
}

func TestParseSemVer_Prerelease(t *testing.T) {
	v, err := ParseSemVer("v2.3.4-beta.1")
	if err != nil {
		t.Fatalf("ParseSemVer error: %v", err)
	}
	if v.Prerelease != "beta.1" {
		t.Errorf("Prerelease = %q, want beta.1", v.Prerelease)
	}
	if v.Major != 2 || v.Minor != 3 || v.Patch != 4 {
		t.Errorf("parsed %d.%d.%d, want 2.3.4", v.Major, v.Minor, v.Patch)
	}
	if v.String() != "2.3.4" {
		t.Errorf("String() = %q, want the patch-precision rendering", v.String())
	}
}

func TestSemVerCompare_PatchPrecedenceEdge(t *testing.T) {
	a := SemVer{Major: 1, Minor: 1, Patch: 2}
	b := SemVer{Major: 1, Minor: 2, Patch: 0}
	if a.Compare(b) != -1 {
		t.Errorf("minor must dominate patch: got %d", a.Compare(b))
	}
}
