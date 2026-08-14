package version

// Candidate is one selected update candidate.
type Candidate struct {
	Tag     string
	Version SemVer
}

// CandidateSet is the full result of analyzing one running tag.
type CandidateSet struct {
	CurrentRaw string
	Current    TagVersion

	// Patch/Minor/Major apply to plain tags.
	Patch *Candidate
	Minor *Candidate
	Major *Candidate

	// FamilyAdvancement applies only to major-precision standalone tags.
	FamilyAdvancement *Candidate

	// ApplicationPatch/Minor/Major are the composite-tag equivalents.
	ApplicationPatch *Candidate
	ApplicationMinor *Candidate
	ApplicationMajor *Candidate

	// BaseAdvancement is the composite tag's base-component update.
	BaseAdvancement *Candidate

	// Combined is the actual tag carrying both application and base candidates.
	Combined *Candidate
}

// AnalyzeCandidates selects update candidates from available tags.
func AnalyzeCandidates(currentRaw string, availableRaw []string) CandidateSet {
	current := ParseTag(currentRaw)
	cs := CandidateSet{CurrentRaw: currentRaw, Current: current}

	currentApp, hasApp := current.Application()
	if !hasApp {
		// Opaque tags get no version candidates.
		return cs
	}

	isComposite := current.IsComposite()
	currentBase, hasBase := current.Base()

	var bestPatch, bestMinor, bestMajor, bestFamily, bestBase *Candidate

	for _, raw := range availableRaw {
		if raw == currentRaw {
			continue
		}
		cand := ParseTag(raw)

		// Only compare tags of the same shape on the application axis.
		if app, ok := cand.Application(); ok && cand.IsComposite() == isComposite {
			classifyApplicationCandidate(raw, app.Version, currentApp.Version, &bestPatch, &bestMinor, &bestMajor, &bestFamily)
		}

		// Base tracking only requires a usable base component from the same family.
		if hasBase && currentBase.Version.Precision != PrecisionUnknown {
			if base, ok := cand.Base(); ok && base.Name == currentBase.Name {
				if base.Version.Compare(currentBase.Version) > 0 {
					if bestBase == nil || base.Version.Compare(bestBase.Version) > 0 {
						bestBase = &Candidate{Tag: raw, Version: base.Version}
					}
				}
			}
		}
	}

	assignApplicationCandidates(&cs, isComposite, bestPatch, bestMinor, bestMajor, bestFamily)
	cs.BaseAdvancement = bestBase

	cs.Combined = findCombinedCandidate(availableRaw, currentRaw, isComposite, bestApplicationOverall(&cs), bestBase)

	return cs
}

// classifyApplicationCandidate classifies application-axis candidates.
func classifyApplicationCandidate(raw string, cand, cur SemVer, bestPatch, bestMinor, bestMajor, bestFamily **Candidate) {
	if cand.Major > cur.Major {
		update(bestMajor, raw, cand, cur)
		return
	}
	if cand.Major != cur.Major {
		return // older major; not a candidate
	}

	switch cur.Precision {
	case PrecisionPatch:
		if cand.Minor > cur.Minor {
			update(bestMinor, raw, cand, cur)
			return
		}
		if cand.Minor == cur.Minor && cand.Patch > cur.Patch {
			update(bestPatch, raw, cand, cur)
		}
	case PrecisionMinor:
		// Any same-major advancement is reported as patch.
		if cand.Compare(cur) > 0 {
			update(bestPatch, raw, cand, cur)
		}
	case PrecisionMajor:
		if cand.Compare(cur) > 0 {
			update(bestFamily, raw, cand, cur)
		}
	}
}

func update(slot **Candidate, raw string, cand, _ SemVer) {
	if *slot == nil || cand.Compare((*slot).Version) > 0 {
		*slot = &Candidate{Tag: raw, Version: cand}
	}
}

// assignApplicationCandidates routes the computed candidates into the set.
func assignApplicationCandidates(cs *CandidateSet, isComposite bool, bestPatch, bestMinor, bestMajor, bestFamily *Candidate) {
	if isComposite {
		cs.ApplicationPatch = bestPatch
		cs.ApplicationMinor = bestMinor
		cs.ApplicationMajor = bestMajor
		// FamilyAdvancement is never populated for composite tags.
		return
	}
	cs.Patch = bestPatch
	cs.Minor = bestMinor
	cs.Major = bestMajor
	cs.FamilyAdvancement = bestFamily
}

// bestApplicationOverall picks the most advanced application candidate.
func bestApplicationOverall(cs *CandidateSet) *Candidate {
	for _, c := range []*Candidate{cs.Major, cs.ApplicationMajor, cs.Minor, cs.ApplicationMinor, cs.FamilyAdvancement, cs.Patch, cs.ApplicationPatch} {
		if c != nil {
			return c
		}
	}
	return nil
}

// findCombinedCandidate looks for an existing tag that carries both candidates.
func findCombinedCandidate(availableRaw []string, currentRaw string, isComposite bool, bestApp, bestBase *Candidate) *Candidate {
	if !isComposite || bestApp == nil || bestBase == nil {
		return nil
	}
	for _, raw := range availableRaw {
		if raw == currentRaw {
			continue
		}
		cand := ParseTag(raw)
		app, ok := cand.Application()
		if !ok || app.Version.Compare(bestApp.Version) != 0 {
			continue
		}
		base, ok := cand.Base()
		if !ok || base.Version.Compare(bestBase.Version) != 0 {
			continue
		}
		return &Candidate{Tag: raw, Version: app.Version}
	}
	return nil
}
