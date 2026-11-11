// Package costreport builds the cost report: token and (opt-in) USD
// estimates for the window, grouped by the operator's choice, with each
// row labeled by the owning provider's billing mode (bundled plan,
// metered API spend or free local inference) and, for release groupings,
// with the shipped units each release contains.
//
// The engine is data-source-agnostic: it takes a Source (spend records
// for a window), an optional ShippedSource (the releases the daemon has
// shipped), a rate function (USD per 1K tokens per backend) and a billing
// profile function (the operator's usage.billing mode per provider).
// Grouping, windowing and pricing stay here; recording stays with the
// daemon.
package costreport

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// GroupBy is the vocabulary of Report.GroupBy.
type GroupBy string

// The groupings the report contract allows.
const (
	GroupByAutomaton  GroupBy = "automaton"
	GroupByAutomation GroupBy = "automation"
	GroupByBackend    GroupBy = "backend"
	// GroupByRelease groups by the release the owning PRD / automation
	// shipped in (earliest shipment wins); unshipped work falls under
	// UnreleasedKey. Requires a ShippedSource for the per-release unit
	// counts.
	GroupByRelease GroupBy = "release"
	GroupByWeek    GroupBy = "week"
)

// UnattributedKey is the row key records fall under when their owner
// field is blank under the automaton, automation and backend groupings —
// "unattributed" read as a name, not a missing one.
const UnattributedKey = "unattributed"

// UnreleasedKey is the release-grouping key for work that has not shipped
// yet.
const UnreleasedKey = "unreleased"

// MixedBillingMode is the Row.BillingMode value when a row spans more
// than one billing mode (mixed providers) and only the USD estimate is
// unambiguous.
const MixedBillingMode = "mixed"

// RetentionDays is the retention horizon of the spend ledger the row
// dollar figures rely on (internal/autonomous keeps 31 days). The report
// carries it so a surface can explain which horizon a number is bounded
// by.
const RetentionDays = 31

// Valid reports whether the grouping is one of the five the contract
// allows.
func (g GroupBy) Valid() bool {
	switch g {
	case GroupByAutomaton, GroupByAutomation, GroupByBackend, GroupByRelease, GroupByWeek:
		return true
	}
	return false
}

