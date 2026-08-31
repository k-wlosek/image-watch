package services

import (
	"fmt"
	"strings"

	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/secret"
	nlib "github.com/nikoksr/notify"
	"github.com/nikoksr/notify/service/pushover"
)

func init() {
	notify.Register("pushover", func(params map[string]string) (notify.Notifier, error) {
		cfg, err := ParsePushoverConfig(params)
		if err != nil {
			return nil, err
		}
		return NewPushoverFromConfig(cfg)
	})
}

// PushoverConfig configures the Pushover notifier.
type PushoverConfig struct {
	AppToken   string
	Recipients []string
}

// ParsePushoverConfig extracts Pushover configuration from a params map.
func ParsePushoverConfig(params map[string]string) (PushoverConfig, error) {
	appTokenFile := params["app_token_file"]
	if appTokenFile == "" {
		return PushoverConfig{}, fmt.Errorf("pushover: app_token_file is required")
	}
	appToken, err := secret.ReadFile(appTokenFile)
	if err != nil {
		return PushoverConfig{}, fmt.Errorf("pushover: app_token_file: %w", err)
	}
	if appToken == "" {
		return PushoverConfig{}, fmt.Errorf("pushover: app_token_file is empty")
	}

	userStr := params["user"]
	if userStr == "" {
		return PushoverConfig{}, fmt.Errorf("pushover: user is required")
	}
	users := strings.Split(userStr, ",")
	for i := range users {
		users[i] = strings.TrimSpace(users[i])
	}

	return PushoverConfig{AppToken: appToken, Recipients: users}, nil
}

// NewPushoverFromConfig constructs a Pushover notifier from a PushoverConfig.
func NewPushoverFromConfig(cfg PushoverConfig) (*Notifier, error) {
	svc := pushover.New(cfg.AppToken)
	svc.AddReceivers(cfg.Recipients...)
	return New(nlib.NewWithServices(svc)), nil
}
