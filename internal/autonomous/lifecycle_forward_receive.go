// BL427 (phase 5) — receiving side of federation peer forwarding. Design:
// docs/plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md
// (section 2(d) receiving daemon, section 3 loop prevention).
//
// ReceiveForwardedEvent takes the envelope a peer's PeerForwarder sent to
// ForwardedEventsPath, checks it, and re-emits its event into this daemon's
// own dispatcher. From there every local subscription of every sink type
// (comm, session, topic, alert, mcp_queue, webhook) sees the event like any
// other, with Host set to the origin host and Forward set, which marks it as
// forwarded. A forwarded event:
//
//   - is never forwarded on (the peer sink and the forwarder both refuse it);
//   - is never written to the executor's state, and does not reach the
//     Automation coordinator or any other status transition. The coordinator
//     and the session-push engine both check Forward and skip it, so this path
//     only feeds notification sinks.
//
// The HTTP layer (internal/server/autonomous_forwarded_events.go) authenticates
// the presenting peer, checks the signature and audits the outcome. This file
// is the envelope check and the injection, with no I/O of its own.

package autonomous

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ForwardedEnvelope is the envelope a peer sends to ForwardedEventsPath. It is
// the same wire type the outbound sinks build (WebhookEnvelope), read on the
// receiving side; its Forward block is required.
type ForwardedEnvelope = WebhookEnvelope

// maxForwardHostLen caps each host name in a forwarded envelope (origin, visited
// list and sender). Pending operator confirmation: a DNS name is at most 255
// octets.
const maxForwardHostLen = 255

// ErrForwardReceiveDisabled means this daemon does not accept forwarded events:
// forwarding is off, or the receiver is not wired. The HTTP layer answers 503.
// ErrForwardReceiveInvalid means the envelope failed a shape or size check (400).
// ErrForwardReceiveLoop means the envelope would loop: its origin, its visited
// list or its hop count refuses this host (409).
var (
	ErrForwardReceiveDisabled = errors.New("forwarded events are not accepted by this daemon")
	ErrForwardReceiveInvalid  = errors.New("invalid forwarded event")
	ErrForwardReceiveLoop     = errors.New("forwarded event would loop")
)

// ForwardReceiveConfig wires the receiving side. Settings is the same live hook
// the peer forwarder reads (its Enabled flag and MaxHops cap), so one config
// change applies to both directions. LocalHost is the federation self-name the
// forwarder also uses: the name that appears in a visited list. A nil Settings
// or an empty local name disables receiving.
type ForwardReceiveConfig struct {
	Settings  func() ForwardSettings
	LocalHost func() string
}

// SetForwardReceive wires the receiving side of peer forwarding. Calling it
// again replaces the config; a zero config disables receiving.
func (m *Manager) SetForwardReceive(cfg ForwardReceiveConfig) {
	if m == nil {
		return
	}
	m.forwardRecv.Store(&cfg)
}

// receiveConfig returns the wired config and its live settings, or ok=false when
// receiving is not wired, is disabled, or has no local host name.
func (m *Manager) receiveConfig() (set ForwardSettings, local string, ok bool) {
	if m == nil {
		return ForwardSettings{}, "", false
	}
	cfg := m.forwardRecv.Load()
	if cfg == nil || cfg.Settings == nil || cfg.LocalHost == nil {
		return ForwardSettings{}, "", false
	}
	set = cfg.Settings().withDefaults()
	local = strings.TrimSpace(cfg.LocalHost())
	return set, local, set.Enabled && local != ""
}

// ForwardReceiveEnabled reports whether this daemon accepts forwarded events
// right now. The HTTP layer checks it before it reads the body.
func (m *Manager) ForwardReceiveEnabled() bool {
	_, _, ok := m.receiveConfig()
	return ok
}

// ReceiveForwardedEvent checks env and, when it is acceptable, publishes its
// event to the local dispatcher marked as forwarded. It returns nil on accept.
// The error wraps ErrForwardReceiveDisabled, ErrForwardReceiveInvalid or
// ErrForwardReceiveLoop, and its text is fixed: it never echoes the envelope.
//
// The check order is: receiving enabled; envelope shape and size; the event
// was not already a forward; then the loop rules (origin, visited list, hop cap).
func (m *Manager) ReceiveForwardedEvent(env ForwardedEnvelope) error {
	set, local, ok := m.receiveConfig()
	if !ok {
		return ErrForwardReceiveDisabled
	}
	if err := checkForwardedEnvelope(env, set.MaxBodyBytes); err != nil {
		return err
	}
	chain := *env.Forward
	if chain.OriginHost == local {
		return fmt.Errorf("%w: the event originated on this host", ErrForwardReceiveLoop)
	}
	if forwardVisited(chain.Visited, local) {
		return fmt.Errorf("%w: this host already appears in the visited list", ErrForwardReceiveLoop)
	}
	if chain.Hops >= set.MaxHops {
		return fmt.Errorf("%w: the hop cap was reached", ErrForwardReceiveLoop)
	}

	ev := env.Event
	ev.Host = chain.OriginHost
	ev.Forward = &chain
	if ev.ID == "" {
		ev.ID = newLifecycleEventID()
	}
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	// Publish directly, not through emit: emit would stamp this daemon's own
	// host name over the origin host, and the event is not a local write site.
	m.lifecycleBus().publish(ev)
	return nil
}

