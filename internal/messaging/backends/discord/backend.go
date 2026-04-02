// Package discord implements the messaging.Backend for Discord bots.
// Requires a bot token and a channel ID to listen on.
package discord

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/dmz006/datawatch/internal/messaging"
)

// Backend implements messaging.Backend using a Discord bot.
type Backend struct {
	token     string
	channelID string
	session   *discordgo.Session
}

// New creates a new Discord messaging backend.
func New(token, channelID string) (*Backend, error) {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("discord session: %w", err)
	}
	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages
	return &Backend{token: token, channelID: channelID, session: dg}, nil
}

func (b *Backend) Name() string { return "discord" }

// Send sends a message to the given recipient (channel ID).
func (b *Backend) Send(recipient, message string) error {
	_, err := b.session.ChannelMessageSend(recipient, message)
	return err
}

// SendMarkdown sends a markdown-formatted message to Discord (Discord natively supports markdown).
func (b *Backend) SendMarkdown(recipient, markdown string) error {
	_, err := b.session.ChannelMessageSend(recipient, markdown)
	return err
}

// SendWithButtons sends a message with Discord component buttons.
func (b *Backend) SendWithButtons(recipient, message string, buttons []messaging.Button, threadID string) (string, error) {
	targetCh := recipient
	if threadID != "" {
		targetCh = threadID
	}
	var components []discordgo.MessageComponent
	var btns []discordgo.MessageComponent
	for _, btn := range buttons {
		style := discordgo.SecondaryButton
		if btn.Style == "primary" {
			style = discordgo.PrimaryButton
		} else if btn.Style == "danger" {
			style = discordgo.DangerButton
		}
		btns = append(btns, discordgo.Button{
			Label:    btn.Label,
			Style:    style,
			CustomID: btn.Value,
		})
	}
	if len(btns) > 0 {
		components = []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: btns},
		}
	}
	msg, err := b.session.ChannelMessageSendComplex(targetCh, &discordgo.MessageSend{
		Content:    message,
		Components: components,
	})
	if err != nil {
		return "", err
	}
	if threadID == "" {
		return msg.ID, nil
	}
	return threadID, nil
}

// SendFile uploads a file to a Discord channel.
func (b *Backend) SendFile(recipient, filename, content, threadID string) error {
	targetCh := recipient
	if threadID != "" {
		targetCh = threadID
	}
	_, err := b.session.ChannelMessageSendComplex(targetCh, &discordgo.MessageSend{
		Files: []*discordgo.File{{
			Name:   filename,
			Reader: strings.NewReader(content),
		}},
	})
	return err
}

// SendThreaded sends a message in a Discord thread. If threadID is empty, creates a new
// thread from the first message. Returns the thread/channel ID for follow-up replies.
func (b *Backend) SendThreaded(recipient, message, threadID string) (string, error) {
	if threadID != "" {
		// Send to existing thread
		_, err := b.session.ChannelMessageSend(threadID, message)
		return threadID, err
	}
	// No thread yet — send a message and create a thread from it
	msg, err := b.session.ChannelMessageSend(recipient, message)
	if err != nil {
		return "", err
	}
	// Create a thread on the message
	thread, err := b.session.MessageThreadStartComplex(recipient, msg.ID, &discordgo.ThreadStart{
		Name:                message[:min(len(message), 50)],
		AutoArchiveDuration: 1440, // 24 hours
	})
	if err != nil {
		// Thread creation failed — return the message ID as fallback
		return msg.ID, nil
	}
	return thread.ID, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Subscribe starts receiving Discord messages and calls handler for each one.
// Blocks until ctx is cancelled.
func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	b.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author.ID == s.State.User.ID {
			return
		}
		if b.channelID != "" && m.ChannelID != b.channelID {
			return
		}
		handler(messaging.Message{
			ID:         m.ID,
			GroupID:    m.ChannelID,
			Sender:     m.Author.ID,
			SenderName: m.Author.Username,
			Text:       m.Content,
			Backend:    "discord",
		})
	})
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("discord open: %w", err)
	}
	<-ctx.Done()
	return b.session.Close()
}

// Link is not applicable for Discord (bot token-based).
func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }

// SelfID returns the Discord bot's user ID.
func (b *Backend) SelfID() string {
	if b.session.State != nil && b.session.State.User != nil {
		return b.session.State.User.ID
	}
	return ""
}

// Close shuts down the Discord session.
func (b *Backend) Close() error { return b.session.Close() }
