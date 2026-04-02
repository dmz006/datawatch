// Package telegram implements the messaging.Backend for Telegram bots.
package telegram

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

// SendMarkdown sends a Markdown-formatted message to Telegram.
func (b *Backend) SendMarkdown(recipient, markdown string) error {
	id := b.chatID
	if recipient != "" {
		if parsed, err := strconv.ParseInt(recipient, 10, 64); err == nil {
			id = parsed
		}
	}
	msg := tgbotapi.NewMessage(id, markdown)
	msg.ParseMode = "Markdown"
	_, err := b.bot.Send(msg)
	return err
}

// SendThreaded sends a message in a Telegram reply thread. threadID is the message ID
// of the first message in the thread. Returns the message ID for follow-up replies.
func (b *Backend) SendThreaded(recipient, message, threadID string) (string, error) {
	id := b.chatID
	if recipient != "" {
		if parsed, err := strconv.ParseInt(recipient, 10, 64); err == nil {
			id = parsed
		}
	}
	msg := tgbotapi.NewMessage(id, message)
	if threadID != "" {
		if parsed, err := strconv.Atoi(threadID); err == nil {
			msg.ReplyToMessageID = parsed
		}
	}
	sent, err := b.bot.Send(msg)
	if err != nil {
		return "", err
	}
	// If first message, return its ID as the thread ID
	if threadID == "" {
		return strconv.Itoa(sent.MessageID), nil
	}
	return threadID, nil
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
			msg := messaging.Message{
				ID:         strconv.Itoa(update.Message.MessageID),
				GroupID:    strconv.FormatInt(update.Message.Chat.ID, 10),
				GroupName:  update.Message.Chat.Title,
				Sender:     strconv.FormatInt(update.Message.From.ID, 10),
				SenderName: update.Message.From.UserName,
				Text:       update.Message.Text,
				Backend:    "telegram",
			}

			// Handle voice messages and audio files
			var fileID, mime string
			if update.Message.Voice != nil {
				fileID = update.Message.Voice.FileID
				mime = update.Message.Voice.MimeType
			} else if update.Message.Audio != nil {
				fileID = update.Message.Audio.FileID
				mime = update.Message.Audio.MimeType
			}
			if fileID != "" {
				if localPath, err := b.downloadFile(fileID); err != nil {
					log.Printf("[telegram] failed to download voice: %v", err)
				} else {
					if mime == "" {
						mime = "audio/ogg"
					}
					msg.Attachments = append(msg.Attachments, messaging.Attachment{
						ContentType: mime,
						FilePath:    localPath,
					})
				}
			}

			handler(msg)
		}
	}
}

// downloadFile retrieves a Telegram file by ID and saves it to a temp file.
func (b *Backend) downloadFile(fileID string) (string, error) {
	fileConfig := tgbotapi.FileConfig{FileID: fileID}
	tgFile, err := b.bot.GetFile(fileConfig)
	if err != nil {
		return "", fmt.Errorf("get file info: %w", err)
	}

	url := tgFile.Link(b.bot.Token)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	ext := filepath.Ext(tgFile.FilePath)
	if ext == "" {
		ext = ".ogg"
	}
	tmpFile, err := os.CreateTemp("", "tg-voice-*"+ext)
	if err != nil {
		return "", fmt.Errorf("temp file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("save: %w", err)
	}
	return tmpFile.Name(), nil
}

func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }
func (b *Backend) SelfID() string                                   { return b.bot.Self.UserName }
func (b *Backend) Close() error {
	b.bot.StopReceivingUpdates()
	return nil
}
