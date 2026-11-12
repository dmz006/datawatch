package autonomous

import (
	"context"
	"strings"
	"time"
)

// peerNoBackendNote is the verdict note when a policy names peers but no
// backend: peer usage is per LLM, so there is nothing to ask them for.
const peerNoBackendNote = "peer_window is set but the policy has no backend, so no peer spend is counted; set the backend to count the opted-in peers"

// ResetFor is the built-in Resets seam. It resolves when the breached window of p frees up. A day, week or
// month window ends at a time the daemon defines itself. The lifetime window
// has no end, so its reset is known only while the policy's backend is backed
// off until a reset time parsed from a rate-limit message; a configured
// fallback or operator back-off is not a known reset.
func (b *AutomationBudget) ResetFor(p *BudgetPolicy, span BudgetWindowSpan, now time.Time) ResetInfo {
	if span.HasEnd {
		end := span.End
		return ResetInfo{At: &end, Known: true, Source: BudgetResetWindow}
	}
	if b.Backoffs == nil || strings.TrimSpace(p.Backend) == "" {
		return ResetInfo{}
	}
	for _, e := range b.Backoffs() {
		if e.Backend == p.Backend && e.Until.After(now) && e.ResetParsed() {
			until := e.Until
			return ResetInfo{At: &until, Known: true, Source: BudgetResetRateLimit}
		}
	}
	return ResetInfo{}
}

// peerSpendFor is the default PeerSpend: the spend of the peers p names, asked
// of the wired PeerBudget. It returns a note for the verdict when peers are
// named but cannot be counted.
func (b *AutomationBudget) peerSpendFor(p *BudgetPolicy, from, to time.Time) (usd float64, lines []PeerBudgetStatus, note string) {
	if len(p.PeerWindow) == 0 {
		return 0, nil, ""
	}
	if strings.TrimSpace(p.Backend) == "" {
		return 0, nil, peerNoBackendNote
	}
	if b.Peers == nil {
		return 0, nil, ""
	}
	usd, lines = b.Peers.SpendForPeers(context.Background(), p.Backend, p.PeerWindow, from, to)
	return usd, lines, ""
}
