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

// SendMarkdown sends a mrkdwn-formatted message to Slack.
func (b *Backend) SendMarkdown(recipient, markdown string) error {
	_, _, err := b.client.PostMessage(recipient, slack.MsgOptionText(markdown, false))
	return err
}

// SendWithButtons sends a message with interactive Block Kit buttons.
func (b *Backend) SendWithButtons(recipient, message string, buttons []messaging.Button, threadID string) (string, error) {
	// Build Block Kit blocks
	textBlock := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn", message, false, false),
		nil, nil,
	)
	var btnElements []slack.BlockElement
	for _, btn := range buttons {
		style := slack.StyleDefault
		if btn.Style == "primary" {
			style = slack.StylePrimary
		} else if btn.Style == "danger" {
			style = slack.StyleDanger
		}
		btnEl := slack.NewButtonBlockElement(btn.Value, btn.Value,
			slack.NewTextBlockObject("plain_text", btn.Label, false, false))
		if style != slack.StyleDefault {
			btnEl.Style = style
		}
		btnElements = append(btnElements, btnEl)
	}
	actionBlock := slack.NewActionBlock("session_actions", btnElements...)

	opts := []slack.MsgOption{
		slack.MsgOptionBlocks(textBlock, actionBlock),
	}
	if threadID != "" {
		opts = append(opts, slack.MsgOptionTS(threadID))
	}
	_, ts, err := b.client.PostMessage(recipient, opts...)
	if err != nil {
		return "", err
	}
	if threadID == "" {
		return ts, nil
	}
	return threadID, nil
}

// SendFile uploads a file to a Slack channel.
func (b *Backend) SendFile(recipient, filename, content, threadID string) error {
	params := slack.FileUploadParameters{
		Filename: filename,
		Content:  content,
		Channels: []string{recipient},
	}
	if threadID != "" {
		params.ThreadTimestamp = threadID
	}
	_, err := b.client.UploadFile(params)
	return err
}

// SendThreaded posts a message in a thread. If threadID is empty, creates a new thread.
// Returns the message timestamp (Slack's thread ID) for follow-up replies.
func (b *Backend) SendThreaded(recipient, message, threadID string) (string, error) {
	opts := []slack.MsgOption{slack.MsgOptionText(message, false)}
	if threadID != "" {
		opts = append(opts, slack.MsgOptionTS(threadID))
	}
	_, ts, err := b.client.PostMessage(recipient, opts...)
	if err != nil {
		return "", err
	}
	// If this was the first message (no thread), the returned ts IS the thread ID
	if threadID == "" {
		return ts, nil
	}
	return threadID, nil
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
