package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k-wlosek/image-watch/internal/notify"
)

func writeSecretFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseDiscordConfig_MissingTokenFile(t *testing.T) {
	_, err := ParseDiscordConfig(map[string]string{"channel_id": "123"})
	if err == nil || !strings.Contains(err.Error(), "token_file is required") {
		t.Fatalf("expected token_file error, got %v", err)
	}
}

func TestParseDiscordConfig_EmptyTokenFile(t *testing.T) {
	f := writeSecretFile(t, "")
	_, err := ParseDiscordConfig(map[string]string{"token_file": f, "channel_id": "123"})
	if err == nil || !strings.Contains(err.Error(), "token_file is empty") {
		t.Fatalf("expected empty token_file error, got %v", err)
	}
}

func TestParseDiscordConfig_MissingChannelID(t *testing.T) {
	f := writeSecretFile(t, "tok123")
	_, err := ParseDiscordConfig(map[string]string{"token_file": f})
	if err == nil || !strings.Contains(err.Error(), "channel_id is required") {
		t.Fatalf("expected channel_id error, got %v", err)
	}
}

func TestParseDiscordConfig_HappyPath(t *testing.T) {
	f := writeSecretFile(t, "tok123")
	cfg, err := ParseDiscordConfig(map[string]string{"token_file": f, "channel_id": "999"})
	if err != nil {
		t.Fatalf("ParseDiscordConfig error: %v", err)
	}
	if cfg.Token != "tok123" {
		t.Errorf("Token = %q, want tok123", cfg.Token)
	}
	if len(cfg.ChannelIDs) != 1 || cfg.ChannelIDs[0] != "999" {
		t.Errorf("ChannelIDs = %v, want [999]", cfg.ChannelIDs)
	}
	if cfg.AuthMethod != "bot" {
		t.Errorf("AuthMethod = %q, want bot (default)", cfg.AuthMethod)
	}
}

func TestParseDiscordConfig_MultipleChannels(t *testing.T) {
	f := writeSecretFile(t, "tok123")
	cfg, err := ParseDiscordConfig(map[string]string{
		"token_file": f,
		"channel_id": "111, 222, 333",
	})
	if err != nil {
		t.Fatalf("ParseDiscordConfig error: %v", err)
	}
	if len(cfg.ChannelIDs) != 3 {
		t.Fatalf("expected 3 channels, got %d", len(cfg.ChannelIDs))
	}
	if cfg.ChannelIDs[0] != "111" || cfg.ChannelIDs[1] != "222" || cfg.ChannelIDs[2] != "333" {
		t.Errorf("ChannelIDs = %v, want [111 222 333]", cfg.ChannelIDs)
	}
}

func TestParseDiscordConfig_OAuth2Auth(t *testing.T) {
	f := writeSecretFile(t, "tok123")
	cfg, err := ParseDiscordConfig(map[string]string{
		"token_file":  f,
		"channel_id":  "999",
		"auth_method": "oauth2",
	})
	if err != nil {
		t.Fatalf("ParseDiscordConfig error: %v", err)
	}
	if cfg.AuthMethod != "oauth2" {
		t.Errorf("AuthMethod = %q, want oauth2", cfg.AuthMethod)
	}
}

func TestParseDiscordConfig_InvalidAuthMethod(t *testing.T) {
	f := writeSecretFile(t, "tok123")
	_, err := ParseDiscordConfig(map[string]string{
		"token_file":  f,
		"channel_id":  "999",
		"auth_method": "webhook",
	})
	if err == nil || !strings.Contains(err.Error(), "auth_method must be") {
		t.Fatalf("expected auth_method error, got %v", err)
	}
}

func TestParseSlackConfig_MissingTokenFile(t *testing.T) {
	_, err := ParseSlackConfig(map[string]string{"channel": "#general"})
	if err == nil || !strings.Contains(err.Error(), "token_file is required") {
		t.Fatalf("expected token_file error, got %v", err)
	}
}

func TestParseSlackConfig_EmptyTokenFile(t *testing.T) {
	f := writeSecretFile(t, "")
	_, err := ParseSlackConfig(map[string]string{"token_file": f, "channel": "#general"})
	if err == nil || !strings.Contains(err.Error(), "token_file is empty") {
		t.Fatalf("expected empty token_file error, got %v", err)
	}
}

