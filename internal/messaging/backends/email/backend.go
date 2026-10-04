// Package email implements a send-only messaging.Backend for email (SMTP).
//
// BL394 security fix (docs/plans/2026-10-03-bl394-security-findings-review.md
// §3f, alert #627): Send hand-built the raw RFC822 header block via
// fmt.Sprintf with no validation on the "To"/"From" values -- a literal
// \r\n in either would inject arbitrary extra SMTP headers (e.g. a Bcc:)
// into the outgoing message, a textbook CWE-93 email-header-injection.
// Fixed by rejecting any header value containing \r or \n before building
// the message, rather than silently stripping (a caller passing a
// corrupted address is a bug worth surfacing, not papering over). The
// message BODY is deliberately NOT touched by this check -- its newlines
// are legitimate multi-line content, and Go's net/smtp.Client.Data()
// already performs RFC 5321 dot-stuffing correctly on the body, so this
// was never a body-content risk, only a header-construction one.
package email

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/dmz006/datawatch/internal/messaging"
)

// hasCRLF reports whether s contains a carriage return or line feed --
// disqualifying it from use as a raw RFC822 header value.
func hasCRLF(s string) bool {
	return strings.ContainsAny(s, "\r\n")
}

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
	// BL394 -- reject a CRLF-bearing header value rather than build a
	// message with it. Subject is a fixed literal, not attacker-
	// influenced, so it needs no check; message is the body, see the
	// package comment for why that's a different, already-safe case.
	if hasCRLF(to) {
		return fmt.Errorf("email: recipient address contains a newline, refusing to send")
	}
	if hasCRLF(b.from) {
		return fmt.Errorf("email: from address contains a newline, refusing to send")
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
func (b *Backend) SelfID() string                                  { return b.from }
func (b *Backend) Close() error                                    { return nil }
