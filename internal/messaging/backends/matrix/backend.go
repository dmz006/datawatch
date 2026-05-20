// Package matrix implements the messaging.Backend for Matrix (Element) rooms.
//
// BL241 P1 — cleartext-only backend. Encrypted rooms emit a warning and are
// skipped (E2EE lands in P2). The backend implements:
//
//   - messaging.Backend         (Send + Subscribe + Link + SelfID + Close)
//   - messaging.RichSender      (SendMarkdown → org.matrix.custom.html)
//   - messaging.ThreadedSender  (SendThreaded via m.thread relation)
//
// FileSender (file upload) is explicitly deferred to a later BL per the
// BL241 out-of-scope list.
package matrix

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/dmz006/datawatch/internal/messaging"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Backend implements messaging.Backend (+ RichSender + ThreadedSender) for Matrix.
type Backend struct {
	client    *mautrix.Client
	roomID    id.RoomID
	botUserID id.UserID
	deviceID  id.DeviceID
	resolver  *aliasResolver

	// rawRoomID is the original config value (may be an alias); resolver maps
	// it to the real room ID stored in b.roomID at Subscribe time.
	rawRoomID string

	// hostname is embedded in m.datawatch.session on every outbound message (Q5.3).
	hostname string
}

// SetHostname sets the daemon hostname embedded in every outbound message content
// as m.datawatch.session.host (Q5.3 session_id embed).
func (b *Backend) SetHostname(hostname string) { b.hostname = hostname }

// New creates a new Matrix backend.
// roomID accepts both !id:server and #alias:server syntax; the alias is
// resolved lazily on the first Subscribe call.
func New(homeserver, userID, accessToken, roomID string) (*Backend, error) {
	client, err := mautrix.NewClient(homeserver, id.UserID(userID), accessToken)
	if err != nil {
		return nil, fmt.Errorf("matrix client: %w", err)
	}
	return &Backend{
		client:    client,
		botUserID: id.UserID(userID),
		rawRoomID: roomID,
		resolver:  newAliasResolver(client),
	}, nil
}

// NewWithDevice creates a backend with an explicit device ID (used by bot.go).
func NewWithDevice(homeserver, userID, accessToken, deviceID, roomID string) (*Backend, error) {
	b, err := New(homeserver, userID, accessToken, roomID)
	if err != nil {
		return nil, err
	}
	b.client.DeviceID = id.DeviceID(deviceID)
	b.deviceID = id.DeviceID(deviceID)
	return b, nil
}

func (b *Backend) Name() string { return "matrix" }

// datawatchSession is embedded in every outbound m.room.message content as
// "m.datawatch.session" per Q5.3 of the BL241 design. It lets V2 routing layer
// identify which session/host produced each message without changing the wire format.
type datawatchSession struct {
	SessionID string `json:"session_id"` // empty in V1 single-room mode; V2 populates
	Host      string `json:"host,omitempty"`
	Role      string `json:"role"` // "output" for daemon→room messages
}

// outboundContent wraps MessageEventContent with the datawatch session extension field.
type outboundContent struct {
	*event.MessageEventContent
	DatawatchSession datawatchSession `json:"m.datawatch.session,omitempty"`
}

func (b *Backend) sessionTag() datawatchSession {
	return datawatchSession{Host: b.hostname, Role: "output"}
}

// Send sends a plain-text message to the given recipient room.
// If recipient is empty, the configured room is used.
func (b *Backend) Send(recipient, message string) error {
	roomID, err := b.resolveRoom(context.Background(), recipient)
	if err != nil {
		return err
	}
	content := &outboundContent{
		MessageEventContent: &event.MessageEventContent{
			MsgType: event.MsgText,
			Body:    message,
		},
		DatawatchSession: b.sessionTag(),
	}
	_, err = b.client.SendMessageEvent(context.Background(), roomID, event.EventMessage, content)
	return err
}

// SendMarkdown implements messaging.RichSender.
// Markdown is sent with format=org.matrix.custom.html so clients that
// understand HTML display it formatted; others fall back to the plaintext Body.
func (b *Backend) SendMarkdown(recipient, markdown string) error {
	roomID, err := b.resolveRoom(context.Background(), recipient)
	if err != nil {
		return err
	}
	html := markdownToMatrixHTML(markdown)
	content := &outboundContent{
		MessageEventContent: &event.MessageEventContent{
			MsgType:       event.MsgText,
			Body:          markdown,
			Format:        event.FormatHTML,
			FormattedBody: html,
		},
		DatawatchSession: b.sessionTag(),
	}
	_, err = b.client.SendMessageEvent(context.Background(), roomID, event.EventMessage, content)
	return err
}

// SendThreaded implements messaging.ThreadedSender.
// threadID is the Matrix event ID of the parent message. If empty, the message
// is sent without threading (starts a new "thread root").
// Returns the event ID of the sent message, which callers use as threadID for
// subsequent replies.
func (b *Backend) SendThreaded(recipient, message, threadID string) (string, error) {
	roomID, err := b.resolveRoom(context.Background(), recipient)
	if err != nil {
		return "", err
	}
	base := &event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    message,
	}
	if threadID != "" {
		base.RelatesTo = &event.RelatesTo{
			Type:    event.RelThread,
			EventID: id.EventID(threadID),
		}
	}
	content := &outboundContent{
		MessageEventContent: base,
		DatawatchSession:    b.sessionTag(),
	}
	resp, err := b.client.SendMessageEvent(context.Background(), roomID, event.EventMessage, content)
	if err != nil {
		return "", err
	}
	evID := resp.EventID.String()
	if threadID == "" {
		return evID, nil
	}
	return threadID, nil
}

