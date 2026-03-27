// Package signalmsg wraps signal.SignalCLIBackend to implement messaging.Backend.
package signalmsg

import (
	"context"

	"github.com/dmz006/datawatch/internal/messaging"
	"github.com/dmz006/datawatch/internal/signal"
)

// Adapter wraps SignalCLIBackend to implement messaging.Backend.
type Adapter struct {
	backend *signal.SignalCLIBackend
	groupID string
}

// New creates a new Signal messaging adapter.
func New(configDir, accountNumber, groupID string) (*Adapter, error) {
	b, err := signal.NewSignalCLIBackend(configDir, accountNumber)
	if err != nil {
		return nil, err
	}
	return &Adapter{backend: b, groupID: groupID}, nil
}

func (a *Adapter) Name() string { return "signal" }

// Send sends a message to the given recipient (group ID).
func (a *Adapter) Send(recipient, message string) error {
	return a.backend.Send(recipient, message)
}

// Subscribe starts receiving messages, translating them to messaging.Message.
func (a *Adapter) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	return a.backend.Subscribe(ctx, func(msg signal.IncomingMessage) {
		handler(messaging.Message{
			GroupID:    msg.GroupID,
			Sender:     msg.Sender,
			SenderName: msg.SenderName,
			Text:       msg.Text,
			Backend:    "signal",
		})
	})
}

// Link is not implemented for this adapter (linking is done via the link command).
func (a *Adapter) Link(deviceName string, onQR func(string)) error { return nil }

// SelfID returns the Signal account number.
func (a *Adapter) SelfID() string { return a.backend.SelfNumber() }

// Close shuts down the underlying backend.
func (a *Adapter) Close() error { return a.backend.Close() }
