package services

import (
	"os"
	"strings"
	"testing"
)

func TestParseDiscordConfig_MissingTokenEnv(t *testing.T) {
	_, err := ParseDiscordConfig(map[string]string{"channel_id": "123"})
	if err == nil || !strings.Contains(err.Error(), "token_env is required") {
		t.Fatalf("expected token_env error, got %v", err)
	}
}

func TestParseDiscordConfig_EmptyTokenEnv(t *testing.T) {
	t.Setenv("IW_EMPTY_DISCORD", "")
	_, err := ParseDiscordConfig(map[string]string{"token_env": "IW_EMPTY_DISCORD", "channel_id": "123"})
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("expected empty env var error, got %v", err)
	}
}

func TestParseDiscordConfig_MissingChannelID(t *testing.T) {
	t.Setenv("IW_DISCORD_TOKEN", "tok123")
	_, err := ParseDiscordConfig(map[string]string{"token_env": "IW_DISCORD_TOKEN"})
	if err == nil || !strings.Contains(err.Error(), "channel_id is required") {
		t.Fatalf("expected channel_id error, got %v", err)
	}
}

func TestParseDiscordConfig_HappyPath(t *testing.T) {
	t.Setenv("IW_DISCORD_TOKEN", "tok123")
	cfg, err := ParseDiscordConfig(map[string]string{"token_env": "IW_DISCORD_TOKEN", "channel_id": "999"})
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
	t.Setenv("IW_DISCORD_TOKEN", "tok123")
	cfg, err := ParseDiscordConfig(map[string]string{
		"token_env":  "IW_DISCORD_TOKEN",
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
	t.Setenv("IW_DISCORD_TOKEN", "tok123")
	cfg, err := ParseDiscordConfig(map[string]string{
		"token_env":   "IW_DISCORD_TOKEN",
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
	t.Setenv("IW_DISCORD_TOKEN", "tok123")
	_, err := ParseDiscordConfig(map[string]string{
		"token_env":   "IW_DISCORD_TOKEN",
		"channel_id":  "999",
		"auth_method": "webhook",
	})
	if err == nil || !strings.Contains(err.Error(), "auth_method must be") {
		t.Fatalf("expected auth_method error, got %v", err)
	}
}

func TestParseSlackConfig_MissingTokenEnv(t *testing.T) {
	_, err := ParseSlackConfig(map[string]string{"channel": "#general"})
	if err == nil || !strings.Contains(err.Error(), "token_env is required") {
		t.Fatalf("expected token_env error, got %v", err)
	}
}

func TestParseSlackConfig_EmptyTokenEnv(t *testing.T) {
	t.Setenv("IW_EMPTY_SLACK", "")
	_, err := ParseSlackConfig(map[string]string{"token_env": "IW_EMPTY_SLACK", "channel": "#general"})
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("expected empty env var error, got %v", err)
	}
}

func TestParseSlackConfig_MissingChannel(t *testing.T) {
	t.Setenv("IW_SLACK_TOKEN", "tok123")
	_, err := ParseSlackConfig(map[string]string{"token_env": "IW_SLACK_TOKEN"})
	if err == nil || !strings.Contains(err.Error(), "channel is required") {
		t.Fatalf("expected channel error, got %v", err)
	}
}

func TestParseSlackConfig_HappyPath(t *testing.T) {
	t.Setenv("IW_SLACK_TOKEN", "tok123")
	cfg, err := ParseSlackConfig(map[string]string{"token_env": "IW_SLACK_TOKEN", "channel": "C123"})
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
	t.Setenv("IW_SLACK_TOKEN", "tok123")
	cfg, err := ParseSlackConfig(map[string]string{
		"token_env": "IW_SLACK_TOKEN",
		"channel":   "C111, C222",
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

func TestParseTelegramConfig_MissingTokenEnv(t *testing.T) {
	_, err := ParseTelegramConfig(map[string]string{"chat_id": "123456"})
	if err == nil || !strings.Contains(err.Error(), "token_env is required") {
		t.Fatalf("expected token_env error, got %v", err)
	}
}

func TestParseTelegramConfig_EmptyTokenEnv(t *testing.T) {
	t.Setenv("IW_EMPTY_TG", "")
	_, err := ParseTelegramConfig(map[string]string{"token_env": "IW_EMPTY_TG", "chat_id": "123456"})
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("expected empty env var error, got %v", err)
	}
}

func TestParseTelegramConfig_MissingChatID(t *testing.T) {
	t.Setenv("IW_TG_TOKEN", "tok123")
	_, err := ParseTelegramConfig(map[string]string{"token_env": "IW_TG_TOKEN"})
	if err == nil || !strings.Contains(err.Error(), "chat_id is required") {
		t.Fatalf("expected chat_id error, got %v", err)
	}
}

func TestParseTelegramConfig_InvalidChatID(t *testing.T) {
	t.Setenv("IW_TG_TOKEN", "tok123")
	_, err := ParseTelegramConfig(map[string]string{"token_env": "IW_TG_TOKEN", "chat_id": "notanumber"})
	if err == nil || !strings.Contains(err.Error(), "invalid chat_id") {
		t.Fatalf("expected invalid chat_id error, got %v", err)
	}
}

func TestParseTelegramConfig_HappyPath(t *testing.T) {
	t.Setenv("IW_TG_TOKEN", "tok123")
	cfg, err := ParseTelegramConfig(map[string]string{"token_env": "IW_TG_TOKEN", "chat_id": "987654"})
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
	t.Setenv("IW_EMAIL_USER", "alice")
	t.Setenv("IW_EMAIL_PASS", "pass123")
	cfg, err := ParseEmailConfig(map[string]string{
		"smtp_host":    "smtp.example.com",
		"smtp_port":    "465",
		"from":         "sender@example.com",
		"to":           "a@example.com, b@example.com",
		"username_env": "IW_EMAIL_USER",
		"password_env": "IW_EMAIL_PASS",
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

func TestParseEmailConfig_UnsetEnvVarsAreEmpty(t *testing.T) {
	os.Unsetenv("IW_NONEXISTENT_USER")
	os.Unsetenv("IW_NONEXISTENT_PASS")
	cfg, err := ParseEmailConfig(map[string]string{
		"smtp_host":    "smtp.example.com",
		"from":         "a@b.com",
		"to":           "c@d.com",
		"username_env": "IW_NONEXISTENT_USER",
		"password_env": "IW_NONEXISTENT_PASS",
	})
	if err != nil {
		t.Fatalf("ParseEmailConfig error: %v", err)
	}
	if cfg.SMTPUsername != "" {
		t.Errorf("SMTPUsername should be empty for unset env, got %q", cfg.SMTPUsername)
	}
}
