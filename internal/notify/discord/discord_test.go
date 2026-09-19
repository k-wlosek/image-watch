package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/k-wlosek/image-watch/internal/event"
	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/secret"
)

func TestParseConfig_MissingWebhookURLFileReturnsError(t *testing.T) {
	_, err := ParseConfig(map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "webhook_url_file is required") {
		t.Fatalf("expected webhook_url_file error, got %v", err)
	}
}

func TestParseConfig_EmptyWebhookURLFileReturnsError(t *testing.T) {
	f := secret.WriteFile(t, "")
	_, err := ParseConfig(map[string]string{"webhook_url_file": f})
	if err == nil || !strings.Contains(err.Error(), "webhook_url_file is empty") {
		t.Fatalf("expected empty webhook_url_file error, got %v", err)
	}
}

func TestParseConfig_HappyPath(t *testing.T) {
	f := secret.WriteFile(t, "https://discord.com/api/webhooks/123/abc")
	cfg, err := ParseConfig(map[string]string{"webhook_url_file": f})
	if err != nil {
		t.Fatalf("ParseConfig error: %v", err)
	}
	if cfg.WebhookURL != "https://discord.com/api/webhooks/123/abc" {
		t.Errorf("WebhookURL = %q", cfg.WebhookURL)
	}
	if cfg.Username != "Image Watch" {
		t.Errorf("Username = %q, want 'Image Watch'", cfg.Username)
	}
	if !cfg.RenderEmbeds {
		t.Errorf("RenderEmbeds = %v, want true", cfg.RenderEmbeds)
	}
	if cfg.Color != defaultColor {
		t.Errorf("Color = %d, want %d", cfg.Color, defaultColor)
	}
	if cfg.Footer != "Image Watch" {
		t.Errorf("Footer = %q, want 'Image Watch'", cfg.Footer)
	}
}

func TestParseConfig_CustomParams(t *testing.T) {
	f := secret.WriteFile(t, "https://discord.com/api/webhooks/123/abc")
	cfg, err := ParseConfig(map[string]string{
		"webhook_url_file": f,
		"username":         "Bot",
		"render_embeds":    "false",
		"color":            "0xFF0000",
		"footer":           "",
	})
	if err != nil {
		t.Fatalf("ParseConfig error: %v", err)
	}
	if cfg.Username != "Bot" {
		t.Errorf("Username = %q, want Bot", cfg.Username)
	}
	if cfg.RenderEmbeds {
		t.Errorf("RenderEmbeds = %v, want false", cfg.RenderEmbeds)
	}
	if cfg.Color != 0xFF0000 {
		t.Errorf("Color = %d, want 0xFF0000", cfg.Color)
	}
	if cfg.Footer != "" {
		t.Errorf("Footer = %q, want empty", cfg.Footer)
	}
}

func TestParseConfig_InvalidColorReturnsError(t *testing.T) {
	f := secret.WriteFile(t, "https://discord.com/api/webhooks/123/abc")
	_, err := ParseConfig(map[string]string{
		"webhook_url_file": f,
		"color":            "not-a-color",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid hex color") {
		t.Fatalf("expected color error, got %v", err)
	}
}

func TestNotify_SendsEmbedWithDescription(t *testing.T) {
	var gotBody webhookBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &gotBody); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := New(Config{
		WebhookURL: srv.URL, Username: "TestBot", RenderEmbeds: true,
		Title: "{{.Image}}", Description: "{{.Category}}\n{{.Image}}:{{.CurrentTag}} -> {{.CandidateTag}}",
		Color: defaultColor, Footer: "Image Watch",
	}, srv.Client())

	note := notify.Notification{Items: []notify.Item{
		{
			Image: "ghcr.io/acme/foo", Type: event.PatchAvailable,
			CurrentTag: "1.2.3", CandidateTag: "1.2.4",
			Platform: "linux/amd64", ContainerNames: []string{"web1"},
		},
	}}

	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if len(gotBody.Embeds) != 1 {
		t.Fatalf("expected 1 embed, got %d", len(gotBody.Embeds))
	}
	embed := gotBody.Embeds[0]
	if embed.Title != "ghcr.io/acme/foo" {
		t.Errorf("Title = %q, want image name", embed.Title)
	}
	if !strings.Contains(embed.Description, "PATCH") {
		t.Errorf("Description should contain category, got %q", embed.Description)
	}
	if !strings.Contains(embed.Description, "1.2.3 -> 1.2.4") {
		t.Errorf("Description should contain version, got %q", embed.Description)
	}
	if embed.Footer == nil || embed.Footer.Text != "Image Watch" {
		t.Errorf("Footer = %v, want 'Image Watch'", embed.Footer)
	}
}

