package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/image-watch/internal/image"
)

// newTLSTestClient points a Client at an httptest TLS server.
func newTLSTestClient(t *testing.T, srv *httptest.Server, creds CredentialProvider) *Client {
	t.Helper()
	host := strings.TrimPrefix(srv.URL, "https://")
	return New(host, srv.Client(), creds)
}

func TestListTags_Basic(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/library/nginx/tags/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tagListResponse{
			Name: "library/nginx",
			Tags: []string{"1.25.3", "1.25.4", "1.24.0", "latest"},
		})
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	tags, err := c.ListTags(context.Background(), "library/nginx")
	if err != nil {
		t.Fatalf("ListTags error: %v", err)
	}
	want := map[string]bool{"1.25.3": true, "1.25.4": true, "1.24.0": true, "latest": true}
	if len(tags) != len(want) {
		t.Fatalf("got %d tags, want %d: %v", len(tags), len(want), tags)
	}
	for _, tag := range tags {
		if !want[tag] {
			t.Errorf("unexpected tag %q", tag)
		}
	}
}

func TestListTags_Pagination(t *testing.T) {
	mux := http.NewServeMux()
	page := 0
	mux.HandleFunc("/v2/foo/bar/tags/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case 0:
			w.Header().Set("Link", `</v2/foo/bar/tags/list?next=page2>; rel="next"`)
			json.NewEncoder(w).Encode(tagListResponse{Tags: []string{"1.0.0", "1.0.1"}})
		default:
			json.NewEncoder(w).Encode(tagListResponse{Tags: []string{"1.0.2"}})
		}
		page++
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	tags, err := c.ListTags(context.Background(), "foo/bar")
	if err != nil {
		t.Fatalf("ListTags error: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("got %d tags across pages, want 3: %v", len(tags), tags)
	}
}

func TestResolve_SinglePlatformManifest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/1.2.3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:aaaa")
		w.Header().Set("Content-Type", MediaTypeOCIManifest)
		fmt.Fprintf(w, `{"schemaVersion":2,"mediaType":%q}`, MediaTypeOCIManifest)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	obs, err := c.Resolve(context.Background(), "foo/bar", "1.2.3")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if obs.PlatformManifestDigest != "sha256:aaaa" {
		t.Errorf("PlatformManifestDigest = %q, want sha256:aaaa", obs.PlatformManifestDigest)
	}
	if obs.IndexDigest != "" {
		t.Errorf("expected no IndexDigest for a single-platform manifest, got %q", obs.IndexDigest)
	}
}

func TestResolveForPlatform_MultiPlatformIndex(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:index0000")
		w.Header().Set("Content-Type", MediaTypeOCIIndex)
		idx := imageIndex{
			SchemaVersion: 2,
			MediaType:     MediaTypeOCIIndex,
			Manifests: []indexEntry{
				{MediaType: MediaTypeOCIManifest, Digest: "sha256:amd64digest", Platform: ociPlatform{OS: "linux", Architecture: "amd64"}},
				{MediaType: MediaTypeOCIManifest, Digest: "sha256:arm64digest", Platform: ociPlatform{OS: "linux", Architecture: "arm64"}},
			},
		}
		json.NewEncoder(w).Encode(idx)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)

	obs, err := c.ResolveForPlatform(context.Background(), "foo/bar", "latest", image.Platform{OS: "linux", Architecture: "amd64"})
	if err != nil {
		t.Fatalf("ResolveForPlatform error: %v", err)
	}
	if obs.PlatformManifestDigest != "sha256:amd64digest" {
		t.Errorf("PlatformManifestDigest = %q, want sha256:amd64digest", obs.PlatformManifestDigest)
	}
	if obs.IndexDigest != "sha256:index0000" {
		t.Errorf("IndexDigest = %q, want sha256:index0000", obs.IndexDigest)
	}

	obsNoMatch, err := c.ResolveForPlatform(context.Background(), "foo/bar", "latest", image.Platform{OS: "linux", Architecture: "riscv64"})
	if err != nil {
		t.Fatalf("ResolveForPlatform (no match) unexpected error: %v", err)
	}
	if obsNoMatch.PlatformManifestDigest != "" {
		t.Errorf("expected no platform manifest digest for unmatched platform, got %q", obsNoMatch.PlatformManifestDigest)
	}
	if len(obsNoMatch.AvailablePlatforms) != 2 {
		t.Errorf("expected AvailablePlatforms to list both index entries even on no-match, got %v", obsNoMatch.AvailablePlatforms)
	}
}

func TestResolve_RepositoryNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/missing/repo/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	_, err := c.Resolve(context.Background(), "missing/repo", "1.0.0")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	derr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if derr.Class != ErrClassRepoNotFound {
		t.Errorf("Class = %s, want %s", derr.Class, ErrClassRepoNotFound)
	}
}

