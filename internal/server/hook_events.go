// v7.0.0-alpha.34 #202 — Claude Code hook event ingestion + per-session
// status board state.
//
// Endpoints:
//
//	POST /api/sessions/<id>/hook-event                 — hook script POSTs an event
//	GET  /api/sessions/<id>/status                     — latest hook state + derived board
//
// Hook event shape (operator-spec'd .claude/sprint/state.json shape):
//
//	{
//	  "event":          "Stop" | "PostToolUse" | "UserPromptSubmit" | "SubagentStop" | ...
//	  "ts":             "RFC3339",
//	  "tool":           "Bash" | "Read" | "Edit" | ...    (PostToolUse only)
//	  "current_task":   "implement story 3 task 2"        (free text)
//	  "last_prompt":    "...",                            (UserPromptSubmit)
//	  "last_assistant": "...",                            (Stop)
//	  "sprint":         { ... },                          (whole .claude/sprint/state.json)
//	  "tests":          { "pass": 15, "fail": 0, ... },   (optional)
//	  "git":            { "branch": "...", "dirty": true } (optional)
//	}
//
// Detection augmentation + alert enrichment based on these events lands
// in alpha.34b — this cut stores + serves them so the PWA Status tab
// has data to display.

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SessionHookEvent — one event from a session's hook script.
type SessionHookEvent struct {
	SessionID  string                 `json:"session_id"`
	Event      string                 `json:"event"`
	Tool       string                 `json:"tool,omitempty"`
	Timestamp  time.Time              `json:"ts"`
	Payload    map[string]any         `json:"payload,omitempty"` // arbitrary hook data
}

// SessionStatusBoard — derived board state for the PWA Status tab.
// Cards: current focus + PRD tree + tests + closed-tasks + council +
// skills + tracker + git.
type SessionStatusBoard struct {
	SessionID    string                 `json:"session_id"`
	State        string                 `json:"state"`       // derived: running | waiting | idle | unknown
	CurrentFocus map[string]any         `json:"current_focus,omitempty"`
	Sprint       map[string]any         `json:"sprint,omitempty"`
	Tests        map[string]any         `json:"tests,omitempty"`
	Git          map[string]any         `json:"git,omitempty"`
	LastEvent    *SessionHookEvent      `json:"last_event,omitempty"`
	IdleSince    *time.Time             `json:"idle_since,omitempty"`
	HookHealth   string                 `json:"hook_health"`   // alive | stale | missing
	UpdatedAt    time.Time              `json:"updated_at"`
}

// hookEventStore — in-memory per-session hook state. Latest payload
// wins for each `sprint`/`tests`/`git` field; history kept for last 50.
type hookEventStore struct {
	mu       sync.RWMutex
	state    map[string]*SessionStatusBoard
	history  map[string][]*SessionHookEvent
}

var globalHookStore = &hookEventStore{
	state:   map[string]*SessionStatusBoard{},
	history: map[string][]*SessionHookEvent{},
}

const hookHistoryMax = 50
const hookStaleAfter = 30 * time.Second

func (s *hookEventStore) record(sessionID string, ev *SessionHookEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board, ok := s.state[sessionID]
	if !ok {
		board = &SessionStatusBoard{SessionID: sessionID}
		s.state[sessionID] = board
	}
	board.LastEvent = ev
	board.UpdatedAt = time.Now().UTC()
	board.HookHealth = "alive"
	// Derive state from event kind.
	switch ev.Event {
	case "Stop", "SubagentStop":
		board.State = "waiting"
		now := time.Now().UTC()
		board.IdleSince = &now
	case "UserPromptSubmit":
		board.State = "running"
		board.IdleSince = nil
	case "PostToolUse", "PreToolUse":
		board.State = "running"
		board.IdleSince = nil
	}
	// Merge payload into typed fields.
	if cf, ok := ev.Payload["current_focus"].(map[string]any); ok {
		board.CurrentFocus = cf
	}
	if sp, ok := ev.Payload["sprint"].(map[string]any); ok {
		board.Sprint = sp
	}
	if ts, ok := ev.Payload["tests"].(map[string]any); ok {
		board.Tests = ts
	}
	if gt, ok := ev.Payload["git"].(map[string]any); ok {
		board.Git = gt
	}
	// History buffer.
	hist := s.history[sessionID]
	hist = append(hist, ev)
	if len(hist) > hookHistoryMax {
		hist = hist[len(hist)-hookHistoryMax:]
	}
	s.history[sessionID] = hist
}

func (s *hookEventStore) board(sessionID string) *SessionStatusBoard {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.state[sessionID]
	if !ok {
		return &SessionStatusBoard{
			SessionID:  sessionID,
			State:      "unknown",
			HookHealth: "missing",
			UpdatedAt:  time.Now().UTC(),
		}
	}
	c := *b
	// Recompute hook health on read.
	if !c.UpdatedAt.IsZero() && time.Since(c.UpdatedAt) > hookStaleAfter {
		c.HookHealth = "stale"
	}
	return &c
}

// handleSessionHookEvent — POST /api/sessions/<id>/hook-event
func (s *Server) handleSessionHookEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sid := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	sid = strings.TrimSuffix(sid, "/hook-event")
	if sid == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	var body struct {
		Event   string         `json:"event"`
		Tool    string         `json:"tool,omitempty"`
		Payload map[string]any `json:"payload,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Event == "" {
		http.Error(w, "event required", http.StatusBadRequest)
		return
	}
	ev := &SessionHookEvent{
		SessionID: sid,
		Event:     body.Event,
		Tool:      body.Tool,
		Timestamp: time.Now().UTC(),
		Payload:   body.Payload,
	}
	globalHookStore.record(sid, ev)
	writeJSONOK(w, map[string]any{"ok": true, "session_id": sid, "event": body.Event})
}

// handleSessionStatus — GET /api/sessions/<id>/status
func (s *Server) handleSessionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sid := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	sid = strings.TrimSuffix(sid, "/status")
	if sid == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	board := globalHookStore.board(sid)
	writeJSONOK(w, board)
}
