package session

import (
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/spendledger"
)

// SpendLedger is the part of the spend ledger the Manager writes to.
type SpendLedger interface {
	Upsert(e spendledger.Entry) error
}

// SetSpendLedger wires the ledger. Nil disables ledger writes.
func (m *Manager) SetSpendLedger(l SpendLedger) {
	m.ledgerMu.Lock()
	m.ledger = l
	m.ledgerMu.Unlock()
}

// upsertLedger writes the session's current attribution and usage to the
// ledger. It is called at start, on usage updates and at end; cleanup and
// delete paths never remove ledger entries.
func (m *Manager) upsertLedger(sess *Session) {
	m.ledgerMu.Lock()
	l := m.ledger
	m.ledgerMu.Unlock()
	if l == nil || sess == nil {
		return
	}
	snap := m.store.Snapshot(sess)
	role := snap.SpendRole
	if role == "" {
		role = spendledger.RoleOther
	}
	e := spendledger.Entry{
		SessionID:   snap.FullID,
		AutomatonID: snap.AutomatonID,
		TaskID:      snap.TaskID,
		Role:        role,
		Backend:     snap.BackendFamily,
		StartedAt:   snap.CreatedAt,
		TokensIn:    int64(snap.TokensIn),
		TokensOut:   int64(snap.TokensOut),

		CacheRead:     int64(snap.CacheReadTokens),
		CacheCreation: int64(snap.CacheCreationTokens),
		ToolCalls:     int64(snap.ToolCalls),
	}
	if snap.State.IsTerminal() {
		e.EndedAt = snap.UpdatedAt
		if e.EndedAt.IsZero() {
			e.EndedAt = time.Now()
		}
	}
	if err := l.Upsert(e); err != nil {
		fmt.Printf("[warn] spend ledger upsert %s: %v\n", snap.FullID, err)
	}
}
