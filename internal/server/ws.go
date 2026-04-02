package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/stats"
)

// MessageType for WebSocket protocol
type MessageType string

const (
	// Server → Client
	MsgSessions     MessageType = "sessions"      // full session list
	MsgSessionState MessageType = "session_state" // one session updated
	MsgOutput       MessageType = "output"        // new output lines
	MsgNeedsInput   MessageType = "needs_input"   // session waiting for input
	MsgNotification  MessageType = "notification"   // general text notification
	MsgError         MessageType = "error"          // error message
	MsgAlert         MessageType = "alert"          // system alert from filter engine
	MsgChannelReply  MessageType = "channel_reply"  // reply from claude via MCP channel
	MsgChannelNotify MessageType = "channel_notify" // notification from MCP channel (e.g. permission relay)
	MsgChannelReady  MessageType = "channel_ready"  // MCP channel server connected and ready

	// Client → Server
	MsgCommand    MessageType = "command"     // raw command string (same as Signal)
	MsgNewSession MessageType = "new_session" // {"task":"..."}
	MsgSendInput  MessageType = "send_input"  // {"session_id":"...","text":"..."}
	MsgSubscribe  MessageType = "subscribe"   // {"session_id":"..."} subscribe to output
	MsgResizeTerm MessageType = "resize_term" // {"session_id":"...","cols":N,"rows":N}
	MsgStats      MessageType = "stats"       // system stats broadcast
	MsgPing       MessageType = "ping"
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

type OutputData struct {
	SessionID string   `json:"session_id"`
	Lines     []string `json:"lines"`
}

type NeedsInputData struct {
	SessionID string `json:"session_id"`
	Prompt    string `json:"prompt"`
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
	hub        *Hub
	conn       *websocket.Conn
	send       chan []byte
	closed     bool                                  // true after send channel is closed
	subscribed     map[string]bool                   // session IDs this client is subscribed to
	captureCancels map[string]context.CancelFunc     // per-session screen capture cancel funcs
	mu             sync.Mutex
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

// Hub manages all WebSocket clients
type Hub struct {
	clients    map[*client]bool
	broadcast  chan []byte
	register   chan *client
	unregister chan *client
	mu         sync.RWMutex
	chanStats  *stats.ChannelCounters // optional channel stats tracker
	version    string                 // daemon version, included in sessions message
}

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true }, // Tailscale handles security
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*client]bool),
		broadcast:  make(chan []byte, 256),
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
		case msg := <-h.broadcast:
			h.mu.RLock()
			for c := range h.clients {
				select {
				case c.send <- msg:
				default:
					c.mu.Lock()
					c.closed = true
					c.mu.Unlock()
					close(c.send)
					delete(h.clients, c)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends a message to all connected clients
func (h *Hub) Broadcast(msgType MessageType, data interface{}) {
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
	h.broadcast <- payload
}

// BroadcastSessions sends the full session list to all clients
func (h *Hub) BroadcastSessions(sessions []*session.Session) {
	h.Broadcast(MsgSessions, SessionsData{Sessions: sessions, Version: h.version})
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
		c.conn.Close()
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