func TestParseSlackConfig_MissingChannel(t *testing.T) {
	f := writeSecretFile(t, "tok123")
	_, err := ParseSlackConfig(map[string]string{"token_file": f})
	if err == nil || !strings.Contains(err.Error(), "channel is required") {
		t.Fatalf("expected channel error, got %v", err)
	}
}

func TestParseSlackConfig_HappyPath(t *testing.T) {
	f := writeSecretFile(t, "tok123")
	cfg, err := ParseSlackConfig(map[string]string{"token_file": f, "channel": "C123"})
	if err != nil {
		t.Fatalf("ParseSlackConfig error: %v", err)
	}
	if cfg.Token != "tok123" {
		t.Errorf("Token = %q, want tok123", cfg.Token)
	}
	if len(cfg.ChannelIDs) != 1 || cfg.ChannelIDs[0] != "C123" {
		t.Errorf("ChannelIDs = %v, want [C123]", cfg.ChannelIDs)
	}
}

func TestParseSlackConfig_MultipleChannels(t *testing.T) {
	f := writeSecretFile(t, "tok123")
	cfg, err := ParseSlackConfig(map[string]string{
		"token_file": f,
		"channel":    "C111, C222",
	})
	if err != nil {
		t.Fatalf("ParseSlackConfig error: %v", err)
	}
	if len(cfg.ChannelIDs) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(cfg.ChannelIDs))
	}
	if cfg.ChannelIDs[0] != "C111" || cfg.ChannelIDs[1] != "C222" {
		t.Errorf("ChannelIDs = %v, want [C111 C222]", cfg.ChannelIDs)
	}
}

func TestParseTelegramConfig_MissingTokenFile(t *testing.T) {
	_, err := ParseTelegramConfig(map[string]string{"chat_id": "123456"})
	if err == nil || !strings.Contains(err.Error(), "token_file is required") {
		t.Fatalf("expected token_file error, got %v", err)
	}
}

func TestParseTelegramConfig_EmptyTokenFile(t *testing.T) {
	f := writeSecretFile(t, "")
	_, err := ParseTelegramConfig(map[string]string{"token_file": f, "chat_id": "123456"})
	if err == nil || !strings.Contains(err.Error(), "token_file is empty") {
		t.Fatalf("expected empty token_file error, got %v", err)
	}
}

func TestParseTelegramConfig_MissingChatID(t *testing.T) {
	f := writeSecretFile(t, "tok123")
	_, err := ParseTelegramConfig(map[string]string{"token_file": f})
	if err == nil || !strings.Contains(err.Error(), "chat_id is required") {
		t.Fatalf("expected chat_id error, got %v", err)
	}
}

func TestParseTelegramConfig_InvalidChatID(t *testing.T) {
	f := writeSecretFile(t, "tok123")
	_, err := ParseTelegramConfig(map[string]string{"token_file": f, "chat_id": "notanumber"})
	if err == nil || !strings.Contains(err.Error(), "invalid chat_id") {
		t.Fatalf("expected invalid chat_id error, got %v", err)
	}
}

func TestParseTelegramConfig_HappyPath(t *testing.T) {
	f := writeSecretFile(t, "tok123")
	cfg, err := ParseTelegramConfig(map[string]string{"token_file": f, "chat_id": "987654"})
	if err != nil {
		t.Fatalf("ParseTelegramConfig error: %v", err)
	}
	if cfg.Token != "tok123" {
		t.Errorf("Token = %q, want tok123", cfg.Token)
	}
	if cfg.ChatID != 987654 {
		t.Errorf("ChatID = %d, want 987654", cfg.ChatID)
	}
}

func TestParseEmailConfig_MissingSMTPHost(t *testing.T) {
	_, err := ParseEmailConfig(map[string]string{"from": "a@b.com", "to": "c@d.com"})
	if err == nil || !strings.Contains(err.Error(), "smtp_host is required") {
		t.Fatalf("expected smtp_host error, got %v", err)
	}
}

func TestParseEmailConfig_MissingFrom(t *testing.T) {
	_, err := ParseEmailConfig(map[string]string{"smtp_host": "smtp.example.com", "to": "c@d.com"})
	if err == nil || !strings.Contains(err.Error(), "from is required") {
		t.Fatalf("expected from error, got %v", err)
	}
}

func TestParseEmailConfig_MissingTo(t *testing.T) {
	_, err := ParseEmailConfig(map[string]string{"smtp_host": "smtp.example.com", "from": "a@b.com"})
	if err == nil || !strings.Contains(err.Error(), "to is required") {
		t.Fatalf("expected to error, got %v", err)
	}
}

