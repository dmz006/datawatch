package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/stats"
	"github.com/gorilla/websocket"
)

// MessageType for WebSocket protocol
type MessageType string

const (
	// Server → Client
	MsgSessions       MessageType = "sessions"        // full session list
	MsgSessionState   MessageType = "session_state"   // one session updated
	MsgOutput         MessageType = "output"          // new output lines
	MsgNeedsInput     MessageType = "needs_input"     // session waiting for input
	MsgNotification   MessageType = "notification"    // general text notification
	MsgError          MessageType = "error"           // error message
	MsgAlert          MessageType = "alert"           // system alert from filter engine
	MsgChannelReply   MessageType = "channel_reply"   // reply from claude via MCP channel
	MsgChannelNotify  MessageType = "channel_notify"  // notification from MCP channel (e.g. permission relay)
	MsgChannelReady   MessageType = "channel_ready"   // MCP channel server connected and ready
	MsgChatMessage    MessageType = "chat_message"    // structured chat message (OpenWebUI interactive)
	MsgResponse       MessageType = "response"        // last LLM response captured (for /copy, alerts)
	MsgSessionAware   MessageType = "session_aware"   // session awareness broadcast (BL76)
	MsgUpdateProgress MessageType = "update_progress" // v4.0.6 — self-update download progress {version, downloaded, total, phase}
	MsgPRDUpdate      MessageType = "prd_update"      // v5.24.0 — autonomous PRD save broadcast {prd_id, status} so PWA Autonomous tab refreshes without manual reload
	MsgHookUpdate     MessageType = "hook_update"     // BL303 S3 — session hook state changed {session_id, board} so Status tab refreshes in real-time
	MsgAlertsRead     MessageType = "alerts_read"     // BL432 — alerts marked read {ids:[...]} or {all:true}, plus unread_count, so badges sync without polling
	MsgSessionRemoved MessageType = "session_removed" // BL432 / GH#242 — delta session-list clients: {session_id} left the list
	MsgHelloAck       MessageType = "hello_ack"       // BL432 / GH#242 — reply to hello with the effective modes + capabilities

	// Client → Server
	MsgCommand    MessageType = "command"     // raw command string (same as Signal)
	MsgNewSession MessageType = "new_session" // {"task":"..."}
	MsgSendInput  MessageType = "send_input"  // {"session_id":"...","text":"..."}
	MsgSubscribe  MessageType = "subscribe"   // {"session_id":"..."} subscribe to output
	MsgResizeTerm MessageType = "resize_term" // {"session_id":"...","cols":N,"rows":N}
	MsgStats      MessageType = "stats"       // system stats broadcast
	MsgPing       MessageType = "ping"
	MsgHello      MessageType = "hello" // BL432 / GH#242 — {"session_list":"full"|"delta","session_view":"full"|"summary"}; see ws_session_stream.go
)

