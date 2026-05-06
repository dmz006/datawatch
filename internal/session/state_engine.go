// Package session — channel-driven state engine (BL266, v6.11.24).
//
// Replaces v6.11.18→v6.11.23's natural-language phrase classifier as the
// PRIMARY state-change driver. Operator-directed (datawatch session
// 2026-05-05): "go with C" — structured signals where they exist
// (opencode-acp), event-rate fallback for everything else (claude-code
// MCP, ollama chat, etc.). NLP classifier is now advisory only — it can
// raise confidence on a transition that already passed the structural
// gate, but it cannot trigger state changes by itself.
//
// Three event types:
//   - EventRunning   — backend says it's working (busy / step-start /
//                      message.part.delta / pane content changed / operator
//                      typed input). Bumps LastChannelEventAt; if state was
//                      WaitingInput, transitions back to Running.
//   - EventIdle      — backend says it stopped (session.idle, ACP idle).
//                      Direct WaitingInput transition. Authoritative.
//   - EventComplete  — backend says the task is done (session.completed,
//                      message.completed, DATAWATCH_COMPLETE marker).
//                      Direct StateComplete transition.
//
// Plus the watcher: any Running session whose LastChannelEventAt is older
// than the configured gap (default 15 s) flips to WaitingInput. This is the
// universal fallback for backends with no structural idle signal.
//
// PWA-side: the LastChannelEventAt timestamp is broadcast as part of the
// Session payload so the UI can render an amber "stale comms" badge after
// ~2 s of silence without any server-side state change.

package session

import (
	"context"
	"strings"
	"time"
)

// ChannelEventKind enumerates the structural channel-event types the state
// engine understands. Distinct from natural-language signals.
type ChannelEventKind int

const (
	EventRunning ChannelEventKind = iota
	EventIdle
	EventComplete
)

// DefaultRunningToWaitingGap is the universal-fallback silence window. A
// Running session whose LastChannelEventAt is older than this is flipped
// to WaitingInput by the watcher. Operator-chosen 2026-05-05 (15 s, see
// rationale in BL266 thread). Long-running tool calls that produce
// no pane changes will trigger this — that's intentional, the operator
// said "having some indicator after a few seconds even if it's wrong is
// better than no indicator for minutes".
const DefaultRunningToWaitingGap = 15 * time.Second

// MarkChannelEvent records a structural channel event for a session.
// kind drives the state transition; ts records when the event happened
// (zero → time.Now). All transitions are write-locked through the store
// callback (Update) so concurrent readers see consistent state.
//
// Behaviour:
//
//   - EventRunning: bumps LastChannelEventAt. If state ∈ {WaitingInput,
//     RateLimited}, transitions to Running. Never resurrects a terminal
//     state (Complete/Failed/Killed are sticky).
//   - EventIdle: bumps LastChannelEventAt. If state == Running, transitions
//     to WaitingInput. Authoritative — bypasses the gap watcher.
//   - EventComplete: bumps LastChannelEventAt. If state ∈ {Running,
//     WaitingInput, RateLimited}, transitions to StateComplete. Sticky.
func (m *Manager) MarkChannelEvent(fullID string, kind ChannelEventKind) {
	m.markChannelEventAt(fullID, kind, time.Now())
}

// markChannelEventAt is the testable form — accepts an explicit timestamp
// so unit tests can drive the gap watcher without sleeping.
func (m *Manager) markChannelEventAt(fullID string, kind ChannelEventKind, ts time.Time) {
	if m == nil || m.store == nil {
		return
	}
	sess, ok := m.store.Get(fullID)
	if !ok || sess == nil {
		return
	}
	if ts.IsZero() {
		ts = time.Now()
	}
	oldState := sess.State
	sess.LastChannelEventAt = ts

	// Sticky locks — never override these from a channel event. RateLimited
	// is intentionally locked too: only the rate-limit cooldown timer (or
	// an operator-issued resume) can leave that state, otherwise a benign
	// channel keep-alive would silently kick us back into Running and
	// re-trigger the rate-limit hammer.
	if oldState == StateComplete || oldState == StateFailed || oldState == StateKilled || oldState == StateRateLimited {
		_ = m.store.Save(sess)
		return
	}

	switch kind {
	case EventRunning:
		if oldState == StateWaitingInput {
			sess.State = StateRunning
			sess.UpdatedAt = ts
			m.debugf("MarkChannelEvent(running): %s %s → Running", sess.FullID, oldState)
		}
	case EventIdle:
		if oldState == StateRunning {
			sess.State = StateWaitingInput
			sess.UpdatedAt = ts
			m.debugf("MarkChannelEvent(idle): %s Running → WaitingInput (structural)", sess.FullID)
		}
	case EventComplete:
		if oldState == StateRunning || oldState == StateWaitingInput {
			sess.State = StateComplete
			sess.UpdatedAt = ts
			m.debugf("MarkChannelEvent(complete): %s %s → Complete (structural)", sess.FullID, oldState)
		}
	}
	_ = m.store.Save(sess)
	// Fire downstream hooks for state changes (mirrors the historical
	// MarkChannelActivityFromText behaviour). onStateChange always; onSessionEnd
	// only on the Running/WaitingInput → Complete transition.
	if oldState != sess.State {
		if m.onStateChange != nil {
			m.onStateChange(sess, oldState)
		}
		if sess.State == StateComplete && m.onSessionEnd != nil {
			m.onSessionEnd(sess)
		}
	}
}