func TestParseEmailConfig_InvalidPort(t *testing.T) {
	_, err := ParseEmailConfig(map[string]string{
		"smtp_host": "smtp.example.com", "from": "a@b.com", "to": "c@d.com", "smtp_port": "notaport",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid smtp_port") {
		t.Fatalf("expected invalid smtp_port error, got %v", err)
	}
}

func TestParseEmailConfig_DefaultPort(t *testing.T) {
	cfg, err := ParseEmailConfig(map[string]string{
		"smtp_host": "smtp.example.com", "from": "a@b.com", "to": "c@d.com",
	})
	if err != nil {
		t.Fatalf("ParseEmailConfig error: %v", err)
	}
	if cfg.SMTPPort != 587 {
		t.Errorf("SMTPPort = %d, want 587 (default)", cfg.SMTPPort)
	}
}

func TestParseEmailConfig_HappyPath(t *testing.T) {
	userFile := writeSecretFile(t, "alice")
	passFile := writeSecretFile(t, "pass123")
	cfg, err := ParseEmailConfig(map[string]string{
		"smtp_host":     "smtp.example.com",
		"smtp_port":     "465",
		"from":          "sender@example.com",
		"to":            "a@example.com, b@example.com",
		"username_file": userFile,
		"password_file": passFile,
	})
	if err != nil {
		t.Fatalf("ParseEmailConfig error: %v", err)
	}
	if cfg.SMTPHost != "smtp.example.com" {
		t.Errorf("SMTPHost = %q", cfg.SMTPHost)
	}
	if cfg.SMTPPort != 465 {
		t.Errorf("SMTPPort = %d, want 465", cfg.SMTPPort)
	}
	if cfg.SMTPUsername != "alice" {
		t.Errorf("SMTPUsername = %q, want alice", cfg.SMTPUsername)
	}
	if cfg.SMTPPassword != "pass123" {
		t.Errorf("SMTPPassword = %q, want pass123", cfg.SMTPPassword)
	}
	if cfg.From != "sender@example.com" {
		t.Errorf("From = %q", cfg.From)
	}
	if len(cfg.To) != 2 || cfg.To[0] != "a@example.com" || cfg.To[1] != "b@example.com" {
		t.Errorf("To = %v, want [a@example.com b@example.com]", cfg.To)
	}
}

func TestParseEmailConfig_OmittedEnvFilesAreEmpty(t *testing.T) {
	cfg, err := ParseEmailConfig(map[string]string{
		"smtp_host": "smtp.example.com",
		"from":      "a@b.com",
		"to":        "c@d.com",
	})
	if err != nil {
		t.Fatalf("ParseEmailConfig error: %v", err)
	}
	if cfg.SMTPUsername != "" {
		t.Errorf("SMTPUsername should be empty when no username_file provided, got %q", cfg.SMTPUsername)
	}
	if cfg.SMTPPassword != "" {
		t.Errorf("SMTPPassword should be empty when no password_file provided, got %q", cfg.SMTPPassword)
	}
}

func TestParseAmazonSNSConfig_MissingAccessKeyFile(t *testing.T) {
	_, err := ParseAmazonSNSConfig(map[string]string{"secret_key_file": "f", "region": "eu-west-1", "topic": "arn:1"})
	if err == nil || !strings.Contains(err.Error(), "access_key_id_file is required") {
		t.Fatalf("expected access_key_id_file error, got %v", err)
	}
}

func TestParseAmazonSNSConfig_HappyPath(t *testing.T) {
	akFile := writeSecretFile(t, "ak")
	skFile := writeSecretFile(t, "sk")
	cfg, err := ParseAmazonSNSConfig(map[string]string{
		"access_key_id_file": akFile,
		"secret_key_file":    skFile,
		"region":             "eu-west-1",
		"topic":              "arn:aws:sns:r:t1, arn:aws:sns:r:t2",
	})
	if err != nil {
		t.Fatalf("ParseAmazonSNSConfig error: %v", err)
	}
	if cfg.AccessKeyID != "ak" || cfg.SecretKey != "sk" || cfg.Region != "eu-west-1" {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
	if len(cfg.TopicARNs) != 2 {
		t.Errorf("TopicARNs = %v, want 2", cfg.TopicARNs)
	}
}

func TestParseMatrixConfig_MissingFields(t *testing.T) {
	cases := map[string]string{
		"user_id":           "matrix: user_id is required",
		"room_id":           "matrix: room_id is required",
		"home_server":       "matrix: home_server is required",
		"access_token_file": "matrix: access_token_file is required",
	}
	for missing, want := range cases {
		f := writeSecretFile(t, "tok")
		base := map[string]string{
			"user_id":           "u",
			"room_id":           "r",
			"home_server":       "https://matrix.org",
			"access_token_file": f,
		}
		delete(base, missing)
		_, err := ParseMatrixConfig(base)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: expected %q, got %v", missing, want, err)
		}
	}
}

func TestParseMatrixConfig_HappyPath(t *testing.T) {
	f := writeSecretFile(t, "tok")
	cfg, err := ParseMatrixConfig(map[string]string{
		"user_id":           "@u:matrix.org",
		"room_id":           "!r:matrix.org",
		"home_server":       "https://matrix.org",
		"access_token_file": f,
	})
	if err != nil {
		t.Fatalf("ParseMatrixConfig error: %v", err)
	}
	if cfg.AccessToken != "tok" || cfg.RoomID != "!r:matrix.org" {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
}

func TestParseMattermostConfig_RequiresURLAndChannel(t *testing.T) {
	_, err := ParseMattermostConfig(map[string]string{"token_file": "f"})
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected url error, got %v", err)
	}
	f := writeSecretFile(t, "tok")
	_, err = ParseMattermostConfig(map[string]string{"url": "https://mm.example.com", "token_file": f})
	if err == nil || !strings.Contains(err.Error(), "channel is required") {
		t.Fatalf("expected channel error, got %v", err)
	}
}

func TestParseMattermostConfig_NeitherAuth(t *testing.T) {
	_, err := ParseMattermostConfig(map[string]string{"url": "https://mm.example.com", "channel": "c1"})
	if err == nil || !strings.Contains(err.Error(), "token_file or login_id_file is required") {
		t.Fatalf("expected auth error, got %v", err)
	}
}

func TestParseMattermostConfig_BothAuth(t *testing.T) {
	f := writeSecretFile(t, "tok")
	_, err := ParseMattermostConfig(map[string]string{
		"url": "https://mm.example.com", "channel": "c1",
		"token_file": f, "login_id_file": f,
	})
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("expected both-auth error, got %v", err)
	}
}