// Record is one session's usage inside the report window, as a Source
// supplies it. The report only aggregates; recording stays with the daemon.
type Record struct {
	SessionID    string
	AutomatonID  string // owning automaton, if any (blank is unattributed)
	AutomationID string // owning automation, if any (blank is unattributed)
	// Release is the release the owning PRD / automation shipped in, for
	// release groupings; blank means the work has not shipped (it falls
	// under UnreleasedKey).
	Release   string
	Backend   string // backend family name, rate-table vocabulary
	Peer      bool   // spend made by a federation peer (see SourceOptions)
	At        time.Time
	TokensIn  int
	TokensOut int
	// Extended usage, all optional (zero means unknown / none).
	CacheRead     int
	CacheCreation int
	ToolCalls     int
	// FirstActivity and LastActivity bound the session's wall-clock span.
	FirstActivity time.Time
	LastActivity  time.Time
	// ActiveSeconds is the time the session was actually working.
	ActiveSeconds float64
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

// BillingFunc answers the resolved billing profile for one backend family
// (the rate-table vocabulary), via the operator's usage.billing block.
// Wire it to config.UsageConfig.EffectiveBillingProfile so the assumed
// (unset) profiles stay labelable. nil means "no billing info": rows keep
// their legacy shape — usd_estimate only, no billing fields at all.
type BillingFunc func(backend string) config.EffectiveBillingProfile

// ShippedUnit is one shipped PRD or automation in a release.
type ShippedUnit struct {
	UnitID  string
	Kind    string // "prd" or "automation"
	Release string
}

// ShippedResult is a ShippedSource's answer: the unit list is non-nil even
// when empty.
type ShippedResult struct {
	Units []ShippedUnit
}

// ShippedSource is the release read interface for group_by=release: the
// releases the daemon has shipped and the PRDs / automations inside them.
type ShippedSource interface {
	ShippedUnits() (*ShippedResult, error)
}

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
	// usd_estimate is nil and absent from the JSON.
	RateConfigured bool `json:"rate_configured"`
	// CacheRead, CacheCreation and ToolCalls are summed over the group.
	CacheRead     int `json:"cache_read"`
	CacheCreation int `json:"cache_creation"`
	ToolCalls     int `json:"tool_calls"`
	// FirstActivity / LastActivity are the earliest and latest activity
	// stamps in the group; SpanSeconds is the wall-clock span between
	// them and ActiveSeconds the summed working time.
	FirstActivity *time.Time `json:"first_activity,omitempty"`
	LastActivity  *time.Time `json:"last_activity,omitempty"`
	SpanSeconds   float64    `json:"span_seconds"`
	ActiveSeconds float64    `json:"active_seconds"`
	// USDLabel names what the dollar figure is: for bundled plans an
	// API-equivalent estimate, never a charge. Absent without a figure.
	USDLabel string `json:"usd_label,omitempty"`
	// USDEstimate is the group's dollar estimate at the configured rates,
	// present only when RateConfigured is true and USD display is enabled.
	USDEstimate *float64 `json:"usd_estimate,omitempty"`
	// BillingMode names the row's billing mode when the report was built
	// with a billing profile resolver: bundled_plan, api_metered,
	// free_local, or mixed when a row spans several modes. Absent when no
	// resolver was wired (legacy shape).
	BillingMode string `json:"billing_mode,omitempty"`
	// BillingLabel is the human wording for the mode: bundled plans read
	// "API-equivalent, not billed", unset providers read as assumed
	// api_metered, ... .
	BillingLabel string `json:"billing_label,omitempty"`
	// APIEquivalentUSD is a bundled-plan row's spend priced at API rates —
	// what the usage would cost if metered — not a charge.
	APIEquivalentUSD *float64 `json:"api_equivalent_usd,omitempty"`
	// EstimatedUsagePoints is APIEquivalentUSD expressed in the plan's
	// points (billing-profile PointCostEquivalent), an estimate.
	EstimatedUsagePoints *float64 `json:"estimated_usage_points,omitempty"`
	// ShareOfMonthlyPrice is APIEquivalentUSD divided by the plan's
	// monthly price — the fraction of one month the usage represents.
	ShareOfMonthlyPrice *float64 `json:"share_of_monthly_price,omitempty"`
	// Units is the count of shipped PRDs / automations in the release;
	// release groupings only.
	Units int `json:"units,omitempty"`
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
	// RetentionNote is the always-present human explanation: the 31-day
	// retention window and, when the window reaches further back, that
	// the rows carry the sessions' lifetime totals instead of in-window
	// spend.
	RetentionNote string `json:"retention_note"`
}

// The billing-mode row labels, surfaced verbatim.
const (
	labelBundled  = "bundled plan — API-equivalent, not billed"
	labelFree     = "free local inference — not billed, token usage only"
	labelMetered  = "api_metered — billed per use"
	labelAssumed  = "api_metered (assumed — no usage.billing entry)"
	labelUSDEquiv = "API-equivalent estimate — bundled plan, not billed"
	labelMixedUSD = "mixed billing modes — only the usd_estimate is unambiguous"
)

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
// Blank owner fields land under UnattributedKey rather than a blank key;
// release grouping keys on the record's release, unreleased work under
// UnreleasedKey.
func groupKey(g GroupBy, r Record, since time.Time, weekStart time.Weekday) string {
	switch g {
	case GroupByAutomaton:
		if r.AutomatonID == "" {
			return UnattributedKey
		}
		return r.AutomatonID
	case GroupByAutomation:
		if r.AutomationID == "" {
			return UnattributedKey
		}
		return r.AutomationID
	case GroupByBackend:
		if r.Backend == "" {
			return UnattributedKey
		}
		return r.Backend
	case GroupByRelease:
		if r.Release == "" {
			return UnreleasedKey
		}
		return r.Release
	case GroupByWeek:
		at := r.At
		if at.IsZero() {
			at = since // unknown stamp: anchor to the window start
		}
		return weekKey(at, weekStart)
	}
	return ""
}

