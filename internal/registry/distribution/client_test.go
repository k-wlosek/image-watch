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

	"github.com/k-wlosek/image-watch/internal/image"
)

// newTLSTestClient points a Client at an httptest TLS server.
func newTLSTestClient(t *testing.T, srv *httptest.Server, creds CredentialProvider) *Client {
	t.Helper()
	host := strings.TrimPrefix(srv.URL, "https://")
	return New(host, srv.Client(), creds)
}

func TestBaseURL_Scheme(t *testing.T) {
	for _, tc := range []struct {
		client Client
		want   string
	}{
		{Client{Host: "registry.example.com"}, "https://registry.example.com"},
		{Client{Host: "registry.example.com", Scheme: "http"}, "http://registry.example.com"},
		{Client{Host: image.DefaultRegistry}, "https://registry-1.docker.io"},
		{Client{Host: image.DefaultRegistry, Scheme: "http"}, "http://registry-1.docker.io"},
	} {
		if got := tc.client.baseURL(); got != tc.want {
			t.Errorf("baseURL(%+v) = %q, want %q", tc.client, got, tc.want)
		}
	}
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

func TestResolveForPlatform_SinglePlatformManifest_Matches(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/1.2.3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:manifestdigest")
		w.Header().Set("Content-Type", MediaTypeOCIManifest)
		fmt.Fprintf(w, `{"schemaVersion":2,"mediaType":%q,"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:configdigest"}}`, MediaTypeOCIManifest)
	})
	mux.HandleFunc("/v2/foo/bar/blobs/sha256:configdigest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"architecture":"amd64","os":"linux"}`)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	obs, err := c.ResolveForPlatform(context.Background(), "foo/bar", "1.2.3", image.Platform{OS: "linux", Architecture: "amd64"})
	if err != nil {
		t.Fatalf("ResolveForPlatform error: %v", err)
	}
	if obs.PlatformManifestDigest != "sha256:manifestdigest" {
		t.Errorf("PlatformManifestDigest = %q, want sha256:manifestdigest (platform matched, should be actionable)", obs.PlatformManifestDigest)
	}
}

