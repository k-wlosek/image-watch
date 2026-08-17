// Package docker implements the Docker runtime adapter.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	iwimage "github.com/example/image-watch/internal/image"
	"github.com/example/image-watch/internal/runtime"
)

// DefaultSocket is the standard Docker Engine socket path.
const DefaultSocket = "unix:///var/run/docker.sock"

// Client is a Runtime implementation backed by the Docker Engine API.
type Client struct {
	httpClient *http.Client
	// baseURL is a fixed placeholder host.
	baseURL string
}

var _ runtime.Runtime = (*Client)(nil)

// New constructs a Client against a Docker endpoint.
func New(endpoint string) (*Client, error) {
	if endpoint == "" {
		endpoint = DefaultSocket
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("docker: invalid endpoint %q: %w", endpoint, err)
	}

	switch u.Scheme {
	case "unix":
		socketPath := u.Path
		httpClient := &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					d := net.Dialer{}
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
		}
		return &Client{httpClient: httpClient, baseURL: "http://unix"}, nil

	case "tcp", "http", "https":
		scheme := u.Scheme
		if scheme == "tcp" {
			scheme = "http"
		}
		return &Client{
			httpClient: &http.Client{Timeout: 10 * time.Second},
			baseURL:    scheme + "://" + u.Host,
		}, nil

	default:
		return nil, fmt.Errorf("docker: unsupported endpoint scheme %q", u.Scheme)
	}
}

// newWithHTTPClient is a test seam for a custom http.Client and base URL.
func newWithHTTPClient(httpClient *http.Client, baseURL string) *Client {
	return &Client{httpClient: httpClient, baseURL: baseURL}
}

func (c *Client) Name() string { return "docker" }

// ListContainers lists all running containers and translates them into observations.
func (c *Client) ListContainers(ctx context.Context) ([]runtime.ContainerObservation, error) {
	containers, err := c.listContainerSummaries(ctx)
	if err != nil {
		return nil, err
	}

	imageCache := make(map[string]imageInspect)
	inspectFailed := make(map[string]bool)
	var observations []runtime.ContainerObservation

	for _, cs := range containers {
		if inspectFailed[cs.ImageID] {
			observations = append(observations, observationFromSummaryOnly(cs))
			continue
		}

		insp, ok := imageCache[cs.ImageID]
		if !ok {
			insp, err = c.inspectImage(ctx, cs.ImageID)
			if err != nil {
				// Skip enrichment for this container.
				observations = append(observations, observationFromSummaryOnly(cs))
				continue
			}
			imageCache[cs.ImageID] = insp
		}

		obs, err := observationFromSummaryAndImage(cs, insp)
		if err != nil {
			// Skip unparseable image references.
			continue
		}
		observations = append(observations, obs)
	}

	return backfillPlatforms(observations), nil
}

// backfillPlatforms recovers the platform for containers whose image
// inspect failed (observationFromSummaryOnly leaves Platform as the
// zero value), by borrowing it from another container in this same
// batch running the identical image reference that resolved successfully.
func backfillPlatforms(observations []runtime.ContainerObservation) []runtime.ContainerObservation {
	type refKey struct{ Registry, Repository, Tag string }

	known := make(map[refKey]iwimage.Platform)
	for _, o := range observations {
		if o.Platform.IsZero() {
			continue
		}
		known[refKey{o.Image.Registry, o.Image.Repository, o.Image.TagOrEmpty()}] = o.Platform
	}

	result := make([]runtime.ContainerObservation, 0, len(observations))
	for _, o := range observations {
		if !o.Platform.IsZero() {
			result = append(result, o)
			continue
		}
		key := refKey{o.Image.Registry, o.Image.Repository, o.Image.TagOrEmpty()}
		if p, ok := known[key]; ok {
			o.Platform = p
			result = append(result, o)
		}
		// else: no sibling to borrow from -- dropped.
	}
	return result
}

func observationFromSummaryOnly(cs containerSummary) runtime.ContainerObservation {
	ref, err := iwimage.ParseReference(cs.Image)
	if err != nil {
		ref = iwimage.Reference{Repository: cs.Image}
	}
	return runtime.ContainerObservation{
		Runtime:   "docker",
		ID:        cs.ID,
		Name:      containerName(cs),
		Image:     ref,
		CreatedAt: time.Unix(cs.Created, 0).UTC(),
		Labels:    cs.Labels,
	}
}

func observationFromSummaryAndImage(cs containerSummary, insp imageInspect) (runtime.ContainerObservation, error) {
	ref, err := iwimage.ParseReference(cs.Image)
	if err != nil {
		return runtime.ContainerObservation{}, err
	}

	return runtime.ContainerObservation{
		Runtime: "docker",
		ID:      cs.ID,
		Name:    containerName(cs),
		Image:   ref,
		Digest:  matchRepoDigest(insp.RepoDigests, ref.Repository),
		Platform: iwimage.Platform{
			OS:           insp.Os,
			Architecture: insp.Architecture,
			Variant:      insp.Variant,
		},
		CreatedAt: time.Unix(cs.Created, 0).UTC(),
		Labels:    cs.Labels,
	}, nil
}

// matchRepoDigest finds the RepoDigest entry for the given repository.
func matchRepoDigest(repoDigests []string, repository string) string {
	for _, rd := range repoDigests {
		idx := strings.LastIndex(rd, "@")
		if idx == -1 {
			continue
		}
		repoPart := rd[:idx]
		if repoNamesEquivalent(repoPart, repository) {
			return rd[idx+1:]
		}
	}
	return ""
}

// repoNamesEquivalent reports whether two repository name strings refer
// to the same repository, accounting for Docker's inconsistent
// registry-host/namespace qualification
func repoNamesEquivalent(a, b string) bool {
	if a == b {
		return true
	}
	return strings.HasSuffix(a, "/"+b) || strings.HasSuffix(b, "/"+a)
}

func containerName(cs containerSummary) string {
	if len(cs.Names) == 0 {
		return cs.ID
	}
	return strings.TrimPrefix(cs.Names[0], "/")
}

func (c *Client) listContainerSummaries(ctx context.Context) ([]containerSummary, error) {
	body, err := c.get(ctx, "/containers/json?all=false")
	if err != nil {
		return nil, err
	}
	var summaries []containerSummary
	if err := json.Unmarshal(body, &summaries); err != nil {
		return nil, fmt.Errorf("docker: malformed /containers/json response: %w", err)
	}
	return summaries, nil
}

func (c *Client) inspectImage(ctx context.Context, imageID string) (imageInspect, error) {
	body, err := c.get(ctx, "/images/"+url.PathEscape(imageID)+"/json")
	if err != nil {
		return imageInspect{}, err
	}
	var insp imageInspect
	if err := json.Unmarshal(body, &insp); err != nil {
		return imageInspect{}, fmt.Errorf("docker: malformed image inspect response: %w", err)
	}
	return insp, nil
}

// get performs a GET against the Docker Engine API and returns the body.
func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("docker: invalid request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &Error{Op: path, Unavailable: true, Err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &Error{Op: path, Err: err}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &Error{
			Op:         path,
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("docker: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
		}
	}
	return body, nil
}