// retentionNote is the always-present explanation of which spend horizon
// a row's dollar figure is bounded by.
func retentionNote(lifetimeFallback bool) string {
	base := "Dollar amounts rest on the spend ledger's"
	note := fmt.Sprintf("%s %d-day retention window", base, RetentionDays)
	if lifetimeFallback {
		return note + ". This request reaches further back, so the rows carry the sessions' lifetime totals: per-event spend lines older than the window are pruned."
	}
	return note + ". Spend older than the window is not in the ledger."
}

// finite reports whether f is a usable finite number.
func finite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// applyBilling resolves the row's backends against the billing profiles
// (via billing) and fills the billing fields from the estimate est (which
// is nil when the row is not rate-completed or USD display is off; the
// label is still set so the mode stays visible).
func applyBilling(row *Row, backends []string, billing BillingFunc, est *float64) {
	if billing == nil || len(backends) == 0 {
		return
	}
	modes := map[string]bool{}
	allDefault := true
	for _, b := range backends {
		p := billing(b)
		modes[p.Profile.Mode] = true
		if !p.IsDefault {
			allDefault = false
		}
	}

	switch {
	case len(modes) > 1:
		row.BillingMode = MixedBillingMode
		row.BillingLabel = labelMixedUSD
		row.USDEstimate = est

	case modes[config.BillingModeBundledPlan]:
		row.BillingMode = config.BillingModeBundledPlan
		row.BillingLabel = labelBundled
		if est == nil {
			return
		}
		// The spend priced at API rates is an equivalence, not a charge:
		// it leaves usd_estimate and moves to api_equivalent_usd.
		row.APIEquivalentUSD = est
		row.USDLabel = labelUSDEquiv
		for _, b := range backends {
			p := billing(b).Profile
			if p.MonthlyPrice <= 0 || !finite(p.MonthlyPrice) {
				continue
			}
			if pc := p.PointCostEquivalent(); pc > 0 && finite(pc) {
				points := *est / pc
				row.EstimatedUsagePoints = &points
			}
			share := *est / p.MonthlyPrice
			row.ShareOfMonthlyPrice = &share
			return
		}

	case modes[config.BillingModeFreeLocal]:
		row.BillingMode = config.BillingModeFreeLocal
		row.BillingLabel = labelFree
		// No USD figure at all: nothing to bill and no rate to price by.

	default: // api_metered
		row.BillingMode = config.BillingModeAPIMetered
		if allDefault {
			row.BillingLabel = labelAssumed
		} else {
			row.BillingLabel = labelMetered
		}
		row.USDEstimate = est
	}
}

