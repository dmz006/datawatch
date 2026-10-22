package autonomous

import "time"

// SetCostSources wires the dollar sources behind PRDCostUSD and
// AutomationSpendUSD: the spend ledger, and sessionTotal, which returns one
// session's lifetime estimated cost and the LLM name it is recorded under in
// the ledger. Either may be nil.
func (m *Manager) SetCostSources(ledger *BudgetLedger, sessionTotal func(sessionID string) (usd float64, llm string, ok bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.costLedger = ledger
	m.sessionTotal = sessionTotal
}

// PRDSessionIDs returns every session that cost money for the PRD: each
// task's worker session across all stories, every verifier and judge session
// recorded on tasks, stories or the PRD, and the decompose sessions found by
// the function set with SetDecomposeSessionsFn. The result is de-duplicated
// with empty ids dropped; a missing PRD yields nil.
func (m *Manager) PRDSessionIDs(prdID string) []string {
	prd, ok := m.store.SnapshotPRD(prdID)
	if !ok {
		return nil
	}
	m.mu.Lock()
	decompose := m.decomposeSessionsFn
	m.mu.Unlock()

	seen := map[string]struct{}{}
	var ids []string
	add := func(id string) {
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	addVerdicts := func(vs []GuardrailVerdict) {
		for _, v := range vs {
			add(v.JudgeSessionID)
		}
	}
	addVerdicts(prd.Verdicts)
	for _, d := range prd.Decisions {
		add(d.JudgeSessionID)
	}
	for i := range prd.Story {
		st := &prd.Story[i]
		addVerdicts(st.Verdicts)
		for j := range st.Tasks {
			t := &st.Tasks[j]
			add(t.SessionID)
			if t.Verification != nil {
				add(t.Verification.VerifierID)
			}
			addVerdicts(t.Verdicts)
		}
	}
	if decompose != nil {
		for _, id := range decompose(prdID) {
			add(id)
		}
	}
	return ids
}

// PRDCostUSD is the one shared per-PRD cost rollup: the dollars spent by every
// session in PRDSessionIDs, optionally limited to one LLM (llm non-empty).
//
// When since is inside the ledger's retention horizon (a non-zero since not
// older than the retention window) the figure is the ledger spend attributed
// to those sessions from since onward. When since is zero, or older than the
// retention horizon, the ledger cannot answer, so it falls back to summing the
// sessions' lifetime totals from the function given to SetCostSources,
// filtered by llm when set; in that case since has no effect. With no ledger
// wired, the lifetime totals are used as well. Peer spend is never included.
func (m *Manager) PRDCostUSD(prdID string, since time.Time, llm string) float64 {
	ids := m.PRDSessionIDs(prdID)
	if len(ids) == 0 {
		return 0
	}
	m.mu.Lock()
	ledger, total := m.costLedger, m.sessionTotal
	m.mu.Unlock()

	if ledger != nil && !since.IsZero() && !since.Before(ledger.now().Add(-budgetRetention)) {
		return ledger.SpendBySessions(ids, llm, since)
	}
	if total == nil {
		return 0
	}
	var sum float64
	for _, id := range ids {
		usd, name, ok := total(id)
		if !ok || (llm != "" && name != llm) {
			continue
		}
		sum += usd
	}
	return sum
}

// AutomationSpendUSD sums PRDCostUSD over the PRD of every kind=prd member of
// a and, for kind=automation members, over the child Automation, recursively.
// A PRD or child Automation reached more than once counts once, so a cycle
// cannot loop; a missing PRD or child contributes zero.
func (m *Manager) AutomationSpendUSD(a *Automation, since time.Time, llm string) float64 {
	return m.automationSpend(a, since, llm, map[string]bool{}, map[string]bool{})
}

func (m *Manager) automationSpend(a *Automation, since time.Time, llm string, seenAuto, seenPRD map[string]bool) float64 {
	if a == nil {
		return 0
	}
	if a.ID != "" {
		if seenAuto[a.ID] {
			return 0
		}
		seenAuto[a.ID] = true
	}
	var sum float64
	for _, mem := range a.Members {
		switch mem.kind() {
		case MemberKindPRD:
			if mem.PRDID == "" || seenPRD[mem.PRDID] {
				continue
			}
			seenPRD[mem.PRDID] = true
			sum += m.PRDCostUSD(mem.PRDID, since, llm)
		case MemberKindAutomation:
			if child, ok := m.GetAutomation(mem.AutomationID); ok {
				sum += m.automationSpend(child, since, llm, seenAuto, seenPRD)
			}
		}
	}
	return sum
}
