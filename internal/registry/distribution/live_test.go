//go:build live

package distribution

import (
	"context"
	"testing"

	"github.com/k-wlosek/image-watch/internal/image"
)

// Live suite: validates the OCI Distribution client against the real Docker
// Hub (registry-1.docker.io) using anonymous bearer auth.
//
// Needs -tags=live to run.
// Note: Not hermetic, network-dependent, and subject to rate-limiting by Docker Hub.

const liveHost = "docker.io"

func liveClient(t *testing.T) *Client {
	t.Helper()
	return New(liveHost, nil, NoCredentials)
}

func TestLive_ListTags_Pagination(t *testing.T) {
	c := liveClient(t)

	// library/nginx currently advertises >1000 tags, forcing Docker Hub's
	// n=1000 pagination with relative Link headers
	tags, err := c.ListTags(context.Background(), "library/nginx")
	if err != nil {
		t.Fatalf("ListTags(library/nginx): %v", err)
	}
	if len(tags) < 1000 {
		t.Errorf("listed %d tags, want at least 1000 (pagination may have failed)", len(tags))
	}

	sawLatest := false
	for _, tag := range tags {
		if tag == "latest" {
			sawLatest = true
			break
		}
	}
	if !sawLatest {
		t.Errorf("expected tag %q in the list of %d tags", "latest", len(tags))
	}
}

func TestLive_Resolve_MultiArchIndex(t *testing.T) {
	c := liveClient(t)

	type ref struct{ repo, tag string }
	// These two also cover both real index media types: alpine resolves to an
	// OCI index, postgres to a Docker manifest list.
	refs := []ref{
		{repo: "library/alpine", tag: "latest"},
		{repo: "library/postgres", tag: "15"},
	}

	for _, r := range refs {
		for _, want := range []image.Platform{
			{OS: "linux", Architecture: "amd64"},
			{OS: "linux", Architecture: "arm64"},
		} {
			obs, err := c.ResolveForPlatform(context.Background(), r.repo, r.tag, want)
			if err != nil {
				t.Fatalf("ResolveForPlatform(%s:%s, %s): %v", r.repo, r.tag, want, err)
			}
			if obs.IndexDigest == "" {
				t.Errorf("%s:%s: expected an index digest", r.repo, r.tag)
			}
			if obs.PlatformManifestDigest == "" {
				t.Errorf("%s:%s: expected a platform manifest digest for %s", r.repo, r.tag, want.String())
			}
			if len(obs.AvailablePlatforms) == 0 {
				t.Errorf("%s:%s: expected AvailablePlatforms to be populated", r.repo, r.tag)
			}
			if obs.MediaType == "" {
				t.Errorf("%s:%s: expected a media type", r.repo, r.tag)
			}
		}

		// A platform the Linux image index does not provide must come back as
		// an unfound platform, not an error.
		obs, err := c.ResolveForPlatform(context.Background(), r.repo, r.tag,
			image.Platform{OS: "darwin", Architecture: "arm64"})
		if err != nil {
			t.Fatalf("%s:%s resolve for an absent platform errored: %v", r.repo, r.tag, err)
		}
		if obs.PlatformManifestDigest != "" {
			t.Errorf("%s:%s: expected an empty platform digest for an absent platform, got %q",
				r.repo, r.tag, obs.PlatformManifestDigest)
		}
	}
}

func TestLive_ErrorClassification(t *testing.T) {
	c := liveClient(t)

	// Docker Hub rejects the token-exchange scope for an unknown repository,
	// surfacing as an authentication failure rather than a 404.
	if _, err := c.ListTags(context.Background(), "library/definitely-not-a-repo-xyz"); err == nil {
		t.Fatal("expected an error for a nonexistent repository")
	} else if derr, ok := err.(*Error); !ok {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	} else if derr.Class != ErrClassRepoNotFound && derr.Class != ErrClassAuthentication {
		t.Errorf("class = %s, want %s or %s (Docker Hub rejects unknown scopes at token exchange)",
			derr.Class, ErrClassRepoNotFound, ErrClassAuthentication)
	}

	// Bogus credentials must fail during the real token exchange against
	// auth.docker.io, mapping the 401 to the authentication class.
	bad := New(liveHost, nil, func(host string) (string, string, bool) {
		return "image-watch-live-bad", "image-watch-live-bad", true
	})
	if _, err := bad.Resolve(context.Background(), "library/alpine", "latest"); err == nil {
		t.Fatal("expected an authentication failure with bogus credentials")
	} else if derr, ok := err.(*Error); !ok {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	} else if derr.Class != ErrClassAuthentication && derr.Class != ErrClassAuthorization {
		t.Errorf("expected an authentication/authorization failure from token exchange, got: %v", derr)
	}
}

func TestLive_Instrumentation(t *testing.T) {
	instr := &recordingInstrumentation{}
	c := liveClient(t)
	c.Instrumentation = instr

	if _, err := c.ListTags(context.Background(), "library/nginx"); err != nil {
		t.Fatalf("ListTags error: %v", err)
	}

	instr.mu.Lock()
	defer instr.mu.Unlock()

	if len(instr.calls) == 0 {
		t.Fatal("expected at least one recorded request")
	}
	for _, call := range instr.calls {
		if call.Host != liveHost {
			t.Errorf("recorded host = %q, want %q", call.Host, liveHost)
		}
		if call.Duration < 0 {
			t.Errorf("negative recorded duration %v", call.Duration)
		}
		if call.Err != nil {
			t.Errorf("unexpected error recorded: %v", call.Err)
		}
	}
}