func TestNotify_TagChangedIncludesDigests(t *testing.T) {
	var gotBody webhookBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := New(Config{
		WebhookURL: srv.URL, RenderEmbeds: true,
		Title: defaultTitle, Description: defaultDescription,
		Color: defaultColor, Footer: "Image Watch",
	}, srv.Client())

	note := notify.Notification{Items: []notify.Item{
		{
			Image: "ghcr.io/acme/foo", Type: event.TagChanged,
			CurrentTag: "latest", CandidateTag: "1.2.4",
			CurrentDigest: "sha256:AAAA", CandidateDigest: "sha256:BBBB",
		},
	}}

	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	embed := gotBody.Embeds[0]
	if !strings.Contains(embed.Description, "sha256:AAAA -> sha256:BBBB") {
		t.Errorf("Description should contain digests for TAG_CHANGED, got %q", embed.Description)
	}
}

func TestNotify_PatchShowsCombinedCandidate(t *testing.T) {
	var gotBody webhookBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := New(Config{
		WebhookURL: srv.URL, RenderEmbeds: true,
		Title: defaultTitle, Description: defaultDescription,
		Color: defaultColor, Footer: "Image Watch",
	}, srv.Client())

	note := notify.Notification{Items: []notify.Item{
		{
			Image: "ghcr.io/acme/foo", Type: event.PatchAvailable,
			CurrentTag: "1.2.3", CandidateTag: "1.2.4",
			CombinedCandidate: "1.2.4+upstream",
		},
	}}

	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	embed := gotBody.Embeds[0]
	if !strings.Contains(embed.Description, "combined: 1.2.4+upstream") {
		t.Errorf("Description should contain combined candidate, got %q", embed.Description)
	}
}

func TestNotify_ShowsSuppressedContainers(t *testing.T) {
	var gotBody webhookBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := New(Config{
		WebhookURL: srv.URL, RenderEmbeds: true,
		Title: defaultTitle, Description: defaultDescription,
		Color: defaultColor, Footer: "Image Watch",
	}, srv.Client())

	note := notify.Notification{Items: []notify.Item{
		{
			Image: "ghcr.io/acme/foo", Type: event.PatchAvailable,
			CurrentTag: "1", CandidateTag: "2",
			ContainerNames: []string{"web1"},
			Suppressed:     []string{"web2"},
		},
	}}

	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	embed := gotBody.Embeds[0]
	if !strings.Contains(embed.Description, "containers: [web1]") {
		t.Errorf("Description should contain containers, got %q", embed.Description)
	}
	if !strings.Contains(embed.Description, "suppressed: [web2]") {
		t.Errorf("Description should contain suppressed, got %q", embed.Description)
	}
}

func TestNotify_EmptyFooterOmitted(t *testing.T) {
	var gotBody webhookBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := New(Config{
		WebhookURL: srv.URL, RenderEmbeds: true,
		Title: defaultTitle, Description: defaultDescription,
		Color: defaultColor, Footer: "",
	}, srv.Client())

	note := notify.Notification{Items: []notify.Item{
		{Image: "a", Type: event.PatchAvailable, CurrentTag: "1", CandidateTag: "2"},
	}}

	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	embed := gotBody.Embeds[0]
	if embed.Footer != nil {
		t.Errorf("expected no footer when empty, got %v", embed.Footer)
	}
}

