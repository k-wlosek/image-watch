// Package services adapts nikoksr/notify services to the notify.Notifier interface.
package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/k-wlosek/image-watch/internal/notify"
	"github.com/k-wlosek/image-watch/internal/notify/stdout"
	nlib "github.com/nikoksr/notify"
)

// Notifier wraps a nikoksr/notify instance as a notify.Notifier.
type Notifier struct {
	notify *nlib.Notify
}

// New constructs a Notifier from a nikoksr/notify instance.
func New(n *nlib.Notify) *Notifier {
	return &Notifier{notify: n}
}

var _ notify.Notifier = (*Notifier)(nil)

// Notify formats the notification and sends it through all configured services.
func (n *Notifier) Notify(ctx context.Context, note notify.Notification) error {
	if len(note.Items) == 0 {
		return nil
	}

	subject := formatSubject(note)
	body := formatBody(note)

	return n.notify.Send(ctx, subject, body)
}

func formatSubject(note notify.Notification) string {
	count := len(note.Items)
	update := "update"
	if count != 1 {
		update = "updates"
	}
	if note.Hostname != "" {
		return fmt.Sprintf("Image Watch (%s) - %d %s", note.Hostname, count, update)
	}
	return fmt.Sprintf("Image Watch - %d %s", count, update)
}

func formatBody(note notify.Notification) string {
	var b strings.Builder
	sn := &stdout.Notifier{Writer: &b}
	_ = sn.Notify(context.Background(), note)
	return b.String()
}
