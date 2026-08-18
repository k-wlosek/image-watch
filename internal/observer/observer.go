// Package observer connects runtime, registry, and version data.
package observer

import (
	"context"
	"time"

	"github.com/example/image-watch/internal/event"
	"github.com/example/image-watch/internal/image"
	"github.com/example/image-watch/internal/policy"
	"github.com/example/image-watch/internal/registry"
	iwruntime "github.com/example/image-watch/internal/runtime"
	"github.com/example/image-watch/internal/state"
	"github.com/example/image-watch/internal/version"
)

// RegistryResolver returns a Registry client for a host.
type RegistryResolver func(registryHost string) registry.Registry

// Observer performs one check cycle.
type Observer struct {
	Runtime       iwruntime.Runtime
	Registries    RegistryResolver
	Store         state.Store
	DefaultPolicy policy.Policy

	// EnrichmentMaxTags and EnrichmentTimeout bound enrichment.
	EnrichmentMaxTags int
	EnrichmentTimeout time.Duration

	// Metrics is an optional enrichment telemetry hook.
	Metrics EnrichmentObserver

	// Now is overridable for tests.
	Now func() time.Time
}

// Result is everything determined for one monitored image.
type Result struct {
	Image           image.Reference
	Platform        image.Platform
	EffectivePolicy policy.Policy
	Events          []event.Event
	ContainerNames  []string

	// ServedDigest is what the registry currently serves for the running
	// tag: the index digest when the tag points to a multi-arch index,
	// otherwise the platform manifest digest. Empty when the registry
	// check failed.
	ServedDigest string

	// ContainerDigests holds the digest each member container is running,
	// aligned with ContainerNames. Entries may be empty when the running
	// digest is unknown (e.g. locally built images).
	ContainerDigests []string

	Stale bool
	Err   error

	// Partial is true when the current tag's own registry check
	// succeeded (Err == nil) but one or more auxiliary lookups this
	// cycle failed - specifically, listing tags for candidate analysis,
	// the previous-observation state read, or per-candidate platform
	// verification.
	Partial bool
}

// groupKey is the grouping key for one Check call.
type groupKey struct {
	Registry   string
	Repository string
	Tag        string
	Platform   image.Platform
}

func (g groupKey) stateKey() state.Key {
	return state.Key{Registry: g.Registry, Repository: g.Repository, Tag: g.Tag, Platform: g.Platform}
}

// Check performs one detection cycle.
func (o *Observer) Check(ctx context.Context) ([]Result, error) {
	containers, err := o.Runtime.ListContainers(ctx)
	if err != nil {
		return nil, err
	}

	groups := groupContainers(containers)

	results := make([]Result, 0, len(groups))
	for key, members := range groups {
		results = append(results, o.checkGroup(ctx, key, members))
	}
	return results, nil
}

// groupContainers collapses containers sharing the same image reference.
func groupContainers(containers []iwruntime.ContainerObservation) map[groupKey][]iwruntime.ContainerObservation {
	groups := make(map[groupKey][]iwruntime.ContainerObservation)
	for _, c := range containers {
		if c.Image.IsDigestPinned() || c.Image.Tag == nil {
			continue
		}
		key := groupKey{
			Registry:   c.Image.Registry,
			Repository: c.Image.Repository,
			Tag:        *c.Image.Tag,
			Platform:   c.Platform,
		}
		groups[key] = append(groups[key], c)
	}
	return groups
}

func (o *Observer) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o *Observer) checkGroup(ctx context.Context, key groupKey, members []iwruntime.ContainerObservation) Result {
	result := Result{
		Image:            image.Reference{Registry: key.Registry, Repository: key.Repository, Tag: &key.Tag},
		Platform:         key.Platform,
		EffectivePolicy:  effectivePolicyFor(o.DefaultPolicy, members),
		ContainerNames:   containerNames(members),
		ContainerDigests: containerDigests(members),
	}

	reg := o.Registries(key.Registry)
	if reg == nil {
		result.Err = &UnresolvedRegistryError{Registry: key.Registry}
		o.markStale(ctx, key, result.Err)
		return result
	}

	previous, found, storeErr := o.Store.GetObservation(ctx, key.stateKey())
	if storeErr != nil {
		result.Partial = true
	}

	registryObs, err := reg.ResolveForPlatform(ctx, key.Repository, key.Tag, key.Platform)
	if err != nil {
		// Retain previous state and mark the result stale.
		result.Err = err
		result.Stale = true
		o.markStale(ctx, key, err)
		return result
	}

	// The served digest is index-level when the tag resolves to a
	// multi-arch index; the Docker adapter's RepoDigests are also
	// index-level, so this is the apples-to-apples comparison target.
	result.ServedDigest = registryObs.IndexDigest
	if result.ServedDigest == "" {
		result.ServedDigest = registryObs.PlatformManifestDigest
	}

	newObs := state.Observation{
		Key:                    key.stateKey(),
		PlatformManifestDigest: registryObs.PlatformManifestDigest,
		IndexDigest:            registryObs.IndexDigest,
		LastSuccess:            o.now(),
		Status:                 state.StatusFresh,
	}
	// Not returning early on PutObservation errors: a storage failure
	// shouldn't discard already-detected events for this cycle
	if err := o.Store.PutObservation(ctx, newObs); err != nil {
		result.Partial = true
	}

	tv := version.ParseTag(key.Tag)

	result.Events = append(result.Events, o.detectDigestEvents(ctx, reg, key, tv, previous, found, registryObs)...)
	candidateEvents, candidatesPartial := o.detectVersionCandidateEvents(ctx, reg, key)
	result.Events = append(result.Events, candidateEvents...)
	if candidatesPartial {
		result.Partial = true
	}

	return result
}

