// Command image-watch is a read-only daemon that monitors container
// images actually running on a host and reports meaningful upstream
// image updates.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/example/image-watch/internal/config"
	"github.com/example/image-watch/internal/event"
	"github.com/example/image-watch/internal/notify"
	"github.com/example/image-watch/internal/notify/stdout"
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
	cfg := config.Default()
	fmt.Printf("image-watch starting (runtime=%s, interval=%s)\n", cfg.Runtime.Type, cfg.CheckInterval)
	os.Exit(1)
}

// runCheck performs one check-and-exit cycle
func runCheck() {
	cfg := config.Default()

	obs, err := buildObserver(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "image-watch check:", err)
		os.Exit(1)
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
		notifiers := []notify.Notifier{stdout.New()}
		delivered, dErr := Deliver(ctx, notifiers, note)
		if dErr != nil {
			fmt.Fprintln(os.Stderr, "notification delivery had errors:", dErr)
		}
		if delivered {
			MarkDelivered(ctx, note, obs.Store)
		} else {
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
	resp, err := http.Get("http://127.0.0.1:9090/healthz")
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