func TestResolve_BearerTokenChallenge(t *testing.T) {
	var sawBasicAuth bool
	var sawBearerToken string

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); ok {
			sawBasicAuth = true
			if u != "testuser" || p != "testpass" {
				t.Errorf("unexpected basic auth credentials: %s/%s", u, p)
			}
		}
		json.NewEncoder(w).Encode(tokenResponse{Token: "fake-bearer-token"})
	}))
	defer tokenSrv.Close()

	mux := http.NewServeMux()
	first := true
	mux.HandleFunc("/v2/private/repo/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s",service="test-registry",scope="repository:private/repo:pull"`, tokenSrv.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sawBearerToken = auth
		w.Header().Set("Docker-Content-Digest", "sha256:private0000")
		w.Header().Set("Content-Type", MediaTypeOCIManifest)
		fmt.Fprintf(w, `{"schemaVersion":2,"mediaType":%q}`, MediaTypeOCIManifest)
		_ = first
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	creds := func(host string) (string, string, bool) {
		return "testuser", "testpass", true
	}

	c := newTLSTestClient(t, srv, creds)
	obs, err := c.Resolve(context.Background(), "private/repo", "1.0.0")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if obs.PlatformManifestDigest != "sha256:private0000" {
		t.Errorf("PlatformManifestDigest = %q, want sha256:private0000", obs.PlatformManifestDigest)
	}
	if !sawBasicAuth {
		t.Errorf("expected token exchange to present basic auth credentials")
	}
	if sawBearerToken != "Bearer fake-bearer-token" {
		t.Errorf("Authorization header = %q, want %q", sawBearerToken, "Bearer fake-bearer-token")
	}
}

func TestParseBearerChallenge(t *testing.T) {
	c, ok := parseBearerChallenge(`Bearer realm="https://auth.example.com/token",service="registry.example.com",scope="repository:foo/bar:pull"`)
	if !ok {
		t.Fatal("expected challenge to parse")
	}
	if c.Realm != "https://auth.example.com/token" {
		t.Errorf("Realm = %q", c.Realm)
	}
	if c.Service != "registry.example.com" {
		t.Errorf("Service = %q", c.Service)
	}
	if c.Scope != "repository:foo/bar:pull" {
		t.Errorf("Scope = %q", c.Scope)
	}
}

func TestParseNextLink(t *testing.T) {
	got := parseNextLink(`</v2/foo/tags/list?n=100&last=x>; rel="next"`, "https://registry.example.com")
	want := "https://registry.example.com/v2/foo/tags/list?n=100&last=x"
	if got != want {
		t.Errorf("parseNextLink = %q, want %q", got, want)
	}

	if got := parseNextLink("", "https://registry.example.com"); got != "" {
		t.Errorf("expected empty string for missing Link header, got %q", got)
	}
}

type recordingInstrumentation struct {
	mu    sync.Mutex
	calls []instrumentationCall
}

type instrumentationCall struct {
	Host     string
	Duration time.Duration
	Err      error
}

func (r *recordingInstrumentation) ObserveRequest(host string, duration time.Duration, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, instrumentationCall{Host: host, Duration: duration, Err: err})
}

func TestInstrumentation_RecordsSuccessfulRequest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/tags/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tagListResponse{Tags: []string{"1.0.0"}})
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	instr := &recordingInstrumentation{}
	c.Instrumentation = instr

	if _, err := c.ListTags(context.Background(), "foo/bar"); err != nil {
		t.Fatalf("ListTags error: %v", err)
	}

	instr.mu.Lock()
	defer instr.mu.Unlock()
	if len(instr.calls) != 1 {
		t.Fatalf("got %d instrumentation calls, want 1", len(instr.calls))
	}
	if instr.calls[0].Err != nil {
		t.Errorf("expected no error recorded, got %v", instr.calls[0].Err)
	}
	if instr.calls[0].Host != c.Host {
		t.Errorf("Host = %q, want %q", instr.calls[0].Host, c.Host)
	}
}

func TestInstrumentation_RecordsFailedRequest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/missing/repo/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	instr := &recordingInstrumentation{}
	c.Instrumentation = instr

	_, err := c.Resolve(context.Background(), "missing/repo", "1.0.0")
	if err == nil {
		t.Fatal("expected an error for a 404")
	}

	instr.mu.Lock()
	defer instr.mu.Unlock()
	if len(instr.calls) != 1 {
		t.Fatalf("got %d instrumentation calls, want 1", len(instr.calls))
	}
	if instr.calls[0].Err == nil {
		t.Errorf("expected the recorded call to carry the error")
	}
}

func TestInstrumentation_NilInstrumentationIsSafe(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/tags/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tagListResponse{Tags: []string{"1.0.0"}})
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	// c.Instrumentation is nil by default -- must not panic.
	if _, err := c.ListTags(context.Background(), "foo/bar"); err != nil {
		t.Fatalf("ListTags error: %v", err)
	}
}
