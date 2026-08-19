package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	iwruntime "github.com/k-wlosek/image-watch/internal/runtime"
)

func newTestClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return newWithHTTPClient(srv.Client(), srv.URL)
}

func TestListContainers_Basic(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]containerSummary{
			{
				ID:      "abc123",
				Names:   []string{"/my-nginx"},
				Image:   "nginx:1.25.3",
				ImageID: "sha256:imageid1",
				Created: 1700000000,
				Labels:  map[string]string{"com.example.foo": "bar"},
			},
		})
	})
	mux.HandleFunc("/images/sha256:imageid1/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(imageInspect{
			ID:           "sha256:imageid1",
			RepoDigests:  []string{"nginx@sha256:digestABCDEF"},
			Architecture: "amd64",
			Os:           "linux",
		})
	})

	c := newTestClient(t, mux)
	obs, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers error: %v", err)
	}
	if len(obs) != 1 {
		t.Fatalf("got %d observations, want 1", len(obs))
	}

	o := obs[0]
	if o.Name != "my-nginx" {
		t.Errorf("Name = %q, want my-nginx", o.Name)
	}
	if o.Image.Repository != "library/nginx" {
		t.Errorf("Repository = %q, want library/nginx", o.Image.Repository)
	}
	if o.Image.TagOrEmpty() != "1.25.3" {
		t.Errorf("Tag = %q, want 1.25.3", o.Image.TagOrEmpty())
	}
	if o.Digest != "sha256:digestABCDEF" {
		t.Errorf("Digest = %q, want sha256:digestABCDEF", o.Digest)
	}
	if o.Platform.OS != "linux" || o.Platform.Architecture != "amd64" {
		t.Errorf("Platform = %+v, want linux/amd64", o.Platform)
	}
	if o.Labels["com.example.foo"] != "bar" {
		t.Errorf("Labels not carried through: %+v", o.Labels)
	}
}

func TestListContainers_DedupesImageInspectCalls(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]containerSummary{
			{ID: "c1", Names: []string{"/a"}, Image: "foo:1.2.3", ImageID: "sha256:shared", Created: 1},
			{ID: "c2", Names: []string{"/b"}, Image: "foo:1.2.3", ImageID: "sha256:shared", Created: 2},
			{ID: "c3", Names: []string{"/c"}, Image: "foo:1.2.3", ImageID: "sha256:shared", Created: 3},
		})
	})

	inspectCalls := 0
	mux.HandleFunc("/images/sha256:shared/json", func(w http.ResponseWriter, r *http.Request) {
		inspectCalls++
		json.NewEncoder(w).Encode(imageInspect{
			ID:           "sha256:shared",
			RepoDigests:  []string{"foo@sha256:xyz"},
			Os:           "linux",
			Architecture: "amd64",
		})
	})

	c := newTestClient(t, mux)
	obs, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers error: %v", err)
	}
	if len(obs) != 3 {
		t.Fatalf("got %d observations, want 3", len(obs))
	}
	if inspectCalls != 1 {
		t.Errorf("image inspect called %d times, want 1", inspectCalls)
	}
	for _, o := range obs {
		if o.Digest != "sha256:xyz" {
			t.Errorf("container %s: Digest = %q, want sha256:xyz", o.ID, o.Digest)
		}
	}
}

func TestListContainers_ImageInspectFailureDoesNotAbortWholeCheck(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]containerSummary{
			{ID: "ok1", Names: []string{"/good"}, Image: "nginx:1.25", ImageID: "sha256:good", Created: 1},
			{ID: "bad1", Names: []string{"/bad"}, Image: "redis:7", ImageID: "sha256:bad", Created: 2},
		})
	})
	mux.HandleFunc("/images/sha256:good/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(imageInspect{
			ID: "sha256:good", RepoDigests: []string{"nginx@sha256:aaa"},
			Os: "linux", Architecture: "amd64",
		})
	})
	mux.HandleFunc("/images/sha256:bad/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	c := newTestClient(t, mux)
	obs, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers should not fail wholesale on one image's inspect error: %v", err)
	}
	if len(obs) != 1 {
		t.Fatalf("got %d observations, want 1 (only the successfully-inspected container; the failed one has no sibling to recover a platform from)", len(obs))
	}
	if obs[0].Name != "good" || obs[0].Digest != "sha256:aaa" {
		t.Errorf("expected the successfully-inspected container to be reported with its digest, got %+v", obs[0])
	}
}

