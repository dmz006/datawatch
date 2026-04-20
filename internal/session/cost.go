// BL6 — per-backend cost rates + summary helpers.
//
// Rates are USD per 1K tokens. Default values reflect the public
// price card for each backend at the time of writing — operators
// override via session.cost_rates in config when their plan differs.
//
// EstimateCost returns the dollar estimate for a given (in, out)
// token count. SummaryFor aggregates a slice of sessions into total
// tokens + dollars. Both are pure helpers; the manager populates the
// per-session counters.

package session

import (
	"errors"
	"strings"
)

// ErrSessionNotFound is returned by helpers that require an existing
// session but the supplied ID didn't match.
var ErrSessionNotFound = errors.New("session not found")

// CostRate holds USD per 1K input/output tokens for one backend.
type CostRate struct {
	InPerK  float64 `json:"in_per_k"`
	OutPerK float64 `json:"out_per_k"`
}

// DefaultCostRates returns the built-in rate table. Operators can
// override per-backend via SetCostRates.
func DefaultCostRates() map[string]CostRate {
	return map[string]CostRate{
		"claude-code":     {InPerK: 0.003, OutPerK: 0.015},   // sonnet 4.x ballpark
		"opencode":        {InPerK: 0.003, OutPerK: 0.015},
		"opencode-acp":    {InPerK: 0.003, OutPerK: 0.015},
		"opencode-prompt": {InPerK: 0.003, OutPerK: 0.015},
		"aider":           {InPerK: 0.003, OutPerK: 0.015},
		"goose":           {InPerK: 0.003, OutPerK: 0.015},
		"gemini":          {InPerK: 0.0035, OutPerK: 0.0105}, // gemini 1.5 pro ballpark
		"openwebui":       {InPerK: 0, OutPerK: 0},           // self-hosted by default
		"ollama":          {InPerK: 0, OutPerK: 0},           // local
		"shell":           {InPerK: 0, OutPerK: 0},
	}
}

// EstimateCost returns the USD cost of (tokensIn, tokensOut) at the
// given rate.
func EstimateCost(rate CostRate, tokensIn, tokensOut int) float64 {
	return float64(tokensIn)/1000*rate.InPerK +
		float64(tokensOut)/1000*rate.OutPerK
}

// CostSummary aggregates one or many sessions for reporting.
type CostSummary struct {
	Sessions    int                    `json:"sessions"`
	TotalIn     int                    `json:"total_tokens_in"`
	TotalOut    int                    `json:"total_tokens_out"`
	TotalUSD    float64                `json:"total_usd"`
	PerBackend  map[string]CostBucket  `json:"per_backend,omitempty"`
}

// CostBucket is the per-backend breakdown.
type CostBucket struct {
	Sessions  int     `json:"sessions"`
	TokensIn  int     `json:"tokens_in"`
	TokensOut int     `json:"tokens_out"`
	USD       float64 `json:"usd"`
}

// SummaryFor rolls a slice of sessions into one CostSummary.
func SummaryFor(sessions []*Session) CostSummary {
	out := CostSummary{PerBackend: map[string]CostBucket{}}
	for _, s := range sessions {
		out.Sessions++
		out.TotalIn += s.TokensIn
		out.TotalOut += s.TokensOut
		out.TotalUSD += s.EstCostUSD
		b := out.PerBackend[s.LLMBackend]
		b.Sessions++
		b.TokensIn += s.TokensIn
		b.TokensOut += s.TokensOut
		b.USD += s.EstCostUSD
		out.PerBackend[s.LLMBackend] = b
	}
	return out
}

// AddUsage updates a session's running token + cost counters using
// the rate for sess.LLMBackend (or override if non-empty).
func (m *Manager) AddUsage(sessID string, tokensIn, tokensOut int, override CostRate) error {
	sess, ok := m.GetSession(sessID)
	if !ok {
		// fall back to short-id resolver
		if alt, ok2 := m.GetSession(sessID); ok2 {
			sess = alt
		} else {
			return ErrSessionNotFound
		}
	}
	rate := override
	if rate.InPerK == 0 && rate.OutPerK == 0 {
		if r, ok := m.costRate(sess.LLMBackend); ok {
			rate = r
		}
	}
	sess.TokensIn += tokensIn
	sess.TokensOut += tokensOut
	sess.EstCostUSD += EstimateCost(rate, tokensIn, tokensOut)
	return m.SaveSession(sess)
}

// costRate returns the rate for backend, falling back to the matching
// family (e.g. "opencode-acp" → "opencode") and finally the empty rate.
func (m *Manager) costRate(backend string) (CostRate, bool) {
	if m.costRates == nil {
		m.costRates = DefaultCostRates()
	}
	if r, ok := m.costRates[backend]; ok {
		return r, true
	}
	if idx := strings.IndexByte(backend, '-'); idx > 0 {
		if r, ok := m.costRates[backend[:idx]]; ok {
			return r, true
		}
	}
	return CostRate{}, false
}

// SetCostRates replaces the rate table (operator config).
func (m *Manager) SetCostRates(rates map[string]CostRate) {
	if rates == nil {
		m.costRates = DefaultCostRates()
		return
	}
	m.costRates = rates
}
