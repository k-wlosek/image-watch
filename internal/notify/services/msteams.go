package services

import (
	"fmt"
	"strings"

	"github.com/k-wlosek/image-watch/internal/notify"
	nlib "github.com/nikoksr/notify"
	"github.com/nikoksr/notify/service/msteams"
)

func init() {
	notify.Register("msteams", func(params map[string]string) (notify.Notifier, error) {
		cfg, err := ParseMSTeamsConfig(params)
		if err != nil {
			return nil, err
		}
		return NewMSTeamsFromConfig(cfg)
	})
}

// MSTeamsConfig configures the Microsoft Teams notifier.
type MSTeamsConfig struct {
	Webhooks []string
}

// ParseMSTeamsConfig extracts Microsoft Teams configuration from a params map.
func ParseMSTeamsConfig(params map[string]string) (MSTeamsConfig, error) {
	webhookStr := params["webhook"]
	if webhookStr == "" {
		return MSTeamsConfig{}, fmt.Errorf("msteams: webhook is required")
	}
	webhooks := strings.Split(webhookStr, ",")
	for i := range webhooks {
		webhooks[i] = strings.TrimSpace(webhooks[i])
	}
	if len(webhooks) == 0 {
		return MSTeamsConfig{}, fmt.Errorf("msteams: at least one webhook is required")
	}
	return MSTeamsConfig{Webhooks: webhooks}, nil
}

// NewMSTeamsFromConfig constructs a Microsoft Teams notifier from an MSTeamsConfig.
func NewMSTeamsFromConfig(cfg MSTeamsConfig) (*Notifier, error) {
	svc := msteams.New()
	svc.AddReceivers(cfg.Webhooks...)
	return New(nlib.NewWithServices(svc)), nil
}
