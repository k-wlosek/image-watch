package services

import (
	"fmt"

	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/secret"
	nlib "github.com/nikoksr/notify"
	"github.com/nikoksr/notify/service/matrix"
	"maunium.net/go/mautrix/id"
)

func init() {
	notify.Register("matrix", func(params map[string]string) (notify.Notifier, error) {
		cfg, err := ParseMatrixConfig(params)
		if err != nil {
			return nil, err
		}
		return NewMatrixFromConfig(cfg)
	})
}

// MatrixConfig configures the Matrix notifier. The room is fixed at construction,
// so a single target delivers to exactly one room.
type MatrixConfig struct {
	UserID      string
	RoomID      string
	HomeServer  string
	AccessToken string
}

// ParseMatrixConfig extracts Matrix configuration from a params map.
func ParseMatrixConfig(params map[string]string) (MatrixConfig, error) {
	userID := params["user_id"]
	if userID == "" {
		return MatrixConfig{}, fmt.Errorf("matrix: user_id is required")
	}
	roomID := params["room_id"]
	if roomID == "" {
		return MatrixConfig{}, fmt.Errorf("matrix: room_id is required")
	}
	homeServer := params["home_server"]
	if homeServer == "" {
		return MatrixConfig{}, fmt.Errorf("matrix: home_server is required")
	}
	tokenFile := params["access_token_file"]
	if tokenFile == "" {
		return MatrixConfig{}, fmt.Errorf("matrix: access_token_file is required")
	}
	token, err := secret.ReadFile(tokenFile)
	if err != nil {
		return MatrixConfig{}, fmt.Errorf("matrix: access_token_file: %w", err)
	}
	if token == "" {
		return MatrixConfig{}, fmt.Errorf("matrix: access_token_file is empty")
	}
	return MatrixConfig{UserID: userID, RoomID: roomID, HomeServer: homeServer, AccessToken: token}, nil
}

// NewMatrixFromConfig constructs a Matrix notifier from a MatrixConfig.
func NewMatrixFromConfig(cfg MatrixConfig) (*Notifier, error) {
	svc, err := matrix.New(id.UserID(cfg.UserID), id.RoomID(cfg.RoomID), cfg.HomeServer, cfg.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("matrix: %w", err)
	}
	return New(nlib.NewWithServices(svc)), nil
}
