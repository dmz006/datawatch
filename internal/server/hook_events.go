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

// TelemetryTask — one task entry from the tasks[] array in a hook payload.
// Status values: pending | in_progress | completed | failed.
// Daemon stamps started_at/completed_at/duration_ms on transitions.
type TelemetryTask struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Status      string     `json:"status"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	DurationMs  int64      `json:"duration_ms,omitempty"`
}

// HookGuardrailVerdict — one guardrail result from the hook payload.
type HookGuardrailVerdict struct {
	Guardrail string `json:"guardrail"`
	Outcome   string `json:"outcome"` // pass | warn | block
	Summary   string `json:"summary,omitempty"`
}

// SessionTelemetry — structured telemetry derived from hook payloads.
// Accumulated in-memory per session; returned by GET /api/sessions/{id}/telemetry.
// Wiped when the session is deleted (ephemeral by design; persist_telemetry_on_stop
// config flag will flush to episodic memory on Stop — T06).
type SessionTelemetry struct {
	CurrentTask       string                 `json:"current_task,omitempty"`
	Tool              string                 `json:"tool,omitempty"`
	File              string                 `json:"file,omitempty"`
	Sprint            map[string]any         `json:"sprint,omitempty"`
	Tasks             []TelemetryTask        `json:"tasks,omitempty"`
	Tests             map[string]any         `json:"tests,omitempty"`
	Progress          float64                `json:"progress,omitempty"`
	GuardrailVerdicts []HookGuardrailVerdict `json:"guardrail_verdicts,omitempty"`
	ParentSessionID   string                 `json:"parent_session_id,omitempty"`
	FailedTaskBuf     []*SessionHookEvent    `json:"failed_task_buf,omitempty"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

