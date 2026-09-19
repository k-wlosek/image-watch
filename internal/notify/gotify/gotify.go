// Package gotify implements notify.Notifier by POSTing messages to a Gotify
// server's /message endpoint.
package gotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/notify/stdout"
	"github.com/k-wlosek/image-watch/internal/secret"
)

func init() {
	notify.Register("gotify", func(params map[string]string) (notify.Notifier, error) {
		cfg, err := ParseConfig(params)
		if err != nil {
			return nil, err
		}
		return New(cfg, nil), nil
	})
}

// ParseConfig extracts Gotify configuration from a params map.
func ParseConfig(params map[string]string) (Config, error) {
	serverURL := params["server_url"]
	if serverURL == "" {
		return Config{}, fmt.Errorf("gotify: server_url is required")
	}
	serverURL = strings.TrimSuffix(serverURL, "/")

	tokenFile := params["token_file"]
	if tokenFile == "" {
		return Config{}, fmt.Errorf("gotify: token_file is required")
	}
	token, err := secret.ReadFile(tokenFile)
	if err != nil {
		return Config{}, fmt.Errorf("gotify: token_file: %w", err)
	}
	if token == "" {
		return Config{}, fmt.Errorf("gotify: token_file is empty")
	}
	priority := params["priority"]
	if priority == "" {
		priority = "5"
	}
	return Config{
		ServerURL: serverURL,
		Token:     token,
		Priority:  priority,
	}, nil
}

// Config configures the Gotify notifier.
type Config struct {
	ServerURL string
	Token     string
	Priority  string // defaults to "5"
}

// Notifier delivers notifications to a Gotify server.
type Notifier struct {
	cfg        Config
	httpClient *http.Client
}

// New constructs a Gotify Notifier. httpClient may be nil, in which case
// a client with a bounded timeout is used.
func New(cfg Config, httpClient *http.Client) *Notifier {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Notifier{cfg: cfg, httpClient: httpClient}
}

var _ notify.Notifier = (*Notifier)(nil)

// gotifyMessage is the JSON payload sent to Gotify.
type gotifyMessage struct {
	Title    string `json:"title"`
	Message  string `json:"message"`
	Priority int    `json:"priority"`
}

// Notify sends the notification batch to the Gotify server.
func (n *Notifier) Notify(ctx context.Context, note notify.Notification) error {
	if n.cfg.ServerURL == "" {
		return fmt.Errorf("gotify: no server URL configured")
	}
	if len(note.Items) == 0 {
		return nil
	}

	title := "Image Watch"
	if note.Hostname != "" {
		title = "Image Watch (" + note.Hostname + ")"
	}

	body := stdout.PlainText(note)

	priority := 5
	if p, err := fmt.Sscanf(n.cfg.Priority, "%d", &priority); err != nil || p != 1 {
		priority = 5
	}

	msg := gotifyMessage{
		Title:    title,
		Message:  body,
		Priority: priority,
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("gotify: failed to marshal payload: %w", err)
	}

	url := n.cfg.ServerURL + "/message"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("gotify: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", n.cfg.Token)

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gotify: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gotify: unexpected status %d", resp.StatusCode)
	}
	return nil
}