func TestParseMattermostConfig_TokenMode(t *testing.T) {
	f := writeSecretFile(t, "pat")
	cfg, err := ParseMattermostConfig(map[string]string{
		"url": "https://mm.example.com", "channel": "c1, c2", "token_file": f,
	})
	if err != nil {
		t.Fatalf("ParseMattermostConfig error: %v", err)
	}
	if cfg.Token != "pat" || len(cfg.ChannelIDs) != 2 {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
}

func TestParseMattermostConfig_LoginMode(t *testing.T) {
	loginFile := writeSecretFile(t, "alice")
	passFile := writeSecretFile(t, "pw")
	cfg, err := ParseMattermostConfig(map[string]string{
		"url": "https://mm.example.com", "channel": "c1",
		"login_id_file": loginFile, "password_file": passFile,
	})
	if err != nil {
		t.Fatalf("ParseMattermostConfig error: %v", err)
	}
	if cfg.LoginID != "alice" || cfg.Password != "pw" || cfg.Token != "" {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
}

func TestParseMSTeamsConfig_MissingWebhook(t *testing.T) {
	_, err := ParseMSTeamsConfig(map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "webhook is required") {
		t.Fatalf("expected webhook error, got %v", err)
	}
}

func TestParseMSTeamsConfig_HappyPath(t *testing.T) {
	cfg, err := ParseMSTeamsConfig(map[string]string{"webhook": "https://a, https://b"})
	if err != nil {
		t.Fatalf("ParseMSTeamsConfig error: %v", err)
	}
	if len(cfg.Webhooks) != 2 {
		t.Errorf("Webhooks = %v, want 2", cfg.Webhooks)
	}
}

func TestParsePagerDutyConfig_MissingService(t *testing.T) {
	f := writeSecretFile(t, "routing")
	_, err := ParsePagerDutyConfig(map[string]string{"token_file": f, "from_address": "ops@example.com"})
	if err == nil || !strings.Contains(err.Error(), "service is required") {
		t.Fatalf("expected service error, got %v", err)
	}
}

func TestParsePagerDutyConfig_HappyPath(t *testing.T) {
	f := writeSecretFile(t, "routing")
	cfg, err := ParsePagerDutyConfig(map[string]string{
		"token_file": f, "from_address": "ops@example.com", "service": "P123, P456",
	})
	if err != nil {
		t.Fatalf("ParsePagerDutyConfig error: %v", err)
	}
	if cfg.Token != "routing" || cfg.FromAddress != "ops@example.com" || cfg.NotificationType != "incident" {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
	if len(cfg.ServiceIDs) != 2 {
		t.Errorf("ServiceIDs = %v, want 2", cfg.ServiceIDs)
	}
}

func TestParsePushoverConfig_HappyPath(t *testing.T) {
	f := writeSecretFile(t, "app")
	cfg, err := ParsePushoverConfig(map[string]string{"app_token_file": f, "user": "u1, u2"})
	if err != nil {
		t.Fatalf("ParsePushoverConfig error: %v", err)
	}
	if cfg.AppToken != "app" || len(cfg.Recipients) != 2 {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
}

func TestParseRocketChatConfig_HappyPath(t *testing.T) {
	f := writeSecretFile(t, "tok")
	cfg, err := ParseRocketChatConfig(map[string]string{
		"server_url": "chat.example.com", "scheme": "https",
		"user_id": "u1", "token_file": f, "channel": "general, alerts",
	})
	if err != nil {
		t.Fatalf("ParseRocketChatConfig error: %v", err)
	}
	if cfg.Scheme != "https" || cfg.UserID != "u1" || cfg.Token != "tok" {
		t.Errorf("unexpected cfg: %+v", cfg)
	}
	if len(cfg.Channels) != 2 {
		t.Errorf("Channels = %v, want 2", cfg.Channels)
	}
}

func TestParseRocketChatConfig_DefaultScheme(t *testing.T) {
	f := writeSecretFile(t, "tok")
	cfg, err := ParseRocketChatConfig(map[string]string{
		"server_url": "chat.example.com", "user_id": "u1", "token_file": f, "channel": "general",
	})
	if err != nil {
		t.Fatalf("ParseRocketChatConfig error: %v", err)
	}
	if cfg.Scheme != "https" {
		t.Errorf("Scheme = %q, want https (default)", cfg.Scheme)
	}
}

// TestBuild_ConstructOnly builds adapters that perform no network I/O at
// construction time. rocketchat and mattermost (login mode) authenticate
// against their server during construction, so they are covered by
// Parse*Config tests only.
func TestBuild_ConstructOnly(t *testing.T) {
	cases := []struct {
		typ     string
		params  map[string]string
		secrets map[string]string // param key -> file content
	}{
		{
			typ: "amazonsns",
			params: map[string]string{
				"access_key_id_file": "", "secret_key_file": "",
				"region": "eu-west-1", "topic": "arn:1",
			},
			secrets: map[string]string{"access_key_id_file": "ak", "secret_key_file": "sk"},
		},
		{
			typ: "matrix",
			params: map[string]string{
				"user_id": "@u:matrix.org", "room_id": "!r:matrix.org",
				"home_server": "https://matrix.org", "access_token_file": "",
			},
			secrets: map[string]string{"access_token_file": "tok"},
		},
		{
			typ:    "msteams",
			params: map[string]string{"webhook": "https://example.com/hook"},
		},
		{
			typ: "pagerduty",
			params: map[string]string{
				"token_file": "", "from_address": "ops@example.com", "service": "P123",
			},
			secrets: map[string]string{"token_file": "routing"},
		},
		{
			typ:     "pushover",
			params:  map[string]string{"app_token_file": "", "user": "u1"},
			secrets: map[string]string{"app_token_file": "app"},
		},
		{
			typ: "mattermost",
			params: map[string]string{
				"url": "https://mm.example.com", "channel": "c1", "token_file": "",
			},
			secrets: map[string]string{"token_file": "pat"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.typ, func(t *testing.T) {
			for key, val := range tc.secrets {
				f := writeSecretFile(t, val)
				tc.params[key] = f
			}
			n, err := notify.Build(tc.typ, tc.params)
			if err != nil {
				t.Fatalf("notify.Build(%q) error: %v", tc.typ, err)
			}
			if n == nil {
				t.Fatalf("notify.Build(%q) returned nil notifier", tc.typ)
			}
		})
	}
}