// SessionStatusBoard — derived board state for the PWA Status tab.
// Cards: current focus + Automata tree + tests + closed-tasks + council +
// skills + tracker + git.
type SessionStatusBoard struct {
	SessionID    string             `json:"session_id"`
	State        string             `json:"state"`      // derived: running | waiting | idle | unknown
	CurrentFocus map[string]any     `json:"current_focus,omitempty"`
	Sprint       map[string]any     `json:"sprint,omitempty"`
	Tests        map[string]any     `json:"tests,omitempty"`
	Git          map[string]any     `json:"git,omitempty"`
	LastEvent    *SessionHookEvent  `json:"last_event,omitempty"`
	IdleSince    *time.Time         `json:"idle_since,omitempty"`
	HookHealth   string             `json:"hook_health"` // alive | stale | missing
	Telemetry    *SessionTelemetry  `json:"telemetry,omitempty"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

// hookEventStore — in-memory per-session hook state. Latest payload
// wins for each `sprint`/`tests`/`git` field; history kept for last 50.
type hookEventStore struct {
	mu        sync.RWMutex
	state     map[string]*SessionStatusBoard
	history   map[string][]*SessionHookEvent
	failedBuf map[string][]*SessionHookEvent // last 5 events before any task→failed
}

var globalHookStore = &hookEventStore{
	state:     map[string]*SessionStatusBoard{},
	history:   map[string][]*SessionHookEvent{},
	failedBuf: map[string][]*SessionHookEvent{},
}

const hookHistoryMax = 50
const failedBufMax = 5
// hookStaleAfter — alpha.36 GATE (operator 2026-05-10): bumped from 30s
// to 5 min. The 30s threshold was too aggressive for real-world tool
// cadence — a long Thinking turn (no PostToolUse) trivially exceeds 30s
// and falsely marked otherwise-installed hooks as "stale". 5 min better
// matches actual usage where the operator wants to know hooks aren't
// firing at all, not that the agent is just thinking hard.
const hookStaleAfter = 5 * time.Minute

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
	// Merge legacy map fields for backward compat with older hook scripts.
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
	// History buffer — update BEFORE mergeTelemetry so failed task buffer
	// captures the current event as context.
	hist := s.history[sessionID]
	hist = append(hist, ev)
	if len(hist) > hookHistoryMax {
		hist = hist[len(hist)-hookHistoryMax:]
	}
	s.history[sessionID] = hist
	// Structured telemetry — merge fields and stamp task transitions.
	if ev.Payload != nil {
		s.mergeTelemetry(board, ev)
	}
}

// mergeTelemetry extracts structured fields from the hook payload and
// merges them into board.Telemetry. Task transitions are daemon-stamped
// here: client sends status string; daemon computes started_at /
// completed_at / duration_ms by diffing successive payloads.
// Must be called with s.mu held.
func (s *hookEventStore) mergeTelemetry(board *SessionStatusBoard, ev *SessionHookEvent) {
	if board.Telemetry == nil {
		board.Telemetry = &SessionTelemetry{}
	}
	t := board.Telemetry
	now := time.Now().UTC()
	t.UpdatedAt = now

	if ct, ok := ev.Payload["current_task"].(string); ok && ct != "" {
		t.CurrentTask = ct
	}
	if ev.Tool != "" {
		t.Tool = ev.Tool
	}
	if f, ok := ev.Payload["file"].(string); ok && f != "" {
		t.File = f
	}
	if sp, ok := ev.Payload["sprint"].(map[string]any); ok {
		t.Sprint = sp
	}
	if ts, ok := ev.Payload["tests"].(map[string]any); ok {
		t.Tests = ts
	}
	if p, ok := ev.Payload["progress"].(float64); ok {
		t.Progress = p
	}
	if pid, ok := ev.Payload["parent_session_id"].(string); ok && pid != "" {
		t.ParentSessionID = pid
	}

	// Guardrail verdicts — replace on each event that carries them.
	if gvRaw, ok := ev.Payload["guardrail_verdicts"].([]any); ok {
		verdicts := make([]HookGuardrailVerdict, 0, len(gvRaw))
		for _, v := range gvRaw {
			if m, ok := v.(map[string]any); ok {
				var gv HookGuardrailVerdict
				gv.Guardrail, _ = m["guardrail"].(string)
				gv.Outcome, _ = m["outcome"].(string)
				gv.Summary, _ = m["summary"].(string)
				verdicts = append(verdicts, gv)
			}
		}
		t.GuardrailVerdicts = verdicts
	}

	// Tasks — merge by ID, stamp transitions server-side.
	tasksRaw, _ := ev.Payload["tasks"].([]any)
	if len(tasksRaw) == 0 {
		return
	}
	idx := make(map[string]int, len(t.Tasks))
	for i, tk := range t.Tasks {
		idx[tk.ID] = i
	}
	for _, raw := range tasksRaw {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		title, _ := m["title"].(string)
		status, _ := m["status"].(string)

		if i, exists := idx[id]; exists {
			prev := t.Tasks[i].Status
			t.Tasks[i].Title = title
			if prev != status {
				switch {
				case status == "in_progress" && t.Tasks[i].StartedAt == nil:
					ts := now
					t.Tasks[i].StartedAt = &ts
				case (status == "completed" || status == "failed") && t.Tasks[i].CompletedAt == nil:
					ts := now
					t.Tasks[i].CompletedAt = &ts
					if t.Tasks[i].StartedAt != nil {
						t.Tasks[i].DurationMs = now.Sub(*t.Tasks[i].StartedAt).Milliseconds()
					}
				}
				t.Tasks[i].Status = status
				if status == "failed" {
					hist := s.history[board.SessionID]
					start := len(hist) - failedBufMax
					if start < 0 {
						start = 0
					}
					buf := make([]*SessionHookEvent, len(hist[start:]))
					copy(buf, hist[start:])
					s.failedBuf[board.SessionID] = buf
					t.FailedTaskBuf = buf
				}
			}
		} else {
			tk := TelemetryTask{ID: id, Title: title, Status: status}
			if status == "in_progress" {
				ts := now
				tk.StartedAt = &ts
			}
			t.Tasks = append(t.Tasks, tk)
			idx[id] = len(t.Tasks) - 1
		}
	}
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

// RecordHookEvent — alpha.34d #281. Public entry point for in-process
// callers (state-change handler, opencode-acp adapter, council runner,
// autonomous executor, ollama-direct dispatcher) to publish a hook
// event without going through the HTTP endpoint. Same store as
// /api/sessions/<id>/hook-event so the Status board, alert enrichment,
// and (future) detection augmentation all see them uniformly.
//
// Operator-rule (feedback_hook_parity_rule.md): every internally-
// controlled session backend MUST emit Start / Activity / Stop events.
// Use this from inside the daemon for backends without external hook
// scripts (everything except claude-code).
func RecordHookEvent(sessionID, event, tool string, payload map[string]any) {
	if sessionID == "" || event == "" {
		return
	}
	globalHookStore.record(sessionID, &SessionHookEvent{
		SessionID: sessionID,
		Event:     event,
		Tool:      tool,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	})
}

// HookContextForAlert — alpha.34b #279. Returns a compact one-line
// suffix to append to alert bodies for needs_input notifications, when
// hook event data is fresh (<2 min old). Empty string when no useful
// context. Format: " · last tool: Bash · last assistant: 'I've added…'"
func HookContextForAlert(sessionID string) string {
	board := globalHookStore.board(sessionID)
	if board == nil || board.LastEvent == nil {
		return ""
	}
	if time.Since(board.UpdatedAt) > 2*time.Minute {
		return ""
	}
	out := ""
	if board.LastEvent.Tool != "" {
		out += " · last tool: " + board.LastEvent.Tool
	}
	if board.LastEvent.Payload != nil {
		if la, ok := board.LastEvent.Payload["last_assistant"].(string); ok && la != "" {
			snippet := la
			if len(snippet) > 80 {
				snippet = snippet[:77] + "…"
			}
			out += " · " + snippet
		}
	}
	return out
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

// handleSessionTelemetry — GET /api/sessions/<id>/telemetry
// Returns the structured telemetry for the session: task list with
// daemon-stamped timings, guardrail verdicts, sprint ancestry, tests,
// and the last 5 hook events before any task failure (for drill-down).
func (s *Server) handleSessionTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sid := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	sid = strings.TrimSuffix(sid, "/telemetry")
	if sid == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	board := globalHookStore.board(sid)
	if board.Telemetry == nil {
		writeJSONOK(w, &SessionTelemetry{UpdatedAt: time.Now().UTC()})
		return
	}
	writeJSONOK(w, board.Telemetry)
}
