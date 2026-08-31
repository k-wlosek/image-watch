package services

import (
	"fmt"
	"strings"

	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/secret"
	nlib "github.com/nikoksr/notify"
	"github.com/nikoksr/notify/service/slack"
)

func init() {
	notify.Register("slack", func(params map[string]string) (notify.Notifier, error) {
		cfg, err := ParseSlackConfig(params)
		if err != nil {
			return nil, err
		}
		return NewSlackFromConfig(cfg)
	})
}

// SlackConfig configures the Slack notifier.
type SlackConfig struct {
	Token      string
	ChannelIDs []string
}

// ParseSlackConfig extracts Slack configuration from a params map.
func ParseSlackConfig(params map[string]string) (SlackConfig, error) {
	tokenFile := params["token_file"]
	if tokenFile == "" {
		return SlackConfig{}, fmt.Errorf("slack: token_file is required")
	}
	token, err := secret.ReadFile(tokenFile)
	if err != nil {
		return SlackConfig{}, fmt.Errorf("slack: token_file: %w", err)
	}
	if token == "" {
		return SlackConfig{}, fmt.Errorf("slack: token_file is empty")
	}
	channelStr := params["channel"]
	if channelStr == "" {
		return SlackConfig{}, fmt.Errorf("slack: channel is required")
	}
	channels := strings.Split(channelStr, ",")
	for i := range channels {
		channels[i] = strings.TrimSpace(channels[i])
	}
	return SlackConfig{Token: token, ChannelIDs: channels}, nil
}

// NewSlackFromConfig constructs a Slack notifier from a SlackConfig.
func NewSlackFromConfig(cfg SlackConfig) (*Notifier, error) {
	svc := slack.New(cfg.Token)
	svc.AddReceivers(cfg.ChannelIDs...)
	return New(nlib.NewWithServices(svc)), nil
}
