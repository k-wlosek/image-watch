package services

import (
	"fmt"
	"os"

	"github.com/k-wlosek/image-watch/internal/notify"
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
	tokenEnv := params["access_token_env"]
	if tokenEnv == "" {
		return MatrixConfig{}, fmt.Errorf("matrix: access_token_env is required")
	}
	token := os.Getenv(tokenEnv)
	if token == "" {
		return MatrixConfig{}, fmt.Errorf("matrix: env var %q is empty", tokenEnv)
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
