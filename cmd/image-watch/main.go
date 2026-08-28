// Command image-watch is a read-only daemon that monitors container
// images actually running on a host and reports meaningful upstream
// image updates.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/k-wlosek/image-watch/internal/config"
	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/log"
	"github.com/k-wlosek/image-watch/internal/metrics"
	"github.com/k-wlosek/image-watch/internal/observer"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		runDaemon()
		return
	}

	switch os.Args[1] {
	case "daemon":
		runDaemon()
	case "check":
		runCheck()
	case "healthcheck":
		os.Exit(runHealthcheck())
	case "version":
		fmt.Println("image-watch " + version)
	default:
		fmt.Fprintf(os.Stderr, "image-watch: unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func runDaemon() {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "image-watch daemon: config error:", err)
		os.Exit(1)
	}
	log.Setup(cfg.Log.Level, cfg.Log.Format)

	var m *metrics.Metrics
	if cfg.Metrics.Enabled {
		m = metrics.New()
	}

	obs, err := buildObserver(cfg, m)
	if err != nil {
		slog.Error("failed to initialize observer", "error", err)
		os.Exit(1)
	}
	if closer, ok := obs.Store.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}

	notifiers := buildNotifiers(cfg)

	daemon := &Daemon{
		Config:          cfg,
		Observer:        obs,
		Notifiers:       notifiers,
		Metrics:         m,
		RegistryOutages: NewRegistryOutageTracker(),
	}

	var httpServer *http.Server
	if cfg.Metrics.Enabled {
		httpServer = newHTTPServer(cfg.Metrics.Listen, m)
		go func() {
			if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("metrics server error", "error", err)
			}
		}()
		slog.Info("metrics endpoint listening", "addr", cfg.Metrics.Listen)
	}

	slog.Info("daemon starting",
		"runtime", cfg.Runtime.Type,
		"interval", cfg.CheckInterval,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runErr := daemon.Run(ctx)
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		slog.Error("scheduler stopped unexpectedly", "error", runErr)
	}

	slog.Info("shutting down")
	if httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdownHTTPServer(shutdownCtx, httpServer); err != nil {
			slog.Error("metrics server shutdown error", "error", err)
		}
	}
}

func runCheck() {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "image-watch check: config error:", err)
		os.Exit(1)
	}
	log.Setup(cfg.Log.Level, cfg.Log.Format)

	obs, err := buildObserver(cfg, nil)
	if err != nil {
		slog.Error("failed to initialize observer", "error", err)
		os.Exit(1)
	}
	if closer, ok := obs.Store.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	results, err := obs.Check(ctx)
	if err != nil {
		slog.Error("failed to list running containers", "error", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		fmt.Println("no monitored images (no running containers with tagged, non-digest-pinned images)")
		return
	}

	fmt.Println("detected events (unfiltered)")
	exitCode := 0
	for _, r := range results {
		printResult(r)
		if r.Err != nil {
			exitCode = 1
		}
	}

	fmt.Println("\nnotification pipeline (policy-filtered, deduped)")
	note := BuildNotification(ctx, results, obs.Store)
	if len(note.Items) == 0 {
		fmt.Println("nothing to notify (either no policy-allowed events, or already notified in a previous run)")
	} else {
		notifiers := buildNotifiers(cfg)
		if err := DeliverAndMark(ctx, notifiers, note, cfg.Notifications.Mode, obs.Store); err != nil {
			slog.Error("notification delivery failed", "error", err)
			exitCode = 1
		}
	}

	os.Exit(exitCode)
}

func printResult(r observer.Result) {
	fmt.Printf("\n%s:%s (%s)\n", r.Image.Registry+"/"+r.Image.Repository, r.Image.TagOrEmpty(), r.Platform.String())
	fmt.Printf("  containers: %v\n", r.ContainerNames)
	fmt.Printf("  policy:     %s\n", enabledCategories(r.EffectivePolicy))

	if r.Err != nil {
		status := "error"
		if r.Stale {
			status = "stale (using last known state)"
		}
		fmt.Printf("  status:     %s: %v\n", status, r.Err)
		return
	}

	if r.Partial {
		fmt.Println("  status:     partial (some candidate checks failed this cycle -- events below may understate what's available)")
	}

	if len(r.Events) == 0 {
		fmt.Println("  no updates detected")
		return
	}
	for _, e := range r.Events {
		printEvent(e)
	}
}

func printEvent(e event.Event) {
	switch e.Type {
	case event.TagChanged, event.TagMutated:
		candidate := ""
		if e.CandidateTag != "" {
			candidate = fmt.Sprintf(" (inferred version: %s)", e.CandidateTag)
		}
		fmt.Printf("  [%s] %s -> %s%s\n", e.Type, e.CurrentDigest, e.CandidateDigest, candidate)
	default:
		combined := ""
		if e.CombinedCandidate != "" {
			combined = fmt.Sprintf(" (combined: %s)", e.CombinedCandidate)
		}
		fmt.Printf("  [%s] %s -> %s%s\n", e.Type, e.CurrentTag, e.CandidateTag, combined)
	}
}

// runHealthcheck queries the daemon's own /healthz endpoint.
func runHealthcheck() int {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck: config error:", err)
		return 1
	}
	log.Setup(cfg.Log.Level, cfg.Log.Format)
	url := "http://" + healthcheckAddr(cfg.Metrics.Listen) + "/healthz"

	resp, err := http.Get(url)
	if err != nil {
		slog.Error("healthcheck request failed", "error", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		slog.Error("healthcheck unhealthy", "status", resp.StatusCode)
		return 1
	}
	return 0
}

// healthcheckAddr rewrites a bind address for local health checks.
func healthcheckAddr(listen string) string {
	if len(listen) > 0 && listen[0] == ':' {
		return "127.0.0.1" + listen
	}
	for i := 0; i < len(listen); i++ {
		if listen[i] == ':' {
			host := listen[:i]
			if host == "0.0.0.0" || host == "" {
				return "127.0.0.1" + listen[i:]
			}
			return listen
		}
	}
	return listen
}
