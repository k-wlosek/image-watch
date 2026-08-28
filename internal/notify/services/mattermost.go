package services

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/k-wlosek/image-watch/internal/notify"
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

	loginEnv := params["login_id_env"]
	passwordEnv := params["password_env"]
	tokenEnv := params["token_env"]

	// Exactly one auth mode must be provided.
	switch {
	case tokenEnv != "" && (loginEnv != "" || passwordEnv != ""):
		return MattermostConfig{}, fmt.Errorf("mattermost: provide token_env OR login_id_env/password_env, not both")
	case tokenEnv == "" && loginEnv == "":
		return MattermostConfig{}, fmt.Errorf("mattermost: token_env or login_id_env is required")
	case tokenEnv == "" && passwordEnv == "":
		return MattermostConfig{}, fmt.Errorf("mattermost: password_env is required with login_id_env")
	}

	cfg := MattermostConfig{URL: url, ChannelIDs: channels}

	if tokenEnv != "" {
		token := os.Getenv(tokenEnv)
		if token == "" {
			return MattermostConfig{}, fmt.Errorf("mattermost: env var %q is empty", tokenEnv)
		}
		cfg.Token = token
		return cfg, nil
	}

	loginID := os.Getenv(loginEnv)
	if loginID == "" {
		return MattermostConfig{}, fmt.Errorf("mattermost: env var %q is empty", loginEnv)
	}
	password := os.Getenv(passwordEnv)
	if password == "" {
		return MattermostConfig{}, fmt.Errorf("mattermost: env var %q is empty", passwordEnv)
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
