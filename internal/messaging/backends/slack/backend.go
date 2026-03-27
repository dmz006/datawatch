// Package slack implements the messaging.Backend for Slack bots using the RTM API.
package slack

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
	"github.com/dmz006/datawatch/internal/messaging"
)

// Backend implements messaging.Backend using the Slack RTM API.
type Backend struct {
	token     string
	channelID string
	client    *slack.Client
	botID     string
}

// New creates a new Slack messaging backend and authenticates the bot.
func New(token, channelID string) (*Backend, error) {
	client := slack.New(token)
	info, err := client.AuthTest()
	if err != nil {
		return nil, fmt.Errorf("slack auth: %w", err)
	}
	return &Backend{
		token:     token,
		channelID: channelID,
		client:    client,
		botID:     info.UserID,
	}, nil
}

func (b *Backend) Name() string { return "slack" }

// Send posts a message to the given recipient (channel ID).
func (b *Backend) Send(recipient, message string) error {
	_, _, err := b.client.PostMessage(recipient, slack.MsgOptionText(message, false))
	return err
}

// Subscribe starts receiving Slack RTM messages and calls handler for each one.
// Blocks until ctx is cancelled.
func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	rtm := b.client.NewRTM()
	go rtm.ManageConnection()
	defer rtm.Disconnect() //nolint:errcheck
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-rtm.IncomingEvents:
			if !ok {
				return nil
			}
			msg, ok := event.Data.(*slack.MessageEvent)
			if !ok {
				continue
			}
			if msg.User == b.botID {
				continue
			}
			if b.channelID != "" && msg.Channel != b.channelID {
				continue
			}
			handler(messaging.Message{
				ID:      msg.Timestamp,
				GroupID: msg.Channel,
				Sender:  msg.User,
				Text:    msg.Text,
				Backend: "slack",
			})
		}
	}
}

// Link is not applicable for Slack (token-based).
func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }

// SelfID returns the Slack bot's user ID.
func (b *Backend) SelfID() string { return b.botID }

// Close is a no-op for Slack (RTM is disconnected in Subscribe).
func (b *Backend) Close() error { return nil }
