package wizard

import (
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/bwmarrin/discordgo"
	slackpkg "github.com/slack-go/slack"

	"github.com/dmz006/datawatch/internal/config"
)

// RegisterAll registers wizard definitions for all supported services into m.
func RegisterAll(m *Manager) {
	m.Register(signalDef())
	m.Register(telegramDef())
	m.Register(discordDef())
	m.Register(slackDef())
	m.Register(matrixDef())
	m.Register(twilioScalarDef())
	m.Register(ntfyDef())
	m.Register(emailDef())
	m.Register(webhookDef())
	m.Register(githubDef())
	m.Register(webDef())
}

// loadAndSave is a helper used by every OnComplete: loads config, applies a
// patch function, then saves.
func loadAndSave(cfgPath string, patch func(*config.Config)) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}
	patch(cfg)
	return config.Save(cfg, cfgPath)
}

// ---- Signal ---------------------------------------------------------------

func signalDef() *Def {
	return &Def{
		Service: "signal",
		Intro: `Signal Setup
============
Signal requires a QR code scan from the command line. Run on the host machine:

  datawatch setup signal

This will link the device and create a control group automatically.`,
		Steps: []Step{}, // No steps — intro only, completes immediately
		OnComplete: func(cfgPath string, data map[string]string) error {
			return nil // Instructions only
		},
	}
}

// ---- Telegram -------------------------------------------------------------

func telegramDef() *Def {
	return &Def{
		Service: "telegram",
		Intro: `Telegram Setup
==============
1. Open Telegram and start a chat with @BotFather
2. Send /newbot and follow the prompts
3. Copy the API token BotFather gives you

Type 'cancel' at any time to abort.`,
		Steps: []Step{
			{
				Key:    "token",
				Prompt: "Paste your Telegram bot token:",
				Validate: func(v string) error {
					if !strings.Contains(v, ":") {
						return fmt.Errorf("invalid token format — should contain a colon, e.g. 123456:ABC-DEF...")
					}
					return nil
				},
			},
			{
				Key:    "chat_id",
				Prompt: "Select the group/chat to use, or enter the chat ID manually:",
				OptionsFunc: func(collected map[string]string) ([]string, error) {
					bot, err := tgbotapi.NewBotAPI(collected["token"])
					if err != nil {
						return nil, fmt.Errorf("connect to Telegram: %w", err)
					}
					u := tgbotapi.NewUpdate(0)
					u.Timeout = 5
					updates, err := bot.GetUpdates(u)
					if err != nil || len(updates) == 0 {
						return nil, nil // Fall through to free-text
					}
					seen := map[int64]string{}
					for _, upd := range updates {
						if upd.Message != nil {
							name := upd.Message.Chat.Title
							if name == "" {
								name = "@" + upd.Message.Chat.UserName
							}
							seen[upd.Message.Chat.ID] = fmt.Sprintf("%s (ID: %d)", name, upd.Message.Chat.ID)
						}
					}
					opts := make([]string, 0, len(seen))
					for _, v := range seen {
						opts = append(opts, v)
					}
					return opts, nil
				},
			},
		},
		OnComplete: func(cfgPath string, data map[string]string) error {
			return loadAndSave(cfgPath, func(cfg *config.Config) {
				cfg.Telegram.Token = data["token"]
				// chat_id may be "Name (ID: 123456)" or a raw number
				raw := data["chat_id"]
				if idx := strings.LastIndex(raw, "ID: "); idx >= 0 {
					raw = strings.TrimSuffix(strings.TrimSpace(raw[idx+4:]), ")")
				}
				id, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
				cfg.Telegram.ChatID = id
				cfg.Telegram.Enabled = true
			})
		},
	}
}

// ---- Discord --------------------------------------------------------------

