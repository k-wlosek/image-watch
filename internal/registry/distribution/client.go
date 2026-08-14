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

// Client is a Registry implementation backed by HTTP calls.
type Client struct {
	Host       string
	httpClient *http.Client
	auth       *authenticator
}

// New constructs a distribution client bound to a single registry host.
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
	return "https://" + host
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

func (h *Client) Resolve(ctx context.Context, repository string, reference string) (registry.ManifestObservation, error) {
	u := fmt.Sprintf("%s/v2/%s/manifests/%s", h.baseURL(), repository, url.PathEscape(reference))

	body, headers, err := h.doAuthenticatedGET(ctx, repository, "pull", u)
	if err != nil {
		return registry.ManifestObservation{}, err
	}

	var env manifestEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return registry.ManifestObservation{}, newError(ErrClassRegistry, repository, "malformed manifest response", 0, err)
	}
	mediaType := env.MediaType
	if mediaType == "" {
		mediaType = headers.Get("Content-Type")
	}

	digest := headers.Get("Docker-Content-Digest")
	if digest == "" {
		// Some registries omit the header; the caller can fall back to
		// content-addressing this themselves if truly needed. For v1 we
		// treat a missing digest header as a registry error rather than
		// computing our own digest, since that requires exact canonical
		// JSON byte-matching the registry's own hashing and is easy to
		// get subtly wrong.
		return registry.ManifestObservation{}, newError(ErrClassRegistry, repository, "registry did not return Docker-Content-Digest header", 0, nil)
	}

	if isIndexMediaType(mediaType) {
		var idx imageIndex
		if err := json.Unmarshal(body, &idx); err != nil {
			return registry.ManifestObservation{}, newError(ErrClassRegistry, repository, "malformed image index", 0, err)
		}
		var platforms []image.Platform
		for _, m := range idx.Manifests {
			platforms = append(platforms, image.Platform{
				OS:           m.Platform.OS,
				Architecture: m.Platform.Architecture,
				Variant:      m.Platform.Variant,
			})
		}
		return registry.ManifestObservation{
			IndexDigest:        digest,
			MediaType:          mediaType,
			AvailablePlatforms: platforms,
		}, nil
	}

	// Single-platform manifest: the platform-manifest digest IS the
	// content digest we just resolved. There is no index in this case.
	return registry.ManifestObservation{
		PlatformManifestDigest: digest,
		MediaType:              mediaType,
	}, nil
}

// ResolveForPlatform resolves a reference for a specific platform.
func (h *Client) ResolveForPlatform(ctx context.Context, repository, reference string, platform image.Platform) (registry.ManifestObservation, error) {
	top, err := h.Resolve(ctx, repository, reference)
	if err != nil {
		return registry.ManifestObservation{}, err
	}
	if top.PlatformManifestDigest != "" {
		// Already single-platform.
		p := platform
		top.Platform = &p
		return top, nil
	}

	// It was an index; find the matching entry and resolve its digest
	// directly to get that platform's manifest digest.
	u := fmt.Sprintf("%s/v2/%s/manifests/%s", h.baseURL(), repository, url.PathEscape(reference))
	body, _, err := h.doAuthenticatedGET(ctx, repository, "pull", u)
	if err != nil {
		return registry.ManifestObservation{}, err
	}
	var idx imageIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return registry.ManifestObservation{}, newError(ErrClassRegistry, repository, "malformed image index", 0, err)
	}

	for _, m := range idx.Manifests {
		if m.Platform.OS == platform.OS &&
			m.Platform.Architecture == platform.Architecture &&
			m.Platform.Variant == platform.Variant {
			top.PlatformManifestDigest = m.Digest
			p := platform
			top.Platform = &p
			return top, nil
		}
	}

	// No entry for the requested platform.
	return top, nil
}

// doAuthenticatedGET performs a GET with bearer-token handling.
func (h *Client) doAuthenticatedGET(ctx context.Context, repository, action, rawURL string) ([]byte, http.Header, error) {
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
			// Cached token was rejected; drop it.
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