func (o *Observer) markStale(ctx context.Context, key groupKey, checkErr error) {
	previous, found, _ := o.Store.GetObservation(ctx, key.stateKey())
	obs := previous
	obs.Key = key.stateKey()
	if !found {
		obs.Status = state.StatusUnknown
	} else {
		obs.Status = state.StatusStale
	}
	obs.LastError = checkErr.Error()
	obs.LastErrorAt = o.now()
	_ = o.Store.PutObservation(ctx, obs)
}

// detectDigestEvents compares digests over time.
func (o *Observer) detectDigestEvents(ctx context.Context, reg registry.Registry, key groupKey, tv version.TagVersion, previous state.Observation, found bool, registryObs registry.ManifestObservation) []event.Event {
	if !found || previous.PlatformManifestDigest == "" || registryObs.PlatformManifestDigest == "" {
		return nil
	}
	if previous.PlatformManifestDigest == registryObs.PlatformManifestDigest {
		return nil
	}

	ev := event.Event{
		Timestamp:       o.now(),
		Image:           image.Reference{Registry: key.Registry, Repository: key.Repository, Tag: &key.Tag},
		CurrentTag:      key.Tag,
		CurrentDigest:   previous.PlatformManifestDigest,
		CandidateDigest: registryObs.PlatformManifestDigest,
		Platform:        key.Platform,
	}

	if tv.IsOpaque() {
		ev.Type = event.TagChanged
		// Best-effort enrichment.
		if inferredTag, ok := o.attemptEnrichment(ctx, reg, key, registryObs.PlatformManifestDigest); ok {
			ev.CandidateTag = inferredTag
		}
	} else {
		ev.Type = event.TagMutated
	}

	return []event.Event{ev}
}

// detectVersionCandidateEvents computes version-based candidates.
func (o *Observer) detectVersionCandidateEvents(ctx context.Context, reg registry.Registry, key groupKey) ([]event.Event, bool) {
	tv := version.ParseTag(key.Tag)
	if tv.IsOpaque() {
		return nil, false
	}
	if _, ok := tv.Application(); !ok {
		return nil, false
	}

	tags, err := reg.ListTags(ctx, key.Repository)
	if err != nil {
		// Candidate analysis simply produces no events if we can't list
		// tags - not a fatal error, but the result should say so
		return nil, true
	}

	cs := version.AnalyzeCandidates(key.Tag, tags)
	ref := image.Reference{Registry: key.Registry, Repository: key.Repository, Tag: &key.Tag}

	// resolveCache memoizes ResolveForPlatform calls by candidate tag
	// within this single invocation.
	type resolveResult struct {
		obs registry.ManifestObservation
		err error
	}
	resolveCache := make(map[string]resolveResult)
	resolveCandidate := func(tag string) (registry.ManifestObservation, error) {
		if r, ok := resolveCache[tag]; ok {
			return r.obs, r.err
		}
		obs, err := reg.ResolveForPlatform(ctx, key.Repository, tag, key.Platform)
		resolveCache[tag] = resolveResult{obs, err}
		return obs, err
	}

	var events []event.Event
	partial := false

	// add resolves each winning candidate's manifest for the running
	// platform before emitting anything
	add := func(t event.Type, c *version.Candidate) {
		if c == nil {
			return
		}

		actualType := t
		obs, err := resolveCandidate(c.Tag)
		if err != nil {
			// Can't determine platform availability.
			partial = true
			return
		}
		if obs.PlatformManifestDigest == "" {
			// No manifest for the running platform.
			actualType = event.OtherPlatformUpdate
		}

		ev := event.Event{
			Timestamp:    o.now(),
			Image:        ref,
			Type:         actualType,
			CurrentTag:   key.Tag,
			CandidateTag: c.Tag,
			Platform:     key.Platform,
		}
		if cs.Combined != nil && (t == event.ApplicationPatchAvailable || t == event.ApplicationMinorAvailable || t == event.ApplicationMajorAvailable || t == event.BaseAdvancementAvailable) {
			ev.CombinedCandidate = cs.Combined.Tag
		}
		events = append(events, ev)
	}

	add(event.PatchAvailable, cs.Patch)
	add(event.MinorAvailable, cs.Minor)
	add(event.MajorAvailable, cs.Major)
	add(event.FamilyAdvancementAvailable, cs.FamilyAdvancement)
	add(event.ApplicationPatchAvailable, cs.ApplicationPatch)
	add(event.ApplicationMinorAvailable, cs.ApplicationMinor)
	add(event.ApplicationMajorAvailable, cs.ApplicationMajor)
	add(event.BaseAdvancementAvailable, cs.BaseAdvancement)

	return events, partial
}

func effectivePolicyFor(base policy.Policy, members []iwruntime.ContainerObservation) policy.Policy {
	perContainer := make([]policy.Policy, 0, len(members))
	for _, c := range members {
		perContainer = append(perContainer, policy.ApplyLabels(base, c.Labels))
	}
	return policy.MergeAll(perContainer)
}

func containerNames(members []iwruntime.ContainerObservation) []string {
	names := make([]string, 0, len(members))
	for _, c := range members {
		names = append(names, c.Name)
	}
	return names
}

// containerDigests returns each member's running digest, aligned with
// containerNames.
func containerDigests(members []iwruntime.ContainerObservation) []string {
	digests := make([]string, 0, len(members))
	for _, c := range members {
		digests = append(digests, c.Digest)
	}
	return digests
}

// UnresolvedRegistryError indicates no Registry client was configured.
type UnresolvedRegistryError struct {
	Registry string
}

func (e *UnresolvedRegistryError) Error() string {
	return "observer: no registry client configured for host " + e.Registry
}
