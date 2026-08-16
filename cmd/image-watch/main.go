// Command image-watch is a read-only daemon that monitors container
// images actually running on a host and reports meaningful upstream
// image updates.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/image-watch/internal/config"
	"github.com/example/image-watch/internal/event"
	"github.com/example/image-watch/internal/metrics"
	"github.com/example/image-watch/internal/observer"
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

	var m *metrics.Metrics
	if cfg.Metrics.Enabled {
		m = metrics.New()
	}

	obs, err := buildObserver(cfg, m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "image-watch daemon:", err)
		os.Exit(1)
	}
	if closer, ok := obs.Store.(interface{ Close() error }); ok {
		defer closer.Close()
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
				fmt.Fprintln(os.Stderr, "image-watch daemon: http server error:", err)
			}
		}()
		fmt.Printf("image-watch: operational endpoints listening on %s\n", cfg.Metrics.Listen)
	}

	fmt.Printf("image-watch: starting (runtime=%s, interval=%s)\n", cfg.Runtime.Type, cfg.CheckInterval)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runErr := daemon.Run(ctx)
	// context.Canceled is the expected outcome of a clean shutdown
	// signal, not a failure worth a non-zero exit.
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		fmt.Fprintln(os.Stderr, "image-watch daemon: scheduler stopped unexpectedly:", runErr)
	}

	fmt.Println("image-watch: shutting down")
	if httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdownHTTPServer(shutdownCtx, httpServer); err != nil {
			fmt.Fprintln(os.Stderr, "image-watch daemon: http server shutdown error:", err)
		}
	}
}

func runCheck() {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "image-watch check: config error:", err)
		os.Exit(1)
	}

	obs, err := buildObserver(cfg, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "image-watch check:", err)
		os.Exit(1)
	}
	if closer, ok := obs.Store.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	results, err := obs.Check(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "image-watch check: failed to list running containers:", err)
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
			fmt.Fprintln(os.Stderr, "notification delivery had errors:", err)
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
	url := "http://" + healthcheckAddr(cfg.Metrics.Listen) + "/healthz"

	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck: request failed:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: unhealthy status", resp.StatusCode)
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
