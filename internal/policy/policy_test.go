package policy

import (
	"testing"

	"github.com/k-wlosek/image-watch/internal/event"
)

func TestDefault(t *testing.T) {
	p := Default()
	if !p.Patch || !p.Minor || !p.Major {
		t.Errorf("expected patch/minor/major enabled by default, got %+v", p)
	}
	if p.FamilyAdvancement {
		t.Errorf("expected family advancement disabled by default")
	}
	if !p.BaseAdvancement || !p.TagChanged || !p.TagMutated {
		t.Errorf("expected base advancement and tag events enabled by default, got %+v", p)
	}
	if p.OtherPlatform {
		t.Errorf("expected other-platform disabled by default")
	}
}

func TestAllows(t *testing.T) {
	p := Default()
	cases := []struct {
		name string
		e    event.Type
		want bool
	}{
		{"patch", event.PatchAvailable, p.Patch},
		{"app patch", event.ApplicationPatchAvailable, p.Patch},
		{"minor", event.MinorAvailable, p.Minor},
		{"app minor", event.ApplicationMinorAvailable, p.Minor},
		{"major", event.MajorAvailable, p.Major},
		{"app major", event.ApplicationMajorAvailable, p.Major},
		{"family", event.FamilyAdvancementAvailable, p.FamilyAdvancement},
		{"base", event.BaseAdvancementAvailable, p.BaseAdvancement},
		{"tag changed", event.TagChanged, p.TagChanged},
		{"tag mutated", event.TagMutated, p.TagMutated},
		{"other platform", event.OtherPlatformUpdate, p.OtherPlatform},
		{"unknown", "NOT_A_REAL_EVENT", false},
	}
	for _, tc := range cases {
		if got := p.Allows(tc.e); got != tc.want {
			t.Errorf("Allows(%s) = %v, want %v", tc.e, got, tc.want)
		}
	}
}

func TestMerge_OrPerField(t *testing.T) {
	a := Default()
	b := Default()
	a.Patch = false
	a.OtherPlatform = true
	b.Minor = false

	got := a.Merge(b)
	if got.Patch != true { // b.Patch true
		t.Errorf("Patch = %v, want true (OR)", got.Patch)
	}
	if got.OtherPlatform != true {
		t.Errorf("OtherPlatform = %v, want true (a enabled it)", got.OtherPlatform)
	}
	if got.Minor != true { // a.Minor true
		t.Errorf("Minor = %v, want true (OR)", got.Minor)
	}
	if got.FamilyAdvancement {
		t.Errorf("FamilyAdvancement should stay false")
	}
}

func TestMergeAll(t *testing.T) {
	if p := MergeAll(nil); p != Default() {
		t.Errorf("MergeAll(nil) should return Default, got %+v", p)
	}

	single := Default()
	single.Patch = false
	if p := MergeAll([]Policy{single}); p != single {
		t.Errorf("MergeAll(single) should return the policy unchanged, got %+v", p)
	}

	a := Default()
	b := Default()
	a.FamilyAdvancement = true
	a.OtherPlatform = true
	b.TagChanged = false

	merged := MergeAll([]Policy{a, b})
	if !merged.FamilyAdvancement || !merged.OtherPlatform {
		t.Errorf("expected union to carry a's enabled fields, got %+v", merged)
	}
	if !merged.TagChanged {
		t.Errorf("TagChanged should be unioned back on by a, got %+v", merged)
	}
}

func TestApplyLabels(t *testing.T) {
	cases := []struct {
		key      string
		field    func(Policy) bool
		set      func(*Policy, bool)
		testName string
	}{
		{"patch", func(p Policy) bool { return p.Patch }, func(p *Policy, b bool) { p.Patch = b }, "patch"},
		{"minor", func(p Policy) bool { return p.Minor }, func(p *Policy, b bool) { p.Minor = b }, "minor"},
		{"major", func(p Policy) bool { return p.Major }, func(p *Policy, b bool) { p.Major = b }, "major"},
		{"family-advancement", func(p Policy) bool { return p.FamilyAdvancement }, func(p *Policy, b bool) { p.FamilyAdvancement = b }, "family-advancement"},
		{"base-advancement", func(p Policy) bool { return p.BaseAdvancement }, func(p *Policy, b bool) { p.BaseAdvancement = b }, "base-advancement"},
		{"tag-changed", func(p Policy) bool { return p.TagChanged }, func(p *Policy, b bool) { p.TagChanged = b }, "tag-changed"},
		{"tag-mutation", func(p Policy) bool { return p.TagMutated }, func(p *Policy, b bool) { p.TagMutated = b }, "tag-mutation"},
		{"other-platform", func(p Policy) bool { return p.OtherPlatform }, func(p *Policy, b bool) { p.OtherPlatform = b }, "other-platform"},
	}

	for _, tc := range cases {
		t.Run(tc.testName, func(t *testing.T) {
			base := Default()
			// Flip the field relative to the default, both ways.
			for _, value := range []string{"true", "false"} {
				labels := map[string]string{labelPrefix + tc.key: value}
				got := ApplyLabels(base, labels)
				want := base
				tc.set(&want, value == "true")
				if got != want {
					t.Errorf("ApplyLabels(%s=%s) = %+v, want %+v", tc.key, value, got, want)
				}
			}
		})
	}
}

func TestApplyLabels_IgnoresInvalidAndUnrelated(t *testing.T) {
	base := Default()
	labels := map[string]string{
		labelPrefix + "patch": "not-a-bool",
		labelPrefix + "minor": "TRUE", // case-sensitive: only "true"/"false"
		"unrelated.label":     "true",
		labelPrefix:           "true",
	}
	if got := ApplyLabels(base, labels); got != base {
		t.Errorf("expected base policy unchanged, got %+v", got)
	}
}

func TestApplyLabels_EmptyLabels(t *testing.T) {
	base := Default()
	if got := ApplyLabels(base, nil); got != base {
		t.Errorf("expected nil labels to leave base unchanged")
	}
}

func TestParseBool(t *testing.T) {
	for _, s := range []string{"true", "false"} {
		if b, ok := parseBool(s); !ok || b != (s == "true") {
			t.Errorf("parseBool(%q) = %v, %v", s, b, ok)
		}
	}
	for _, s := range []string{"", "True", "1", "yes", "no"} {
		if _, ok := parseBool(s); ok {
			t.Errorf("parseBool(%q) should not be accepted", s)
		}
	}
}
