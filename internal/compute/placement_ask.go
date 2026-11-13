package compute

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Placement ask (missing-model policy = "ask"): when placement runs on a
// request whose model is present on no eligible node, and the policy is
// "ask", datawatch records a PlacementAsk with its candidate node and
// surfaces it (REST /api/compute/placements, CLI `compute placements`,
// the comm channel, and MCP) for the operator to approve or deny. A
// pending ask is valid until it is answered or it expires
// (ask_timeout_minutes, 0 = no timeout).

// PlacementAskDecision is the lifecycle state of a PlacementAsk.
type PlacementAskDecision string

const (
	AskDecisionPending  PlacementAskDecision = "pending"
	AskDecisionApproved PlacementAskDecision = "approved"
	AskDecisionDenied   PlacementAskDecision = "denied"
	AskDecisionExpired  PlacementAskDecision = "expired" // auto-resolved once ExpiresAt passed
)

// PlacementAsk is one pending (or resolved) approval question.
type PlacementAsk struct {
	ID          string               `json:"id"`
	Model       string               `json:"model"`
	Node        string               `json:"node"`          // candidate node from the placement decision
	Reason      string               `json:"reason"`        // operator-facing context (policy, headroom, why this node)
	ModelSizeGB int                  `json:"model_size_gb"` // assumed size in GB (0 = unknown)
	RequestedAt time.Time            `json:"requested_at"`
	ExpiresAt   time.Time            `json:"expires_at"` // zero = no timeout (ask_timeout_minutes = 0)
	Decision    PlacementAskDecision `json:"decision"`
	DecidedAt   time.Time            `json:"decided_at,omitempty"` // set when answered or when it expired
	DecidedBy   string               `json:"decided_by,omitempty"` // "operator", "timeout", ...
}

// PlacementAskStore is an in-process registry of placement asks. It is
// the single home for pending asks; REST, CLI, comm-channel and MCP
// surfaces all go through it. Decisions are one-shot: approving a
// denied, expired, or already-approved ask is refused.
type PlacementAskStore struct {
	mu   sync.RWMutex
	next int
	asks map[string]*PlacementAsk
}

// NewPlacementAskStore returns an empty ask registry.
func NewPlacementAskStore() *PlacementAskStore {
	return &PlacementAskStore{asks: map[string]*PlacementAsk{}}
}

// Add records a new pending ask and returns it (with its generated ID
// and RequestedAt filled in). Model is required; it carries the
// candidate node and optional context. ExpiresAt is computed from
// at + timeout (timeout 0 = no timeout → ExpiresAt stays zero).
func (st *PlacementAskStore) Add(model, node, reason string, modelSizeGB int, at time.Time, timeout time.Duration) *PlacementAsk {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.next++
	ask := &PlacementAsk{
		ID:          fmt.Sprintf("ask-%d", st.next),
		Model:       model,
		Node:        node,
		Reason:      reason,
		ModelSizeGB: modelSizeGB,
		RequestedAt: at,
		Decision:    AskDecisionPending,
	}
	if timeout > 0 {
		ask.ExpiresAt = at.Add(timeout)
	}
	st.asks[ask.ID] = ask
	return ask
}

// Get returns one ask by ID.
func (st *PlacementAskStore) Get(id string) (*PlacementAsk, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	a, ok := st.asks[id]
	return a, ok
}

// Pending returns asks still awaiting a decision, oldest first. ExpireStale
// should have run before listing so stale asks are not shown as pending.
func (st *PlacementAskStore) Pending() []*PlacementAsk {
	st.mu.RLock()
	defer st.mu.RUnlock()
	out := make([]*PlacementAsk, 0, len(st.asks))
	for _, a := range st.asks {
		if a.Decision == AskDecisionPending {
			out = append(out, a)
		}
	}
	sortAsks(out)
	return out
}

// All returns every ask (all decisions), oldest first.
func (st *PlacementAskStore) All() []*PlacementAsk {
	st.mu.RLock()
	defer st.mu.RUnlock()
	out := make([]*PlacementAsk, 0, len(st.asks))
	for _, a := range st.asks {
		out = append(out, a)
	}
	sortAsks(out)
	return out
}

// Answer records an operator decision (approve or deny) on a pending ask.
// It fails if the ask does not exist, if it was already answered (by an
// operator or by timeout), and if the decision is not approve/deny.
func (st *PlacementAskStore) Answer(id string, decision PlacementAskDecision, when time.Time, by string) (*PlacementAsk, error) {
	switch decision {
	case AskDecisionApproved, AskDecisionDenied:
	default:
		return nil, fmt.Errorf("decision must be %q or %q (got %q)", AskDecisionApproved, AskDecisionDenied, decision)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	a, ok := st.asks[strings.TrimSpace(id)]
	if !ok {
		return nil, fmt.Errorf("unknown ask %q", id)
	}
	if a.Decision != AskDecisionPending {
		return nil, fmt.Errorf("ask %s is already %s; it cannot be re-decided", a.ID, a.Decision)
	}
	a.Decision = decision
	a.DecidedAt = when
	a.DecidedBy = by
	return a, nil
}

// ExpireStale marks pending asks whose ExpiresAt has passed as expired
// (auto-resolved). It returns how many asks were expired. Asks with no
// timeout (ExpiresAt zero) never expire.
func (st *PlacementAskStore) ExpireStale(now time.Time) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	n := 0
	for _, a := range st.asks {
		if a.Decision == AskDecisionPending && !a.ExpiresAt.IsZero() && !now.Before(a.ExpiresAt) {
			a.Decision = AskDecisionExpired
			a.DecidedAt = a.ExpiresAt
			a.DecidedBy = "timeout"
			n++
		}
	}
	return n
}

func sortAsks(out []*PlacementAsk) {
	// small N; simple insertion sort by RequestedAt then ID.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && beforeAsk(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
}

func beforeAsk(a, b *PlacementAsk) bool {
	if a.RequestedAt.Equal(b.RequestedAt) {
		return a.ID < b.ID
	}
	return a.RequestedAt.Before(b.RequestedAt)
}
