package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Re-exec the test binary in child mode (see TestMain) to exercise main's os.Exit paths.

const execChildEnv = "IMAGE_WATCH_EXEC_CHILD"
const execArgsEnv = "IMAGE_WATCH_EXEC_ARGS"

// TestMain runs main() in child mode when IMAGE_WATCH_EXEC_CHILD is set.
func TestMain(m *testing.M) {
	if os.Getenv(execChildEnv) != "" {
		args := strings.Fields(os.Getenv(execArgsEnv))
		os.Args = append([]string{"image-watch"}, args...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runAsCommand runs main as a child process with the given argv.
func runAsCommand(t *testing.T, configPath string, args ...string) (exitCode int, stdout, stderr string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(),
		execChildEnv+"=1",
		execArgsEnv+"="+strings.Join(args, " "),
	)
	if configPath != "" {
		cmd.Env = append(cmd.Env, "IMAGE_WATCH_CONFIG_PATH="+configPath)
	}
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), out.String(), errOut.String()
		}
		t.Fatalf("failed to run child: %v", err)
	}
	return 0, out.String(), errOut.String()
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return fmt.Sprintf("127.0.0.1:%d", port)
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCommand_Version(t *testing.T) {
	code, out, _ := runAsCommand(t, "", "version")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "image-watch "+version) {
		t.Errorf("output = %q, want it to report %q", out, "image-watch "+version)
	}
}

func TestCommand_UnknownSubcommand(t *testing.T) {
	code, _, errOut := runAsCommand(t, "", "frobnicate")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Errorf("stderr = %q, want an unknown-command message", errOut)
	}
}

func TestCommand_CheckConfigError(t *testing.T) {
	cfg := writeConfig(t, ": : :\n  broken yaml\n")
	code, _, errOut := runAsCommand(t, cfg, "check")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "config") {
		t.Errorf("stderr = %q, want a config error", errOut)
	}
}

func TestCommand_CheckObserverError(t *testing.T) {
	cfg := writeConfig(t, strings.Join([]string{
		"runtime:",
		"  endpoint: ssh://example.com",
	}, "\n"))
	code, _, errOut := runAsCommand(t, cfg, "check")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "failed to initialize docker runtime") {
		t.Errorf("stderr = %q, want a docker init error", errOut)
	}
}

// tcp://127.0.0.1:1 fails deterministically without a Docker daemon.
func TestCommand_CheckRuntimeError(t *testing.T) {
	cfg := writeConfig(t, strings.Join([]string{
		"runtime:",
		"  endpoint: tcp://127.0.0.1:1",
		"state:",
		"  path: " + filepath.Join(t.TempDir(), "state.db"),
	}, "\n"))
	code, _, errOut := runAsCommand(t, cfg, "check")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "failed to list running containers") {
		t.Errorf("stderr = %q, want a container-list error", errOut)
	}
}

func TestCommand_DaemonErrors(t *testing.T) {
	for name, config := range map[string]string{
		"config error": ": : :\n  broken yaml\n",
		"observer error": strings.Join([]string{
			"runtime:",
			"  endpoint: ssh://example.com",
		}, "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			cfg := writeConfig(t, config)
			code, _, errOut := runAsCommand(t, cfg, "daemon")
			if code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if errOut == "" {
				t.Error("expected an error message on stderr")
			}
		})
	}
}

// Start daemon, wait for /healthz, SIGTERM, expect a clean exit.
func TestCommand_DaemonLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daemon lifecycle test in short mode")
	}
	metricsListen := freePort(t)
	cfg := writeConfig(t, strings.Join([]string{
		"runtime:",
		"  endpoint: tcp://127.0.0.1:1",
		"state:",
		"  path: " + filepath.Join(t.TempDir(), "state.db"),
		"metrics:",
		"  enabled: true",
		"  listen: " + metricsListen,
	}, "\n"))

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(),
		execChildEnv+"=1",
		execArgsEnv+"=daemon",
		"IMAGE_WATCH_CONFIG_PATH="+cfg,
	)
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start daemon child: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	healthReady := false
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+metricsListen+"/healthz", nil)
		if err != nil {
			break
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				healthReady = true
				break
			}
		}
		select {
		case <-ctx.Done():
			cmd.Process.Kill()
			t.Fatalf("daemon did not become healthy: %v", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}

	if !healthReady {
		t.Fatalf("daemon never served /healthz; stderr: %s", errOut.String())
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("daemon did not exit after SIGTERM")
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon exited with error: %v; stderr: %s", err, errOut.String())
		}
	}

	if !strings.Contains(out.String(), "shutting down") {
		t.Errorf("stdout = %q, want a shutdown line", out.String())
	}
}