func discordDef() *Def {
	return &Def{
		Service: "discord",
		Intro: `Discord Setup
=============
1. Go to https://discord.com/developers/applications
2. Create a New Application, then go to Bot → Add Bot
3. Copy the bot token
4. Under Bot, enable "Message Content Intent"
5. OAuth2 → URL Generator: scope=bot, permissions=Send Messages + Read Message History
6. Use the generated URL to invite the bot to your server

Type 'cancel' at any time to abort.`,
		Steps: []Step{
			{
				Key:    "token",
				Prompt: "Paste your Discord bot token:",
			},
			{
				Key:    "channel_id",
				Prompt: "Select a text channel, or enter the channel ID manually:",
				OptionsFunc: func(collected map[string]string) ([]string, error) {
					dg, err := discordgo.New("Bot " + collected["token"])
					if err != nil {
						return nil, fmt.Errorf("connect to Discord: %w", err)
					}
					if err := dg.Open(); err != nil {
						return nil, fmt.Errorf("open Discord session: %w", err)
					}
					defer dg.Close() //nolint:errcheck

					guilds, err := dg.UserGuilds(10, "", "", false)
					if err != nil || len(guilds) == 0 {
						return nil, nil
					}

					var opts []string
					for _, g := range guilds {
						channels, _ := dg.GuildChannels(g.ID)
						for _, ch := range channels {
							if ch.Type == discordgo.ChannelTypeGuildText {
								opts = append(opts, fmt.Sprintf("#%s in %s (ID: %s)", ch.Name, g.Name, ch.ID))
							}
						}
					}
					return opts, nil
				},
			},
		},
		OnComplete: func(cfgPath string, data map[string]string) error {
			return loadAndSave(cfgPath, func(cfg *config.Config) {
				cfg.Discord.Token = data["token"]
				raw := data["channel_id"]
				if idx := strings.LastIndex(raw, "ID: "); idx >= 0 {
					raw = strings.TrimSuffix(strings.TrimSpace(raw[idx+4:]), ")")
				}
				cfg.Discord.ChannelID = strings.TrimSpace(raw)
				cfg.Discord.Enabled = true
			})
		},
	}
}

// ---- Slack ----------------------------------------------------------------

func slackDef() *Def {
	return &Def{
		Service: "slack",
		Intro: `Slack Setup
===========
1. Go to https://api.slack.com/apps → Create New App
2. OAuth & Permissions → Bot Token Scopes, add:
   channels:history, channels:read, chat:write, groups:history, groups:read
3. Install to workspace and copy the "Bot User OAuth Token" (starts with xoxb-)

Type 'cancel' at any time to abort.`,
		Steps: []Step{
			{
				Key:    "token",
				Prompt: "Paste your Slack bot token (xoxb-...):",
				Validate: func(v string) error {
					if !strings.HasPrefix(v, "xoxb-") {
						return fmt.Errorf("token should start with xoxb-")
					}
					return nil
				},
			},
			{
				Key:    "channel_id",
				Prompt: "Select a channel, or enter the channel ID manually:",
				OptionsFunc: func(collected map[string]string) ([]string, error) {
					client := slackpkg.New(collected["token"])
					params := &slackpkg.GetConversationsParameters{
						Types: []string{"public_channel", "private_channel"},
						Limit: 50,
					}
					channels, _, err := client.GetConversations(params)
					if err != nil || len(channels) == 0 {
						return nil, nil
					}
					opts := make([]string, 0, len(channels))
					for _, ch := range channels {
						opts = append(opts, fmt.Sprintf("#%s (ID: %s)", ch.Name, ch.ID))
					}
					return opts, nil
				},
			},
		},
		OnComplete: func(cfgPath string, data map[string]string) error {
			return loadAndSave(cfgPath, func(cfg *config.Config) {
				cfg.Slack.Token = data["token"]
				raw := data["channel_id"]
				if idx := strings.LastIndex(raw, "ID: "); idx >= 0 {
					raw = strings.TrimSuffix(strings.TrimSpace(raw[idx+4:]), ")")
				}
				cfg.Slack.ChannelID = strings.TrimSpace(raw)
				cfg.Slack.Enabled = true
			})
		},
	}
}

// ---- Matrix ---------------------------------------------------------------

func matrixDef() *Def {
	return &Def{
		Service: "matrix",
		Intro: `Matrix Setup
============
1. Create a Matrix bot account (e.g. at matrix.org or your own homeserver)
2. Log in with Element.io, go to: Settings → Help & About → scroll to Access Token
3. Copy the homeserver URL, your user ID (@bot:matrix.org), and access token

Type 'cancel' at any time to abort.`,
		Steps: []Step{
			{
				Key:    "homeserver",
				Prompt: "Homeserver URL (e.g. https://matrix.org):",
				Validate: func(v string) error {
					if !strings.HasPrefix(v, "http") {
						return fmt.Errorf("must start with http:// or https://")
					}
					return nil
				},
			},
			{
				Key:    "user_id",
				Prompt: "Bot user ID (e.g. @bot:matrix.org):",
				Validate: func(v string) error {
					if !strings.HasPrefix(v, "@") {
						return fmt.Errorf("user ID must start with @")
					}
					return nil
				},
			},
			{
				Key:    "access_token",
				Prompt: "Access token:",
			},
			{
				Key:    "room_id",
				Prompt: "Room ID or alias to use (e.g. !abcdef:matrix.org or #myroom:matrix.org):",
			},
		},
		OnComplete: func(cfgPath string, data map[string]string) error {
			return loadAndSave(cfgPath, func(cfg *config.Config) {
				cfg.Matrix.Homeserver = data["homeserver"]
				cfg.Matrix.UserID = data["user_id"]
				cfg.Matrix.AccessToken = data["access_token"]
				cfg.Matrix.RoomID = data["room_id"]
				cfg.Matrix.Enabled = true
			})
		},
	}
}

