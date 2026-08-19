package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/k-wlosek/image-watch/internal/metrics"
)

func TestHTTPServer_Healthz(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", metrics.New())
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "ok") {
		t.Errorf("healthz body = %q, want ok", body)
	}
}

func TestHTTPServer_Metrics(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", metrics.New())
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("metrics status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "image_watch_checks_total") {
		t.Errorf("expected image-watch metrics in body")
	}
	if !strings.Contains(string(body), "go_goroutines") {
		t.Errorf("expected Go runtime metrics in body")
	}
}

func TestHTTPServer_MetricsUnavailableWithoutMetrics(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", nil)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("metrics without a Metrics instance = %d, want 404", resp.StatusCode)
	}
}
