package services

import (
	"fmt"
	"strings"

	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/secret"
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
	accessKeyFile := params["access_key_id_file"]
	if accessKeyFile == "" {
		return AmazonSNSConfig{}, fmt.Errorf("amazonsns: access_key_id_file is required")
	}
	accessKey, err := secret.ReadFile(accessKeyFile)
	if err != nil {
		return AmazonSNSConfig{}, fmt.Errorf("amazonsns: access_key_id_file: %w", err)
	}
	if accessKey == "" {
		return AmazonSNSConfig{}, fmt.Errorf("amazonsns: access_key_id_file is empty")
	}

	secretFile := params["secret_key_file"]
	if secretFile == "" {
		return AmazonSNSConfig{}, fmt.Errorf("amazonsns: secret_key_file is required")
	}
	secretKey, err := secret.ReadFile(secretFile)
	if err != nil {
		return AmazonSNSConfig{}, fmt.Errorf("amazonsns: secret_key_file: %w", err)
	}
	if secretKey == "" {
		return AmazonSNSConfig{}, fmt.Errorf("amazonsns: secret_key_file is empty")
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

	return AmazonSNSConfig{AccessKeyID: accessKey, SecretKey: secretKey, Region: region, TopicARNs: topics}, nil
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
