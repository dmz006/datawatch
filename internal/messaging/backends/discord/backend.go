// Package discord implements the messaging.Backend for Discord bots.
// Requires a bot token and a channel ID to listen on.
package discord

import (
	"context"
	"fmt"

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