func TestResolveForPlatform_SinglePlatformManifest_Mismatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/1.2.4", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:arm64onlydigest")
		w.Header().Set("Content-Type", MediaTypeOCIManifest)
		fmt.Fprintf(w, `{"schemaVersion":2,"mediaType":%q,"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:arm64config"}}`, MediaTypeOCIManifest)
	})
	mux.HandleFunc("/v2/foo/bar/blobs/sha256:arm64config", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"architecture":"arm64","os":"linux"}`)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	// Requesting on behalf of an amd64 host.
	obs, err := c.ResolveForPlatform(context.Background(), "foo/bar", "1.2.4", image.Platform{OS: "linux", Architecture: "amd64"})
	if err != nil {
		t.Fatalf("ResolveForPlatform error: %v", err)
	}
	if obs.PlatformManifestDigest != "" {
		t.Errorf("expected NO actionable digest for an arm64-only manifest requested on amd64, got %q", obs.PlatformManifestDigest)
	}
	if len(obs.AvailablePlatforms) != 1 || obs.AvailablePlatforms[0].Architecture != "arm64" {
		t.Errorf("expected AvailablePlatforms = [arm64], got %v", obs.AvailablePlatforms)
	}
}

func TestResolveForPlatform_SinglePlatformManifest_ConfigFetchFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/1.2.5", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:somedigest")
		w.Header().Set("Content-Type", MediaTypeOCIManifest)
		fmt.Fprintf(w, `{"schemaVersion":2,"mediaType":%q,"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:missingconfig"}}`, MediaTypeOCIManifest)
	})
	mux.HandleFunc("/v2/foo/bar/blobs/sha256:missingconfig", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	_, err := c.ResolveForPlatform(context.Background(), "foo/bar", "1.2.5", image.Platform{OS: "linux", Architecture: "amd64"})
	if err == nil {
		t.Fatal("expected an error when the config blob can't be fetched, not a silent success or silent unavailability")
	}
}

func TestResolveForPlatform_IndexBodyFetchedOnce(t *testing.T) {
	var manifestFetches int
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		manifestFetches++
		w.Header().Set("Docker-Content-Digest", "sha256:index0000")
		w.Header().Set("Content-Type", MediaTypeOCIIndex)
		idx := imageIndex{
			SchemaVersion: 2,
			MediaType:     MediaTypeOCIIndex,
			Manifests: []indexEntry{
				{MediaType: MediaTypeOCIManifest, Digest: "sha256:amd64digest", Platform: ociPlatform{OS: "linux", Architecture: "amd64"}},
			},
		}
		json.NewEncoder(w).Encode(idx)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	if _, err := c.ResolveForPlatform(context.Background(), "foo/bar", "latest", image.Platform{OS: "linux", Architecture: "amd64"}); err != nil {
		t.Fatalf("ResolveForPlatform error: %v", err)
	}
	if manifestFetches != 1 {
		t.Errorf("expected exactly 1 manifest fetch, got %d (ResolveForPlatform should not double-fetch the index body)", manifestFetches)
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

func TestPlatformMatch_VariantDefaulting(t *testing.T) {
	entryV8 := image.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}
	entryNoVariant := image.Platform{OS: "linux", Architecture: "arm64"}
	entryArmV7 := image.Platform{OS: "linux", Architecture: "arm", Variant: "v7"}

	cases := []struct {
		entry image.Platform
		want  image.Platform
		match bool
	}{
		{entryV8, image.Platform{OS: "linux", Architecture: "arm64"}, true},
		{entryV8, image.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}, true},
		{entryNoVariant, image.Platform{OS: "linux", Architecture: "arm64"}, true},
		{entryNoVariant, image.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}, true},
		{entryV8, image.Platform{OS: "linux", Architecture: "arm64", Variant: "v7"}, false},
		// arm has no default variant: empty must not match an explicit v7.
		{entryArmV7, image.Platform{OS: "linux", Architecture: "arm"}, false},
		{entryArmV7, image.Platform{OS: "linux", Architecture: "arm", Variant: "v7"}, true},
		{entryNoVariant, image.Platform{OS: "darwin", Architecture: "arm64"}, false},
	}

	for _, tc := range cases {
		if got := platformMatches(tc.entry, tc.want); got != tc.match {
			t.Errorf("platformMatches(%+v, %+v) = %v, want %v", tc.entry, tc.want, got, tc.match)
		}
	}
}

func TestResolveForPlatform_IndexVariantDefaulting(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:index0000")
		w.Header().Set("Content-Type", MediaTypeOCIIndex)
		idx := imageIndex{
			SchemaVersion: 2,
			MediaType:     MediaTypeOCIIndex,
			Manifests: []indexEntry{
				{MediaType: MediaTypeOCIManifest, Digest: "sha256:arm64v8digest", Platform: ociPlatform{OS: "linux", Architecture: "arm64", Variant: "v8"}},
			},
		}
		json.NewEncoder(w).Encode(idx)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	obs, err := c.ResolveForPlatform(context.Background(), "foo/bar", "latest", image.Platform{OS: "linux", Architecture: "arm64"})
	if err != nil {
		t.Fatalf("ResolveForPlatform error: %v", err)
	}
	if obs.PlatformManifestDigest != "sha256:arm64v8digest" {
		t.Errorf("expected the arm64/v8 index entry to satisfy a plain arm64 request, got PlatformManifestDigest=%q", obs.PlatformManifestDigest)
	}
}

func TestResolveForPlatform_SinglePlatformManifest_VariantDefaulting(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:singlearm64digest")
		w.Header().Set("Content-Type", MediaTypeOCIManifest)
		fmt.Fprintf(w, `{"schemaVersion":2,"mediaType":%q,"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:arm64cfg"}}`, MediaTypeOCIManifest)
	})
	mux.HandleFunc("/v2/foo/bar/blobs/sha256:arm64cfg", func(w http.ResponseWriter, r *http.Request) {
		// Config blob reports variant "v8" explicitly.
		fmt.Fprint(w, `{"architecture":"arm64","os":"linux","variant":"v8"}`)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	// Host requests plain arm64, no variant -- must still match.
	obs, err := c.ResolveForPlatform(context.Background(), "foo/bar", "1.0.0", image.Platform{OS: "linux", Architecture: "arm64"})
	if err != nil {
		t.Fatalf("ResolveForPlatform error: %v", err)
	}
	if obs.PlatformManifestDigest != "sha256:singlearm64digest" {
		t.Errorf("expected a config blob reporting arm64/v8 to satisfy a plain arm64 request, got PlatformManifestDigest=%q", obs.PlatformManifestDigest)
	}
}
