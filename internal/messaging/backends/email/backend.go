// Package email implements a send-only messaging.Backend for email (SMTP).
package email

import (
	"context"
	"fmt"
	"net/smtp"

	"github.com/dmz006/datawatch/internal/messaging"
)

// Backend sends email notifications via SMTP.
type Backend struct {
	host     string
	port     int
	username string
	password string
	from     string
	to       string
}

// New creates a new email backend.
func New(host string, port int, username, password, from, to string) *Backend {
	return &Backend{host: host, port: port, username: username, password: password, from: from, to: to}
}

func (b *Backend) Name() string { return "email" }

func (b *Backend) Send(recipient, message string) error {
	to := b.to
	if recipient != "" {
		to = recipient
	}
	addr := fmt.Sprintf("%s:%d", b.host, b.port)
	auth := smtp.PlainAuth("", b.username, b.password, b.host)
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: datawatch notification\r\n\r\n%s", b.from, to, message)
	return smtp.SendMail(addr, auth, b.from, []string{to}, []byte(body))
}

func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	<-ctx.Done()
	return nil
}
func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }
func (b *Backend) SelfID() string                                   { return b.from }
func (b *Backend) Close() error                                     { return nil }
