package services

import (
	"fmt"
	"os"
	"strings"

	"github.com/k-wlosek/image-watch/internal/notify"
	nlib "github.com/nikoksr/notify"
	"github.com/nikoksr/notify/service/pagerduty"
)

func init() {
	notify.Register("pagerduty", func(params map[string]string) (notify.Notifier, error) {
		cfg, err := ParsePagerDutyConfig(params)
		if err != nil {
			return nil, err
		}
		return NewPagerDutyFromConfig(cfg)
	})
}

// PagerDutyConfig configures the PagerDuty notifier.
type PagerDutyConfig struct {
	Token            string
	FromAddress      string
	NotificationType string
	ServiceIDs       []string
}

// ParsePagerDutyConfig extracts PagerDuty configuration from a params map.
func ParsePagerDutyConfig(params map[string]string) (PagerDutyConfig, error) {
	tokenEnv := params["token_env"]
	if tokenEnv == "" {
		return PagerDutyConfig{}, fmt.Errorf("pagerduty: token_env is required")
	}
	token := os.Getenv(tokenEnv)
	if token == "" {
		return PagerDutyConfig{}, fmt.Errorf("pagerduty: env var %q is empty", tokenEnv)
	}

	fromAddress := params["from_address"]
	if fromAddress == "" {
		return PagerDutyConfig{}, fmt.Errorf("pagerduty: from_address is required")
	}

	serviceStr := params["service"]
	if serviceStr == "" {
		return PagerDutyConfig{}, fmt.Errorf("pagerduty: service is required")
	}
	services := strings.Split(serviceStr, ",")
	for i := range services {
		services[i] = strings.TrimSpace(services[i])
	}

	notificationType := params["notification_type"]
	if notificationType == "" {
		notificationType = "incident"
	}

	return PagerDutyConfig{
		Token:            token,
		FromAddress:      fromAddress,
		NotificationType: notificationType,
		ServiceIDs:       services,
	}, nil
}

// NewPagerDutyFromConfig constructs a PagerDuty notifier from a PagerDutyConfig.
func NewPagerDutyFromConfig(cfg PagerDutyConfig) (*Notifier, error) {
	svc, err := pagerduty.New(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("pagerduty: %w", err)
	}
	svc.SetFromAddress(cfg.FromAddress)
	svc.SetNotificationType(cfg.NotificationType)
	svc.AddReceivers(cfg.ServiceIDs...)
	return New(nlib.NewWithServices(svc)), nil
}
