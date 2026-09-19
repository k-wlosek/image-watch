// Package discord implements notify.Notifier by POSTing embeds to a Discord
// webhook URL.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/notify/stdout"
	"github.com/k-wlosek/image-watch/internal/secret"
)

const (
	defaultTitle       = "{{.Image}} - {{.Host}}"
	defaultDescription = `{{.Category}}
{{.Image}}:{{.CurrentTag}} -> {{.CandidateTag}}
{{- if or (eq .Event "TAG_CHANGED") (eq .Event "TAG_MUTATED")}}
{{.CurrentDigest}} -> {{.CandidateDigest}}
{{- if .CandidateTag}} (inferred version: {{.CandidateTag}}){{end}}
{{- end}}
{{- if .CombinedCandidate}}
combined: {{.CombinedCandidate}}
{{- end}}
{{- if .Containers}}
containers: {{.Containers}}
{{- end}}
{{- if .Suppressed}}
suppressed: {{.Suppressed}}
{{- end}}`
	defaultColor  = 0x2ECC71
	defaultFooter = "Image Watch"
)

func init() {
	notify.Register("discord", func(params map[string]string) (notify.Notifier, error) {
		cfg, err := ParseConfig(params)
		if err != nil {
			return nil, err
		}
		return New(cfg, nil), nil
	})
}

// ParseConfig extracts Discord webhook configuration from a params map.
func ParseConfig(params map[string]string) (Config, error) {
	webhookURLFile := params["webhook_url_file"]
	if webhookURLFile == "" {
		return Config{}, fmt.Errorf("discord: webhook_url_file is required")
	}
	webhookURL, err := secret.ReadFile(webhookURLFile)
	if err != nil {
		return Config{}, fmt.Errorf("discord: webhook_url_file: %w", err)
	}
	if webhookURL == "" {
		return Config{}, fmt.Errorf("discord: webhook_url_file is empty")
	}

	username := params["username"]
	if username == "" {
		username = "Image Watch"
	}
	renderEmbeds := true
	if v := params["render_embeds"]; v != "" {
		renderEmbeds = v == "true"
	}

	title := params["title"]
	if title == "" {
		title = defaultTitle
	}
	description := params["description"]
	if description == "" {
		description = defaultDescription
	}
	color := defaultColor
	if v := params["color"]; v != "" {
		c, err := parseColor(v)
		if err != nil {
			return Config{}, fmt.Errorf("discord: color: %w", err)
		}
		color = c
	}
	footer := defaultFooter
	if v, ok := params["footer"]; ok {
		footer = v
	}

	return Config{
		WebhookURL:   webhookURL,
		Username:     username,
		RenderEmbeds: renderEmbeds,
		Title:        title,
		Description:  description,
		Color:        color,
		Footer:       footer,
	}, nil
}

// Config configures the Discord webhook notifier.
type Config struct {
	WebhookURL   string
	Username     string // defaults to "Image Watch"
	RenderEmbeds bool   // defaults to true
	Title        string // Go template, defaults to "{{.Image}}"
	Description  string // Go template, defaults to stdout-style formatting
	Color        int    // embed color, defaults to 0x2ECC71 (green)
	Footer       string // footer text, defaults to "Image Watch"; empty = no footer
}

// templateData is the context passed to user-provided Go templates.
type templateData struct {
	Image             string
	Event             string
	Category          string
	CurrentTag        string
	CandidateTag      string
	CurrentDigest     string
	CandidateDigest   string
	CombinedCandidate string
	Platform          string
	Host              string
	Containers        string
	Suppressed        string
}

// Notifier delivers notifications as Discord embeds via webhook.
type Notifier struct {
	cfg        Config
	httpClient *http.Client
	titleTmpl  *template.Template
	descTmpl   *template.Template
}

