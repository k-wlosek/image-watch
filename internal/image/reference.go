package image

import (
	"fmt"
	"strings"
)

// DefaultRegistry is used when an image reference omits a registry host.
const DefaultRegistry = "docker.io"

// Reference identifies an image by registry, repository, and tag or digest.
type Reference struct {
	Registry   string
	Repository string
	Tag        *string
	Digest     *string
}

// IsDigestPinned reports whether the reference is pinned to a digest.
func (r Reference) IsDigestPinned() bool {
	return r.Digest != nil && r.Tag == nil
}

// TagOrEmpty returns the tag string, or "" if unset.
func (r Reference) TagOrEmpty() string {
	if r.Tag == nil {
		return ""
	}
	return *r.Tag
}

// String renders the reference in canonical form.
func (r Reference) String() string {
	base := fmt.Sprintf("%s/%s", r.Registry, r.Repository)
	if r.Tag != nil {
		return fmt.Sprintf("%s:%s", base, *r.Tag)
	}
	if r.Digest != nil {
		return fmt.Sprintf("%s@%s", base, *r.Digest)
	}
	return base
}

// ParseReference normalizes a raw image string into a Reference.
func ParseReference(raw string) (Reference, error) {
	if raw == "" {
		return Reference{}, fmt.Errorf("image: empty reference")
	}

	// Split off digest, if present.
	var digest *string
	name := raw
	if idx := strings.Index(raw, "@"); idx != -1 {
		d := raw[idx+1:]
		digest = &d
		name = raw[:idx]
	}

	// Determine registry versus repository.
	registry := DefaultRegistry
	repoAndTag := name

	if idx := strings.Index(name, "/"); idx != -1 {
		firstComponent := name[:idx]
		if strings.ContainsAny(firstComponent, ".:") || firstComponent == "localhost" {
			registry = firstComponent
			repoAndTag = name[idx+1:]
		}
	}

	// Split off tag only if no digest is present.
	var tag *string
	repository := repoAndTag
	if digest == nil {
		if idx := strings.LastIndex(repoAndTag, ":"); idx != -1 {
			t := repoAndTag[idx+1:]
			repository = repoAndTag[:idx]
			tag = &t
		}
	}

	// Docker Hub implicitly namespaces single-component repositories under "library/".
	if registry == DefaultRegistry && !strings.Contains(repository, "/") {
		repository = "library/" + repository
	}

	if repository == "" {
		return Reference{}, fmt.Errorf("image: could not determine repository from %q", raw)
	}

	if tag == nil && digest == nil {
		defaultTag := "latest"
		tag = &defaultTag
	}

	return Reference{
		Registry:   registry,
		Repository: repository,
		Tag:        tag,
		Digest:     digest,
	}, nil
}
