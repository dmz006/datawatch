package signal

import (
	"context"

	"github.com/dmz006/datawatch/internal/messaging"
)

// MessagingAdapter wraps a SignalBackend to implement the messaging.Backend interface.
type MessagingAdapter struct {
	backend SignalBackend
}

// NewMessagingAdapter wraps a SignalBackend as a messaging.Backend.
func NewMessagingAdapter(b SignalBackend) messaging.Backend {
	return &MessagingAdapter{backend: b}
}

func (a *MessagingAdapter) Name() string { return "signal" }

func (a *MessagingAdapter) Send(recipient, message string) error {
	return a.backend.Send(recipient, message)
}

func (a *MessagingAdapter) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	return a.backend.Subscribe(ctx, func(msg IncomingMessage) {
		handler(messaging.Message{
			GroupID:    msg.GroupID,
			Sender:     msg.Sender,
			SenderName: msg.SenderName,
			Text:       msg.Text,
			Backend:    "signal",
		})
	})
}

func (a *MessagingAdapter) Link(deviceName string, onQR func(string)) error {
	return a.backend.Link(deviceName, onQR)
}

func (a *MessagingAdapter) SelfID() string {
	return a.backend.SelfNumber()
}

func (a *MessagingAdapter) Close() error {
	return a.backend.Close()
}
