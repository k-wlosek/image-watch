package services

import (
	"fmt"
	"strings"

	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/secret"
	nlib "github.com/nikoksr/notify"
	"github.com/nikoksr/notify/service/discord"
)

func init() {
	notify.Register("discord", func(params map[string]string) (notify.Notifier, error) {
		cfg, err := ParseDiscordConfig(params)
		if err != nil {
			return nil, err
		}
		return NewDiscordFromConfig(cfg)
	})
}

// DiscordConfig configures the Discord notifier.
type DiscordConfig struct {
	Token      string
	AuthMethod string // "bot" (default) or "oauth2"
	ChannelIDs []string
}

// ParseDiscordConfig extracts Discord configuration from a params map.
func ParseDiscordConfig(params map[string]string) (DiscordConfig, error) {
	tokenFile := params["token_file"]
	if tokenFile == "" {
		return DiscordConfig{}, fmt.Errorf("discord: token_file is required")
	}
	token, err := secret.ReadFile(tokenFile)
	if err != nil {
		return DiscordConfig{}, fmt.Errorf("discord: token_file: %w", err)
	}
	if token == "" {
		return DiscordConfig{}, fmt.Errorf("discord: token_file is empty")
	}
	channelIDStr := params["channel_id"]
	if channelIDStr == "" {
		return DiscordConfig{}, fmt.Errorf("discord: channel_id is required")
	}
	channelIDs := strings.Split(channelIDStr, ",")
	for i := range channelIDs {
		channelIDs[i] = strings.TrimSpace(channelIDs[i])
	}
	authMethod := params["auth_method"]
	if authMethod == "" {
		authMethod = "bot"
	}
	if authMethod != "bot" && authMethod != "oauth2" {
		return DiscordConfig{}, fmt.Errorf("discord: auth_method must be \"bot\" or \"oauth2\", got %q", authMethod)
	}
	return DiscordConfig{Token: token, AuthMethod: authMethod, ChannelIDs: channelIDs}, nil
}

// NewDiscordFromConfig constructs a Discord notifier from a DiscordConfig.
func NewDiscordFromConfig(cfg DiscordConfig) (*Notifier, error) {
	svc := discord.New()
	switch cfg.AuthMethod {
	case "oauth2":
		if err := svc.AuthenticateWithOAuth2Token(cfg.Token); err != nil {
			return nil, fmt.Errorf("discord: %w", err)
		}
	default:
		if err := svc.AuthenticateWithBotToken(cfg.Token); err != nil {
			return nil, fmt.Errorf("discord: %w", err)
		}
	}
	svc.AddReceivers(cfg.ChannelIDs...)
	return New(nlib.NewWithServices(svc)), nil
}
