// Package messaging defines the MessagingBackend interface for pluggable protocols.
// Current implementation: Signal via signal-cli.
// Future: Slack, Discord, Telegram, Matrix, SMS.
package messaging

import "context"

// Message represents an inbound message from any messaging backend.
type Message struct {
	ID         string // opaque message ID (may be empty)
	GroupID    string // channel/group this came from
	GroupName  string // human-readable group name (optional)
	Sender     string // sender identifier (phone, user ID)
	SenderName string // display name (optional)
	Text       string // message body
	Backend    string // which backend produced this
}

// Backend is the interface all messaging backends must implement.
type Backend interface {
	// Name returns the backend identifier (e.g. "signal", "slack").
	Name() string

	// Send sends text to the given recipient (group ID, channel ID, etc.).
	Send(recipient, message string) error

	// Subscribe starts receiving messages. Calls handler for each one.
	// Blocks until ctx is cancelled.
	Subscribe(ctx context.Context, handler func(Message)) error

	// Link initiates device/bot linking. onQR is called with a QR URI if needed.
	// May be a no-op for token-based backends.
	Link(deviceName string, onQR func(qrURI string)) error

	// SelfID returns the backend's own identifier (e.g. phone number, bot ID).
	SelfID() string

	// Close cleans up resources.
	Close() error
}
