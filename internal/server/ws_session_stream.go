// WebSocket session-list negotiation and delta stream (BL432 / GH#242).
//
// Every session-list change used to go out to every WebSocket client as a
// full "sessions" frame — the whole list, multiple megabytes on a busy
// daemon. A client can now negotiate something smaller with a hello
// message, sent any time after connecting:
//
//	client → server  {"type":"hello","data":{"session_list":"delta","session_view":"summary"}}
//	server → client  {"type":"hello_ack","data":{"session_list":"delta","session_view":"summary","capabilities":[...]}}
//
// session_list:
//   - "full"  — the legacy stream: a "sessions" frame with the whole list
//     on every change (plus the existing per-change "session_state").
//   - "delta" — no "sessions" frames at all. Instead, one "session_state"
//     {session} per session that is new or changed since the last update,
//     and one "session_removed" {session_id} per session that left the
//     list. A delta client must fetch the initial list itself over REST
//     (GET /api/sessions?view=summary) right after hello_ack, then apply
//     the stream on top. Frames received before hello_ack can be ignored.
//   - "window" — the bounded form (capability ws.session_list_window).
//     After hello_ack the server sends exactly one "sessions" frame whose
//     data.sessions are summary rows (the GET /api/sessions?view=summary
//     row shape, scheduled_count included) for the default-view sessions
//     only, plus data.version, data.window_minutes (the window below),
//     data.total (sessions the daemon holds) and data.more (true when the
//     frame omits some). After that there is no "sessions" frame at all:
//     every change is one "session_state" {session:<summary row>} for the
//     changed session. "session_removed" is sent only when a session is
//     deleted, never because it aged out of the window.
//
// Default view and the window: a session is in the default view when it is
// not finished, or when it is finished (complete, failed or killed, the
// PWA's DONE_STATES) and was updated within the last
// server.recent_session_minutes minutes (a value <= 0 means 5; the PWA
// reads the same setting). Finished sessions older than the window are
// never sent over the websocket, to any client mode; a client fetches them
// with GET /api/sessions?view=summary&limit=50, paged by cursor (and
// history=1 for the archive). The setting is read live (see
// ws_session_window.go), so PUT /api/config changes the window without a
// restart.
//
// Connect ordering (for client authors): a client can only say hello after
// the socket is open, so the connect-time frame is always the bounded
// legacy "sessions" frame described below — a window client receives it
// first and should treat it as provisional. The client's hello is answered
// with hello_ack, immediately followed by the one summary-row "sessions"
// frame; that frame is the window client's initial list and replaces
// whatever the connect-time frame held. A hello that selects window again
// is answered the same way (a fresh list). A hello that does not select
// window sends no rows frame.
//
// A client that does not announce window (no hello, or session_list "full")
// keeps the legacy "sessions" frame and the legacy full session_state, but
// the frame is bounded: only default-view sessions, long text fields cut by
// summarizeSession at server.session_summary_text_chars (so
// truncated_fields is set, as for the summary view), plus data.more,
// data.total and data.window_minutes.
//
// session_view:
//   - "full"    — session_state carries the full session record.
//   - "summary" — session_state carries the compact record used by
//     GET /api/sessions?view=summary (long text fields cut to
//     server.session_summary_text_chars, plus truncated_fields). It
//     applies to session_state frames only; "sessions" frames stay full.
//
// Unknown hello keys are ignored, and an unknown value leaves that mode
// unchanged, so later options can be added without breaking older
// daemons; hello_ack always echoes the effective modes. The shape is
// client-neutral: the PWA, Android and iOS can all adopt it.
//
// A client that never sends hello gets server.ws_session_list_default
// ("full" unless configured). That key is read from the live config each
// time a session update is broadcast, so PUT /api/config applies to such
// clients without a reconnect. When the default is "delta", the usual
// connect-time "sessions" frame is skipped as well.
//
// Diff detection: the hub keeps a snapshot FullID → fingerprint, where the
// fingerprint is an FNV-64a hash of the session's JSON encoding. Any
// change to a serialized field — rename, state, needs-input prompt,
// updated_at, last response — therefore produces a session_state. The
// snapshot is maintained on every BroadcastSessions call even while no
// delta client is connected, so a client that joins later is diffed
// against the true previous state. BroadcastSessionState (the per-change
// call in NotifyStateChange) goes through the same snapshot, so a delta
// client never receives the same unchanged session twice.

package server

