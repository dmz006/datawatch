// BL427 (phase 3) — session-push delivery for persisted lifecycle
// subscriptions. Design: docs/plans/2026-10-13-prd-lifecycle-events.md
// (Phases item 3).
//
// LifecycleSubscriptionEngine runs the operator's persisted
// Subscription records (SubscriptionStore) against the live dispatcher:
// for each subscription it registers the subscription's LifecycleFilter
// via Manager.Subscribe and delivers matching events to the sink —
// session sinks go to a datawatch-managed session, preferring that
// session's Claude Code channel bridge (POST http://127.0.0.1:<ChannelPort>
// //send) and falling back to typing the text into the session
// (Manager.SendInput, source tag "lifecycle-sub") when the port is 0,
// the bridge POST errors, or no bridge is configured. Delivery runs on
// the dispatcher's per-subscriber goroutine, so a slow or stalled target
// can never block the executor's emit path.
//
// The engine resyncs whenever the store changes (create, delete, expiry
// prune, one-shot consumption) and cancels every registration on Close.
// One-shot subscriptions are removed from the store ONLY after a
// successful delivery — on failure the engine retries once, then logs,
// leaving the subscription armed for a later event.
//
// Import cycle: session lookup, the bridge POST and SendInput are
// injected as function values (SessionPushConfig), so this package
// still does not import internal/session or internal/server (see
// SinkConfig). The daemon wires them next to InstallLifecycleSinks.

package autonomous

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// sessionPushSourceTag is the source tag for the SendInput fallback
// path, so a session's input history shows where the line came from
// (same convention as the discussion_subscribe dispatch).
const sessionPushSourceTag = "lifecycle-sub"

// sessionPushBridgeTimeout bounds the bridge POST so a dead or stalled
// channel bridge cannot stall delivery — the sender falls back to
// SendInput on any error, including timeout.
const sessionPushBridgeTimeout = 5 * time.Second

// sessionPushJanitorInterval is how often the janitor prunes expired
// subscriptions from the store (the delete fires the store's OnChanged
// hook → resync drops the filters → audit records the removal).
const sessionPushJanitorInterval = 30 * time.Second

// auditLifecycleSubCreate / auditLifecycleSubRemove are the audit
// actions for subscription create/delete records, emitted through the
// existing SetAuditFn path (JSON-lines + CEF mirror in the daemon's
// audit log writer — same pattern as the file-drift audit).
const (
	auditLifecycleSubCreate = "autonomous_lifecycle_subscription_create"
	auditLifecycleSubRemove = "autonomous_lifecycle_subscription_remove"
)

// CommSinkConfig drives the comm-sink delivery from persisted
// subscriptions: Send delivers a (possibly coalesced) message through
// the daemon's comm pipeline (routers / ntfy / email) and Window is
// the quiet window used when the subscription sets Coalesce (<= 0 falls
// back to commFanoutDefaultWindow). Send == nil disables comm sinks.
type CommSinkConfig struct {
	Send   func(text string)
	Window time.Duration
}

// SessionPushConfig is the dependency set for the session-push adapter.
// Every entry is an injected function value — internal/autonomous must
// not import internal/session or internal/server (import cycle). Nil
// entries degrade delivery to the next fallback: no FindSession means
// the target is unresolvable (an error), no PostBridge means no bridge
// path (SendInput only), no SendInput means the session sink fails.
type SessionPushConfig struct {
	// FindSession resolves a session name, full ID, or short ID to the
	// session's FullID and channel-bridge port (0 when the session has
	// no bridge). ok==false means no such session.
	FindSession func(nameOrID string) (fullID string, channelPort int, ok bool)
	// PostBridge POSTs text to the session channel bridge's /send
	// endpoint on port. Nil → DefaultBridgePost.
	PostBridge func(port int, sessionID, text string) error
	// SendInput types text into a datawatch-managed session; source is
	// always sessionPushSourceTag. Nil → the fallback is unavailable.
	SendInput func(fullID, text, source string) error
	// Comm drives comm-sink subscriptions.
	Comm CommSinkConfig
	// RecordAlert drives alert-sink subscriptions (nil → disabled).
	RecordAlert func(level, title, body string)
	// PublishTopics drives topic-sink subscriptions (nil → disabled).
	PublishTopics func(topics []string, title, message string)
}

