// BL427 — Automaton lifecycle events, Phase 3: persisted event
// subscriptions and their validation. Design:
// docs/plans/2026-10-13-prd-lifecycle-events.md (Phases item 3, Q2, Q3,
// Q4, Follow-up implementation PRD).
//
// A Subscription selects lifecycle events (LifecycleFilter) and routes
// them to one sink. In this phase the sink types comm, session, topic
// and alert are accepted and validated; topic and alert are stored for
// the existing Phase 2 sinks, and comm/session execution (including the
// send_input action) happens when the daemon wires the store to the
// dispatcher. The webhook and mcp_queue sink types are rejected with a
// clear error — they arrive in later phases (outbound webhook delivery,
// pending-events queue for generic MCP clients).

package autonomous

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Sink types. Only comm/session/topic/alert are accepted in this phase.
const (
	SinkComm    = "comm"    // existing comm-router fan-out (routers, ntfy, email)
	SinkSession = "session" // a datawatch-managed session, via SendInput / channel bridge
	SinkTopic   = "topic"   // SSE / mobile push topic bus (server.PublishToTopic)
	SinkAlert   = "alert"   // the in-app alert store (alerts.Store.Add)
)

// SinkActionSendInput is the only action type executed in this phase:
// when a matching event fires, type Text into Sink.Target (the session
// sink's target). This removes the watch-and-inject polling scripts per
// the design doc's motivating example 1.
const SinkActionSendInput = "send_input"

// SubscriptionActionTextLimit caps Action.Text so a subscription cannot
// smuggle an unbounded blob into a live session's input.
const SubscriptionActionTextLimit = 4096

// KnownEventKinds is the closed set of event kinds a subscription may
// filter on: every PRD/task status-change kind plus the non-status
// lifecycle kinds.
var KnownEventKinds = func() map[string]bool {
	m := map[string]bool{
		KindTaskSessionStarted: true,
		KindTaskSessionCleared: true,
		KindPRDUpdated:         true,
	}
	for _, s := range []PRDStatus{
		PRDDraft, PRDPlanning, PRDNeedsReview, PRDApproved, PRDRevisionsAsked,
		PRDActive, PRDRunning, PRDCompleted, PRDRejected, PRDCancelled,
		PRDArchived, PRDBlocked, PRDFailed, PRDPaused,
	} {
		m[PRDStatusKind(s)] = true
	}
	for _, s := range []TaskStatus{
		TaskPending, TaskQueued, TaskInProgress, TaskRunningTests,
		TaskVerifying, TaskCompleted, TaskFailed, TaskBlocked,
		TaskCancelled, TaskWaitingCapacity, TaskAwaitingApproval,
	} {
		m[TaskStatusKind(s)] = true
	}
	return m
}()

// IsKnownEventKind reports whether k is a kind a subscription may
// filter on.
func IsKnownEventKind(k string) bool { return KnownEventKinds[k] }

// SubscriptionSink is where a matching event is delivered.
type SubscriptionSink struct {
	Type   string `json:"type"`
	Target string `json:"target,omitempty"`
}

// SubscriptionAction is the optional follow-up performed when an event
// fires. Only SinkActionSendInput is executed in this phase, and only
// with a session sink (type the text into a datawatch-managed tmux
// session, exactly like discussion_subscribe delivery).
type SubscriptionAction struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Subscription is one persisted lifecycle event subscription (design
// doc, Follow-up implementation PRD "Data model"). In-process
// subscriptions (Manager.Subscribe) are code-registered and are NOT
// stored here.
type Subscription struct {
	ID        string              `json:"id"`
	Owner     string              `json:"owner,omitempty"`
	Filter    LifecycleFilter     `json:"filter"`
	Sink      SubscriptionSink    `json:"sink"`
	Action    *SubscriptionAction `json:"action,omitempty"`
	Coalesce  bool                `json:"coalesce,omitempty"` // human-facing sinks batch per Automaton during a quiet window
	CreatedAt time.Time           `json:"created_at"`
	ExpiresAt *time.Time          `json:"expires_at,omitempty"`
	OneShot   bool                `json:"one_shot,omitempty"` // fire once, then delete
}

// ValidateSubscription checks a subscription for acceptance (called by
// the store on Add, and usable by the REST/MCP/CLI surfaces before
// storing). now is the reference time for the ExpiresAt check.
func ValidateSubscription(sub Subscription, now time.Time) error {
	// A filter must actually select something: at least one of kinds
	// or level (PRDID/TaskID alone would match every kind at every
	// level and is almost always a mistake).
	if len(sub.Filter.Kinds) == 0 && sub.Filter.Level == "" {
		return fmt.Errorf("filter must set at least one of kinds or level")
	}
	if sub.Filter.Level != "" && sub.Filter.Level != LevelPRD && sub.Filter.Level != LevelTask {
		return fmt.Errorf("filter level must be %q or %q, got %q", LevelPRD, LevelTask, sub.Filter.Level)
	}
	for i, k := range sub.Filter.Kinds {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("filter kind %d is empty", i)
		}
		if !IsKnownEventKind(k) {
			return fmt.Errorf("filter kind %q is not a known event kind", k)
		}
	}
	if strings.TrimSpace(sub.Sink.Type) == "" {
		return fmt.Errorf("sink type is required")
	}
	switch sub.Sink.Type {
	case SinkComm, SinkSession, SinkTopic, SinkAlert:
		// accepted in this phase
	case "webhook", "mcp_queue":
		return fmt.Errorf(
			"sink type %q is not available in this phase: outbound webhook delivery and the MCP pending-events queue arrive in later phases",
			sub.Sink.Type)
	default:
		return fmt.Errorf("unknown sink type %q (want one of %q, %q, %q, %q)",
			sub.Sink.Type, SinkComm, SinkSession, SinkTopic, SinkAlert)
	}
	if strings.TrimSpace(sub.Sink.Target) == "" {
		return fmt.Errorf("sink target is required")
	}
	if sub.Action != nil {
		if sub.Sink.Type != SinkSession {
			return fmt.Errorf("action is only valid with a session sink (got %q)", sub.Sink.Type)
		}
		if sub.Action.Type != SinkActionSendInput {
			return fmt.Errorf("unknown action type %q (only %q is supported in this phase)", sub.Action.Type, SinkActionSendInput)
		}
		text := sub.Action.Text
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("%q action requires non-empty text", SinkActionSendInput)
		}
		if n := utf8.RuneCountInString(text); n > SubscriptionActionTextLimit {
			return fmt.Errorf("action text is %d characters; limit is %d", n, SubscriptionActionTextLimit)
		}
	}
	if sub.ExpiresAt != nil && !sub.ExpiresAt.After(now) {
		return fmt.Errorf("expires_at must be in the future")
	}
	return nil
}
