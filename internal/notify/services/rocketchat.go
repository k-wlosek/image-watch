package services

import (
	"fmt"
	"os"
	"strings"

	"github.com/k-wlosek/image-watch/internal/notify"
	nlib "github.com/nikoksr/notify"
	"github.com/nikoksr/notify/service/rocketchat"
)

func init() {
	notify.Register("rocketchat", func(params map[string]string) (notify.Notifier, error) {
		cfg, err := ParseRocketChatConfig(params)
		if err != nil {
			return nil, err
		}
		return NewRocketChatFromConfig(cfg)
	})
}

// RocketChatConfig configures the Rocket.Chat notifier.
type RocketChatConfig struct {
	ServerURL string
	Scheme    string
	UserID    string
	Token     string
	Channels  []string
}

// ParseRocketChatConfig extracts Rocket.Chat configuration from a params map.
func ParseRocketChatConfig(params map[string]string) (RocketChatConfig, error) {
	serverURL := params["server_url"]
	if serverURL == "" {
		return RocketChatConfig{}, fmt.Errorf("rocketchat: server_url is required")
	}

	scheme := params["scheme"]
	if scheme == "" {
		scheme = "https"
	}

	userID := params["user_id"]
	if userID == "" {
		return RocketChatConfig{}, fmt.Errorf("rocketchat: user_id is required")
	}

	tokenEnv := params["token_env"]
	if tokenEnv == "" {
		return RocketChatConfig{}, fmt.Errorf("rocketchat: token_env is required")
	}
	token := os.Getenv(tokenEnv)
	if token == "" {
		return RocketChatConfig{}, fmt.Errorf("rocketchat: env var %q is empty", tokenEnv)
	}

	channelStr := params["channel"]
	if channelStr == "" {
		return RocketChatConfig{}, fmt.Errorf("rocketchat: channel is required")
	}
	channels := strings.Split(channelStr, ",")
	for i := range channels {
		channels[i] = strings.TrimSpace(channels[i])
	}

	return RocketChatConfig{
		ServerURL: serverURL,
		Scheme:    scheme,
		UserID:    userID,
		Token:     token,
		Channels:  channels,
	}, nil
}

// NewRocketChatFromConfig constructs a Rocket.Chat notifier from a RocketChatConfig.
func NewRocketChatFromConfig(cfg RocketChatConfig) (*Notifier, error) {
	svc, err := rocketchat.New(cfg.ServerURL, cfg.Scheme, cfg.UserID, cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("rocketchat: %w", err)
	}
	svc.AddReceivers(cfg.Channels...)
	return New(nlib.NewWithServices(svc)), nil
}
