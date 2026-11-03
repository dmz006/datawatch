package costreport

import (
	"fmt"
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// GroupBy is the vocabulary of report.GroupBy.
type GroupBy string

// The four groupings the report contract allows.
const (
	GroupByAutomaton  GroupBy = "automaton"
	GroupByAutomation GroupBy = "automation"
	GroupByBackend    GroupBy = "backend"
	GroupByWeek       GroupBy = "week"
)

// RetentionDays is the retention horizon of the spend ledger the row
// dollar figures rely on (internal/autonomous keeps 31 days). The report
// carries it so a surface can explain which horizon a number is bounded by.
const RetentionDays = 31

// Valid reports whether the grouping is one of the four the contract allows.
func (g GroupBy) Valid() bool {
	switch g {
	case GroupByAutomaton, GroupByAutomation, GroupByBackend, GroupByWeek:
		return true
	}
	return false
}

// Record is one session's usage inside the report window, as a Source
// supplies it. The report only aggregates; recording stays with the daemon.
type Record struct {
	SessionID    string
	AutomatonID  string // owning automaton, if any
	AutomationID string // owning automation, if any
	Backend      string // backend family name, rate-table vocabulary
	// Peer marks spend made by a federation peer that opted into reporting
	// it to this daemon. The report drops peer records unless
	// cost_report.include_peer_spend is true, whether or not the Source
	// already filtered them.
	Peer bool
	// At is when the usage happened; a zero At is treated as unknown and
	// kept in-window rather than dropped.
	At        time.Time
	TokensIn  int
	TokensOut int
}

// SourceOptions is the query the report hands to a Source after normalizing
// the window against the config defaults.
type SourceOptions struct {
	Since            time.Time // open when zero
	Until            time.Time // open when zero
	IncludePeerSpend bool
}

// SourceResult is a Source's answer for one window.
type SourceResult struct {
	Records []Record
	// LifetimeFallback is true when the window reaches further back than
	// the store's retention horizon and its dollar answers fell back to
	// lifetime session totals — the same fallback the per-PRD USD rollup
	// in internal/autonomous already performs.
	LifetimeFallback bool
}

// Source is the spend read interface the report builds on. Today it is
// served from the daemon's recorded sessions; a per-call metering store or
// a long-history store can replace it without touching this package or the
// report's surfaces, because they only speak this interface.
type Source interface {
	Records(opts SourceOptions) (*SourceResult, error)
}

// RateFunc answers the USD-per-1K-token rate for one backend and whether a
// rate exists at all. Wire it to the per-backend table in internal/session
// (cost.go), including its family fallback, so the report prices rows the
// same way the daemon records them. A nil RateFunc means "no rate is
// configured for anything": every row then has rate_configured false.
type RateFunc func(backend string) (session.CostRate, bool)

// Request is one report request: the grouping and the optional window
// bounds. Zero / empty fields fall back to the cost_report config
// defaults.
type Request struct {
	GroupBy GroupBy // zero → the config default (automaton)
	Since   time.Time
	Until   time.Time
}

// Row is one grouped line of the report.
type Row struct {
	Key string `json:"key"`
	// Sessions in the group over the window.
	Sessions int `json:"sessions"`
	// Tokens come first: always present, no rate required.
	TokensIn  int `json:"tokens_in"`
	TokensOut int `json:"tokens_out"`
	// RateConfigured is true when a rate exists for every backend in the
	// group; false when at least one backend has no rate, in which case
	// USDEstimate is nil and absent from the JSON.
	RateConfigured bool `json:"rate_configured"`
	// USDEstimate is the group's dollar estimate at the configured rates,
	// present only when RateConfigured is true and USD display is enabled.
	USDEstimate *float64 `json:"usd_estimate,omitempty"`
}

// Report is the finished cost report.
type Report struct {
	GroupBy GroupBy `json:"group_by"`
	Since   time.Time
	Until   time.Time
	// Rows is sorted by Key for stable output.
	Rows []Row `json:"rows"`
	// RetentionDays is the retention horizon of the spend store the
	// dollar figures rest on.
	RetentionDays int `json:"retention_days"`
	// LifetimeFallback is true when the window fell back to lifetime
	// session totals because it reaches beyond the retention horizon.
	LifetimeFallback bool `json:"lifetime_fallback"`
}

const (
	weekStartMonday = "monday"
	weekStartSunday = "sunday"
)

// weekStartIndex maps the config vocabulary (monday / sunday) to
// time.Weekday indices. Unknown values cannot occur: the caller goes
// through config.EffectiveWeekStart, which falls back to monday.
func weekStartIndex(name string) int {
	if name == weekStartSunday {
		return int(time.Sunday)
	}
	return int(time.Monday)
}

// weekKey is the date (UTC, YYYY-MM-DD) of the week-start day containing
// at, using the configured week start.
func weekKey(at time.Time, weekStart time.Weekday) string {
	offset := (at.Weekday() - weekStart + 7) % 7
	first := at.AddDate(0, 0, -int(offset)).UTC()
	return first.Format("2006-01-02")
}

// groupKey picks the row key for one record under the given grouping.
func groupKey(g GroupBy, r Record, since time.Time, weekStart time.Weekday) string {
	switch g {
	case GroupByAutomaton:
		return r.AutomatonID
	case GroupByAutomation:
		return r.AutomationID
	case GroupByBackend:
		return r.Backend
	case GroupByWeek:
		at := r.At
		if at.IsZero() {
			at = since // unknown stamp: anchor to the window start
		}
		return weekKey(at, weekStart)
	}
	return ""
}

// Build assembles the cost report over src using the cost_report config
// section cfg, pricing rows with rateFor.
//
// Window normalization uses the config only: an open Until becomes now, an
// open Since becomes Until minus the config default window in days. Rows
// group under the requested or defaulted GroupBy, keep peer records only
// when the config says so, and carry USD only when the config and the rate
// table allow it.
func Build(cfg config.CostReportConfig, req Request, src Source, rateFor RateFunc, now time.Time) (*Report, error) {
	if src == nil {
		return nil, fmt.Errorf("costreport: source is required")
	}
	if now.IsZero() {
		now = time.Now()
	}

	groupBy := req.GroupBy
	if groupBy == "" {
		groupBy = GroupBy(cfg.EffectiveGroupBy())
	}
	if !groupBy.Valid() {
		return nil, fmt.Errorf("costreport: group_by %q must be one of automaton, automation, backend, week", string(groupBy))
	}

	until := req.Until
	if until.IsZero() {
		until = now
	}
	since := req.Since
	if since.IsZero() {
		window := time.Duration(cfg.EffectiveWindowDays()) * 24 * time.Hour
		since = until.Add(-window)
	}
	peer := cfg.EffectiveIncludePeerSpend()

	res, err := src.Records(SourceOptions{
		Since:            since,
		Until:            until,
		IncludePeerSpend: peer,
	})
	if err != nil {
		return nil, fmt.Errorf("costreport: fetching spend: %w", err)
	}

	showUSD := cfg.EffectiveShowUSDEstimates()
	weekStart := time.Weekday(weekStartIndex(cfg.EffectiveWeekStart()))

	type acc struct {
		sessions  int
		tin, tout int
		usd       float64
		allRate   bool
	}
	buckets := map[string]*acc{}
	add := func(key string) *acc {
		a := buckets[key]
		if a == nil {
			a = &acc{allRate: rateFor != nil}
			buckets[key] = a
		}
		return a
	}

	for _, r := range res.Records {
		if r.Peer && !peer {
			continue
		}
		if !r.At.IsZero() && (r.At.Before(since) || r.At.After(until)) {
			continue
		}
		a := add(groupKey(groupBy, r, since, weekStart))
		a.sessions++
		a.tin += r.TokensIn
		a.tout += r.TokensOut
		if rateFor == nil {
			a.allRate = false
			continue
		}
		rate, ok := rateFor(r.Backend)
		if !ok {
			a.allRate = false
			continue
		}
		a.usd += session.EstimateCost(rate, r.TokensIn, r.TokensOut)
	}

	out := make([]Row, 0, len(buckets))
	for key, a := range buckets {
		row := Row{Key: key, Sessions: a.sessions, TokensIn: a.tin, TokensOut: a.tout}
		if a.allRate && showUSD {
			v := a.usd
			row.RateConfigured = true
			row.USDEstimate = &v
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })

	return &Report{
		GroupBy:          groupBy,
		Since:            since,
		Until:            until,
		Rows:             out,
		RetentionDays:    RetentionDays,
		LifetimeFallback: res.LifetimeFallback,
	}, nil
}