import (
	"encoding/json"
	"hash/fnv"
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// Negotiable hello modes.
const (
	sessionListFull   = "full"
	sessionListDelta  = "delta"
	sessionListWindow = "window"

	sessionViewFull    = "full"
	sessionViewSummary = "summary"
)

// HelloData is the client → server hello payload.
type HelloData struct {
	SessionList string `json:"session_list,omitempty"`
	SessionView string `json:"session_view,omitempty"`
	Output      string `json:"output,omitempty"` // "all" | "subscribed"; see ws_output_scope.go
}

// HelloAckData is the server → client hello_ack payload.
type HelloAckData struct {
	SessionList  string   `json:"session_list"`
	SessionView  string   `json:"session_view"`
	Output       string   `json:"output"`
	Capabilities []string `json:"capabilities"`
}

// SessionRemovedData is the session_removed payload for delta clients.
type SessionRemovedData struct {
	SessionID string `json:"session_id"`
}

// sessionSummaryStateData is session_state for summary-view clients; the
// record has the same shape as one element of GET /api/sessions?view=summary
// (without scheduled_count, which the hub cannot see).
type sessionSummaryStateData struct {
	Session sessionListRecord `json:"session"`
}

// normalizeSessionListMode returns the mode, or "" if it is not one.
func normalizeSessionListMode(v string) string {
	switch v {
	case sessionListFull, sessionListDelta, sessionListWindow:
		return v
	}
	return ""
}

// normalizeSessionListDefault is normalizeSessionListMode for the
// server.ws_session_list_default setting: a client that never said hello
// cannot be a window client, so window is not a valid default.
func normalizeSessionListDefault(v string) string {
	if v == sessionListWindow {
		return ""
	}
	return normalizeSessionListMode(v)
}

// normalizeSessionViewMode returns the view, or "" if it is not one.
func normalizeSessionViewMode(v string) string {
	switch v {
	case sessionViewFull, sessionViewSummary:
		return v
	}
	return ""
}

// setSessionStreamConfig wires the live-config readers. Must be called
// before Run starts.
func (h *Hub) setSessionStreamConfig(listDefault func() string, textChars func() int) {
	h.sessionListDefaultFn = listDefault
	h.summaryTextCharsFn = textChars
}

// defaultSessionList is the mode for clients that never chose one.
func (h *Hub) defaultSessionList() string {
	if h.sessionListDefaultFn != nil {
		if v := normalizeSessionListDefault(h.sessionListDefaultFn()); v != "" {
			return v
		}
	}
	return sessionListFull
}

func (h *Hub) summaryTextChars() int {
	if h.summaryTextCharsFn != nil {
		return h.summaryTextCharsFn()
	}
	return defaultSessionSummaryTextChars
}

// sessionModes returns c's effective session_list and session_view.
func (h *Hub) sessionModes(c *client) (list, view string) {
	c.mu.Lock()
	list, view = c.sessionList, c.sessionView
	c.mu.Unlock()
	if list == "" {
		list = h.defaultSessionList()
	}
	if view == "" {
		view = sessionViewFull
	}
	return list, view
}

func (h *Hub) isDeltaClient(c *client) bool {
	list, _ := h.sessionModes(c)
	return list == sessionListDelta
}

func (h *Hub) isSummaryClient(c *client) bool {
	_, view := h.sessionModes(c)
	return view == sessionViewSummary
}

// hasClient reports whether any connected client matches want. Not for
// use inside a want predicate (it takes h.mu).
func (h *Hub) hasClient(want func(*client) bool) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if want(c) {
			return true
		}
	}
	return false
}

// sessionFingerprint hashes the session's JSON encoding.
func sessionFingerprint(sess *session.Session) (uint64, bool) {
	b, err := json.Marshal(sess)
	if err != nil {
		return 0, false
	}
	f := fnv.New64a()
	_, _ = f.Write(b)
	return f.Sum64(), true
}

