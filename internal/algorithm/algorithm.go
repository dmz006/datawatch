// Package algorithm (BL258, v6.9.0) implements the PAI 7-phase
// Algorithm Mode as a per-session harness.
//
// Phases:
//
//	Observe   — gather raw context: code, logs, environment.
//	Orient    — make sense of the context: dependencies, constraints, prior art.
//	Decide    — choose an approach.
//	Act       — execute the chosen approach.
//	Measure   — quantify the outcome (tests, benchmarks, eval graders).
//	Learn     — distill what was learned.
//	Improve   — fold the lesson back into rules / docs / future approaches.
//
// State is stored in-memory per session ID. Operator advances through
// phases via REST/MCP/CLI/comm/PWA. Phase output (free-form string) is
// captured at each gate.
//
// PAI source: docs/plans/2026-05-02-pai-comparison-analysis.md §2 +
// Recommendation H1. The shipped Guided Mode (BL221) is a 5-phase
// PRD-only subset; Algorithm Mode is the strict superset applicable
// to any session.
package algorithm

import (
	"fmt"
	"sync"
	"time"
)

// Phase identifies one of the 7 algorithm phases. Order matters:
// Phases() returns the canonical sequence and Next/Prev implement the
// state machine.
type Phase string

const (
	Observe Phase = "observe"
	Orient  Phase = "orient"
	Decide  Phase = "decide"
	Act     Phase = "act"
	Measure Phase = "measure"
	Learn   Phase = "learn"
	Improve Phase = "improve"
)

// Phases returns the canonical phase order.
func Phases() []Phase {
	return []Phase{Observe, Orient, Decide, Act, Measure, Learn, Improve}
}

// IsValid reports whether the given string is one of the seven phases.
func IsValid(p Phase) bool {
	switch p {
	case Observe, Orient, Decide, Act, Measure, Learn, Improve:
		return true
	}
	return false
}

// PhaseOutput is the operator-captured (or LLM-emitted) text for one
// phase boundary. Includes a timestamp so the audit trail records when
// each gate was crossed.
type PhaseOutput struct {
	Phase     Phase     `json:"phase"`
	Output    string    `json:"output,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// State is the per-session algorithm record.
type State struct {
	SessionID string         `json:"session_id"`
	Current   Phase          `json:"current"`
	History   []PhaseOutput  `json:"history,omitempty"`
	StartedAt time.Time      `json:"started_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Aborted   bool           `json:"aborted,omitempty"`
}

// Tracker maintains in-memory state for every session running in
// Algorithm Mode. Concurrent-safe.
type Tracker struct {
	mu    sync.RWMutex
	state map[string]*State
}

// NewTracker creates an empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{state: make(map[string]*State)}
}

// Start registers a session in Algorithm Mode beginning at the Observe
// phase. Returns the new state. Idempotent — existing state is
// returned unchanged.
func (t *Tracker) Start(sessionID string) *State {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.state[sessionID]; ok {
		return s
	}
	now := time.Now().UTC()
	s := &State{
		SessionID: sessionID,
		Current:   Observe,
		History:   []PhaseOutput{},
		StartedAt: now,
		UpdatedAt: now,
	}
	t.state[sessionID] = s
	return s
}

// Get returns the state for sessionID. Returns nil when the session is
// not in Algorithm Mode.
func (t *Tracker) Get(sessionID string) *State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if s, ok := t.state[sessionID]; ok {
		// return a copy so callers can't mutate
		cp := *s
		cp.History = append([]PhaseOutput(nil), s.History...)
		return &cp
	}
	return nil
}

// All returns a snapshot of every tracked session. Used by REST list.
func (t *Tracker) All() []*State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*State, 0, len(t.state))
	for _, s := range t.state {
		cp := *s
		cp.History = append([]PhaseOutput(nil), s.History...)
		out = append(out, &cp)
	}
	return out
}

// Advance closes the current phase by recording its output, then moves
// to the next phase. Returns the updated state. Errors when the
// session isn't in Algorithm Mode or already completed/aborted.
func (t *Tracker) Advance(sessionID string, output string) (*State, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.state[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not in Algorithm Mode", sessionID)
	}
	if s.Aborted {
		return nil, fmt.Errorf("session %s aborted", sessionID)
	}
	now := time.Now().UTC()
	s.History = append(s.History, PhaseOutput{Phase: s.Current, Output: output, Timestamp: now})
	next, ok := nextPhase(s.Current)
	if !ok {
		// Already at Improve — no-op; the operator can call Reset to start over.
		s.UpdatedAt = now
		cp := *s
		cp.History = append([]PhaseOutput(nil), s.History...)
		return &cp, nil
	}
	s.Current = next
	s.UpdatedAt = now
	cp := *s
	cp.History = append([]PhaseOutput(nil), s.History...)
	return &cp, nil
}

// Edit replaces the output captured at the most recent gate (the
// previous phase). Useful when the operator wants to revise the
// recorded answer without rewinding the state machine.
func (t *Tracker) Edit(sessionID string, output string) (*State, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.state[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not in Algorithm Mode", sessionID)
	}
	if len(s.History) == 0 {
		return nil, fmt.Errorf("no phase output recorded yet")
	}
	s.History[len(s.History)-1].Output = output
	s.UpdatedAt = time.Now().UTC()
	cp := *s
	cp.History = append([]PhaseOutput(nil), s.History...)
	return &cp, nil
}

// Abort marks the session as terminated mid-algorithm. Subsequent
// Advance calls error.
func (t *Tracker) Abort(sessionID string) (*State, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.state[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %s not in Algorithm Mode", sessionID)
	}
	s.Aborted = true
	s.UpdatedAt = time.Now().UTC()
	cp := *s
	cp.History = append([]PhaseOutput(nil), s.History...)
	return &cp, nil
}

// Reset removes the session entirely so a fresh Start can begin from
// Observe.
func (t *Tracker) Reset(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.state, sessionID)
}

// nextPhase returns the next phase in canonical order, or false when
// already at Improve.
func nextPhase(p Phase) (Phase, bool) {
	all := Phases()
	for i, c := range all {
		if c == p && i+1 < len(all) {
			return all[i+1], true
		}
	}
	return p, false
}

// IndexOf returns the 0-based ordinal of phase p (0=Observe, 6=Improve)
// or -1 when invalid.
func IndexOf(p Phase) int {
	for i, c := range Phases() {
		if c == p {
			return i
		}
	}
	return -1
}

// AdvanceWithEval is BL259 Phase 2 (v6.10.1) — close the current phase
// with an eval-suite summary as its output, then advance. The output
// argument is the human-readable summary; eval JSON details are
// captured by the caller and stored separately.
//
// Stays inside the algorithm package to avoid circular deps with
// internal/evals; the orchestration lives in the server REST handler.
func (t *Tracker) AdvanceWithEval(sessionID, output string) (*State, error) {
	return t.Advance(sessionID, output)
}
