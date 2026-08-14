package version

import "testing"

func TestParseTag_BareVersions(t *testing.T) {
	cases := []struct {
		raw       string
		wantMajor int
		wantMinor int
		wantPatch int
		wantPrec  Precision
	}{
		{"1.2.3", 1, 2, 3, PrecisionPatch},
		{"v1.2.3", 1, 2, 3, PrecisionPatch},
		{"1", 1, 0, 0, PrecisionMajor},
		{"16", 16, 0, 0, PrecisionMajor},
		{"16.2", 16, 2, 0, PrecisionMinor},
		{"16.2.3", 16, 2, 3, PrecisionPatch},
	}
	for _, c := range cases {
		tv := ParseTag(c.raw)
		app, ok := tv.Application()
		if !ok {
			t.Fatalf("ParseTag(%q): expected recognized application version, got opaque", c.raw)
		}
		if app.Version.Major != c.wantMajor || app.Version.Minor != c.wantMinor || app.Version.Patch != c.wantPatch {
			t.Errorf("ParseTag(%q): got %d.%d.%d, want %d.%d.%d",
				c.raw, app.Version.Major, app.Version.Minor, app.Version.Patch,
				c.wantMajor, c.wantMinor, c.wantPatch)
		}
		if app.Version.Precision != c.wantPrec {
			t.Errorf("ParseTag(%q): got precision %s, want %s", c.raw, app.Version.Precision, c.wantPrec)
		}
		if tv.IsComposite() {
			t.Errorf("ParseTag(%q): expected non-composite, got composite", c.raw)
		}
	}
}

// TestParseTag_Composite covers a composite tag.
func TestParseTag_Composite(t *testing.T) {
	tv := ParseTag("1.2.3-alpine3.22")
	app, ok := tv.Application()
	if !ok {
		t.Fatalf("expected application component")
	}
	if app.Version.String() != "1.2.3" {
		t.Errorf("application = %s, want 1.2.3", app.Version.String())
	}
	base, ok := tv.Base()
	if !ok {
		t.Fatalf("expected base component")
	}
	if base.Name != "alpine" || base.Version.String() != "3.22" {
		t.Errorf("base = %s %s, want alpine 3.22", base.Name, base.Version.String())
	}
	if !tv.IsComposite() {
		t.Errorf("expected composite tag")
	}
}

// TestParseTag_CompositeNoBaseVersion covers a composite tag with no base version.
func TestParseTag_CompositeNoBaseVersion(t *testing.T) {
	tv := ParseTag("1.2.3-alpine")
	app, ok := tv.Application()
	if !ok || app.Version.String() != "1.2.3" {
		t.Fatalf("expected application 1.2.3, got %+v ok=%v", app, ok)
	}
	base, ok := tv.Base()
	if !ok || base.Name != "alpine" {
		t.Fatalf("expected recognized base 'alpine' with no version, got %+v ok=%v", base, ok)
	}
}

// TestParseTag_UnrecognizedSuffixStaysOpaque covers an opaque suffix.
func TestParseTag_UnrecognizedSuffixStaysOpaque(t *testing.T) {
	tv := ParseTag("1.2.3-foo42")
	app, ok := tv.Application()
	if !ok || app.Version.String() != "1.2.3" {
		t.Fatalf("expected application 1.2.3 to survive opaque suffix, got %+v ok=%v", app, ok)
	}
	if _, ok := tv.Base(); ok {
		t.Errorf("expected no usable base component for 'foo42'")
	}
}

// TestParseTag_PrefixedApplication covers a prefixed application tag.
func TestParseTag_PrefixedApplication(t *testing.T) {
	tv := ParseTag("release-1.2.3")
	app, ok := tv.Application()
	if !ok || app.Version.String() != "1.2.3" {
		t.Fatalf("expected application 1.2.3, got %+v ok=%v", app, ok)
	}
}

// TestParseTag_FullyOpaque covers an opaque tag.
func TestParseTag_FullyOpaque(t *testing.T) {
	tv := ParseTag("edge")
	if !tv.IsOpaque() {
		t.Errorf("expected 'edge' to be fully opaque")
	}
}

// TestFamilyAdvancementEligible checks family advancement eligibility.
func TestFamilyAdvancementEligible(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"16", true},
		{"16.2", false},
		{"16.2.3", false},
		{"16-alpine", false},
		{"edge", false},
	}
	for _, c := range cases {
		tv := ParseTag(c.raw)
		if got := tv.FamilyAdvancementEligible(); got != c.want {
			t.Errorf("FamilyAdvancementEligible(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

// TestSemVerCompare checks basic ordering.
func TestSemVerCompare(t *testing.T) {
	a, _ := ParseSemVer("1.2.3")
	b, _ := ParseSemVer("1.2.4")
	if a.Compare(b) >= 0 {
		t.Errorf("expected 1.2.3 < 1.2.4")
	}
	if b.Compare(a) <= 0 {
		t.Errorf("expected 1.2.4 > 1.2.3")
	}
	c, _ := ParseSemVer("1.2.3")
	if a.Compare(c) != 0 {
		t.Errorf("expected 1.2.3 == 1.2.3")
	}
}