// DefaultBridgePost POSTs text to the session channel bridge's
// http://127.0.0.1:<port>/send endpoint with the payload the bridge
// accepts — {"text","source":"datawatch","session_id":fullID} — and
// returns nil on a 2xx response. It has a 5-second timeout so a dead
// bridge falls through to the SendInput fallback instead of hanging.
func DefaultBridgePost(port int, sessionID, text string) error {
	if port <= 0 {
		return fmt.Errorf("bridge port must be positive (got %d)", port)
	}
	payload, err := json.Marshal(map[string]string{
		"text":       text,
		"source":     "datawatch",
		"session_id": sessionID,
	})
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: sessionPushBridgeTimeout}
	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/send", port), "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("bridge /send: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// activeSubscription is one persisted subscription's live registration:
// the dispatcher cancel plus, for a coalescing comm subscription, the
// per-subscription CommFanoutSink the engine owns for it.
type activeSubscription struct {
	cancel func()
	comm   *CommFanoutSink
}

// LifecycleSubscriptionEngine wires persisted subscriptions from a
// SubscriptionStore to the dispatcher. Zero-value is not usable; build
// it with NewLifecycleSubscriptionEngine and call Start (idempotent) /
// Close (idempotent).
type LifecycleSubscriptionEngine struct {
	m     *Manager
	store *SubscriptionStore
	cfg   SessionPushConfig
	nowFn func() time.Time

	mu      sync.Mutex
	active  map[string]*activeSubscription
	started bool
	closed  bool
	stop    chan struct{}

	// auditHook overrides the Manager's audit callback (tests capture
	// the records without a file audit log).
	auditHook func(action string, details map[string]any)
}

// NewLifecycleSubscriptionEngine builds the engine over m's dispatcher
// and the persisted subscriptions in store. cfg's nil PostBridge
// defaults to DefaultBridgePost.
func NewLifecycleSubscriptionEngine(m *Manager, store *SubscriptionStore, cfg SessionPushConfig) *LifecycleSubscriptionEngine {
	if cfg.PostBridge == nil {
		cfg.PostBridge = DefaultBridgePost
	}
	return &LifecycleSubscriptionEngine{
		m:      m,
		store:  store,
		cfg:    cfg,
		nowFn:  time.Now,
		active: make(map[string]*activeSubscription),
		stop:   make(chan struct{}),
	}
}

// Start wires the store's OnChanged hook (resync on every mutation),
// performs the initial resync and starts the expiry janitor. Idempotent;
// an error only on a closed engine.
func (e *LifecycleSubscriptionEngine) Start() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return fmt.Errorf("lifecycle subscription engine already closed")
	}
	shouldStart := !e.started
	e.started = true
	e.mu.Unlock()

	e.store.OnChanged(e.resync)
	if shouldStart {
		e.resync()
		go e.janitor()
	}
	return nil
}

// Close cancels every live registration, flushes any pending coalesced
// comm lines (no matched event is dropped on shutdown), detaches the
// store hook and stops the janitor. Idempotent.
func (e *LifecycleSubscriptionEngine) Close() {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	prevActive := e.active
	e.active = make(map[string]*activeSubscription)
	close(e.stop)
	e.mu.Unlock()

	e.store.OnChanged(nil)
	for _, a := range prevActive {
		if a.comm != nil {
			a.comm.Close()
		}
		if a.cancel != nil {
			a.cancel()
		}
	}
}

// janitor prunes expired subscriptions on a fixed cadence. Delete
// Expired fires OnChanged → resync drops the filters and the audit hook
// records the removal.
func (e *LifecycleSubscriptionEngine) janitor() {
	tick := time.NewTicker(sessionPushJanitorInterval)
	defer tick.Stop()
	for {
		select {
		case <-e.stop:
			return
		case <-tick.C:
			if _, err := e.store.DeleteExpired(e.nowFn()); err != nil {
				log.Printf("[autonomous] lifecycle subscriptions: prune expired: %v", err)
			}
		}
	}
}

