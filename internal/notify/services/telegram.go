package services

import (
	"fmt"
	"strconv"

	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/secret"
	nlib "github.com/nikoksr/notify"
	"github.com/nikoksr/notify/service/telegram"
)

func init() {
	notify.Register("telegram", func(params map[string]string) (notify.Notifier, error) {
		cfg, err := ParseTelegramConfig(params)
		if err != nil {
			return nil, err
		}
		return NewTelegramFromConfig(cfg)
	})
}

// TelegramConfig configures the Telegram notifier.
type TelegramConfig struct {
	Token  string
	ChatID int64
}

// ParseTelegramConfig extracts Telegram configuration from a params map.
func ParseTelegramConfig(params map[string]string) (TelegramConfig, error) {
	tokenFile := params["token_file"]
	if tokenFile == "" {
		return TelegramConfig{}, fmt.Errorf("telegram: token_file is required")
	}
	token, err := secret.ReadFile(tokenFile)
	if err != nil {
		return TelegramConfig{}, fmt.Errorf("telegram: token_file: %w", err)
	}
	if token == "" {
		return TelegramConfig{}, fmt.Errorf("telegram: token_file is empty")
	}
	chatIDStr := params["chat_id"]
	if chatIDStr == "" {
		return TelegramConfig{}, fmt.Errorf("telegram: chat_id is required")
	}
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return TelegramConfig{}, fmt.Errorf("telegram: invalid chat_id: %w", err)
	}
	return TelegramConfig{Token: token, ChatID: chatID}, nil
}

// NewTelegramFromConfig constructs a Telegram notifier from a TelegramConfig.
func NewTelegramFromConfig(cfg TelegramConfig) (*Notifier, error) {
	svc, err := telegram.New(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("telegram: %w", err)
	}
	svc.SetParseMode(telegram.ModeHTML)
	svc.AddReceivers(cfg.ChatID)
	return New(nlib.NewWithServices(svc)), nil
}
