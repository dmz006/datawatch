// BL274 Sprint 3, v6.18.0 — approval-token store for docs_apply execute mode.
//
// Operator decision Q3c: docs_apply defaults to plan-then-execute. The
// plan call returns an approval_token; the execute call must include it.
// Tokens are single-use (or single-step when risk_gate=true) and expire
// after 5 minutes. The store is in-memory only — daemon restart drops
// pending approvals, which is the correct UX (an operator who walked
// away should re-plan).
//
// Q3d adds risk_gate=true: pause before each mutating step and issue a
// fresh continuation token, so the operator must explicitly approve
// every write.

package docsindex

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

const (
	defaultApprovalTTL = 5 * time.Minute
	tokenBytes         = 32
)

// Approval is the server-side record of a pending docs_apply execution.
type Approval struct {
	Token     string
	HowtoID   string
	Steps     []ExecStep
	Params    map[string]string
	NextStep  int  // index of the next step to execute (advances after each run)
	RiskGate  bool // when true, pause before each mutating (read_only=false) step
	CreatedAt time.Time
}

// ApprovalStore is an in-memory map of token → Approval with TTL eviction.
// Safe for concurrent use.
type ApprovalStore struct {
	mu     sync.Mutex
	byTok  map[string]*Approval
	ttl    time.Duration
}

// NewApprovalStore returns an empty store with the default 5-minute TTL.
func NewApprovalStore() *ApprovalStore {
	return &ApprovalStore{
		byTok: map[string]*Approval{},
		ttl:   defaultApprovalTTL,
	}
}

// SetTTL overrides the eviction window (mainly for tests).
func (a *ApprovalStore) SetTTL(d time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ttl = d
}

// Issue creates and stores a new approval, returning its token.
func (a *ApprovalStore) Issue(howtoID string, steps []ExecStep, params map[string]string, riskGate bool) (string, error) {
	tok, err := newToken()
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.gcLocked()
	a.byTok[tok] = &Approval{
		Token:     tok,
		HowtoID:   howtoID,
		Steps:     steps,
		Params:    params,
		NextStep:  0,
		RiskGate:  riskGate,
		CreatedAt: time.Now(),
	}
	return tok, nil
}

// ErrApprovalNotFound — token unknown or expired.
var ErrApprovalNotFound = errors.New("approval token not found or expired")

// ErrHowtoMismatch — token exists but for a different howto.
var ErrHowtoMismatch = errors.New("approval token does not match howto_id")

// Get returns the approval for the given token. Returns ErrApprovalNotFound
// if the token is unknown or has expired.
func (a *ApprovalStore) Get(token, howtoID string) (*Approval, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.gcLocked()
	ap, ok := a.byTok[token]
	if !ok {
		return nil, ErrApprovalNotFound
	}
	if howtoID != "" && ap.HowtoID != howtoID {
		return nil, ErrHowtoMismatch
	}
	return ap, nil
}

// Advance bumps the NextStep counter for risk-gated continuations.
func (a *ApprovalStore) Advance(token string, n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ap, ok := a.byTok[token]; ok {
		ap.NextStep = n
	}
}

// Delete removes an approval (after a clean run completes).
func (a *ApprovalStore) Delete(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.byTok, token)
}

// Count returns the number of live approvals (post-GC).
func (a *ApprovalStore) Count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.gcLocked()
	return len(a.byTok)
}

// gcLocked sweeps expired approvals. Caller must hold a.mu.
func (a *ApprovalStore) gcLocked() {
	cutoff := time.Now().Add(-a.ttl)
	for tok, ap := range a.byTok {
		if ap.CreatedAt.Before(cutoff) {
			delete(a.byTok, tok)
		}
	}
}

func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
