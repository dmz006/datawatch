// F10 sprint 7 (S7.6) — peer-to-peer messaging broker.
//
// Workers don't talk to each other directly. They post messages to
// the parent's PeerBroker, which routes them into the recipient
// worker's per-agent inbox. The recipient pulls via the parent's
// agent reverse proxy (S3.5) — same trust boundary as every other
// worker→parent call (TLS-pinned bootstrap, agent-id auth).
//
// Why broker rather than direct mesh:
//   * Trust: workers already trust the parent — no new peer trust
//     to establish, no per-worker keypair distribution
//   * Audit: every peer message lands in the broker's audit log
//   * Quotas: per-worker rate-limit + inbox cap defended in one place
//   * Recursive nesting: a worker's spawned children inherit the
//     same routing layer (parent stays the only addressable hub)
//
// Per the project rules: AllowPeerMessaging on the sender's profile
// is checked before any deliver — opt-in per profile.

package agents

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// PeerMessage is the structured payload of a P2P send.
type PeerMessage struct {
	From      string    `json:"from"`              // sender's agent ID
	To        string    `json:"to"`                // recipient's agent ID (per-recipient copy)
	Topic     string    `json:"topic,omitempty"`   // operator-defined channel name
	Body      string    `json:"body"`              // free-form payload (JSON-as-string is fine)
	Timestamp time.Time `json:"timestamp"`
}

// PeerBroker holds per-recipient inboxes + enforces per-profile
// allow-list on send. Inbox capacity is bounded so a runaway sender
// can't DoS a recipient by filling its queue.
type PeerBroker struct {
	mu       sync.Mutex
	inboxes  map[string][]PeerMessage // key: recipient agent ID
	manager  *Manager
	maxInbox int

	// Now is overridable for tests; defaults to time.Now.
	Now func() time.Time
}

// NewPeerBroker builds a broker bound to the given Manager. Manager
// is used for AllowPeerMessaging enforcement (sender's profile) +
// recipient existence checks. maxInbox <= 0 defaults to 100.
func NewPeerBroker(m *Manager, maxInbox int) *PeerBroker {
	if maxInbox <= 0 {
		maxInbox = 100
	}
	return &PeerBroker{
		inboxes:  map[string][]PeerMessage{},
		manager:  m,
		maxInbox: maxInbox,
	}
}

// Send delivers a copy of msg to each recipient in `to`. Order:
//   1. Sender's profile must have AllowPeerMessaging=true
//   2. Each recipient must be a tracked agent in non-terminal state
//   3. Each recipient's inbox must have room (else that recipient is
//      dropped + the dropped count returned for partial-failure handling)
//
// Returns (deliveredCount, undeliveredRecipients, error).
// error is non-nil only on validation failure (sender not allowed,
// no recipients); per-recipient drops surface in the slice.
func (b *PeerBroker) Send(senderID string, to []string, topic, body string) (int, []string, error) {
	if senderID == "" {
		return 0, nil, errors.New("peer broker: sender ID required")
	}
	if len(to) == 0 {
		return 0, nil, errors.New("peer broker: at least one recipient required")
	}
	if b.manager == nil {
		return 0, nil, errors.New("peer broker: manager not wired")
	}

	// Sender authorization (per-profile opt-in).
	senderProj := b.manager.GetProjectFor(senderID)
	if senderProj == nil {
		return 0, nil, fmt.Errorf("peer broker: sender %q has no resolvable profile", senderID)
	}
	if !senderProj.AllowPeerMessaging {
		return 0, nil, fmt.Errorf("peer broker: sender profile %q does not allow_peer_messaging",
			senderProj.Name)
	}

	now := b.now()
	delivered := 0
	var dropped []string
	for _, rid := range to {
		// Recipient existence + non-terminal state.
		recip := b.manager.Get(rid)
		if recip == nil || recip.State == StateStopped || recip.State == StateFailed {
			dropped = append(dropped, rid)
			continue
		}
		msg := PeerMessage{
			From: senderID, To: rid, Topic: topic, Body: body,
			Timestamp: now,
		}
		b.mu.Lock()
		queue := b.inboxes[rid]
		if len(queue) >= b.maxInbox {
			b.mu.Unlock()
			dropped = append(dropped, rid)
			continue
		}
		b.inboxes[rid] = append(queue, msg)
		b.mu.Unlock()
		delivered++
	}
	return delivered, dropped, nil
}

// Drain returns + clears every queued message for recipientID. Used
// by the worker's pull loop on each /api/proxy/agent/{id}/peer/inbox
// poll (or the equivalent push channel — TBD with the broker proxy
// route in BL104).
func (b *PeerBroker) Drain(recipientID string) []PeerMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	msgs := b.inboxes[recipientID]
	delete(b.inboxes, recipientID)
	return msgs
}

// Peek returns a snapshot of the recipient's queue without clearing
// it. Useful for /api/agents/{id} status responses + tests.
func (b *PeerBroker) Peek(recipientID string) []PeerMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	src := b.inboxes[recipientID]
	if len(src) == 0 {
		return nil
	}
	out := make([]PeerMessage, len(src))
	copy(out, src)
	return out
}

// InboxLen returns the current queue depth for recipientID. Useful
// for sweeper logic + UI badges.
func (b *PeerBroker) InboxLen(recipientID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.inboxes[recipientID])
}

func (b *PeerBroker) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now().UTC()
}
