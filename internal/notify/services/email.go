package services

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/secret"
	nlib "github.com/nikoksr/notify"
	"github.com/nikoksr/notify/service/mail"
)

func init() {
	notify.Register("email", func(params map[string]string) (notify.Notifier, error) {
		cfg, err := ParseEmailConfig(params)
		if err != nil {
			return nil, err
		}
		return NewEmailFromConfig(cfg)
	})
}

// EmailConfig configures the email notifier.
type EmailConfig struct {
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	From         string
	To           []string
}

// ParseEmailConfig extracts email configuration from a params map.
func ParseEmailConfig(params map[string]string) (EmailConfig, error) {
	host := params["smtp_host"]
	if host == "" {
		return EmailConfig{}, fmt.Errorf("email: smtp_host is required")
	}
	portStr := params["smtp_port"]
	if portStr == "" {
		portStr = "587"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return EmailConfig{}, fmt.Errorf("email: invalid smtp_port: %w", err)
	}
	from := params["from"]
	if from == "" {
		return EmailConfig{}, fmt.Errorf("email: from is required")
	}
	toStr := params["to"]
	if toStr == "" {
		return EmailConfig{}, fmt.Errorf("email: to is required")
	}
	to := strings.Split(toStr, ",")
	for i := range to {
		to[i] = strings.TrimSpace(to[i])
	}

	var username, password string
	if p := params["username_file"]; p != "" {
		username, err = secret.ReadFile(p)
		if err != nil {
			return EmailConfig{}, fmt.Errorf("email: username_file: %w", err)
		}
	}
	if p := params["password_file"]; p != "" {
		password, err = secret.ReadFile(p)
		if err != nil {
			return EmailConfig{}, fmt.Errorf("email: password_file: %w", err)
		}
	}

	return EmailConfig{
		SMTPHost:     host,
		SMTPPort:     port,
		SMTPUsername: username,
		SMTPPassword: password,
		From:         from,
		To:           to,
	}, nil
}

// NewEmailFromConfig constructs an email notifier from an EmailConfig.
func NewEmailFromConfig(cfg EmailConfig) (*Notifier, error) {
	svc := mail.New(cfg.From, fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort))
	svc.BodyFormat(mail.PlainText)
	if cfg.SMTPUsername != "" {
		svc.AuthenticateSMTP("", cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPHost)
	}
	svc.AddReceivers(cfg.To...)
	return New(nlib.NewWithServices(svc)), nil
}
