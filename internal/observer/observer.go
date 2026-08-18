// Package observer connects runtime, registry, and version data.
package observer

import (
	"context"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/image"
	"github.com/k-wlosek/image-watch/internal/policy"
	"github.com/k-wlosek/image-watch/internal/registry"
	iwruntime "github.com/k-wlosek/image-watch/internal/runtime"
	"github.com/k-wlosek/image-watch/internal/state"
	"github.com/k-wlosek/image-watch/internal/version"
)

// RegistryResolver returns a Registry client for a host.
type RegistryResolver func(registryHost string) registry.Registry

// skipLabel is a container label that excludes the container from
// observation entirely: no result, events, metrics, or notifications.
const skipLabel = "image-watch.skip"

// defaultConcurrencyWorkers bounds how many image groups are checked in
// parallel when the observer's ConcurrencyWorkers is left unset.
const defaultConcurrencyWorkers = 4

// Observer performs one check cycle.
type Observer struct {
	Runtime       iwruntime.Runtime
	Registries    RegistryResolver
	Store         state.Store
	DefaultPolicy policy.Policy

	// EnrichmentMaxTags and EnrichmentTimeout bound enrichment.
	EnrichmentMaxTags int
	EnrichmentTimeout time.Duration

	// ConcurrencyWorkers bounds how many image groups are resolved in
	// parallel during Check, and how many manifest fetches an enrichment
	// scan runs concurrently.
	ConcurrencyWorkers int

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

// less orders groupKey deterministically (registry, repository, tag, platform).
func (g groupKey) less(o groupKey) bool {
	if g.Registry != o.Registry {
		return g.Registry < o.Registry
	}
	if g.Repository != o.Repository {
		return g.Repository < o.Repository
	}
	if g.Tag != o.Tag {
		return g.Tag < o.Tag
	}
	if g.Platform.OS != o.Platform.OS {
		return g.Platform.OS < o.Platform.OS
	}
	if g.Platform.Architecture != o.Platform.Architecture {
		return g.Platform.Architecture < o.Platform.Architecture
	}
	return g.Platform.Variant < o.Platform.Variant
}

// Check performs one detection cycle.
func (o *Observer) Check(ctx context.Context) ([]Result, error) {
	containers, err := o.Runtime.ListContainers(ctx)
	if err != nil {
		return nil, err
	}

	groups := groupContainers(containers)
	if len(groups) == 0 {
		return nil, nil
	}

	// Groups run on a bounded worker pool. Keys are sorted first so the
	// result order is deterministic regardless of scheduling or timing;
	// each worker writes its own results slot, so no locking is needed.
	keys := sortedGroupKeys(groups)
	results := make([]Result, len(keys))

	g := new(errgroup.Group)
	g.SetLimit(o.workers(len(keys)))
	for i, key := range keys {
		members := groups[key]
		g.Go(func() error {
			results[i] = o.checkGroup(ctx, key, members)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	return results, nil
}

// sortedGroupKeys returns the group keys in deterministic order.
func sortedGroupKeys(groups map[groupKey][]iwruntime.ContainerObservation) []groupKey {
	keys := make([]groupKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].less(keys[j]) })
	return keys
}

// workerLimit returns the configured parallel registry-operation budget.
func (o *Observer) workerLimit() int {
	w := o.ConcurrencyWorkers
	if w <= 0 {
		w = defaultConcurrencyWorkers
	}
	if w < 1 {
		w = 1
	}
	return w
}

// workers bounds the pool size by both the configured limit and n.
func (o *Observer) workers(n int) int {
	w := min(o.workerLimit(), n)
	return w
}

// groupContainers collapses containers sharing the same image reference.
func groupContainers(containers []iwruntime.ContainerObservation) map[groupKey][]iwruntime.ContainerObservation {
	groups := make(map[groupKey][]iwruntime.ContainerObservation)
	for _, c := range containers {
		if c.Image.IsDigestPinned() || c.Image.Tag == nil {
			continue
		}
		if c.Labels[skipLabel] == "true" {
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

	// Opaque tags may need best-effort enrichment; share one tag-list
	// fetch across every enrichment attempt in this group so multiple
	// drifted running digests don't each re-list the repository.
	var cache *groupCache
	if tv.IsOpaque() {
		cache = newGroupCache()
	}

	digestEvents := o.detectDigestEvents(ctx, reg, key, tv, previous, found, registryObs, cache)
	candidateEvents, candidatesPartial := o.detectVersionCandidateEvents(ctx, reg, key)
	result.Events = append(result.Events, digestEvents...)
	if len(digestEvents) == 0 {
		// No digest transition this cycle, so surface point-in-time drift:
		// containers running a different digest than the registry serves.
		result.Events = append(result.Events, o.detectDigestDriftEvents(ctx, reg, key, tv, result.ServedDigest, registryObs.PlatformManifestDigest, result.ContainerDigests, cache)...)
	}
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
func (o *Observer) detectDigestEvents(ctx context.Context, reg registry.Registry, key groupKey, tv version.TagVersion, previous state.Observation, found bool, registryObs registry.ManifestObservation, cache *groupCache) []event.Event {
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

	ev.Type = digestEventType(tv)
	if tv.IsOpaque() {
		// Best-effort enrichment.
		if inferredTag, ok := o.attemptEnrichment(ctx, reg, key, registryObs.PlatformManifestDigest, cache); ok {
			ev.CandidateTag = inferredTag
		}
	}

	return []event.Event{ev}
}

// digestEventType reports the event type for a same-tag digest change or
// point-in-time drift: opaque tags get TagChanged, versionable tags get
// TagMutated.
func digestEventType(tv version.TagVersion) event.Type {
	if tv.IsOpaque() {
		return event.TagChanged
	}
	return event.TagMutated
}

// detectDigestDriftEvents surfaces containers running a different digest
// than the registry currently serves for their tag.
func (o *Observer) detectDigestDriftEvents(ctx context.Context, reg registry.Registry, key groupKey, tv version.TagVersion, served, platformDigest string, running []string, cache *groupCache) []event.Event {
	if served == "" {
		return nil
	}

	// Distinct running digests (the enrichment work is shared across them).
	var distinct []string
	seen := make(map[string]bool)
	for _, dig := range running {
		if dig == "" || dig == served || seen[dig] {
			continue
		}
		seen[dig] = true
		distinct = append(distinct, dig)
	}
	if len(distinct) == 0 {
		return nil
	}

	// Enrichment for each drifted digest is independent, so run the scans
	// concurrently, bounded by the same worker budget.
	inferred := make([]string, len(distinct))
	enriched := make([]bool, len(distinct))
	if tv.IsOpaque() && len(distinct) > 1 {
		var wg sync.WaitGroup
		sem := make(chan struct{}, o.workerLimit())
		for i := range distinct {
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				if t, ok := o.attemptEnrichment(ctx, reg, key, platformDigest, cache); ok {
					inferred[i] = t
					enriched[i] = true
				}
			})
		}
		wg.Wait()
	} else if tv.IsOpaque() {
		if t, ok := o.attemptEnrichment(ctx, reg, key, platformDigest, cache); ok {
			inferred[0] = t
			enriched[0] = true
		}
	}

	ref := image.Reference{Registry: key.Registry, Repository: key.Repository, Tag: &key.Tag}
	events := make([]event.Event, 0, len(distinct))
	for i, dig := range distinct {
		ev := event.Event{
			Timestamp:       o.now(),
			Image:           ref,
			Type:            digestEventType(tv),
			CurrentTag:      key.Tag,
			CurrentDigest:   dig,
			CandidateDigest: served,
			Platform:        key.Platform,
		}
		if enriched[i] {
			ev.CandidateTag = inferred[i]
		}
		events = append(events, ev)
	}
	return events
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

	// Winners are resolved concurrently (they're independent), then events
	// are assembled in canonical order so the result is deterministic.
	type candidateEntry struct {
		t event.Type
		c *version.Candidate
	}
	var entries []candidateEntry
	add := func(t event.Type, c *version.Candidate) {
		if c != nil {
			entries = append(entries, candidateEntry{t, c})
		}
	}
	add(event.PatchAvailable, cs.Patch)
	add(event.MinorAvailable, cs.Minor)
	add(event.MajorAvailable, cs.Major)
	add(event.FamilyAdvancementAvailable, cs.FamilyAdvancement)
	add(event.ApplicationPatchAvailable, cs.ApplicationPatch)
	add(event.ApplicationMinorAvailable, cs.ApplicationMinor)
	add(event.ApplicationMajorAvailable, cs.ApplicationMajor)
	add(event.BaseAdvancementAvailable, cs.BaseAdvancement)
	if len(entries) == 0 {
		return nil, false
	}

	// Resolve each distinct winning candidate once for the running platform.
	distinct := make([]string, 0, len(entries))
	index := make(map[string]int)
	for _, e := range entries {
		if _, ok := index[e.c.Tag]; !ok {
			index[e.c.Tag] = len(distinct)
			distinct = append(distinct, e.c.Tag)
		}
	}

	opts := make([]registry.ManifestObservation, len(distinct))
	errs := make([]error, len(distinct))
	var wg sync.WaitGroup
	sem := make(chan struct{}, o.workerLimit())
	for i, tag := range distinct {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			opts[i], errs[i] = reg.ResolveForPlatform(ctx, key.Repository, tag, key.Platform)
		})
	}
	wg.Wait()

	partial := false
	events := make([]event.Event, 0, len(entries))
	for _, e := range entries {
		obs, err := opts[index[e.c.Tag]], errs[index[e.c.Tag]]
		if err != nil {
			// Can't determine platform availability.
			partial = true
			continue
		}
		actualType := e.t
		if obs.PlatformManifestDigest == "" {
			// No manifest for the running platform.
			actualType = event.OtherPlatformUpdate
		}

		ev := event.Event{
			Timestamp:    o.now(),
			Image:        ref,
			Type:         actualType,
			CurrentTag:   key.Tag,
			CandidateTag: e.c.Tag,
			Platform:     key.Platform,
		}
		if cs.Combined != nil && (e.t == event.ApplicationPatchAvailable || e.t == event.ApplicationMinorAvailable || e.t == event.ApplicationMajorAvailable || e.t == event.BaseAdvancementAvailable) {
			ev.CombinedCandidate = cs.Combined.Tag
		}
		events = append(events, ev)
	}

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
