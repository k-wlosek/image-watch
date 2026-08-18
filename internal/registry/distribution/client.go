// Package distribution implements the registry client.
package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/example/image-watch/internal/image"
	"github.com/example/image-watch/internal/registry"
)

// maxTagListPages bounds pagination for tag listing.
const maxTagListPages = 50

// Instrumentation records per-request registry telemetry.
type Instrumentation interface {
	ObserveRequest(registryHost string, duration time.Duration, err error)
}

// Client is a Registry implementation backed by HTTP calls.
type Client struct {
	Host string

	// Scheme is the connection scheme for baseURL(): "https" (default)
	// or "http" for insecure registries.
	Scheme     string
	httpClient *http.Client
	auth       *authenticator

	// Instrumentation is optional.
	Instrumentation Instrumentation
}

// New constructs a distribution client bound to a registry host.
func New(host string, httpClient *http.Client, credentials CredentialProvider) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		Host:       host,
		httpClient: httpClient,
		auth:       newAuthenticator(httpClient, credentials),
	}
}

var _ registry.Registry = (*Client)(nil)

func (h *Client) baseURL() string {
	// docker.io uses a different API host.
	host := h.Host
	if host == image.DefaultRegistry {
		host = "registry-1.docker.io"
	}
	scheme := h.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + host
}

func (h *Client) ListTags(ctx context.Context, repository string) ([]string, error) {
	var allTags []string
	next := fmt.Sprintf("%s/v2/%s/tags/list", h.baseURL(), repository)

	for page := 0; page < maxTagListPages && next != ""; page++ {
		body, headers, err := h.doAuthenticatedGET(ctx, repository, "pull", next)
		if err != nil {
			return nil, err
		}

		var resp tagListResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, newError(ErrClassRegistry, repository, "malformed tags/list response", 0, err)
		}
		allTags = append(allTags, resp.Tags...)

		next = parseNextLink(headers.Get("Link"), h.baseURL())
	}

	return allTags, nil
}

// resolveRaw performs the single underlying manifest/index HTTP fetch
// and returns everything callers need without a second round trip
// eliminates.
func (h *Client) resolveRaw(ctx context.Context, repository, reference string) (body []byte, digest, mediaType string, err error) {
	u := fmt.Sprintf("%s/v2/%s/manifests/%s", h.baseURL(), repository, url.PathEscape(reference))

	body, headers, err := h.doAuthenticatedGET(ctx, repository, "pull", u)
	if err != nil {
		return nil, "", "", err
	}

	var env manifestEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, "", "", newError(ErrClassRegistry, repository, "malformed manifest response", 0, err)
	}
	mediaType = env.MediaType
	if mediaType == "" {
		mediaType = headers.Get("Content-Type")
	}

	digest = headers.Get("Docker-Content-Digest")
	if digest == "" {
		// Some registries omit the header.
		return nil, "", "", newError(ErrClassRegistry, repository, "registry did not return Docker-Content-Digest header", 0, nil)
	}

	return body, digest, mediaType, nil
}

// parseIndex decodes an OCI image index / Docker manifest list body,
// returning both the parsed index and the ManifestObservation shape
func parseIndex(body []byte, digest, mediaType, repository string) (imageIndex, registry.ManifestObservation, error) {
	var idx imageIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return imageIndex{}, registry.ManifestObservation{}, newError(ErrClassRegistry, repository, "malformed image index", 0, err)
	}
	var platforms []image.Platform
	for _, m := range idx.Manifests {
		platforms = append(platforms, image.Platform{
			OS:           m.Platform.OS,
			Architecture: m.Platform.Architecture,
			Variant:      m.Platform.Variant,
		})
	}
	return idx, registry.ManifestObservation{
		IndexDigest:        digest,
		MediaType:          mediaType,
		AvailablePlatforms: platforms,
	}, nil
}

func (h *Client) Resolve(ctx context.Context, repository string, reference string) (registry.ManifestObservation, error) {
	body, digest, mediaType, err := h.resolveRaw(ctx, repository, reference)
	if err != nil {
		return registry.ManifestObservation{}, err
	}

	if isIndexMediaType(mediaType) {
		_, obs, err := parseIndex(body, digest, mediaType, repository)
		return obs, err
	}

	// Single-platform manifest
	return registry.ManifestObservation{
		PlatformManifestDigest: digest,
		MediaType:              mediaType,
	}, nil
}

// ResolveForPlatform resolves a reference to an actionable,
// platform-verified digest
func (h *Client) ResolveForPlatform(ctx context.Context, repository, reference string, platform image.Platform) (registry.ManifestObservation, error) {
	body, digest, mediaType, err := h.resolveRaw(ctx, repository, reference)
	if err != nil {
		return registry.ManifestObservation{}, err
	}

	if isIndexMediaType(mediaType) {
		idx, obs, err := parseIndex(body, digest, mediaType, repository)
		if err != nil {
			return registry.ManifestObservation{}, err
		}
		for _, m := range idx.Manifests {
			entryPlatform := image.Platform{OS: m.Platform.OS, Architecture: m.Platform.Architecture, Variant: m.Platform.Variant}
			if platformMatches(entryPlatform, platform) {
				obs.PlatformManifestDigest = m.Digest
				p := platform
				obs.Platform = &p
				return obs, nil
			}
		}
		// No entry for the running platform
		return obs, nil
	}

	// Single-platform manifest
	actual, err := h.fetchConfigPlatform(ctx, repository, body)
	if err != nil {
		// Can't determine the manifest's actual platform
		return registry.ManifestObservation{}, err
	}

	if !platformMatches(actual, platform) {
		return registry.ManifestObservation{AvailablePlatforms: []image.Platform{actual}}, nil
	}

	p := actual
	return registry.ManifestObservation{
		PlatformManifestDigest: digest,
		MediaType:              mediaType,
		Platform:               &p,
	}, nil
}

