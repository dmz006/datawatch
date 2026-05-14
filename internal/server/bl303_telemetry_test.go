// BL303 S1 — unit tests for the ephemeral telemetry store.
// Covers: task transition stamping, failed task event buffer,
// telemetry merge, and the /api/sessions/{id}/telemetry endpoint.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// makeStore returns a fresh store with no global side effects.
func makeStore() *hookEventStore {
	return &hookEventStore{
		state:     map[string]*SessionStatusBoard{},
		history:   map[string][]*SessionHookEvent{},
		failedBuf: map[string][]*SessionHookEvent{},
	}
}

func makeEv(event, tool string, payload map[string]any) *SessionHookEvent {
	return &SessionHookEvent{
		SessionID: "test-session",
		Event:     event,
		Tool:      tool,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

// TestTelemetryTaskTransitionStamping verifies daemon-side timing stamps.
func TestTelemetryTaskTransitionStamping(t *testing.T) {
	s := makeStore()
	sid := "test-session"

	// Emit task as pending.
	s.record(sid, makeEv("PostToolUse", "TodoWrite", map[string]any{
		"tasks": []any{
			map[string]any{"id": "t1", "title": "Task One", "status": "pending"},
		},
	}))

	board := s.board(sid)
	if board.Telemetry == nil {
		t.Fatal("telemetry nil after first event")
	}
	if len(board.Telemetry.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(board.Telemetry.Tasks))
	}
	if board.Telemetry.Tasks[0].StartedAt != nil {
		t.Error("pending task should not have StartedAt")
	}

	// Transition to in_progress.
	before := time.Now().UTC()
	s.record(sid, makeEv("PostToolUse", "Bash", map[string]any{
		"tasks": []any{
			map[string]any{"id": "t1", "title": "Task One", "status": "in_progress"},
		},
	}))
	after := time.Now().UTC()

	board = s.board(sid)
	tk := board.Telemetry.Tasks[0]
	if tk.Status != "in_progress" {
		t.Errorf("expected in_progress, got %s", tk.Status)
	}
	if tk.StartedAt == nil {
		t.Fatal("StartedAt not stamped on in_progress transition")
	}
	if tk.StartedAt.Before(before) || tk.StartedAt.After(after) {
		t.Errorf("StartedAt %v outside expected window [%v, %v]", tk.StartedAt, before, after)
	}

	// Transition to completed.
	s.record(sid, makeEv("PostToolUse", "Bash", map[string]any{
		"tasks": []any{
			map[string]any{"id": "t1", "title": "Task One", "status": "completed"},
		},
	}))

	board = s.board(sid)
	tk = board.Telemetry.Tasks[0]
	if tk.Status != "completed" {
		t.Errorf("expected completed, got %s", tk.Status)
	}
	if tk.CompletedAt == nil {
		t.Fatal("CompletedAt not stamped on completed transition")
	}
	if tk.DurationMs < 0 {
		t.Errorf("expected non-negative DurationMs, got %d", tk.DurationMs)
	}
	if !tk.CompletedAt.After(*tk.StartedAt) && !tk.CompletedAt.Equal(*tk.StartedAt) {
		t.Errorf("CompletedAt %v should be >= StartedAt %v", tk.CompletedAt, tk.StartedAt)
	}
}

// TestTelemetryFailedTaskBuffer verifies last-5 event capture on failure.
func TestTelemetryFailedTaskBuffer(t *testing.T) {
	s := makeStore()
	sid := "test-session"

	// Seed history with 8 events.
	for i := 0; i < 8; i++ {
		s.record(sid, makeEv("PostToolUse", "Bash", map[string]any{
			"tasks": []any{
				map[string]any{"id": "t1", "title": "Task One", "status": "in_progress"},
			},
		}))
	}

	// Transition to failed.
	s.record(sid, makeEv("PostToolUse", "Bash", map[string]any{
		"tasks": []any{
			map[string]any{"id": "t1", "title": "Task One", "status": "failed"},
		},
	}))

	board := s.board(sid)
	buf := board.Telemetry.FailedTaskBuf
	if len(buf) != failedBufMax {
		t.Errorf("expected %d events in failed buf, got %d", failedBufMax, len(buf))
	}
}

// TestTelemetryMultipleTasks verifies independent tracking of multiple tasks.
func TestTelemetryMultipleTasks(t *testing.T) {
	s := makeStore()
	sid := "test-session"

	s.record(sid, makeEv("PostToolUse", "TodoWrite", map[string]any{
		"tasks": []any{
			map[string]any{"id": "t1", "title": "Alpha", "status": "pending"},
			map[string]any{"id": "t2", "title": "Beta", "status": "pending"},
		},
	}))
	s.record(sid, makeEv("PostToolUse", "Bash", map[string]any{
		"tasks": []any{
			map[string]any{"id": "t1", "title": "Alpha", "status": "in_progress"},
		},
	}))
	s.record(sid, makeEv("PostToolUse", "Bash", map[string]any{
		"tasks": []any{
			map[string]any{"id": "t1", "title": "Alpha", "status": "completed"},
			map[string]any{"id": "t2", "title": "Beta", "status": "in_progress"},
		},
	}))

	board := s.board(sid)
	if len(board.Telemetry.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(board.Telemetry.Tasks))
	}
	byID := map[string]TelemetryTask{}
	for _, tk := range board.Telemetry.Tasks {
		byID[tk.ID] = tk
	}
	if byID["t1"].Status != "completed" {
		t.Errorf("t1 expected completed, got %s", byID["t1"].Status)
	}
	if byID["t2"].Status != "in_progress" {
		t.Errorf("t2 expected in_progress, got %s", byID["t2"].Status)
	}
	if byID["t1"].CompletedAt == nil {
		t.Error("t1 CompletedAt not stamped")
	}
	if byID["t2"].StartedAt == nil {
		t.Error("t2 StartedAt not stamped")
	}
}

