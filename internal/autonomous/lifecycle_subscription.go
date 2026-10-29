// BL427 — Automaton lifecycle events, Phase 3: persisted event
// subscriptions and their validation. Design:
// docs/plans/2026-10-13-prd-lifecycle-events.md (Phases item 3, Q2, Q3,
// Q4, Follow-up implementation PRD).
//
// A Subscription selects lifecycle events (LifecycleFilter) and routes
// them to one sink. The sink types comm, session, topic, alert, mcp_queue
// and webhook are accepted and validated. topic and alert are stored for
// the existing Phase 2 sinks, and comm/session execution (including the
// send_input action) happens when the daemon wires the store to the
// dispatcher. The mcp_queue sink appends to a named pending-events
// queue that generic MCP clients poll. The webhook sink POSTs a signed
// JSON envelope to an allow-listed https receiver (lifecycle_webhook_sink.go).
// A webhook subscription needs a ${secret:name} reference in sink.secret_ref
// and never carries an action. ValidateSubscription checks the target URL's
// structure; ValidateWebhookSubscriptionTarget checks it against the live
// webhook settings. The store runs both on create and update and refuses
// webhook subscriptions when it has no live check installed.

package autonomous

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/secrets"
)

// Sink types. comm/session/topic/alert/mcp_queue/webhook are accepted.
const (
	SinkComm    = "comm"    // existing comm-router fan-out (routers, ntfy, email)
	SinkSession = "session" // a datawatch-managed session, via SendInput / channel bridge
	SinkTopic   = "topic"   // SSE / mobile push topic bus (server.PublishToTopic)
	SinkAlert   = "alert"   // the in-app alert store (alerts.Store.Add)
	// SinkMCPQueue appends to a named pending-events queue (Sink.Target)
	// that generic MCP clients poll with a cursor.
	SinkMCPQueue = "mcp_queue"
	// SinkWebhook POSTs each matching event, signed with the secret named by
	// Sink.SecretRef, to the URL in Sink.Target. Coalesce is ignored for it:
	// every matching event is its own delivery.
	SinkWebhook = "webhook"
	// SinkPeer forwards each matching event to a registered federation peer.
	// Sink.Target is the peer's registered name; the forwarder (lifecycle_forward.go)
	// resolves the address, token and capability from the peer registry.
	// Coalesce is ignored for it, as for the webhook sink.
	SinkPeer = "peer"
)

// MaxQueueNameLen caps an mcp_queue sink's queue name.
const MaxQueueNameLen = 64

var queueNameRe = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

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
		KindScopeWarning:       true,
		KindGuardrailWarn:      true,
		KindPRDUpdated:         true,
		// The operator-question kinds are subscribable but never in the
		// default set: a subscriber lists them explicitly.
		KindQuestionAnswered:  true,
		KindQuestionWithdrawn: true,
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
		TaskCancelled, TaskWaitingCapacity, TaskAwaitingApproval, TaskReviewNeeded,
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
	// SecretRef is a single ${secret:name} vault reference naming the HMAC key
	// of a webhook sink; a webhook sink requires it and every other sink
	// refuses it. It is never a literal: the webhook sink resolves it on
	// every delivery (WebhookSinkConfig.ResolveSecret) and the resolved
	// value is never stored, logged or audited.
	SecretRef string `json:"secret_ref,omitempty"`
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
//
// Gating: delivery is subject to the notify-event resolver applied
// FIRST — the Automaton's resolved notify-event set decides which of its
// kinds can ever be delivered to this subscription, and Filter.Kinds
// only narrows within that set (it can never widen it). A
// subscription to a kind outside the resolved set therefore delivers
// nothing: no delivery attempt, no pending-event queueing, no one-shot
// consumption — the record stays armed until a DELIVERED event consumes
// it.
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
	if sub.Filter.Level != "" && sub.Filter.Level != LevelPRD && sub.Filter.Level != LevelTask && sub.Filter.Level != LevelAutomation {
		return fmt.Errorf("filter level must be %q, %q or %q, got %q", LevelPRD, LevelTask, LevelAutomation, sub.Filter.Level)
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
	case SinkMCPQueue:
		name := strings.TrimSpace(sub.Sink.Target)
		if name == "" {
			return fmt.Errorf("sink target is required")
		}
		if len(name) > MaxQueueNameLen {
			return fmt.Errorf("mcp_queue name is %d characters; limit is %d", len(name), MaxQueueNameLen)
		}
		if !queueNameRe.MatchString(name) {
			return fmt.Errorf("mcp_queue name %q may contain only letters, digits and . _ : -", name)
		}
	case SinkWebhook:
		if err := validateWebhookSecretRef(sub.Sink.SecretRef); err != nil {
			return err
		}
		// The structural URL policy needs no live settings, so a store
		// without the live check still refuses a bad scheme, userinfo or host.
		if _, _, _, err := parseWebhookURL(sub.Sink.Target); err != nil {
			return err
		}
	case SinkPeer:
		if err := ValidatePeerName(sub.Sink.Target); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown sink type %q (want one of %q, %q, %q, %q, %q, %q, %q)",
			sub.Sink.Type, SinkComm, SinkSession, SinkTopic, SinkAlert, SinkMCPQueue, SinkWebhook, SinkPeer)
	}
	if sub.Sink.Type != SinkWebhook && sub.Sink.SecretRef != "" {
		return fmt.Errorf("sink secret_ref is only valid with a webhook sink (got %q)", sub.Sink.Type)
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

// validateWebhookSecretRef requires ref to be exactly one ${secret:name}
// reference. A literal is refused without echoing it into the error.
func validateWebhookSecretRef(ref string) error {
	switch {
	case strings.TrimSpace(ref) == "":
		return fmt.Errorf("a webhook sink requires a signing secret reference, such as ${secret:name}")
	case !secrets.IsRef(ref):
		return fmt.Errorf("the webhook signing secret must be a single ${secret:name} reference; a literal value is refused")
	}
	return nil
}

// ValidateWebhookSubscriptionTarget checks the parts of a webhook sink that
// depend on the live webhook settings: delivery must be enabled, and the
// target URL must pass the outbound policy (https, no userinfo, a host on
// the allow-list). It returns nil for every other sink type. The store runs
// it on Add and Update (SetSinkValidator), so a subscription is refused at
// creation rather than at its first event.
func ValidateWebhookSubscriptionTarget(sub Subscription, set WebhookSettings) error {
	if sub.Sink.Type != SinkWebhook {
		return nil
	}
	if !set.Enabled {
		return fmt.Errorf("webhook delivery is disabled; set autonomous.notify.webhook.enabled to true before creating a webhook subscription")
	}
	if _, err := ValidateWebhookURL(sub.Sink.Target, set.AllowHosts); err != nil {
		return fmt.Errorf("webhook target: %w", err)
	}
	return nil
}

// NewLifecycleSinkValidator returns the live check a daemon installs on the
// subscription store (SetSinkValidator). A webhook sink must pass the webhook
// settings (delivery enabled, target on the allow-list); a peer sink must pass
// the forwarding settings and the peer registry. The daemon and the parity
// tests install this one constructor, so the two cannot drift apart.
func NewLifecycleSinkValidator(webhook func() WebhookSettings, forward func() ForwardSettings, lookup func(name string) (PeerRef, bool)) func(Subscription) error {
	return func(sub Subscription) error {
		if err := ValidateWebhookSubscriptionTarget(sub, webhook()); err != nil {
			return err
		}
		return ValidatePeerSubscriptionTarget(sub, forward(), lookup)
	}
}