// resync rebuilds the live registrations from the current store: new
// subscriptions get their filter registered, vanished ones (deleted,
// expired, or consumed one-shots) lose theirs, and the diff is emitted
// to the audit log via the existing SetAuditFn path.
func (e *LifecycleSubscriptionEngine) resync() {
	now := e.nowFn()
	subs := e.store.List("")
	subByID := make(map[string]Subscription, len(subs))
	for _, sub := range subs {
		subByID[sub.ID] = sub
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	prevActive := e.active
	prevIDs := make(map[string]bool, len(prevActive))
	for id := range prevActive {
		prevIDs[id] = true
	}

	next := make(map[string]*activeSubscription, len(subs))
	var created, removed []string
	for _, sub := range subs {
		if sub.ExpiresAt != nil && !sub.ExpiresAt.After(now) {
			continue // expired: not registered; the janitor deletes it
		}
		if prevIDs[sub.ID] {
			prevIDs[sub.ID] = false
		} else {
			created = append(created, sub.ID)
		}
		subCancel := e.m.Subscribe(sub.Filter, func(ev LifecycleEvent) { e.handle(sub, ev) })
		var comm *CommFanoutSink
		if sub.Sink.Type == SinkComm && sub.Coalesce {
			comm = NewCommFanoutSink(e.cfg.Comm.Send, e.cfg.Comm.Window)
		}
		next[sub.ID] = &activeSubscription{cancel: subCancel, comm: comm}
	}
	for id := range prevIDs {
		if prevIDs[id] {
			removed = append(removed, id)
		}
	}
	e.active = next
	e.mu.Unlock()

	// Tear the previous set down OUTSIDE e.mu: cancel may be called from
	// any goroutine (dispatcher contract), and comm Close flushes
	// pending lines through the injected comm send.
	for _, a := range prevActive {
		if a.comm != nil {
			a.comm.Close()
		}
		if a.cancel != nil {
			a.cancel()
		}
	}

	sort.Strings(created)
	sort.Strings(removed)
	for _, id := range created {
		e.audit(auditLifecycleSubCreate, subscriptionAuditDetails(subByID[id]))
	}
	for _, id := range removed {
		if sub, ok := subByID[id]; ok {
			e.audit(auditLifecycleSubRemove, subscriptionAuditDetails(sub))
		} else {
			e.audit(auditLifecycleSubRemove, map[string]any{"subscription_id": id})
		}
	}
}

// subscriptionAuditDetails renders the fields an operator needs to
// audit a subscription create/delete from the audit log alone.
func subscriptionAuditDetails(sub Subscription) map[string]any {
	kinds := strings.Join(sub.Filter.Kinds, ",")
	if kinds == "" {
		kinds = "*"
	}
	actionType := ""
	if sub.Action != nil {
		actionType = sub.Action.Type
	}
	return map[string]any{
		"subscription_id": sub.ID,
		"owner":           sub.Owner,
		"sink":            sub.Sink.Type,
		"action":          actionType,
		"one_shot":        sub.OneShot,
		"kinds":           kinds,
	}
}

// handle is the subscribe callback for one persisted subscription. It
// runs on the dispatcher's per-subscriber goroutine — never on the
// executor's emit path — so slow targets (a stalled bridge, a busy
// session) delay only this subscription's queue.
func (e *LifecycleSubscriptionEngine) handle(sub Subscription, ev LifecycleEvent) {
	if sub.ExpiresAt != nil && !sub.ExpiresAt.After(e.nowFn()) {
		return // expired: never fires (resync also drops the filter)
	}
	if sub.Filter.AutomationID != "" {
		// Matches() treats AutomationID as a wildcard (the event carries
		// no Automation membership); resolve it here, at dispatch time,
		// per the LifecycleFilter.AutomationID contract.
		owner := ""
		if store := e.m.AutomationStore(); store != nil {
			owner = store.OwnerOf(ev.PRDID)
		}
		if owner != sub.Filter.AutomationID {
			return
		}
	}

	switch sub.Sink.Type {
	case SinkSession:
		text := DescribeLifecycleEvent(ev)
		target := sub.Sink.Target
		if sub.Action != nil {
			// An action subscription delivers its ACTION text — and for
			// task.session_started that means the session the event
			// records (the newly started worker), not the subscription's
			// owner target.
			text = sub.Action.Text
			if ev.SessionID != "" {
				target = ev.SessionID
			}
		}
		e.deliverSession(sub, target, text)

	case SinkComm:
		message := DescribeLifecycleEvent(ev)
		if a, ok := e.commSink(sub.ID); ok && a.comm != nil {
			a.comm.Handle(ev) // coalescing: per-Automaton quiet window
			return
		}
		if e.cfg.Comm.Send != nil {
			e.cfg.Comm.Send(message)
		}

	case SinkTopic:
		if e.cfg.PublishTopics == nil {
			return
		}
		topics := []string{"alerts"}
		if ev.PRDID != "" {
			topics = append(topics, "automaton-"+ev.PRDID)
		}
		e.cfg.PublishTopics(topics, ev.Kind, DescribeLifecycleEvent(ev))

	case SinkAlert:
		if e.cfg.RecordAlert == nil {
			return
		}
		e.cfg.RecordAlert(sinkLevel(ev), ev.Kind, DescribeLifecycleEvent(ev))
	}
}

// commSink returns the live registration for subID (for its coalescing
// comm sink), if any.
func (e *LifecycleSubscriptionEngine) commSink(subID string) (*activeSubscription, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	a, ok := e.active[subID]
	return a, ok
}

// deliverSession delivers one session-sink message to target (a session
// name, full ID, or short ID): channel bridge first when the session
// has a port, SendInput as the fallback. One-shot subscriptions are
// consumed (deleted from the store) only after a successful delivery;
// on failure the engine retries once, then logs and leaves it armed.
func (e *LifecycleSubscriptionEngine) deliverSession(sub Subscription, target, text string) {
	var err error
	const attempts = 2
	for attempt := 1; attempt <= attempts; attempt++ {
		err = e.deliverSessionOnce(target, text)
		if err == nil {
			break
		}
		log.Printf("[autonomous] lifecycle sub %s: delivery attempt %d/%d to session %q failed: %v",
			sub.ID, attempt, attempts, target, err)
	}
	if err == nil && sub.OneShot {
		if derr := e.store.Delete(sub.ID); derr != nil {
			// Concurrent consumption (two matching events racing) is
			// benign: the subscription was already removed.
			log.Printf("[autonomous] lifecycle sub %s: one-shot removal: %v", sub.ID, derr)
		}
		return
	}
}

// deliverSessionOnce performs a single delivery: bridge when the
// resolved session has a ChannelPort, SendInput otherwise or when the
// bridge POST errors. Returns an error when no path succeeded.
func (e *LifecycleSubscriptionEngine) deliverSessionOnce(target, text string) error {
	if e.cfg.FindSession == nil {
		return fmt.Errorf("no session lookup configured")
	}
	fullID, port, ok := e.cfg.FindSession(target)
	if !ok {
		return fmt.Errorf("session %q not found", target)
	}
	if port > 0 {
		if err := e.cfg.PostBridge(port, fullID, text); err == nil {
			return nil
		} else if e.cfg.SendInput == nil {
			return fmt.Errorf("bridge: %v", err)
		}
		// else: bridge failed — fall through to SendInput.
	}
	if e.cfg.SendInput == nil {
		return fmt.Errorf("no delivery path for session %q (bridge port %d, SendInput not configured)", target, port)
	}
	return e.cfg.SendInput(fullID, text, sessionPushSourceTag)
}

// audit emits one record through the existing SetAuditFn path (the
// daemon's audit log writer renders it as JSON-lines + CEF — same
// pattern as file-drift's appendDriftAudit).
func (e *LifecycleSubscriptionEngine) audit(action string, details map[string]any) {
	if e.auditHook != nil {
		e.auditHook(action, details)
		return
	}
	if e.m == nil {
		return
	}
	var fn func(action string, details map[string]any)
	e.m.mu.Lock()
	fn = e.m.auditFn
	e.m.mu.Unlock()
	if fn == nil {
		return
	}
	fn(action, details)
}