// New constructs a Discord Notifier. httpClient may be nil, in which case
// a client with a bounded timeout is used.
func New(cfg Config, httpClient *http.Client) *Notifier {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	n := &Notifier{cfg: cfg, httpClient: httpClient}
	n.titleTmpl = template.Must(template.New("title").Parse(cfg.Title))
	n.descTmpl = template.Must(template.New("description").Parse(cfg.Description))
	return n
}

var _ notify.Notifier = (*Notifier)(nil)

// Notify sends one embed per item to the configured webhook.
func (n *Notifier) Notify(ctx context.Context, note notify.Notification) error {
	if n.cfg.WebhookURL == "" {
		return fmt.Errorf("discord: no webhook URL configured")
	}
	if len(note.Items) == 0 {
		return nil
	}

	var errs []error
	for _, item := range note.Items {
		if err := n.sendOne(ctx, item, note.Hostname); err != nil {
			errs = append(errs, fmt.Errorf("discord: item %s/%s: %w", item.Image, item.CandidateTag, err))
		}
	}
	return errors.Join(errs...)
}

func (n *Notifier) sendOne(ctx context.Context, item notify.Item, hostname string) error {
	var body webhookBody
	body.Username = n.cfg.Username

	if n.cfg.RenderEmbeds {
		body.Embeds = []discordEmbed{n.buildEmbed(item, hostname)}
	} else {
		body.Content = formatPlainText(item, hostname)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	return n.postWithRetry(ctx, payload)
}

func (n *Notifier) buildEmbed(item notify.Item, hostname string) discordEmbed {
	data := templateData{
		Image:             item.Image,
		Event:             string(item.Type),
		Category:          event.CategoryLabel(item.Type),
		CurrentTag:        item.CurrentTag,
		CandidateTag:      item.CandidateTag,
		CurrentDigest:     item.CurrentDigest,
		CandidateDigest:   item.CandidateDigest,
		CombinedCandidate: item.CombinedCandidate,
		Platform:          item.Platform,
		Host:              hostname,
		Containers:        formatStringSlice(item.ContainerNames),
		Suppressed:        formatStringSlice(item.Suppressed),
	}

	title := executeTemplate(n.titleTmpl, data)
	description := executeTemplate(n.descTmpl, data)

	embed := discordEmbed{
		Title:       title,
		Description: description,
		Color:       n.cfg.Color,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	if n.cfg.Footer != "" {
		embed.Footer = &embedFooter{Text: n.cfg.Footer}
	}

	return embed
}

func (n *Notifier) postWithRetry(ctx context.Context, payload []byte) error {
	maxAttempts := 3
	for attempt := range maxAttempts {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.WebhookURL, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("failed to build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := n.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusNoContent || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
			return nil
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			wait := n.parseRateLimit(resp)
			if attempt < maxAttempts-1 {
				time.Sleep(wait)
				continue
			}
			return fmt.Errorf("rate limited after %d attempts", maxAttempts)
		}

		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) parseRateLimit(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if sec, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(sec*1000) * time.Millisecond
		}
	}
	if v := resp.Header.Get("X-RateLimit-Reset-After"); v != "" {
		if sec, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(sec*1000) * time.Millisecond
		}
	}
	var rlBody struct {
		RetryAfter float64 `json:"retry_after"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rlBody); err == nil && rlBody.RetryAfter > 0 {
		return time.Duration(rlBody.RetryAfter*1000) * time.Millisecond
	}
	return 5 * time.Second
}

func executeTemplate(tmpl *template.Template, data templateData) string {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Sprintf("(template error: %v)", err)
	}
	return strings.TrimSpace(buf.String())
}

func formatStringSlice(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return fmt.Sprintf("%v", ss)
}

func formatPlainText(item notify.Item, hostname string) string {
	return stdout.PlainText(notify.Notification{
		Hostname: hostname,
		Items:    []notify.Item{item},
	})
}

func parseColor(s string) (int, error) {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	c, err := strconv.ParseInt(s, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid hex color %q", s)
	}
	return int(c), nil
}