// ---- Twilio ---------------------------------------------------------------

func twilioScalarDef() *Def {
	return &Def{
		Service: "twilio",
		Intro: `Twilio SMS Setup
================
1. Log in to console.twilio.com
2. Find your Account SID and Auth Token on the dashboard
3. Buy or use an existing Twilio phone number

Type 'cancel' at any time to abort.`,
		Steps: []Step{
			{Key: "account_sid", Prompt: "Account SID (starts with AC):"},
			{Key: "auth_token", Prompt: "Auth Token:"},
			{Key: "from_number", Prompt: "Your Twilio phone number (e.g. +12125551234):"},
			{Key: "to_number", Prompt: "Destination phone number (your number, e.g. +12125559876):"},
			{Key: "webhook_addr", Prompt: "Webhook listen address [default: :9003]:", Optional: true},
		},
		OnComplete: func(cfgPath string, data map[string]string) error {
			return loadAndSave(cfgPath, func(cfg *config.Config) {
				cfg.Twilio.AccountSID = data["account_sid"]
				cfg.Twilio.AuthToken = data["auth_token"]
				cfg.Twilio.FromNumber = data["from_number"]
				cfg.Twilio.ToNumber = data["to_number"]
				if addr := data["webhook_addr"]; addr != "" {
					cfg.Twilio.WebhookAddr = addr
				} else {
					cfg.Twilio.WebhookAddr = ":9003"
				}
				cfg.Twilio.Enabled = true
			})
		},
	}
}

// ---- ntfy -----------------------------------------------------------------

func ntfyDef() *Def {
	return &Def{
		Service: "ntfy",
		Intro: `ntfy Setup
==========
ntfy is a simple push notification service. You can use the free ntfy.sh server
or self-host your own. Choose a unique topic name.

Type 'cancel' at any time to abort.`,
		Steps: []Step{
			{Key: "server_url", Prompt: "ntfy server URL [default: https://ntfy.sh]:", Optional: true},
			{Key: "topic", Prompt: "Topic name (unique string, e.g. datawatch-myhost-abc123):"},
			{Key: "token", Prompt: "Access token (optional — for authenticated topics):", Optional: true},
		},
		OnComplete: func(cfgPath string, data map[string]string) error {
			return loadAndSave(cfgPath, func(cfg *config.Config) {
				if u := data["server_url"]; u != "" {
					cfg.Ntfy.ServerURL = u
				} else {
					cfg.Ntfy.ServerURL = "https://ntfy.sh"
				}
				cfg.Ntfy.Topic = data["topic"]
				cfg.Ntfy.Token = data["token"]
				cfg.Ntfy.Enabled = true
			})
		},
	}
}

// ---- Email ----------------------------------------------------------------

func emailDef() *Def {
	return &Def{
		Service: "email",
		Intro: `Email (SMTP) Setup
==================
You need SMTP server credentials. For Gmail, create an App Password at
https://myaccount.google.com/apppasswords

Type 'cancel' at any time to abort.`,
		Steps: []Step{
			{Key: "host", Prompt: "SMTP server hostname (e.g. smtp.gmail.com):"},
			{Key: "port", Prompt: "SMTP port [default: 587]:", Optional: true},
			{Key: "username", Prompt: "SMTP username (usually your email address):"},
			{Key: "password", Prompt: "SMTP password / app password:"},
			{Key: "from", Prompt: "From address (e.g. bot@example.com):"},
			{Key: "to", Prompt: "To address (where alerts go):"},
		},
		OnComplete: func(cfgPath string, data map[string]string) error {
			return loadAndSave(cfgPath, func(cfg *config.Config) {
				cfg.Email.Host = data["host"]
				port := 587
				if p := data["port"]; p != "" {
					fmt.Sscanf(p, "%d", &port)
				}
				cfg.Email.Port = port
				cfg.Email.Username = data["username"]
				cfg.Email.Password = data["password"]
				cfg.Email.From = data["from"]
				cfg.Email.To = data["to"]
				cfg.Email.Enabled = true
			})
		},
	}
}

