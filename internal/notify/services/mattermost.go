package services

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/secret"
	nlib "github.com/nikoksr/notify"
	"github.com/nikoksr/notify/service/mattermost"
)

func init() {
	notify.Register("mattermost", func(params map[string]string) (notify.Notifier, error) {
		cfg, err := ParseMattermostConfig(params)
		if err != nil {
			return nil, err
		}
		return NewMattermostFromConfig(cfg)
	})
}

// MattermostConfig configures the Mattermost notifier. Two auth modes are
// supported: login credentials (user/password) or a personal access token.
type MattermostConfig struct {
	URL        string
	ChannelIDs []string
	LoginID    string
	Password   string
	Token      string
}

// ParseMattermostConfig extracts Mattermost configuration from a params map.
func ParseMattermostConfig(params map[string]string) (MattermostConfig, error) {
	url := params["url"]
	if url == "" {
		return MattermostConfig{}, fmt.Errorf("mattermost: url is required")
	}

	channelStr := params["channel"]
	if channelStr == "" {
		return MattermostConfig{}, fmt.Errorf("mattermost: channel is required")
	}
	channels := strings.Split(channelStr, ",")
	for i := range channels {
		channels[i] = strings.TrimSpace(channels[i])
	}

	loginFile := params["login_id_file"]
	passwordFile := params["password_file"]
	tokenFile := params["token_file"]

	// Exactly one auth mode must be provided.
	switch {
	case tokenFile != "" && (loginFile != "" || passwordFile != ""):
		return MattermostConfig{}, fmt.Errorf("mattermost: provide token_file OR login_id_file/password_file, not both")
	case tokenFile == "" && loginFile == "":
		return MattermostConfig{}, fmt.Errorf("mattermost: token_file or login_id_file is required")
	case tokenFile == "" && passwordFile == "":
		return MattermostConfig{}, fmt.Errorf("mattermost: password_file is required with login_id_file")
	}

	cfg := MattermostConfig{URL: url, ChannelIDs: channels}

	if tokenFile != "" {
		token, err := secret.ReadFile(tokenFile)
		if err != nil {
			return MattermostConfig{}, fmt.Errorf("mattermost: token_file: %w", err)
		}
		if token == "" {
			return MattermostConfig{}, fmt.Errorf("mattermost: token_file is empty")
		}
		cfg.Token = token
		return cfg, nil
	}

	loginID, err := secret.ReadFile(loginFile)
	if err != nil {
		return MattermostConfig{}, fmt.Errorf("mattermost: login_id_file: %w", err)
	}
	if loginID == "" {
		return MattermostConfig{}, fmt.Errorf("mattermost: login_id_file is empty")
	}
	password, err := secret.ReadFile(passwordFile)
	if err != nil {
		return MattermostConfig{}, fmt.Errorf("mattermost: password_file: %w", err)
	}
	if password == "" {
		return MattermostConfig{}, fmt.Errorf("mattermost: password_file is empty")
	}
	cfg.LoginID = loginID
	cfg.Password = password
	return cfg, nil
}

// NewMattermostFromConfig constructs a Mattermost notifier from a MattermostConfig.
func NewMattermostFromConfig(cfg MattermostConfig) (*Notifier, error) {
	svc := mattermost.New(cfg.URL)
	svc.AddReceivers(cfg.ChannelIDs...)

	if cfg.Token != "" {
		svc.PreSend(func(req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
			return nil
		})
	} else {
		if err := svc.LoginWithCredentials(context.Background(), cfg.LoginID, cfg.Password); err != nil {
			return nil, fmt.Errorf("mattermost: %w", err)
		}
	}

	return New(nlib.NewWithServices(svc)), nil
}