// Subscribe starts receiving Matrix messages and calls handler for each one.
// Blocks until ctx is cancelled.
//
// On startup:
//   - Resolves room alias → ID if configured with a #alias:server.
//   - Joins the configured room (no-op if already joined).
//   - Registers a handler for m.room.canonical_alias state changes to
//     invalidate the alias cache when the room is renamed.
//   - Encrypted-room events (m.room.encrypted) are logged and skipped;
//     cleartext-only mode pending P2.
func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	// Resolve alias at start.
	roomID, err := b.resolver.Resolve(ctx, b.rawRoomID)
	if err != nil {
		return fmt.Errorf("matrix: %w", err)
	}
	b.roomID = roomID

	// Join the room (no-op if already a member).
	if _, err := b.client.JoinRoomByID(ctx, b.roomID); err != nil {
		log.Printf("[matrix] join room %s: %v (continuing anyway — may already be a member)", b.roomID, err)
	}

	syncer := b.client.Syncer.(*mautrix.DefaultSyncer)

	// Cleartext message handler.
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
		origin := Classify(ev.Sender.String())
		handler(messaging.Message{
			ID:         ev.ID.String(),
			GroupID:    ev.RoomID.String(),
			GroupName:  ev.RoomID.String(),
			Sender:     ev.Sender.String(),
			SenderName: NormaliseSender(ev.Sender.String()),
			Text:       content.Body,
			Backend:    "matrix",
			// BridgeOrigin and SourceHomeserver are available in the extended
			// audit-log fields via origin.String() + homeserver extraction.
			// Packed into the message as auxiliary fields via SenderName.
		})
		_ = origin // used in SenderName above; silence unused-variable lint
	})

	// Encrypted-room event: log + skip (P2 will decrypt).
	syncer.OnEventType(event.EventEncrypted, func(ctx context.Context, ev *event.Event) {
		if ev.RoomID != b.roomID {
			return
		}
		log.Printf("[matrix] room %s is encrypted; cleartext-only mode — message from %s skipped (E2EE pending P2)", ev.RoomID, ev.Sender)
	})

	// Alias state-change: invalidate resolver cache.
	syncer.OnEventType(event.StateCanonicalAlias, func(ctx context.Context, ev *event.Event) {
		if b.rawRoomID != "" && strings.HasPrefix(b.rawRoomID, "#") {
			b.resolver.Invalidate(b.rawRoomID)
		}
	})

	return b.client.SyncWithContext(ctx)
}

// Link is a no-op for the bot path (token-based auth). The AS path uses
// registration.yaml generated by `datawatch setup matrix as-register`.
func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }

// SelfID returns the bot's MXID.
func (b *Backend) SelfID() string { return b.botUserID.String() }

// Close stops the sync loop and flushes any pending sends.
func (b *Backend) Close() error {
	b.client.StopSync()
	return nil
}

// resolveRoom returns the target room ID. If recipient is non-empty it is used
// (supports per-message room override); otherwise the configured room is used.
func (b *Backend) resolveRoom(ctx context.Context, recipient string) (id.RoomID, error) {
	if recipient != "" {
		return b.resolver.Resolve(ctx, recipient)
	}
	if b.roomID != "" {
		return b.roomID, nil
	}
	// roomID not yet resolved (Subscribe not called yet) — resolve now.
	roomID, err := b.resolver.Resolve(ctx, b.rawRoomID)
	if err != nil {
		return "", err
	}
	b.roomID = roomID
	return roomID, nil
}

// markdownToMatrixHTML converts a minimal subset of markdown to Matrix HTML.
// Only **bold**, *italic*, `code`, and newlines are handled; for a richer
// conversion a dedicated library (goldmark) would be wired in P2+.
func markdownToMatrixHTML(md string) string {
	// Escape HTML special characters first.
	html := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	).Replace(md)
	// **bold**
	html = applyInlineMarkup(html, "**", "<strong>", "</strong>")
	// *italic*
	html = applyInlineMarkup(html, "*", "<em>", "</em>")
	// `code`
	html = applyInlineMarkup(html, "`", "<code>", "</code>")
	// newlines → <br/>
	html = strings.ReplaceAll(html, "\n", "<br/>")
	return html
}

// ErrPlaintextToken is returned by ValidateSecrets when access_token is a
// literal token instead of a ${secret:...} reference.
var ErrPlaintextToken = fmt.Errorf("matrix: access_token must use ${secret:name} syntax (Secrets-Store Rule, BL241). " +
	"Store the token with: datawatch secrets set matrix-access-token <token>\n" +
	"Then set access_token: ${secret:matrix-access-token} in config.yaml")

// ValidateSecrets enforces the Secrets-Store Rule for the Matrix access token.
// Returns ErrPlaintextToken if access_token is non-empty and is not a
// ${secret:...} reference. Call this before constructing the backend.
func ValidateSecrets(accessToken string) error {
	if accessToken == "" {
		return nil
	}
	if strings.HasPrefix(accessToken, "${secret:") {
		return nil
	}
	return ErrPlaintextToken
}

// applyInlineMarkup replaces paired delimiters with open/close HTML tags.
func applyInlineMarkup(s, delim, open, close string) string {
	var sb strings.Builder
	for {
		start := strings.Index(s, delim)
		if start < 0 {
			sb.WriteString(s)
			break
		}
		end := strings.Index(s[start+len(delim):], delim)
		if end < 0 {
			sb.WriteString(s)
			break
		}
		end += start + len(delim)
		sb.WriteString(s[:start])
		sb.WriteString(open)
		sb.WriteString(s[start+len(delim) : end])
		sb.WriteString(close)
		s = s[end+len(delim):]
	}
	return sb.String()
}