// ---- Webhook --------------------------------------------------------------

func webhookDef() *Def {
	return &Def{
		Service: "webhook",
		Intro: `Generic Webhook Setup
=====================
datawatch will listen for HTTP POST requests. Point any webhook sender at:
  http://your-host:<port>/webhook

Type 'cancel' at any time to abort.`,
		Steps: []Step{
			{Key: "addr", Prompt: "Listen address [default: :9002]:", Optional: true},
			{Key: "token", Prompt: "Bearer token (optional — leave blank for no auth):", Optional: true},
		},
		OnComplete: func(cfgPath string, data map[string]string) error {
			return loadAndSave(cfgPath, func(cfg *config.Config) {
				if addr := data["addr"]; addr != "" {
					cfg.Webhook.Addr = addr
				} else {
					cfg.Webhook.Addr = ":9002"
				}
				cfg.Webhook.Token = data["token"]
				cfg.Webhook.Enabled = true
			})
		},
	}
}

// ---- GitHub Webhook -------------------------------------------------------

func githubDef() *Def {
	return &Def{
		Service: "github",
		Intro: `GitHub Webhook Setup
====================
1. In your GitHub repo, go to Settings → Webhooks → Add webhook
2. Set Content type to application/json
3. Set the Payload URL to: http://your-host:<port>/webhook
4. Choose a secret (any random string)

Type 'cancel' at any time to abort.`,
		Steps: []Step{
			{Key: "addr", Prompt: "Listen address [default: :9001]:", Optional: true},
			{Key: "secret", Prompt: "Webhook secret (must match what you set in GitHub):"},
		},
		OnComplete: func(cfgPath string, data map[string]string) error {
			return loadAndSave(cfgPath, func(cfg *config.Config) {
				if addr := data["addr"]; addr != "" {
					cfg.GitHubWebhook.Addr = addr
				} else {
					cfg.GitHubWebhook.Addr = ":9001"
				}
				cfg.GitHubWebhook.Secret = data["secret"]
				cfg.GitHubWebhook.Enabled = true
			})
		},
	}
}

// ---- Web server -----------------------------------------------------------

func webDef() *Def {
	return &Def{
		Service: "web",
		Intro: `Web Server Setup
================
datawatch includes a web UI and HTTP API. Configure it here.

Type 'cancel' at any time to abort.`,
		Steps: []Step{
			{
				Key:    "enable",
				Prompt: "Enable the web server? (y/n) [default: y]:",
				Validate: func(v string) error {
					switch strings.ToLower(v) {
					case "y", "yes", "n", "no", "":
						return nil
					}
					return fmt.Errorf("enter y or n")
				},
				Optional: true,
			},
			{Key: "host", Prompt: "Bind address [default: 0.0.0.0]:", Optional: true},
			{Key: "port", Prompt: "Port [default: 8080]:", Optional: true},
			{Key: "token", Prompt: "Bearer token for authentication (leave blank for no auth):", Optional: true},
			{
				Key:    "tls",
				Prompt: "Enable TLS with auto-generated certificate? (y/n) [default: y]:",
				Validate: func(v string) error {
					switch strings.ToLower(v) {
					case "y", "yes", "n", "no", "":
						return nil
					}
					return fmt.Errorf("enter y or n")
				},
				Optional: true,
			},
		},
		OnComplete: func(cfgPath string, data map[string]string) error {
			return loadAndSave(cfgPath, func(cfg *config.Config) {
				enable := strings.ToLower(data["enable"])
				if enable == "n" || enable == "no" {
					cfg.Server.Enabled = false
					return
				}
				cfg.Server.Enabled = true
				if h := data["host"]; h != "" {
					cfg.Server.Host = h
				}
				if p := data["port"]; p != "" {
					fmt.Sscanf(p, "%d", &cfg.Server.Port)
				}
				cfg.Server.Token = data["token"]
				tls := strings.ToLower(data["tls"])
				cfg.Server.TLSEnabled = tls != "n" && tls != "no"
				cfg.Server.TLSAutoGenerate = cfg.Server.TLSEnabled
			})
		},
	}
}

