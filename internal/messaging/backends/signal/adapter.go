// Package signalmsg wraps signal.SignalCLIBackend to implement messaging.Backend.
package signalmsg

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/messaging"
	"github.com/dmz006/datawatch/internal/signal"
)

// Adapter wraps SignalCLIBackend to implement messaging.Backend.
// It automatically reconnects when signal-cli dies.
type Adapter struct {
	mu            sync.Mutex
	backend       *signal.SignalCLIBackend
	configDir     string
	accountNumber string
	groupID       string
}

// New creates a new Signal messaging adapter.
func New(configDir, accountNumber, groupID string) (*Adapter, error) {
	b, err := signal.NewSignalCLIBackend(configDir, accountNumber)
	if err != nil {
		return nil, err
	}
	return &Adapter{
		backend:       b,
		configDir:     configDir,
		accountNumber: accountNumber,
		groupID:       groupID,
	}, nil
}

func (a *Adapter) Name() string { return "signal" }

// Send sends a message to the given recipient (group ID).
func (a *Adapter) Send(recipient, message string) error {
	a.mu.Lock()
	b := a.backend
	a.mu.Unlock()
	return b.Send(recipient, message)
}

// Subscribe starts receiving messages with automatic reconnect on signal-cli failure.
// It runs until ctx is cancelled, restarting signal-cli with exponential backoff.
func (a *Adapter) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	backoff := 2 * time.Second
	const maxBackoff = 5 * time.Minute

	for {
		// Ensure we have a live backend
		a.mu.Lock()
		b := a.backend
		a.mu.Unlock()

		err := b.Subscribe(ctx, func(msg signal.IncomingMessage) {
			m := messaging.Message{
				GroupID:    msg.GroupID,
				Sender:     msg.Sender,
				SenderName: msg.SenderName,
				Text:       msg.Text,
				Backend:    "signal",
			}
			for _, att := range msg.Attachments {
				m.Attachments = append(m.Attachments, messaging.Attachment{
					ContentType: att.ContentType,
					Filename:    att.Filename,
					FilePath:    att.StoredFilename,
					Size:        att.Size,
				})
			}
			handler(m)
		})

		// Context cancelled — clean exit
		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Printf("[signal] signal-cli exited: %v — reconnecting in %s", err, backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		// Reconnect: create a fresh backend
		newB, newErr := signal.NewSignalCLIBackend(a.configDir, a.accountNumber)
		if newErr != nil {
			log.Printf("[signal] reconnect failed: %v — retrying in %s", newErr, backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		a.mu.Lock()
		old := a.backend
		a.backend = newB
		a.mu.Unlock()

		// Close the old backend in background (it may already be dead)
		go func() { _ = old.Close() }()

		backoff = min(backoff*2, maxBackoff)
	}
}


// Link is not implemented for this adapter (linking is done via the link command).
func (a *Adapter) Link(deviceName string, onQR func(string)) error { return nil }

// SelfID returns the Signal account number.
func (a *Adapter) SelfID() string { return a.backend.SelfNumber() }

// Close shuts down the underlying backend.
func (a *Adapter) Close() error { return a.backend.Close() }