// checkForwardedEnvelope validates the envelope's shape and size, with no regard
// to the loop rules. maxBytes is the live envelope cap; below 1 it is the
// default. Every failure wraps ErrForwardReceiveInvalid.
func checkForwardedEnvelope(env ForwardedEnvelope, maxBytes int) error {
	invalid := func(reason string) error {
		return fmt.Errorf("%w: %s", ErrForwardReceiveInvalid, reason)
	}
	if env.Version != WebhookEnvelopeVersion {
		return invalid("unsupported envelope version")
	}
	chain := env.Forward
	if chain == nil {
		return invalid("the envelope is not a forwarded envelope")
	}
	if env.Event.Forward != nil {
		return invalid("the event is already marked as forwarded")
	}
	if !validForwardHost(chain.OriginHost) {
		return invalid("the origin host is missing or too long")
	}
	if chain.Hops < 1 || chain.Hops != len(chain.Visited) {
		return invalid("the hop count does not match the visited list")
	}
	for _, h := range chain.Visited {
		if !validForwardHost(h) {
			return invalid("a visited host is missing or too long")
		}
	}
	if chain.Visited[0] != chain.OriginHost {
		return invalid("the visited list does not start at the origin host")
	}
	if env.Host != chain.Visited[len(chain.Visited)-1] {
		return invalid("the sender host is not the last visited host")
	}
	if !knownForwardKind(env.Event) {
		return invalid("the event kind is not one this daemon emits")
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return invalid("the envelope cannot be encoded")
	}
	if maxBytes < 1 {
		maxBytes = WebhookMaxBodyBytes
	}
	if len(raw) > maxBytes {
		return invalid("the envelope exceeds the size cap")
	}
	return nil
}

func validForwardHost(h string) bool {
	return h != "" && len(h) <= maxForwardHostLen && strings.TrimSpace(h) == h
}

// knownPRDStatuses, knownTaskStatuses and knownAutomationStatuses list the
// statuses this daemon emits in event kinds (lifecycle_event.go). A kind outside
// them comes from a peer that is not running this vocabulary.
var (
	knownPRDStatuses = map[string]bool{
		string(PRDDraft): true, string(PRDPlanning): true, string(PRDNeedsReview): true,
		string(PRDApproved): true, string(PRDRevisionsAsked): true, string(PRDActive): true,
		string(PRDRunning): true, string(PRDCompleted): true, string(PRDRejected): true,
		string(PRDCancelled): true, string(PRDArchived): true, string(PRDBlocked): true,
		string(PRDFailed): true, string(PRDPaused): true,
	}
	knownTaskStatuses = map[string]bool{
		string(TaskPending): true, string(TaskQueued): true, string(TaskInProgress): true,
		string(TaskRunningTests): true, string(TaskVerifying): true, string(TaskCompleted): true,
		string(TaskFailed): true, string(TaskBlocked): true, string(TaskCancelled): true,
		string(TaskWaitingCapacity): true, string(TaskAwaitingApproval): true,
	}
	knownAutomationStatuses = map[string]bool{
		string(AutomationDraft): true, string(AutomationActive): true, string(AutomationPaused): true,
		string(AutomationCompleted): true, string(AutomationBlocked): true, string(AutomationCancelled): true,
	}
)

// knownForwardKind reports whether ev's kind and level are a pair this daemon
// emits: a status change for the level it names, a task session event, or the
// PRD update notification.
func knownForwardKind(ev LifecycleEvent) bool {
	switch ev.Kind {
	case KindTaskSessionStarted, KindTaskSessionCleared:
		return ev.Level == LevelTask
	case KindPRDUpdated:
		return ev.Level == LevelPRD
	}
	level, status, ok := strings.Cut(ev.Kind, ".")
	if !ok {
		return false
	}
	switch {
	case level == string(LevelPRD) && ev.Level == LevelPRD:
		return knownPRDStatuses[status]
	case level == string(LevelTask) && ev.Level == LevelTask:
		return knownTaskStatuses[status]
	case level == string(LevelAutomation) && ev.Level == LevelAutomation:
		return knownAutomationStatuses[status]
	}
	return false
}
