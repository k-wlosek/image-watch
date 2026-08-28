package services

import (
	"fmt"
	"os"
	"strings"

	"github.com/k-wlosek/image-watch/internal/notify"
	nlib "github.com/nikoksr/notify"
	"github.com/nikoksr/notify/service/amazonsns"
)

func init() {
	notify.Register("amazonsns", func(params map[string]string) (notify.Notifier, error) {
		cfg, err := ParseAmazonSNSConfig(params)
		if err != nil {
			return nil, err
		}
		return NewAmazonSNSFromConfig(cfg)
	})
}

// AmazonSNSConfig configures the Amazon SNS notifier.
type AmazonSNSConfig struct {
	AccessKeyID string
	SecretKey   string
	Region      string
	TopicARNs   []string
}

// ParseAmazonSNSConfig extracts Amazon SNS configuration from a params map.
func ParseAmazonSNSConfig(params map[string]string) (AmazonSNSConfig, error) {
	accessKeyEnv := params["access_key_id_env"]
	if accessKeyEnv == "" {
		return AmazonSNSConfig{}, fmt.Errorf("amazonsns: access_key_id_env is required")
	}
	accessKey := os.Getenv(accessKeyEnv)
	if accessKey == "" {
		return AmazonSNSConfig{}, fmt.Errorf("amazonsns: env var %q is empty", accessKeyEnv)
	}

	secretEnv := params["secret_key_env"]
	if secretEnv == "" {
		return AmazonSNSConfig{}, fmt.Errorf("amazonsns: secret_key_env is required")
	}
	secret := os.Getenv(secretEnv)
	if secret == "" {
		return AmazonSNSConfig{}, fmt.Errorf("amazonsns: env var %q is empty", secretEnv)
	}

	region := params["region"]
	if region == "" {
		return AmazonSNSConfig{}, fmt.Errorf("amazonsns: region is required")
	}

	topicStr := params["topic"]
	if topicStr == "" {
		return AmazonSNSConfig{}, fmt.Errorf("amazonsns: topic is required")
	}
	topics := strings.Split(topicStr, ",")
	for i := range topics {
		topics[i] = strings.TrimSpace(topics[i])
	}

	return AmazonSNSConfig{AccessKeyID: accessKey, SecretKey: secret, Region: region, TopicARNs: topics}, nil
}

// NewAmazonSNSFromConfig constructs an Amazon SNS notifier from an AmazonSNSConfig.
func NewAmazonSNSFromConfig(cfg AmazonSNSConfig) (*Notifier, error) {
	svc, err := amazonsns.New(cfg.AccessKeyID, cfg.SecretKey, cfg.Region)
	if err != nil {
		return nil, fmt.Errorf("amazonsns: %w", err)
	}
	svc.AddReceivers(cfg.TopicARNs...)
	return New(nlib.NewWithServices(svc)), nil
}
