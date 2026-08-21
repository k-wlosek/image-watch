// Package ntfy implements notify.Notifier against the ntfy.sh publish API.
package ntfy

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/notify/stdout"
)

// defaultServerURL is ntfy's public hosted instance.
const defaultServerURL = "https://ntfy.sh"

// Config configures the ntfy notifier.
type Config struct {
	ServerURL string // defaults to https://ntfy.sh if empty
	Topic     string

	Username string // optional
	Password string // optional

	Priority string // ntfy priority header value, e.g. "default", "high"
	Title    string // defaults to "Image Watch" if empty
}

// Notifier publishes notifications to an ntfy topic.
type Notifier struct {
	cfg        Config
	httpClient *http.Client
}

// New constructs an ntfy Notifier. httpClient may be nil, in which case
// a client with a bounded timeout is used.
func New(cfg Config, httpClient *http.Client) *Notifier {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Notifier{cfg: cfg, httpClient: httpClient}
}

var _ notify.Notifier = (*Notifier)(nil)

// Notify publishes one message per Notification.
func (n *Notifier) Notify(ctx context.Context, note notify.Notification) error {
	if n.cfg.Topic == "" {
		return fmt.Errorf("ntfy: no topic configured")
	}
	if len(note.Items) == 0 {
		return nil
	}

	server := n.cfg.ServerURL
	if server == "" {
		server = defaultServerURL
	}
	url := strings.TrimSuffix(server, "/") + "/" + n.cfg.Topic

	body := formatBody(note)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("ntfy: failed to build request: %w", err)
	}

	title := n.cfg.Title
	if title == "" {
		if note.Hostname != "" {
			title = "Image Watch (" + note.Hostname + ")"
		} else {
			title = "Image Watch"
		}
	}
	req.Header.Set("Title", title)
	if n.cfg.Priority != "" {
		req.Header.Set("Priority", n.cfg.Priority)
	}
	if n.cfg.Username != "" {
		req.SetBasicAuth(n.cfg.Username, n.cfg.Password)
	}

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// formatBody reuses the stdout notifier's batch-summary formatting.
func formatBody(note notify.Notification) string {
	var b strings.Builder
	sn := &stdout.Notifier{Writer: &b}
	_ = sn.Notify(context.Background(), note)
	return b.String()
}