// TestTelemetryScalarFields verifies non-task payload fields are merged.
func TestTelemetryScalarFields(t *testing.T) {
	s := makeStore()
	sid := "test-session"

	s.record(sid, makeEv("PostToolUse", "Read", map[string]any{
		"current_task":     "implement feature",
		"file":             "main.go",
		"progress":         float64(42),
		"parent_session_id": "parent-abc",
		"sprint": map[string]any{
			"name": "Sprint 1",
			"id":   "s1",
		},
		"guardrail_verdicts": []any{
			map[string]any{
				"guardrail": "sast-scan",
				"outcome":   "pass",
				"summary":   "no issues",
			},
		},
	}))

	board := s.board(sid)
	tel := board.Telemetry
	if tel == nil {
		t.Fatal("telemetry nil")
	}
	if tel.CurrentTask != "implement feature" {
		t.Errorf("CurrentTask: got %q", tel.CurrentTask)
	}
	if tel.Tool != "Read" {
		t.Errorf("Tool: got %q", tel.Tool)
	}
	if tel.File != "main.go" {
		t.Errorf("File: got %q", tel.File)
	}
	if tel.Progress != 42 {
		t.Errorf("Progress: got %v", tel.Progress)
	}
	if tel.ParentSessionID != "parent-abc" {
		t.Errorf("ParentSessionID: got %q", tel.ParentSessionID)
	}
	if tel.Sprint == nil || tel.Sprint["name"] != "Sprint 1" {
		t.Errorf("Sprint not merged: %v", tel.Sprint)
	}
	if len(tel.GuardrailVerdicts) != 1 || tel.GuardrailVerdicts[0].Guardrail != "sast-scan" {
		t.Errorf("GuardrailVerdicts not merged: %v", tel.GuardrailVerdicts)
	}
}

// TestTelemetryEndpoint verifies GET /api/sessions/{id}/telemetry HTTP handler.
func TestTelemetryEndpoint(t *testing.T) {
	// Seed global store with a known session.
	sid := "endpoint-test-session"
	globalHookStore.record(sid, &SessionHookEvent{
		SessionID: sid,
		Event:     "PostToolUse",
		Tool:      "Bash",
		Timestamp: time.Now().UTC(),
		Payload: map[string]any{
			"current_task": "running endpoint test",
			"tasks": []any{
				map[string]any{"id": "e1", "title": "Endpoint Task", "status": "in_progress"},
			},
		},
	})

	srv := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sid+"/telemetry", nil)
	w := httptest.NewRecorder()
	srv.handleSessionTelemetry(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var tel SessionTelemetry
	if err := json.NewDecoder(w.Body).Decode(&tel); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if tel.CurrentTask != "running endpoint test" {
		t.Errorf("CurrentTask: got %q", tel.CurrentTask)
	}
	if len(tel.Tasks) != 1 || tel.Tasks[0].ID != "e1" {
		t.Errorf("Tasks not returned: %v", tel.Tasks)
	}

	// Clean up global store.
	globalHookStore.mu.Lock()
	delete(globalHookStore.state, sid)
	delete(globalHookStore.history, sid)
	globalHookStore.mu.Unlock()
}

// TestTelemetryEndpointUnknownSession verifies empty telemetry for unknown session.
func TestTelemetryEndpointUnknownSession(t *testing.T) {
	srv := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/no-such-session/telemetry", nil)
	w := httptest.NewRecorder()
	srv.handleSessionTelemetry(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var tel SessionTelemetry
	if err := json.NewDecoder(w.Body).Decode(&tel); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tel.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set even for unknown session")
	}
}

// TestTelemetryEndpointMethodNotAllowed verifies POST is rejected.
func TestTelemetryEndpointMethodNotAllowed(t *testing.T) {
	srv := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/x/telemetry",
		strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	srv.handleSessionTelemetry(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}
