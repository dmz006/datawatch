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

// SetLocalUsageReader overrides the local usage reader (tests inject fakes).
func (m *Manager) SetLocalUsageReader(r LocalUsageReader) {
	m.ledgerMu.Lock()
	m.localUsage = r
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
	// By-model attribution for the cost report: the session's own model
	// selection (v8.7+), falling back to the LLM registry entry name the
	// session was launched against; legacy sessions leave both empty.
	model := snap.Model
	if model == "" {
		model = snap.LLMRef
	}
	e := spendledger.Entry{
		SessionID:   snap.FullID,
		AutomatonID: snap.AutomatonID,
		TaskID:      snap.TaskID,
		Project:     snap.SpendProject,
		Role:        role,
		Backend:     snap.BackendFamily,
		Model:       model,
		StartedAt:   snap.CreatedAt,
		TokensIn:    int64(snap.TokensIn),
		TokensOut:   int64(snap.TokensOut),

		CacheRead:     int64(snap.CacheReadTokens),
		CacheCreation: int64(snap.CacheCreationTokens),
		ToolCalls:     int64(snap.ToolCalls),
		ToolSeconds:   snap.ToolSeconds,
		ActiveSeconds: snap.ActiveSeconds(activeClock()),
	}
	if IsLocalBackend(snap.BackendFamily) {
		var rd LocalUsageReader = LogUsageReader{}
		m.ledgerMu.Lock()
		if m.localUsage != nil {
			rd = m.localUsage
		}
		m.ledgerMu.Unlock()
		u := rd.ReadLocalUsage(snap.BackendFamily, snap.LogFile)
		e.Local = true
		e.TokensReported = u.Reported
		e.LocalTokensIn, e.LocalTokensOut = u.TokensIn, u.TokensOut
		e.ToolCalls = u.ToolCalls
		// Claude token fields stay zero for local sessions.
		e.TokensIn, e.TokensOut, e.CacheRead, e.CacheCreation = 0, 0, 0, 0
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
