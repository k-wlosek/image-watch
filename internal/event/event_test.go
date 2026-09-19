package event

import "testing"

func TestCategoryLabel(t *testing.T) {
	cases := map[Type]string{
		PatchAvailable:             "PATCH",
		ApplicationPatchAvailable:  "PATCH",
		MinorAvailable:             "MINOR",
		ApplicationMinorAvailable:  "MINOR",
		MajorAvailable:             "MAJOR",
		ApplicationMajorAvailable:  "MAJOR",
		FamilyAdvancementAvailable: "FAMILY ADVANCEMENT",
		BaseAdvancementAvailable:   "BASE ADVANCEMENT",
		TagChanged:                 "TAG CHANGED",
		TagMutated:                 "TAG MUTATED",
		OtherPlatformUpdate:        "OTHER PLATFORM UPDATE",
	}
	for typ, want := range cases {
		if got := CategoryLabel(typ); got != want {
			t.Errorf("CategoryLabel(%s) = %q, want %q", typ, got, want)
		}
	}
	if got := CategoryLabel("UNKNOWN_EVENT"); got != "UNKNOWN_EVENT" {
		t.Errorf("CategoryLabel(unknown) = %q, want the raw string", got)
	}
}