func TestNotify_CustomColor(t *testing.T) {
	var gotBody webhookBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := New(Config{
		WebhookURL: srv.URL, RenderEmbeds: true,
		Title: defaultTitle, Description: defaultDescription,
		Color: 0xFF0000, Footer: "Image Watch",
	}, srv.Client())

	note := notify.Notification{Items: []notify.Item{
		{Image: "a", Type: event.PatchAvailable, CurrentTag: "1", CandidateTag: "2"},
	}}

	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	embed := gotBody.Embeds[0]
	if embed.Color != 0xFF0000 {
		t.Errorf("Color = %d, want 0xFF0000", embed.Color)
	}
}

func TestNotify_PlainTextFallback(t *testing.T) {
	var gotBody webhookBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := New(Config{
		WebhookURL: srv.URL, Username: "TestBot", RenderEmbeds: false,
		Title: defaultTitle, Description: defaultDescription,
		Color: defaultColor, Footer: "Image Watch",
	}, srv.Client())

	note := notify.Notification{Items: []notify.Item{
		{Image: "ghcr.io/acme/foo", Type: event.PatchAvailable, CurrentTag: "1", CandidateTag: "2"},
	}}

	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if gotBody.Content == "" {
		t.Errorf("expected non-empty content for plain text fallback")
	}
	if len(gotBody.Embeds) != 0 {
		t.Errorf("expected no embeds when render_embeds=false, got %d", len(gotBody.Embeds))
	}
}

func TestNotify_EmptyItemsSendsNothing(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := New(Config{
		WebhookURL: srv.URL, RenderEmbeds: true,
		Title: defaultTitle, Description: defaultDescription,
		Color: defaultColor, Footer: "Image Watch",
	}, srv.Client())

	if err := n.Notify(context.Background(), notify.Notification{}); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if called {
		t.Errorf("expected no HTTP request for empty notification")
	}
}

func TestNotify_NoWebhookConfigured(t *testing.T) {
	n := New(Config{}, nil)
	err := n.Notify(context.Background(), notify.Notification{Items: []notify.Item{{}}})
	if err == nil {
		t.Fatal("expected error when no webhook URL configured")
	}
}

func TestNotify_RetryOnRateLimit(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("Retry-After", "0.01")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := New(Config{
		WebhookURL: srv.URL, RenderEmbeds: true,
		Title: defaultTitle, Description: defaultDescription,
		Color: defaultColor, Footer: "Image Watch",
	}, srv.Client())

	note := notify.Notification{Items: []notify.Item{
		{Image: "a", Type: event.PatchAvailable},
	}}

	if err := n.Notify(context.Background(), note); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (1 retry), got %d", callCount)
	}
}

func TestNotify_NonSuccessStatusIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	n := New(Config{
		WebhookURL: srv.URL, RenderEmbeds: true,
		Title: defaultTitle, Description: defaultDescription,
		Color: defaultColor, Footer: "Image Watch",
	}, srv.Client())

	err := n.Notify(context.Background(), notify.Notification{Items: []notify.Item{{Image: "a"}}})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected a 403 error, got %v", err)
	}
}

func TestParseColor(t *testing.T) {
	cases := []struct {
		input string
		want  int
		err   bool
	}{
		{"0x2ECC71", 0x2ECC71, false},
		{"0XFF0000", 0xFF0000, false},
		{"FF0000", 0xFF0000, false},
		{"not-a-color", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseColor(tc.input)
			if tc.err && err == nil {
				t.Errorf("parseColor(%q) expected error, got nil", tc.input)
			}
			if !tc.err && err != nil {
				t.Errorf("parseColor(%q) unexpected error: %v", tc.input, err)
			}
			if !tc.err && got != tc.want {
				t.Errorf("parseColor(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}
