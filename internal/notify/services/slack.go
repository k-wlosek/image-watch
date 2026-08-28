package services

import (
	"fmt"
	"os"
	"strings"

	"github.com/k-wlosek/image-watch/internal/notify"
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
	tokenEnv := params["token_env"]
	if tokenEnv == "" {
		return SlackConfig{}, fmt.Errorf("slack: token_env is required")
	}
	token := os.Getenv(tokenEnv)
	if token == "" {
		return SlackConfig{}, fmt.Errorf("slack: env var %q is empty", tokenEnv)
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