// platformMatches reports whether a candidate platform (from a registry
// manifest/index entry, or a single manifest's resolved config blob)
// satisfies a requested platform
func platformMatches(candidate, want image.Platform) bool {
	if candidate.OS != want.OS || candidate.Architecture != want.Architecture {
		return false
	}
	return variantCompatible(candidate.Architecture, want.Variant, candidate.Variant)
}

// variantCompatible treats an empty variant and the architecture's default
// variant as the same machine.
func variantCompatible(arch, requested, entry string) bool {
	if requested == entry {
		return true
	}
	if arch == "arm64" {
		switch {
		case requested == "" && entry == "v8":
			return true
		case requested == "v8" && entry == "":
			return true
		}
	}
	return false
}

// fetchConfigPlatform reads the platform (OS/architecture/variant) a
// single-platform manifest actually targets, by fetching the config
// blob its "config" descriptor points at.
func (h *Client) fetchConfigPlatform(ctx context.Context, repository string, manifestBody []byte) (image.Platform, error) {
	var ref manifestConfigRef
	if err := json.Unmarshal(manifestBody, &ref); err != nil || ref.Config.Digest == "" {
		return image.Platform{}, newError(ErrClassRegistry, repository, "manifest has no usable config descriptor", 0, err)
	}

	u := fmt.Sprintf("%s/v2/%s/blobs/%s", h.baseURL(), repository, url.PathEscape(ref.Config.Digest))
	body, _, err := h.doAuthenticatedGET(ctx, repository, "pull", u)
	if err != nil {
		return image.Platform{}, err
	}

	var cfg imageConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return image.Platform{}, newError(ErrClassRegistry, repository, "malformed image config blob", 0, err)
	}
	return image.Platform{OS: cfg.OS, Architecture: cfg.Architecture, Variant: cfg.Variant}, nil
}

// doAuthenticatedGET performs a GET with bearer-token handling.
func (h *Client) doAuthenticatedGET(ctx context.Context, repository, action, rawURL string) ([]byte, http.Header, error) {
	start := time.Now()
	body, headers, err := h.doAuthenticatedGETInner(ctx, repository, action, rawURL)
	if h.Instrumentation != nil {
		h.Instrumentation.ObserveRequest(h.Host, time.Since(start), err)
	}
	return body, headers, err
}

func (h *Client) doAuthenticatedGETInner(ctx context.Context, repository, action, rawURL string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, newError(ErrClassInvalidReference, repository, "invalid request URL", 0, err)
	}
	req.Header.Set("Accept", strings.Join(Accept, ", "))

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, nil, newError(ErrClassNetwork, repository, "request failed", 0, err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		wwwAuth := resp.Header.Get("WWW-Authenticate")
		resp.Body.Close()

		c, ok := parseBearerChallenge(wwwAuth)
		if !ok {
			return nil, nil, newError(ErrClassAuthentication, repository, "authentication required, no usable challenge", http.StatusUnauthorized, nil)
		}
		if c.Scope == "" {
			c.Scope = fmt.Sprintf("repository:%s:%s", repository, action)
		}

		token, terr := h.auth.tokenFor(ctx, h.Host, c)
		if terr != nil {
			return nil, nil, terr
		}

		req2, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, nil, newError(ErrClassInvalidReference, repository, "invalid request URL", 0, err)
		}
		req2.Header.Set("Accept", strings.Join(Accept, ", "))
		req2.Header.Set("Authorization", "Bearer "+token)

		resp2, err := h.httpClient.Do(req2)
		if err != nil {
			return nil, nil, newError(ErrClassNetwork, repository, "request failed after authentication", 0, err)
		}
		defer resp2.Body.Close()

		if resp2.StatusCode == http.StatusUnauthorized {
			// Cached token was rejected.
			h.auth.invalidate(c)
			return nil, nil, newError(ErrClassAuthentication, repository, "authentication rejected after token exchange", http.StatusUnauthorized, nil)
		}
		return readBody(resp2, repository)
	}

	defer resp.Body.Close()
	return readBody(resp, repository)
}

func readBody(resp *http.Response, repository string) ([]byte, http.Header, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		class := classifyStatus(resp.StatusCode)
		msg := "unexpected registry response"
		switch class {
		case ErrClassRepoNotFound:
			msg = "repository or reference not found"
		case ErrClassRateLimit:
			msg = "registry rate limit exceeded"
		case ErrClassAuthorization:
			msg = "not authorized for this repository"
		}
		return nil, resp.Header, newError(class, repository, msg, resp.StatusCode, nil)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, newError(ErrClassNetwork, repository, "failed reading response body", resp.StatusCode, err)
	}
	return body, resp.Header, nil
}

// linkPattern extracts the URL portion of a next-page Link header.
var linkPattern = regexp.MustCompile(`<([^>]+)>;\s*rel="?next"?`)

// parseNextLink extracts the next-page URL from a Link header.
func parseNextLink(linkHeader, baseURL string) string {
	if linkHeader == "" {
		return ""
	}
	m := linkPattern.FindStringSubmatch(linkHeader)
	if m == nil {
		return ""
	}
	next := m[1]
	if strings.HasPrefix(next, "/") {
		return baseURL + next
	}
	return next
}
