// Command image-watch is a read-only daemon that monitors container
// images actually running on a host and reports meaningful upstream
// image updates.
package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/example/image-watch/internal/config"
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
	os.Exit(1)
}

// runHealthcheck queries the daemon's own /healthz endpoint and exits 0
// if healthy, non-zero otherwise
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
