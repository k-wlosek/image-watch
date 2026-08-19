package main

import (
	"context"
	"net/http"

	"github.com/k-wlosek/image-watch/internal/metrics"
)

// newHTTPServer builds the operational HTTP server.
func newHTTPServer(listen string, m *metrics.Metrics) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	if m != nil {
		mux.Handle("/metrics", m.Handler())
	}
	return &http.Server{Addr: listen, Handler: mux}
}

// shutdownHTTPServer performs a bounded graceful shutdown.
func shutdownHTTPServer(ctx context.Context, srv *http.Server) error {
	return srv.Shutdown(ctx)
}
