// Package ntfy implements a send-only messaging.Backend for ntfy.sh push notifications.
// Use this to send session state notifications to your phone via ntfy.
package ntfy

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/dmz006/datawatch/internal/messaging"
)

// Backend sends push notifications via ntfy.sh or a self-hosted ntfy instance.
type Backend struct {
	serverURL string
	topic     string
	token     string
	client    *http.Client
}

// New creates a new ntfy backend.
func New(serverURL, topic, token string) *Backend {
	if serverURL == "" {
		serverURL = "https://ntfy.sh"
	}
	// A redirect is not followed: a sign-in page that answers 200 would read
	// as a message that was sent.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Backend{serverURL: serverURL, topic: topic, token: token, client: client}
}

func (b *Backend) Name() string { return "ntfy" }

func (b *Backend) Send(recipient, message string) error {
	topic := b.topic
	if recipient != "" {
		topic = recipient
	}
	url := fmt.Sprintf("%s/%s", b.serverURL, topic)
	req, err := http.NewRequest("POST", url, bytes.NewBufferString(message))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	if b.token != "" {
		req.Header.Set("Authorization", "Bearer "+b.token)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Subscribe is a no-op for ntfy (send-only).
func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	<-ctx.Done()
	return nil
}
func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }
func (b *Backend) SelfID() string                                  { return "ntfy:" + b.topic }
func (b *Backend) Close() error                                    { return nil }
