package log_test

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/k-wlosek/image-watch/internal/log"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestSetup_TextFormat(t *testing.T) {
	out := captureStderr(t, func() {
		log.Setup("info", "text")
		slog.Info("test message", "key", "value")
	})
	if !strings.Contains(out, "test message") {
		t.Errorf("expected log output to contain 'test message', got: %s", out)
	}
	if !strings.Contains(out, "key=value") {
		t.Errorf("expected log output to contain 'key=value', got: %s", out)
	}
}

func TestSetup_JSONFormat(t *testing.T) {
	out := captureStderr(t, func() {
		log.Setup("info", "json")
		slog.Info("test message", "key", "value")
	})
	if !strings.Contains(out, `"msg":"test message"`) {
		t.Errorf("expected JSON log output to contain msg field, got: %s", out)
	}
	if !strings.Contains(out, `"key":"value"`) {
		t.Errorf("expected JSON log output to contain key field, got: %s", out)
	}
}

func TestSetup_DebugLevel(t *testing.T) {
	out := captureStderr(t, func() {
		log.Setup("debug", "text")
		slog.Debug("debug msg")
	})
	if !strings.Contains(out, "debug msg") {
		t.Errorf("debug message should appear at debug level, got: %s", out)
	}
}

func TestSetup_InfoLevelSuppressesDebug(t *testing.T) {
	out := captureStderr(t, func() {
		log.Setup("info", "text")
		slog.Debug("should not appear")
	})
	if strings.Contains(out, "should not appear") {
		t.Errorf("debug message should not appear at info level, got: %s", out)
	}
}

func TestSetup_WarnLevel(t *testing.T) {
	out := captureStderr(t, func() {
		log.Setup("warn", "text")
		slog.Info("should not appear")
		slog.Warn("warning msg")
	})
	if strings.Contains(out, "should not appear") {
		t.Errorf("info message should not appear at warn level")
	}
	if !strings.Contains(out, "warning msg") {
		t.Errorf("warn message should appear at warn level, got: %s", out)
	}
}

func TestSetup_ErrorLevel(t *testing.T) {
	out := captureStderr(t, func() {
		log.Setup("error", "text")
		slog.Info("no")
		slog.Warn("no")
		slog.Error("error msg")
	})
	if strings.Contains(out, "no") {
		t.Errorf("info/warn messages should not appear at error level")
	}
	if !strings.Contains(out, "error msg") {
		t.Errorf("error message should appear at error level, got: %s", out)
	}
}