// BroadcastSessions sends the full session list to full-mode clients and
// the per-session diff to delta-mode clients. Call sites pass the
// manager's whole list; they need no knowledge of client modes.
func (h *Hub) BroadcastSessions(sessions []*session.Session) {
	h.broadcastTo(MsgSessions, SessionsData{Sessions: sessions, Version: h.version},
		func(c *client) bool { return !h.isDeltaClient(c) })

	// The lock is held while queueing so frames reach the hub in the same
	// order as snapshot updates.
	h.snapMu.Lock()
	defer h.snapMu.Unlock()
	next := make(map[string]uint64, len(sessions))
	var changed []*session.Session
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		fp, ok := sessionFingerprint(sess)
		if !ok {
			continue
		}
		next[sess.FullID] = fp
		if old, seen := h.snapshot[sess.FullID]; !seen || old != fp {
			changed = append(changed, sess)
		}
	}
	var removed []string
	for id := range h.snapshot {
		if _, ok := next[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	h.snapshot = next

	if !h.hasClient(h.isDeltaClient) {
		return
	}
	for _, sess := range changed {
		h.sendSessionState(sess, h.isDeltaClient)
	}
	for _, id := range removed {
		h.broadcastTo(MsgSessionRemoved, SessionRemovedData{SessionID: id}, h.isDeltaClient)
	}
}

// BroadcastSessionState (v8.37.0, GH#162) sends a single updated session.
// Full-mode clients always get it, alongside the full list (see
// HTTPServer.NotifyStateChange), exactly as before. Delta-mode clients get
// it only when it differs from the snapshot, so the session_state that
// BroadcastSessions already streamed for the same change is not repeated.
func (h *Hub) BroadcastSessionState(sess *session.Session) {
	if sess == nil {
		return
	}
	h.snapMu.Lock()
	defer h.snapMu.Unlock()
	changed := true
	if fp, ok := sessionFingerprint(sess); ok {
		if old, seen := h.snapshot[sess.FullID]; seen && old == fp {
			changed = false
		} else {
			if h.snapshot == nil {
				h.snapshot = make(map[string]uint64)
			}
			h.snapshot[sess.FullID] = fp
		}
	}
	h.sendSessionState(sess, func(c *client) bool { return changed || !h.isDeltaClient(c) })
}

// sendSessionState queues session_state for the clients want selects,
// in the full or summary representation each client negotiated.
func (h *Hub) sendSessionState(sess *session.Session, want func(*client) bool) {
	h.broadcastTo(MsgSessionState, SessionStateData{Session: sess},
		func(c *client) bool { return want(c) && !h.isSummaryClient(c) })
	summaryWant := func(c *client) bool { return want(c) && h.isSummaryClient(c) }
	if !h.hasClient(summaryWant) {
		return
	}
	rec := sessionListRecord{}
	rec.Session, rec.TruncatedFields = summarizeSession(sess, h.summaryTextChars())
	h.broadcastTo(MsgSessionState, sessionSummaryStateData{Session: rec}, summaryWant)
}

// handleHello applies a client's hello and replies with hello_ack.
func (h *Hub) handleHello(c *client, raw json.RawMessage) {
	var d HelloData
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &d) // malformed data → no change, still ack
	}
	wantWindow := false
	c.mu.Lock()
	if v := normalizeSessionListMode(d.SessionList); v != "" {
		c.sessionList = v
		wantWindow = v == sessionListWindow
	}
	if v := normalizeSessionViewMode(d.SessionView); v != "" {
		c.sessionView = v
	}
	if v := normalizeOutputScope(d.Output); v != "" {
		c.outputScope = v
	}
	c.mu.Unlock()
	list, view := h.sessionModes(c)
	ackRaw, _ := json.Marshal(HelloAckData{SessionList: list, SessionView: view, Output: h.outputScope(c), Capabilities: wsCapabilities()})
	c.safeSend(mustMarshal(WSMessage{Type: MsgHelloAck, Data: ackRaw, Timestamp: time.Now()}))
	if wantWindow {
		c.safeSend(h.windowSessionsFrame())
	}
}

// wsSessionListDefault reads server.ws_session_list_default from the live
// config; anything unset or invalid means "full".
func (s *Server) wsSessionListDefault() string {
	if s.cfg != nil {
		if v := normalizeSessionListDefault(s.cfg.Server.WSSessionListDefault); v != "" {
			return v
		}
	}
	return sessionListFull
}

// wsSummaryTextChars is the text length for summary-view session_state,
// shared with GET /api/sessions?view=summary.
func (s *Server) wsSummaryTextChars() int {
	_, n := s.sessionSummaryLimits()
	return n
}

// wireHubSessionStream connects the hub to the live config.
func (s *Server) wireHubSessionStream() {
	if s.hub != nil {
		s.hub.setSessionStreamConfig(s.wsSessionListDefault, s.wsSummaryTextChars)
		s.hub.setSessionWindowConfig(s.recentSessionWindow, s.sessionSummaryRow, func() []*session.Session {
			if s.manager == nil {
				return nil
			}
			return s.manager.ListSessions()
		})
		s.hub.outputScopeDefaultFn = s.wsOutputScopeDefault
	}
}
