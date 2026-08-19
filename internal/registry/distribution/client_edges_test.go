package distribution

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/k-wlosek/image-watch/internal/image"
)

func TestListTags_ServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/tags/list", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	_, err := c.ListTags(context.Background(), "foo/bar")
	if derr, ok := err.(*Error); !ok || derr.Class != ErrClassRegistry {
		t.Fatalf("expected ErrClassRegistry, got %v", err)
	}
}

func TestListTags_MalformedBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/tags/list", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{bad`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	_, err := c.ListTags(context.Background(), "foo/bar")
	if err == nil || !strings.Contains(err.Error(), "malformed tags/list") {
		t.Fatalf("expected malformed response error, got %v", err)
	}
}

func TestListTags_Forbidden(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/tags/list", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	_, err := c.ListTags(context.Background(), "foo/bar")
	if derr, ok := err.(*Error); !ok || derr.Class != ErrClassAuthorization {
		t.Fatalf("expected ErrClassAuthorization, got %v", err)
	}
}

func TestListTags_RateLimit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/tags/list", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	_, err := c.ListTags(context.Background(), "foo/bar")
	if derr, ok := err.(*Error); !ok || derr.Class != ErrClassRateLimit {
		t.Fatalf("expected ErrClassRateLimit, got %v", err)
	}
}

func TestListTags_NetworkError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	host := strings.TrimPrefix(srv.URL, "https://")
	srv.Close()

	c := New(host, srv.Client(), NoCredentials)
	_, err := c.ListTags(context.Background(), "foo/bar")
	if derr, ok := err.(*Error); !ok || derr.Class != ErrClassNetwork {
		t.Fatalf("expected ErrClassNetwork, got %v", err)
	}
}

func TestResolve_MissingDigestHeader(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", MediaTypeOCIManifest)
		fmt.Fprintf(w, `{"schemaVersion":2,"mediaType":%q}`, MediaTypeOCIManifest)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	_, err := c.Resolve(context.Background(), "foo/bar", "1.0.0")
	if err == nil || !strings.Contains(err.Error(), "Docker-Content-Digest") {
		t.Fatalf("expected missing-digest error, got %v", err)
	}
}

func TestResolve_MalformedManifestBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:digest")
		w.Header().Set("Content-Type", MediaTypeOCIManifest)
		w.Write([]byte(`{bad`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	_, err := c.Resolve(context.Background(), "foo/bar", "1.0.0")
	if err == nil || !strings.Contains(err.Error(), "malformed manifest response") {
		t.Fatalf("expected malformed manifest error, got %v", err)
	}
}

func TestResolveForPlatform_IndexParseError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:idx")
		w.Header().Set("Content-Type", MediaTypeOCIIndex)
		w.Write([]byte(`{"mediaType":"application/vnd.oci.image.index.v1+json","manifests":3}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	_, err := c.ResolveForPlatform(context.Background(), "foo/bar", "latest", image.Platform{OS: "linux", Architecture: "amd64"})
	if err == nil || !strings.Contains(err.Error(), "malformed image index") {
		t.Fatalf("expected malformed index error, got %v", err)
	}
}

func TestResolveForPlatform_ManifestWithoutConfig(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:digest")
		w.Header().Set("Content-Type", MediaTypeOCIManifest)
		fmt.Fprintf(w, `{"schemaVersion":2,"mediaType":%q}`, MediaTypeOCIManifest)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	_, err := c.ResolveForPlatform(context.Background(), "foo/bar", "1.0.0", image.Platform{OS: "linux", Architecture: "amd64"})
	if err == nil || !strings.Contains(err.Error(), "no usable config descriptor") {
		t.Fatalf("expected no-config error, got %v", err)
	}
}

func TestResolveForPlatform_MalformedConfigBlob(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:digest")
		w.Header().Set("Content-Type", MediaTypeOCIManifest)
		fmt.Fprintf(w, `{"schemaVersion":2,"mediaType":%q,"config":{"digest":"sha256:cfg"}}`, MediaTypeOCIManifest)
	})
	mux.HandleFunc("/v2/foo/bar/blobs/sha256:cfg", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{bad`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	_, err := c.ResolveForPlatform(context.Background(), "foo/bar", "1.0.0", image.Platform{OS: "linux", Architecture: "amd64"})
	if err == nil || !strings.Contains(err.Error(), "malformed image config") {
		t.Fatalf("expected malformed config error, got %v", err)
	}
}

func TestDoAuthenticatedGET_UnauthorizedNoChallenge(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	_, err := c.Resolve(context.Background(), "foo/bar", "1.0.0")
	if derr, ok := err.(*Error); !ok || derr.Class != ErrClassAuthentication {
		t.Fatalf("expected ErrClassAuthentication, got %v", err)
	}
}

func TestDoAuthenticatedGET_ChallengeWithoutScope(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"token":"tok"}`)
	}))
	defer tokenSrv.Close()

	var sawBearer string
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s"`, tokenSrv.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sawBearer = r.Header.Get("Authorization")
		w.Header().Set("Docker-Content-Digest", "sha256:d")
		w.Header().Set("Content-Type", MediaTypeOCIManifest)
		fmt.Fprintf(w, `{"schemaVersion":2,"mediaType":%q}`, MediaTypeOCIManifest)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	if _, err := c.Resolve(context.Background(), "foo/bar", "1.0.0"); err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if sawBearer != "Bearer tok" {
		t.Errorf("Authorization = %q, want Bearer tok (scope should be defaulted)", sawBearer)
	}
}

func TestDoAuthenticatedGET_TokenRejectedAfterExchange(t *testing.T) {
	var tokenRequests int
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequests++
		fmt.Fprint(w, `{"token":"stale"}`)
	}))
	defer tokenSrv.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s",scope="repository:foo/bar:pull"`, tokenSrv.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := newTLSTestClient(t, srv, NoCredentials)
	_, err := c.Resolve(context.Background(), "foo/bar", "1.0.0")
	if derr, ok := err.(*Error); !ok || derr.Class != ErrClassAuthentication {
		t.Fatalf("expected ErrClassAuthentication, got %v", err)
	}
	if tokenRequests != 1 {
		t.Errorf("token requests = %d, want 1 (no refetch after rejection)", tokenRequests)
	}
}

func TestParseNextLink_Edges(t *testing.T) {
	if got := parseNextLink(`<https://other.example.com/v2/foo/tags/list?p=2>; rel="next"`, "https://reg.example.com"); got != "https://other.example.com/v2/foo/tags/list?p=2" {
		t.Errorf("absolute link = %q", got)
	}
	if got := parseNextLink(`nope`, "https://reg.example.com"); got != "" {
		t.Errorf("malformed link = %q, want empty", got)
	}
}

func TestDefaultClientWithNilTransport(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/foo/bar/tags/list", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tags":["1.0.0"]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	c := New(host, nil, NoCredentials)
	c.Scheme = "http"
	tags, err := c.ListTags(context.Background(), "foo/bar")
	if err != nil {
		t.Fatalf("ListTags over the default client: %v", err)
	}
	if len(tags) != 1 || tags[0] != "1.0.0" {
		t.Errorf("tags = %v", tags)
	}
}
