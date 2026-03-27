// Package telegram implements the messaging.Backend for Telegram bots.
package telegram

import (
	"context"
	"fmt"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/dmz006/datawatch/internal/messaging"
)

// Backend implements messaging.Backend for Telegram.
type Backend struct {
	bot    *tgbotapi.BotAPI
	chatID int64
}

// New creates a new Telegram backend.
func New(token string, chatID int64) (*Backend, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("telegram bot: %w", err)
	}
	return &Backend{bot: bot, chatID: chatID}, nil
}

func (b *Backend) Name() string { return "telegram" }

func (b *Backend) Send(recipient, message string) error {
	// recipient is a chat ID string; fall back to configured chatID
	id := b.chatID
	if recipient != "" {
		if parsed, err := strconv.ParseInt(recipient, 10, 64); err == nil {
			id = parsed
		}
	}
	msg := tgbotapi.NewMessage(id, message)
	_, err := b.bot.Send(msg)
	return err
}

func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.bot.GetUpdatesChan(u)
	for {
		select {
		case <-ctx.Done():
			b.bot.StopReceivingUpdates()
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			if update.Message == nil {
				continue
			}
			if b.chatID != 0 && update.Message.Chat.ID != b.chatID {
				continue
			}
			handler(messaging.Message{
				ID:         strconv.Itoa(update.Message.MessageID),
				GroupID:    strconv.FormatInt(update.Message.Chat.ID, 10),
				GroupName:  update.Message.Chat.Title,
				Sender:     strconv.FormatInt(update.Message.From.ID, 10),
				SenderName: update.Message.From.UserName,
				Text:       update.Message.Text,
				Backend:    "telegram",
			})
		}
	}
}

func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }
func (b *Backend) SelfID() string                                   { return b.bot.Self.UserName }
func (b *Backend) Close() error {
	b.bot.StopReceivingUpdates()
	return nil
}