// mergeReleaseUnits renders one row per release the daemon has shipped
// (with the shipped unit count) and fills Units on rows that already
// exist for the same release; rows keep their token totals either way.
func mergeReleaseUnits(out []Row, units []ShippedUnit) []Row {
	byRef := map[string]int{}
	for _, u := range units {
		if u.Release == "" {
			continue
		}
		byRef[u.Release]++
	}
	exists := map[string]bool{}
	for i := range out {
		exists[out[i].Key] = true
		if n, ok := byRef[out[i].Key]; ok {
			out[i].Units = n
		}
	}
	extra := make([]Row, 0, len(byRef))
	for ref, n := range byRef {
		if !exists[ref] {
			extra = append(extra, Row{Key: ref, Units: n})
		}
	}
	out = append(out, extra...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Build assembles the cost report over src using the cost_report config
// section cfg, pricing rows with rateFor, billing them with billing (nil
// keeps the legacy USD-only shape), and — for release groupings — counting
// the shipped units from shipped.
//
// Window normalization uses the config only: an open Until becomes now, an
// open Since becomes Until minus the config default window in days. Rows
// group under the requested or defaulted GroupBy (releases under their
// shipped ref, unreleased work under "unreleased"; blank owners under
// "unattributed"), keep peer records only when the config says so, and
// carry USD only when the config and the rate table allow it.
//
// Build rejects group_by=release without a shipped ShippedSource: without
// it the per-release unit column would be wrong rather than absent.
func Build(
	cfg config.CostReportConfig,
	req Request,
	src Source,
	shipped ShippedSource,
	rateFor RateFunc,
	billing BillingFunc,
	now time.Time,
) (*Report, error) {
	if src == nil {
		return nil, fmt.Errorf("costreport: source is required")
	}
	if now.IsZero() {
		now = time.Now()
	}
	if req.GroupBy == "" {
		req.GroupBy = GroupBy(cfg.EffectiveGroupBy())
	}
	if !req.GroupBy.Valid() {
		return nil, fmt.Errorf("costreport: group_by %q must be one of automaton, automation, backend, release, week", string(req.GroupBy))
	}
	if req.GroupBy == GroupByRelease && shipped == nil {
		return nil, fmt.Errorf("costreport: group_by=release requires a shipped source; this surface wires none")
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
		sessions              int
		tin, tout             int
		cread, ccreate, tools int
		first, last           time.Time
		active                float64
		usd                   float64
		allRate               bool
		backends              []string
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
		a := add(groupKey(req.GroupBy, r, since, weekStart))
		a.sessions++
		a.tin += r.TokensIn
		a.tout += r.TokensOut
		a.cread += r.CacheRead
		a.ccreate += r.CacheCreation
		a.tools += r.ToolCalls
		a.active += r.ActiveSeconds
		lo, hi := r.FirstActivity, r.LastActivity
		if lo.IsZero() {
			lo = r.At
		}
		if hi.IsZero() {
			hi = r.At
		}
		if !lo.IsZero() && (a.first.IsZero() || lo.Before(a.first)) {
			a.first = lo
		}
		if !hi.IsZero() && hi.After(a.last) {
			a.last = hi
		}
		a.backends = append(a.backends, r.Backend)
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
		row := Row{Key: key, Sessions: a.sessions, TokensIn: a.tin, TokensOut: a.tout,
			CacheRead: a.cread, CacheCreation: a.ccreate, ToolCalls: a.tools, ActiveSeconds: a.active}
		if !a.first.IsZero() {
			f, l := a.first, a.last
			row.FirstActivity, row.LastActivity = &f, &l
			row.SpanSeconds = l.Sub(f).Seconds()
		}
		var est *float64
		if a.allRate && showUSD {
			v := a.usd
			est = &v
			row.RateConfigured = true
		}
		if billing != nil {
			applyBilling(&row, a.backends, billing, est)
		} else {
			row.USDEstimate = est
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })

	if req.GroupBy == GroupByRelease {
		sres, err := shipped.ShippedUnits()
		if err != nil {
			return nil, fmt.Errorf("costreport: loading shipped releases: %w", err)
		}
		if sres != nil {
			out = mergeReleaseUnits(out, sres.Units)
		}
	}

	lifetimeFallback := res.LifetimeFallback ||
		since.Before(now.AddDate(0, 0, -RetentionDays))

	return &Report{
		GroupBy:          req.GroupBy,
		Since:            since,
		Until:            until,
		Rows:             out,
		RetentionDays:    RetentionDays,
		LifetimeFallback: lifetimeFallback,
		RetentionNote:    retentionNote(lifetimeFallback),
	}, nil
}