// WSMessage is the envelope for all WebSocket messages
type WSMessage struct {
	Type      MessageType     `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
	Timestamp time.Time       `json:"ts"`
}

type SessionsData struct {
	Sessions []*session.Session `json:"sessions"`
	Version  string             `json:"version,omitempty"`
}

// SessionStateData carries a single updated session for the lighter-weight
// session_state broadcast — see BroadcastSessionState's doc comment.
type SessionStateData struct {
	Session *session.Session `json:"session"`
}

type OutputData struct {
	SessionID string   `json:"session_id"`
	Lines     []string `json:"lines"`
}

type NeedsInputData struct {
	SessionID string `json:"session_id"`
	Prompt    string `json:"prompt"`
}

// ChatMessageData carries a structured chat message for chat-mode sessions.
type ChatMessageData struct {
	SessionID string `json:"session_id"`
	Role      string `json:"role"` // "user", "assistant", "system"
	Content   string `json:"content"`
	Streaming bool   `json:"streaming"` // true = partial chunk, false = complete message
}

type NotificationData struct {
	Message string `json:"message"`
}

type CommandData struct {
	Text string `json:"text"`
}

type NewSessionData struct {
	Task       string `json:"task"`
	ProjectDir string `json:"project_dir,omitempty"`
	Backend    string `json:"backend,omitempty"`
	Name       string `json:"name,omitempty"`
	ResumeID   string `json:"resume_id,omitempty"` // LLM session ID to resume
}

type SendInputData struct {
	SessionID string `json:"session_id"` // short or full ID
	Text      string `json:"text"`
	Raw       bool   `json:"raw,omitempty"` // if true, send literal bytes via tmux send-keys -l
}

type SubscribeData struct {
	SessionID string `json:"session_id"`
}

// client represents one WebSocket connection
type client struct {
	hub            *Hub
	conn           *websocket.Conn
	send           chan []byte
	closed         bool                          // true after send channel is closed
	subscribed     map[string]bool               // session IDs this client is subscribed to
	captureCancels map[string]context.CancelFunc // per-session screen capture cancel funcs
	// sessionList / sessionView are the modes negotiated by hello (see
	// ws_session_stream.go). "" means the client never chose: sessionList
	// then follows server.ws_session_list_default, sessionView is "full".
	sessionList string
	sessionView string
	mu          sync.Mutex
}

// safeSend sends a message to the client's send channel without panicking
// if the channel is closed (client disconnected).
func (c *client) safeSend(msg []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	select {
	case c.send <- msg:
	default:
		// Channel full or closed — drop the message
	}
}

// hubMessage is one queued broadcast. want selects the receiving clients;
// nil means every connected client (the behavior of plain Broadcast).
// want runs on the Hub.Run goroutine while it holds h.mu, so it must not
// take h.mu itself; locking the client's own mutex is fine.
type hubMessage struct {
	payload []byte
	want    func(*client) bool
}

// Hub manages all WebSocket clients
type Hub struct {
	clients    map[*client]bool
	broadcast  chan hubMessage
	register   chan *client
	unregister chan *client
	mu         sync.RWMutex
	chanStats  *stats.ChannelCounters // optional channel stats tracker
	version    string                 // daemon version, included in sessions message

	// Delta session-list state (ws_session_stream.go). snapshot maps a
	// session's FullID to the fingerprint last streamed; guarded by
	// snapMu because BroadcastSessions runs on many goroutines.
	snapMu   sync.Mutex
	snapshot map[string]uint64

	// Live-config hooks wired by NewServer before Run starts; nil in
	// bare-hub tests, which then get the built-in defaults.
	sessionListDefaultFn func() string
	summaryTextCharsFn   func() int
}

var upgrader = websocket.Upgrader{
	CheckOrigin:     wsCheckOrigin, // SEC-007
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// wsCheckOrigin enforces same-origin for browser WS clients while staying
// permissive for non-browser ones (SEC-007). Origin is a browser-enforced
// header — a non-browser client (the CLI, a mobile app, a raw websocat
// script) is not guaranteed to send one at all, so treating "no Origin" as
// "reject" would break those, not just attackers; the admin/peer bearer
// token is the real gate for them regardless. What this closes is the
// actual CSRF-adjacent case: a browser tab on an attacker's page opening a
// WS connection to this daemon with a mismatched, present Origin.
func wsCheckOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*client]bool),
		broadcast:  make(chan hubMessage, 256),
		register:   make(chan *client),
		unregister: make(chan *client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = true
			h.mu.Unlock()
		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				c.mu.Lock()
				c.closed = true
				c.mu.Unlock()
				close(c.send)
			}
			h.mu.Unlock()
		case m := <-h.broadcast:
			// Write lock: a slow client is deleted from h.clients below.
			h.mu.Lock()
			for c := range h.clients {
				if m.want != nil && !m.want(c) {
					continue
				}
				select {
				case c.send <- m.payload:
				default:
					// Slow client (send buffer full): drop it.
					c.mu.Lock()
					c.closed = true
					c.mu.Unlock()
					close(c.send)
					delete(h.clients, c)
				}
			}
			h.mu.Unlock()
		}
	}
}

// Broadcast sends a message to all connected clients
func (h *Hub) Broadcast(msgType MessageType, data interface{}) {
	h.broadcastTo(msgType, data, nil)
}

// broadcastTo marshals one frame and queues it for the clients want
// selects (nil = all). chanStats semantics are unchanged from before
// per-client filtering existed: bytes are counted once per queued frame,
// not once per receiving client.
func (h *Hub) broadcastTo(msgType MessageType, data interface{}, want func(*client) bool) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := WSMessage{Type: msgType, Data: raw, Timestamp: time.Now()}
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	if h.chanStats != nil {
		h.chanStats.RecordSent(len(payload))
	}
	h.broadcast <- hubMessage{payload: payload, want: want}
}

// broadcastRaw queues an already-encoded frame for every client without
// touching chanStats, matching the channel-reply call sites that used to
// write to h.broadcast directly.
func (h *Hub) broadcastRaw(payload []byte) {
	h.broadcast <- hubMessage{payload: payload}
}

// SetVersion sets the daemon version included in sessions broadcasts.
func (h *Hub) SetVersion(v string) { h.version = v }

// BroadcastOutput sends new output lines for a session to subscribed clients
func (h *Hub) BroadcastOutput(sessionID string, lines []string) {
	h.Broadcast(MsgOutput, OutputData{SessionID: sessionID, Lines: lines})
}

// BroadcastStats sends system stats to all clients
func (h *Hub) BroadcastStats(data interface{}) {
	h.Broadcast(MsgStats, data)
}

// BroadcastRawOutput sends raw output (ANSI preserved) for xterm.js rendering
func (h *Hub) BroadcastRawOutput(sessionID string, lines []string) {
	h.Broadcast("raw_output", OutputData{SessionID: sessionID, Lines: lines})
}

// BroadcastNeedsInput notifies clients that a session needs input
func (h *Hub) BroadcastNeedsInput(sessionID, prompt string) {
	h.Broadcast(MsgNeedsInput, NeedsInputData{SessionID: sessionID, Prompt: prompt})
}

// BroadcastChatMessage sends a structured chat message for chat-mode sessions.
func (h *Hub) BroadcastChatMessage(sessionID, role, content string, streaming bool) {
	h.Broadcast(MsgChatMessage, ChatMessageData{
		SessionID: sessionID, Role: role, Content: content, Streaming: streaming,
	})
}

// BroadcastResponse sends a captured LLM response to all clients.
func (h *Hub) BroadcastResponse(sessionID, response string) {
	h.Broadcast(MsgResponse, map[string]string{"session_id": sessionID, "response": response})
}

// BroadcastSessionAwareness sends a session summary to all clients for cross-session awareness.
func (h *Hub) BroadcastSessionAwareness(sessionID, summary, task, state string) {
	h.Broadcast(MsgSessionAware, map[string]string{
		"session_id": sessionID, "summary": summary, "task": task, "state": state,
	})
}

// BroadcastHookUpdate sends the updated session status board to all clients
// when a hook event is received, enabling real-time Status tab refresh.
// BL303 S3 T12/T18.
func (h *Hub) BroadcastHookUpdate(sessionID string, board interface{}) {
	h.Broadcast(MsgHookUpdate, map[string]interface{}{"session_id": sessionID, "board": board})
}

// BroadcastNotification sends a general notification to all clients
func (h *Hub) BroadcastNotification(msg string) {
	h.Broadcast(MsgNotification, NotificationData{Message: msg})
}

// BroadcastAlert pushes a new system alert to all connected clients.
func (h *Hub) BroadcastAlert(a *alerts.Alert) {
	h.Broadcast(MsgAlert, a)
}

// SetChannelStats sets the stats counters for WS message tracking.
func (h *Hub) SetChannelStats(cs *stats.ChannelCounters) {
	h.chanStats = cs
}

// ClientCount returns the number of connected WebSocket clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (c *client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{}) //nolint:errcheck
				return
			}
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(msg) //nolint:errcheck
			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