func TestListContainers_InspectFailureRecoversPlatformFromSibling(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]containerSummary{
			{ID: "c1", Names: []string{"/web1"}, Image: "nginx:1.25", ImageID: "sha256:good", Created: 1},
			{ID: "c2", Names: []string{"/web2"}, Image: "nginx:1.25", ImageID: "sha256:flaky", Created: 2},
		})
	})
	mux.HandleFunc("/images/sha256:good/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(imageInspect{
			ID: "sha256:good", RepoDigests: []string{"nginx@sha256:aaa"},
			Os: "linux", Architecture: "amd64",
		})
	})
	mux.HandleFunc("/images/sha256:flaky/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	c := newTestClient(t, mux)
	obs, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers error: %v", err)
	}
	if len(obs) != 2 {
		t.Fatalf("got %d observations, want 2", len(obs))
	}

	var web2 *iwruntime.ContainerObservation
	for i := range obs {
		if obs[i].Name == "web2" {
			web2 = &obs[i]
		}
	}
	if web2 == nil {
		t.Fatalf("expected to find the web2 container in results")
	}
	if web2.Platform.OS != "linux" || web2.Platform.Architecture != "amd64" {
		t.Errorf("expected web2's platform to be recovered from its sibling (linux/amd64), got %+v", web2.Platform)
	}
	if web2.Digest != "" {
		t.Errorf("expected web2's digest to remain empty, got %q", web2.Digest)
	}
}

func TestMatchRepoDigest(t *testing.T) {
	cases := []struct {
		name        string
		repoDigests []string
		repository  string
		want        string
	}{
		{"exact match", []string{"library/nginx@sha256:aaa"}, "library/nginx", "sha256:aaa"},
		{"docker hub short form", []string{"nginx@sha256:aaa"}, "library/nginx", "sha256:aaa"},
		{"qualified ghcr", []string{"ghcr.io/acme/foo@sha256:bbb"}, "acme/foo", "sha256:bbb"},
		{"single unmatched entry is not trusted", []string{"weird@sha256:ccc"}, "library/totallydifferent", ""},
		{"substring without boundary is rejected", []string{"mynginx@sha256:ddd"}, "nginx", ""},
		{"substring without boundary, reverse direction", []string{"nginx@sha256:eee"}, "mynginx", ""},
		{"no digests", nil, "library/nginx", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchRepoDigest(c.repoDigests, c.repository)
			if got != c.want {
				t.Errorf("matchRepoDigest(%v, %q) = %q, want %q", c.repoDigests, c.repository, got, c.want)
			}
		})
	}
}

func TestListContainers_DaemonUnreachable(t *testing.T) {
	c := newWithHTTPClient(&http.Client{}, "http://127.0.0.1:1") // nothing listens here
	_, err := c.ListContainers(context.Background())
	if err == nil {
		t.Fatal("expected error when daemon is unreachable")
	}
	derr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if !derr.Unavailable {
		t.Errorf("expected Unavailable=true for connection failure, got false")
	}
}

func TestNew_EndpointVariants(t *testing.T) {
	for _, endpoint := range []string{"", "unix:///var/run/docker.sock", "tcp://127.0.0.1:2375", "http://127.0.0.1:2375", "https://docker.example.com"} {
		c, err := New(endpoint)
		if err != nil {
			t.Errorf("New(%q) error: %v", endpoint, err)
		}
		if c == nil {
			t.Errorf("New(%q) returned a nil client", endpoint)
		}
	}
}

func TestNew_UnsupportedScheme(t *testing.T) {
	if _, err := New("ftp://example.com/repo"); err == nil {
		t.Errorf("expected an error for an unsupported scheme")
	}
}

func TestNew_InvalidURL(t *testing.T) {
	if _, err := New("tcp://host:badport"); err == nil {
		t.Errorf("expected an error for an invalid endpoint URL")
	}
}
