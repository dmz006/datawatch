// Package matrix implements the messaging.Backend for Matrix (Element) rooms.
package matrix

import (
	"context"
	"fmt"

	"github.com/dmz006/datawatch/internal/messaging"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Backend implements messaging.Backend for Matrix.
type Backend struct {
	client    *mautrix.Client
	roomID    id.RoomID
	botUserID id.UserID
}

// New creates a new Matrix backend.
func New(homeserver, userID, accessToken, roomID string) (*Backend, error) {
	client, err := mautrix.NewClient(homeserver, id.UserID(userID), accessToken)
	if err != nil {
		return nil, fmt.Errorf("matrix client: %w", err)
	}
	return &Backend{
		client:    client,
		roomID:    id.RoomID(roomID),
		botUserID: id.UserID(userID),
	}, nil
}

func (b *Backend) Name() string { return "matrix" }

func (b *Backend) Send(recipient, message string) error {
	roomID := b.roomID
	if recipient != "" {
		roomID = id.RoomID(recipient)
	}
	_, err := b.client.SendText(context.Background(), roomID, message)
	return err
}

func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	syncer := b.client.Syncer.(*mautrix.DefaultSyncer)
	syncer.OnEventType(event.EventMessage, func(ctx context.Context, ev *event.Event) {
		if ev.Sender == b.botUserID {
			return
		}
		if b.roomID != "" && ev.RoomID != b.roomID {
			return
		}
		content, ok := ev.Content.Parsed.(*event.MessageEventContent)
		if !ok {
			return
		}
		handler(messaging.Message{
			ID:      ev.ID.String(),
			GroupID: ev.RoomID.String(),
			Sender:  ev.Sender.String(),
			Text:    content.Body,
			Backend: "matrix",
		})
	})
	return b.client.SyncWithContext(ctx)
}

func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }
func (b *Backend) SelfID() string                                   { return b.botUserID.String() }
func (b *Backend) Close() error {
	b.client.StopSync()
	return nil
}
