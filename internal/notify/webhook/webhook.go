// Package webhook implements notify.Notifier by POSTing structured JSON to a configured URL.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/k-wlosek/image-watch/internal/notify"
)

// Config configures the webhook notifier.
type Config struct {
	URL string
}

// Notifier delivers notifications as JSON POST requests.
type Notifier struct {
	cfg        Config
	httpClient *http.Client
}

// New constructs a webhook Notifier. httpClient may be nil, in which
// case a client with a bounded timeout is used.
func New(cfg Config, httpClient *http.Client) *Notifier {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Notifier{cfg: cfg, httpClient: httpClient}
}

var _ notify.Notifier = (*Notifier)(nil)

// payload matches the JSON sent by the webhook notifier.
type payload struct {
	Event      string           `json:"event"`
	Image      string           `json:"image"`
	Current    string           `json:"current"`
	Candidate  string           `json:"candidate"`
	Platform   *platformPayload `json:"platform,omitempty"`
	Containers []string         `json:"containers,omitempty"`
	Suppressed []string         `json:"suppressed,omitempty"`
}

type platformPayload struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

// Notify sends one POST per item and returns any failures.
func (n *Notifier) Notify(ctx context.Context, note notify.Notification) error {
	if n.cfg.URL == "" {
		return fmt.Errorf("webhook: no URL configured")
	}

	var errs []error
	for _, item := range note.Items {
		if err := n.sendOne(ctx, item); err != nil {
			errs = append(errs, fmt.Errorf("webhook: item %s/%s: %w", item.Image, item.CandidateTag, err))
		}
	}
	return errors.Join(errs...)
}

func (n *Notifier) sendOne(ctx context.Context, item notify.Item) error {
	p := payload{
		Event:     string(item.Type),
		Image:     item.Image,
		Current:   item.CurrentTag,
		Candidate: item.CandidateTag,
	}
	if item.Platform != "" {
		os, arch := splitPlatform(item.Platform)
		p.Platform = &platformPayload{OS: os, Architecture: arch}
	}
	p.Containers = item.ContainerNames
	p.Suppressed = item.Suppressed

	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func splitPlatform(p string) (os, arch string) {
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			return p[:i], p[i+1:]
		}
	}
	return p, ""
}
