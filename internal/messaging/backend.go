// Package messaging defines the MessagingBackend interface for pluggable protocols.
// Current implementation: Signal via signal-cli.
// Future: Slack, Discord, Telegram, Matrix, SMS.
package messaging

import "context"

// Attachment represents a file attached to a message (voice note, audio, etc.).
type Attachment struct {
	ContentType string // MIME type (e.g. "audio/ogg", "audio/mpeg")
	Filename    string // original filename (may be empty)
	FilePath    string // local path to downloaded file (set by backend)
	Size        int64  // file size in bytes
}

// IsAudio returns true if the attachment has an audio MIME type.
func (a Attachment) IsAudio() bool {
	return len(a.ContentType) >= 5 && a.ContentType[:5] == "audio"
}

// Message represents an inbound message from any messaging backend.
type Message struct {
	ID          string      // opaque message ID (may be empty)
	GroupID     string      // channel/group this came from
	GroupName   string      // human-readable group name (optional)
	Sender      string      // sender identifier (phone, user ID)
	SenderName  string      // display name (optional)
	Text        string      // message body
	Backend     string      // which backend produced this
	Attachments []Attachment // optional file attachments (voice, audio, etc.)
}

// ThreadedSender is an optional interface for backends that support message threading.
// When implemented, the router uses SendThreaded instead of Send for session alerts,
// keeping per-session conversation in threads.
type ThreadedSender interface {
	// SendThreaded sends a message to a recipient, optionally in a thread.
	// threadID is the parent message/thread ID. If empty, starts a new thread.
	// Returns the message/thread ID for follow-up replies.
	SendThreaded(recipient, message, threadID string) (string, error)
}

// RichSender is an optional interface for backends that support formatted messages.
// When implemented, the router sends markdown-formatted messages instead of plain text.
type RichSender interface {
	// SendMarkdown sends a markdown-formatted message. The backend converts to its
	// native format (Slack mrkdwn, Discord markdown, Telegram MarkdownV2, etc.).
	SendMarkdown(recipient, markdown string) error
}

// ButtonSender is an optional interface for backends that support interactive buttons.
// When the session is waiting for input, buttons are attached to the alert message.
type ButtonSender interface {
	// SendWithButtons sends a message with action buttons. Each button has a label
	// and a value that gets sent back when clicked.
	SendWithButtons(recipient, message string, buttons []Button, threadID string) (string, error)
}

// Button represents an interactive action button in a message.
type Button struct {
	Label string // display text
	Value string // value sent back when clicked
	Style string // "primary", "danger", or "" for default
}

// FileSender is an optional interface for backends that support file uploads.
type FileSender interface {
	// SendFile uploads a file to the recipient channel, optionally in a thread.
	SendFile(recipient, filename, content, threadID string) error
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
