package version

import "testing"

// TestAnalyzeCandidates_Scenario1_StandardSemVer covers a standard SemVer tag.
func TestAnalyzeCandidates_Scenario1_StandardSemVer(t *testing.T) {
	available := []string{"1.2.4", "1.2.5", "1.3.0", "1.3.1", "2.0.0"}
	cs := AnalyzeCandidates("1.2.3", available)

	assertCandidate(t, "patch", cs.Patch, "1.2.5")
	assertCandidate(t, "minor", cs.Minor, "1.3.1")
	assertCandidate(t, "major", cs.Major, "2.0.0")
}

// TestAnalyzeCandidates_Scenario2_AlpineComposite covers a composite tag.
func TestAnalyzeCandidates_Scenario2_AlpineComposite(t *testing.T) {
	available := []string{"1.2.4-alpine3.22", "1.2.3-alpine3.23", "1.2.4-alpine3.23"}
	cs := AnalyzeCandidates("1.2.3-alpine3.22", available)

	assertCandidate(t, "application patch", cs.ApplicationPatch, "1.2.4-alpine3.22")
	assertCandidate(t, "base advancement", cs.BaseAdvancement, "1.2.3-alpine3.23")
	assertCandidate(t, "combined", cs.Combined, "1.2.4-alpine3.23")
}

// TestAnalyzeCandidates_Scenario5_PostgresFamily covers family advancement.
func TestAnalyzeCandidates_Scenario5_PostgresFamily(t *testing.T) {
	t.Run("major precision (postgres:16)", func(t *testing.T) {
		available := []string{"16", "16.1", "16.2", "16.3", "17"}
		cs := AnalyzeCandidates("16", available)

		assertCandidate(t, "family advancement", cs.FamilyAdvancement, "16.3")
		assertCandidate(t, "major", cs.Major, "17")
		if cs.Patch != nil {
			t.Errorf("expected no PATCH candidate for major-precision tag, got %v", cs.Patch)
		}
	})

	t.Run("minor precision (postgres:16.2)", func(t *testing.T) {
		available := []string{"16.2", "16.3", "17"}
		cs := AnalyzeCandidates("16.2", available)

		// Same-major advancement on a minor-precision tag is classified as patch.
		assertCandidate(t, "patch", cs.Patch, "16.3")
		assertCandidate(t, "major", cs.Major, "17")
		if cs.FamilyAdvancement != nil {
			t.Errorf("expected no FAMILY_ADVANCEMENT for minor-precision tag, got %v", cs.FamilyAdvancement)
		}
		if cs.Minor != nil {
			t.Errorf("expected no MINOR candidate for minor-precision tag, got %v", cs.Minor)
		}
	})

	t.Run("patch precision (postgres:16.2.3)", func(t *testing.T) {
		available := []string{"16.2.4", "16.3.0", "17.0.0"}
		cs := AnalyzeCandidates("16.2.3", available)

		assertCandidate(t, "patch", cs.Patch, "16.2.4")
		assertCandidate(t, "minor", cs.Minor, "16.3.0")
		assertCandidate(t, "major", cs.Major, "17.0.0")
	})
}

func TestAnalyzeCandidates_NoOlderCandidatesSelected(t *testing.T) {
	// Older or equal tags must never be selected.
	cs := AnalyzeCandidates("2.0.0", []string{"1.9.9", "1.0.0", "2.0.0"})
	if cs.Patch != nil || cs.Minor != nil || cs.Major != nil {
		t.Errorf("expected no candidates when nothing is newer, got %+v", cs)
	}
}

func TestAnalyzeCandidates_OnlyNewestPerCategory(t *testing.T) {
	// Only the newest patch candidate survives.
	cs := AnalyzeCandidates("1.2.3", []string{"1.2.4", "1.2.5", "1.2.6"})
	assertCandidate(t, "patch", cs.Patch, "1.2.6")
}

func TestAnalyzeCandidates_CrossStreamNotCompared(t *testing.T) {
	// Plain and composite tags are different streams.
	cs := AnalyzeCandidates("1.2.3", []string{"1.2.4-alpine3.22"})
	if cs.Patch != nil {
		t.Errorf("expected no cross-stream patch candidate, got %v", cs.Patch)
	}
}

func TestAnalyzeCandidates_OpaqueCurrentTagYieldsNoCandidates(t *testing.T) {
	cs := AnalyzeCandidates("latest", []string{"1.2.3", "1.2.4"})
	if cs.Patch != nil || cs.Minor != nil || cs.Major != nil || cs.FamilyAdvancement != nil {
		t.Errorf("expected no version candidates for an opaque running tag, got %+v", cs)
	}
}

func TestAnalyzeCandidates_BaseAdvancementRequiresVersionedCurrentBase(t *testing.T) {
	// A base without a version cannot be compared for advancement.
	cs := AnalyzeCandidates("1.2.3-alpine", []string{"1.2.3-alpine3.22"})
	if cs.BaseAdvancement != nil {
		t.Errorf("expected no base advancement when current base has no version, got %v", cs.BaseAdvancement)
	}
}

func assertCandidate(t *testing.T, label string, got *Candidate, wantTag string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: expected candidate %q, got nil", label, wantTag)
		return
	}
	if got.Tag != wantTag {
		t.Errorf("%s: got tag %q, want %q", label, got.Tag, wantTag)
	}
}