// StartChannelStateWatcher launches the gap-based fallback watcher. Runs
// until ctx is cancelled. Tick interval and gap are configurable; pass
// zero to use defaults (1 s tick, DefaultRunningToWaitingGap).
//
// On each tick: every Running session whose LastChannelEventAt is older
// than gap flips to WaitingInput. Sessions with zero LastChannelEventAt
// are skipped (no events seen yet — could be a fresh session that hasn't
// produced output). Sessions whose UpdatedAt is fresher than
// LastChannelEventAt are also skipped (treats operator-driven transitions
// as recent activity).
func (m *Manager) StartChannelStateWatcher(ctx context.Context, tick, gap time.Duration) {
	if tick <= 0 {
		tick = time.Second
	}
	if gap <= 0 {
		gap = DefaultRunningToWaitingGap
	}
	go func() {
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.runChannelStateWatcherTick(time.Now(), gap)
			}
		}
	}()
}

// runChannelStateWatcherTick is the testable single-tick body — no
// sleep, no goroutine. Tests call it directly with a synthetic clock.
func (m *Manager) runChannelStateWatcherTick(now time.Time, gap time.Duration) {
	if m == nil || m.store == nil {
		return
	}
	for _, sess := range m.store.List() {
		if sess == nil || sess.State != StateRunning {
			continue
		}
		if sess.LastChannelEventAt.IsZero() {
			continue
		}
		// Treat operator-driven UpdatedAt as activity too.
		ref := sess.LastChannelEventAt
		if sess.UpdatedAt.After(ref) {
			ref = sess.UpdatedAt
		}
		if now.Sub(ref) < gap {
			continue
		}
		oldState := sess.State
		sess.State = StateWaitingInput
		sess.UpdatedAt = now
		_ = m.store.Save(sess)
		if m.onStateChange != nil {
			m.onStateChange(sess, oldState)
		}
		m.debugf("ChannelStateWatcher: %s %s → WaitingInput (gap %.0fs ≥ %.0fs)", sess.FullID, oldState, now.Sub(ref).Seconds(), gap.Seconds())
	}
}

// classifyACPEventType maps an opencode-acp event type string to a
// structural ChannelEventKind, or returns (0, false) if no mapping
// exists (the caller should fall back to text-event handling).
//
// Authoritative mappings (per opencode SSE protocol observed in
// internal/llm/backends/opencode/acpbackend.go event handler):
//
//   - busy / step-start / message.part.delta / message.part.updated → Running
//   - idle / session.idle → Idle
//   - session.completed / message.completed → Complete
func classifyACPEventType(eventType, statusType string) (ChannelEventKind, bool) {
	switch eventType {
	case "session.completed", "message.completed":
		return EventComplete, true
	case "session.idle":
		return EventIdle, true
	case "session.status":
		switch statusType {
		case "busy":
			return EventRunning, true
		case "idle":
			return EventIdle, true
		}
	case "step-start", "message.part.delta", "message.part.updated":
		return EventRunning, true
	}
	return 0, false
}

// MarkACPEvent is the typed entry point opencode-acp calls for each SSE
// event. Bypasses the natural-language classifier entirely — opencode's
// own state machine is the source of truth for opencode-acp sessions.
//
// rawText is optional and only used for logging; it does NOT drive state.
func (m *Manager) MarkACPEvent(fullID, eventType, statusType string) {
	kind, ok := classifyACPEventType(eventType, statusType)
	if !ok {
		// Unknown event — treat as activity (bump timestamp, no transition).
		m.MarkChannelEvent(fullID, EventRunning)
		return
	}
	m.MarkChannelEvent(fullID, kind)
}

// classifyMCPMarker checks a tmux/log line for the explicit
// DATAWATCH_COMPLETE: marker which datawatch's claude-code MCP injects on
// task completion. Authoritative — same standing as ACP session.completed.
func classifyMCPMarker(line string) (ChannelEventKind, bool) {
	if strings.Contains(line, "DATAWATCH_COMPLETE:") {
		return EventComplete, true
	}
	return 0, false
}
