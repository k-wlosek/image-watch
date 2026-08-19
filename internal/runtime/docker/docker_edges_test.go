package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestClientName(t *testing.T) {
	c := newWithHTTPClient(http.DefaultClient, "http://localhost")
	if got := c.Name(); got != "docker" {
		t.Errorf("Name() = %q, want docker", got)
	}
}

func TestListContainers_UnparseableImageRefIsSkipped(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]containerSummary{
			{ID: "c1", Names: []string{"/ok"}, Image: "nginx:1.25", ImageID: "sha256:good", Created: 1},
			{ID: "c2", Names: []string{"/bad"}, Image: "", ImageID: "sha256:good", Created: 2},
		})
	})
	mux.HandleFunc("/images/sha256:good/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(imageInspect{ID: "sha256:good", RepoDigests: []string{"nginx@sha256:aaa"}, Os: "linux", Architecture: "amd64"})
	})

	c := newTestClient(t, mux)
	obs, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers error: %v", err)
	}
	if len(obs) != 1 {
		t.Fatalf("got %d observations, want 1 (unparseable image ref must be dropped)", len(obs))
	}
	if obs[0].Name != "ok" {
		t.Errorf("expected only the parseable container, got %+v", obs[0])
	}
}

func TestListContainers_InspectFailureWithUnparseableRef(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]containerSummary{
			{ID: "c1", Names: []string{"/odd"}, Image: "", ImageID: "sha256:bad", Created: 1},
		})
	})
	mux.HandleFunc("/images/sha256:bad/json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	c := newTestClient(t, mux)
	obs, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers error: %v", err)
	}
	// Summary-only observation has no platform, so it is dropped.
	if len(obs) != 0 {
		t.Fatalf("got %d observations, want 0 (summary-only observation with unparseable ref and no platform is dropped)", len(obs))
	}
}

func TestListContainers_RepoDigestWithoutDigestSeparator(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]containerSummary{
			{ID: "c1", Names: []string{"/x"}, Image: "nginx:1.25", ImageID: "sha256:g", Created: 1},
		})
	})
	mux.HandleFunc("/images/sha256:g/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(imageInspect{ID: "sha256:g", RepoDigests: []string{"nginx"}, Os: "linux", Architecture: "amd64"})
	})

	c := newTestClient(t, mux)
	obs, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers error: %v", err)
	}
	if obs[0].Digest != "" {
		t.Errorf("Digest = %q, want empty when RepoDigests has no @ separator", obs[0].Digest)
	}
}

func TestListContainers_ContainerWithoutNames(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]containerSummary{
			{ID: "solo123", Image: "redis:7", ImageID: "sha256:g", Created: 1},
		})
	})
	mux.HandleFunc("/images/sha256:g/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(imageInspect{ID: "sha256:g", RepoDigests: []string{"redis@sha256:x"}, Os: "linux", Architecture: "amd64"})
	})

	c := newTestClient(t, mux)
	obs, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers error: %v", err)
	}
	if obs[0].Name != "solo123" {
		t.Errorf("Name = %q, want the container ID as fallback", obs[0].Name)
	}
}

func TestListContainers_MalformedContainersResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{bad`))
	})

	c := newTestClient(t, mux)
	_, err := c.ListContainers(context.Background())
	if err == nil || !strings.Contains(err.Error(), "malformed /containers/json") {
		t.Fatalf("expected malformed list error, got %v", err)
	}
}

func TestListContainers_MalformedImageInspect(t *testing.T) {
	var goodInspected bool
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]containerSummary{
			{ID: "c1", Names: []string{"/x"}, Image: "nginx:1.25", ImageID: "sha256:g", Created: 1},
			{ID: "c2", Names: []string{"/y"}, Image: "nginx:1.25", ImageID: "sha256:g2", Created: 2},
		})
	})
	mux.HandleFunc("/images/sha256:g/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{bad`))
	})
	mux.HandleFunc("/images/sha256:g2/json", func(w http.ResponseWriter, r *http.Request) {
		goodInspected = true
		json.NewEncoder(w).Encode(imageInspect{ID: "sha256:g2", RepoDigests: []string{"nginx@sha256:good"}, Os: "linux", Architecture: "amd64"})
	})

	c := newTestClient(t, mux)
	obs, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers error: %v", err)
	}
	if !goodInspected {
		t.Fatal("expected the sibling image to be inspected")
	}
	if len(obs) != 2 {
		t.Fatalf("got %d observations, want 2 (both containers survive with platform borrowed)", len(obs))
	}
	for i := range obs {
		if obs[i].ID == "c1" && obs[i].Digest != "" {
			t.Errorf("expected c1 to fall back to summary-only with empty digest, got %q", obs[i].Digest)
		}
		if obs[i].ID == "c1" && (obs[i].Platform.OS != "linux" || obs[i].Platform.Architecture != "amd64") {
			t.Errorf("expected c1 platform borrowed from its sibling, got %+v", obs[i].Platform)
		}
	}
}

func TestError_StringVariants(t *testing.T) {
	cases := []struct {
		name string
		e    *Error
		want string
	}{
		{"unavailable", &Error{Op: "/containers/json", Unavailable: true, Err: errors.New("err1")}, "docker: runtime unavailable during /containers/json: err1"},
		{"with status", &Error{Op: "/images/x/json", StatusCode: 500, Err: errors.New("err2")}, "docker: /images/x/json failed (status 500): err2"},
		{"plain", &Error{Op: "/images/x/json", Err: errors.New("err3")}, "docker: /images/x/json failed: err3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.e.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
	inner := errors.New("inner")
	e := &Error{Op: "x", Err: inner}
	if !errors.Is(e, inner) {
		t.Error("expected errors.Is to reach the wrapped error via Unwrap")
	}
}
