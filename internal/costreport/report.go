// Package costreport builds the cost report: token and (opt-in) USD
// estimates for the window, grouped by the operator's choice, with each
// row labeled by the owning provider's billing mode (bundled plan,
// metered API spend or free local inference) and, for release groupings,
// with the shipped units each release contains — plus the per-profile
// and per-role startup token tables: how many attempts each worker
// profile and role ran, the average startup token size over the attempts
// that carry one, and the estimated / reported / no-data source counts.
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
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
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

// StartupSource values: how an attempt's startup token size was obtained.
const (
	// StartupSourceEstimated marks a size estimated from the generated
	// starting context at launch (the launch never saw real usage).
	StartupSourceEstimated = "estimated"
	// StartupSourceReported marks a size the worker or its runner
	// reported at runtime.
	StartupSourceReported = "reported"
)

// Row keys for attempts that ran with no profile or no role recorded.
const (
	// UnprofiledKey is the StartupByProfile row key for attempts that ran
	// with no worker profile.
	UnprofiledKey = "unprofiled"
	// NoRoleKey is the StartupByRole row key for attempts that ran with
	// no role.
	NoRoleKey = "no role"
)

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
	TaskID       string // task the spend belongs to, from the spend ledger
	Role         string // spend role (worker, verification, ...), from the spend ledger
	// FromLedger is true when the record is a spend-ledger entry;
	// LedgerAutomatonID is that entry's own automaton id, before any
	// session-owner fallback. The attribution check reads only these.
	FromLedger        bool
	LedgerAutomatonID string
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
	// Attempts are the window's per-attempt startup records (see
	// Attempt); empty when the source reports none. They feed the
	// report's per-profile and per-role startup tables (Report.
	// StartupByProfile / StartupByRole).
	Attempts []Attempt
	// LifetimeFallback is true when the window reaches further back than
	// the store's retention horizon and its dollar answers fell back to
	// lifetime session totals — the same fallback the per-PRD USD rollup
	// in internal/autonomous already performs.
	LifetimeFallback bool
}

// Attempt is one worker attempt's startup record a Source supplies
// alongside the spend records: the profile and role the attempt's worker
// started with, the startup token size the launch recorded for it, and
// how that size was obtained (StartupSourceEstimated,
// StartupSourceReported, or neither). An attempt with zero tokens is a
// record from before startup sizes were captured: it still counts as an
// attempt and as "no data", but stays out of the average.
type Attempt struct {
	// Profile names the worker profile the attempt's worker started with;
	// empty for an unprofiled attempt (row key UnprofiledKey).
	Profile string
	// Role names the role of the attempt's worker session; empty for a
	// role-less attempt (row key NoRoleKey).
	Role string
	// StartupTokens is the token size the attempt's worker started with;
	// zero when the launch carried no starting context (or was recorded
	// before startup sizes existed).
	StartupTokens int
	// Source is how StartupTokens was obtained: StartupSourceEstimated,
	// StartupSourceReported, or "" when neither.
	Source string
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

// Units a total can be expressed in. A group never sums across units.
const (
	UnitDollars = "dollars"
	UnitPoints  = "points"
	UnitNone    = "none"
)

// UnitTotal is the total of every record whose backend bills in one unit:
// dollars for api backends, points (and percent of the allowance) for
// allowance backends, tokens and time only for local backends.
type UnitTotal struct {
	Unit          string   `json:"unit"`
	BillingMode   string   `json:"billing_mode"`
	Sessions      int      `json:"sessions"`
	TokensIn      int      `json:"tokens_in"`
	TokensOut     int      `json:"tokens_out"`
	ActiveSeconds float64  `json:"active_seconds"`
	Dollars       *float64 `json:"dollars,omitempty"`
	Points        *float64 `json:"points,omitempty"`
	Percent       *float64 `json:"percent,omitempty"`
}

// LLMLookup finds the LLM entry for a backend name.
type LLMLookup func(backend string) (inference.LLM, bool)

// ResolvedBilling builds a BillingFunc whose mode comes from
// config.ResolveBillingMode on the backend's LLM entry (which itself falls
// back to the usage.billing profile). Backends with no entry resolve as an
// entry of that kind. lookup and cfg may be nil.
func ResolvedBilling(cfg *config.Config, lookup LLMLookup) BillingFunc {
	return func(backend string) config.EffectiveBillingProfile {
		var entry inference.LLM
		found := false
		if lookup != nil {
			entry, found = lookup(backend)
		}
		if !found {
			entry = inference.LLM{Name: backend, Kind: inference.Kind(backend)}
		}
		var eff config.EffectiveBillingProfile
		if cfg != nil {
			eff = cfg.Usage.EffectiveBillingProfile(backend)
		} else {
			eff = config.EffectiveBillingProfile{Profile: config.DefaultBillingProfile(), IsDefault: true}
		}
		switch config.ResolveBillingMode(entry, cfg) {
		case config.BillingModeAllowance:
			eff.Profile.Mode = config.BillingModeBundledPlan
		case config.BillingModeLocal:
			eff.Profile.Mode = config.BillingModeFreeLocal
		default:
			eff.Profile.Mode = config.BillingModeAPIMetered
		}
		if found && strings.TrimSpace(entry.BillingMode) != "" {
			eff.IsDefault = false
		}
		return eff
	}
}

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
	// UnitTotals is one total per billing unit the row spans; present when
	// a billing resolver was wired. Dollars are never summed with points.
	UnitTotals []UnitTotal `json:"unit_totals,omitempty"`

	// WindowPartial is true when the row's first activity precedes the
	// window start: the row is the in-window slice of a longer life, so
	// its figures are not the Automaton's whole spend.
	WindowPartial bool `json:"window_partial,omitempty"`
	// WindowNote says so plainly when WindowPartial is set.
	WindowNote string `json:"window_note,omitempty"`
	// SpendUnknown is true when the window reaches before the ledger
	// horizon: spend before the horizon is unknown, never zero and never
	// backfilled.
	SpendUnknown bool `json:"spend_unknown,omitempty"`
	// SpendUnknownNote explains SpendUnknown on the row.
	SpendUnknownNote string `json:"spend_unknown_note,omitempty"`
}

// Report is the finished cost report.
type Report struct {
	GroupBy GroupBy `json:"group_by"`
	Since   time.Time
	Until   time.Time
	// Rows is sorted by Key for stable output.
	Rows []Row `json:"rows"`
	// TotalsByUnit is one total per billing unit across all rows; dollars
	// are never summed with points. Present when a billing resolver was wired.
	TotalsByUnit []UnitTotal `json:"totals_by_unit,omitempty"`
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

	// StartupByProfile and StartupByRole aggregate the window's attempt
	// startup token sizes by the worker profile and role the attempt's
	// worker started with: attempts per key, the average startup size
	// over the attempts that carry one (omitted when none do), and the
	// estimated / reported / no-data source counts. Rows are sorted by
	// key; the slice is empty (not nil) when the source reports no
	// attempts — records from before startup sizes were captured land
	// in a key's no_data count, never in the average.
	StartupByProfile []StartupRow `json:"startup_by_profile"`
	StartupByRole    []StartupRow `json:"startup_by_role"`
	// WindowNote is the top-level note, present when any row is a partial
	// slice of a longer life. It names the default window and the ledger.
	WindowNote string `json:"window_note,omitempty"`
	// UnknownSpendNote is present when the window reaches before the
	// ledger horizon.
	UnknownSpendNote string `json:"unknown_spend_note,omitempty"`
	// UnknownBefore is the ledger horizon: spend before it is unknown.
	// Set only with UnknownSpendNote.
	UnknownBefore *time.Time `json:"unknown_before,omitempty"`
	// Attribution is the attribution check over the window's ledger
	// records.
	Attribution AttributionCheck `json:"attribution"`
}

// StartupRow is one row of Report.StartupByProfile / StartupByRole (see
// Attempt for what an attempt record is): the profile or role key, how
// many attempts ran with it, the average startup token size over the
// attempts that have one, and the source counts — estimated, reported,
// and no data (zero tokens, or a source the engine does not name).
type StartupRow struct {
	// Key is the worker profile (or role) the row counts attempts for;
	// UnprofiledKey / NoRoleKey when the attempts ran with no profile /
	// role.
	Key string `json:"key"`
	// Attempts is how many attempts ran with this key, including the no-
	// data ones.
	Attempts int `json:"attempts"`
	// AvgStartupTokens is the mean startup token size over the attempts
	// that carry one (zero-token attempts stay out of it); nil when no
	// attempt for this key has a size.
	AvgStartupTokens *float64 `json:"avg_startup_tokens,omitempty"`
	// Estimated counts the attempts whose size came from the launch
	// estimate (StartupSourceEstimated).
	Estimated int `json:"estimated"`
	// Reported counts the attempts whose size was reported at runtime
	// (StartupSourceReported).
	Reported int `json:"reported"`
	// NoData counts the attempts with zero startup tokens (records from
	// before startup sizes were captured) or an unrecognized source.
	NoData int `json:"no_data"`
}

// windowRowNote is the per-row text for a row that began before the window.
func windowRowNote(since time.Time, defaultDays int) string {
	return fmt.Sprintf("Activity began before this window (starts %s); figures cover the window only, not the whole life. The default window is %d days and the spend ledger keeps %d days. A missing row means no activity in the window, not zero spend.",
		since.UTC().Format("2006-01-02"), defaultDays, RetentionDays)
}

// windowReportNote is the top-level text for partial rows.
func windowReportNote(since time.Time, defaultDays, partial int) string {
	return fmt.Sprintf("%d row(s) began before the window start (%s) and show only their in-window part. The default window is %d days and the spend ledger keeps %d days; widen the window to see more. An Automaton absent from the report had no activity in the window; that is not zero spend.",
		partial, since.UTC().Format("2006-01-02"), defaultDays, RetentionDays)
}

// unknownSpendNote is the text for a window reaching before the ledger
// horizon.
func unknownSpendNote(horizon time.Time) string {
	return fmt.Sprintf("Spend before %s is unknown: it is older than the %d-day ledger. It is not zero and is not backfilled.",
		horizon.UTC().Format("2006-01-02"), RetentionDays)
}

// The billing-mode row labels, surfaced verbatim.
const (
	labelBundled  = "bundled plan — API-equivalent, not billed"
	labelFree     = "free local inference — not billed, token usage only"
	labelMetered  = "api_metered — billed per use"
	labelAssumed  = "api_metered (assumed — no usage.billing entry)"
	labelUSDEquiv = "API-equivalent estimate — bundled plan, not billed"
	labelMixedUSD = "mixed billing modes — see the per-unit totals"
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
		// Dollars of api backends and points of allowance backends are
		// never summed; the per-unit totals carry each figure.

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

func addPtr(p *float64, v float64) *float64 {
	if p == nil {
		return &v
	}
	n := *p + v
	return &n
}

// sortedUnits returns the unit totals in a stable order (dollars, points,
// none), or nil when there are none.
func sortedUnits(m map[string]*UnitTotal) []UnitTotal {
	var out []UnitTotal
	for _, k := range []string{UnitDollars, UnitPoints, UnitNone} {
		if u := m[k]; u != nil {
			out = append(out, *u)
		}
	}
	return out
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
// the shipped units from shipped. The report always carries the
// per-profile and per-role startup tables, aggregated from the attempt
// records src's answer reports (Report.StartupByProfile /
// StartupByRole; empty when the source reports none).
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
		units                 map[string]*UnitTotal
	}
	unitAcc := func(m map[string]*UnitTotal, mode string) *UnitTotal {
		unit := UnitDollars
		switch mode {
		case config.BillingModeBundledPlan:
			unit = UnitPoints
		case config.BillingModeFreeLocal:
			unit = UnitNone
		}
		u := m[unit]
		if u == nil {
			u = &UnitTotal{Unit: unit, BillingMode: mode}
			m[unit] = u
		}
		return u
	}
	total := map[string]*UnitTotal{}
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
		var usdRec *float64
		if rateFor != nil {
			if rate, ok := rateFor(r.Backend); ok && showUSD {
				v := session.EstimateCost(rate, r.TokensIn, r.TokensOut)
				usdRec = &v
			}
		}
		if billing != nil {
			if a.units == nil {
				a.units = map[string]*UnitTotal{}
			}
			p := billing(r.Backend).Profile
			for _, m := range []map[string]*UnitTotal{a.units, total} {
				u := unitAcc(m, p.Mode)
				u.Sessions++
				u.TokensIn += r.TokensIn
				u.TokensOut += r.TokensOut
				u.ActiveSeconds += r.ActiveSeconds
				if usdRec == nil {
					continue
				}
				switch u.Unit {
				case UnitDollars:
					u.Dollars = addPtr(u.Dollars, *usdRec)
				case UnitPoints:
					if p.MonthlyPrice > 0 && finite(p.MonthlyPrice) {
						if pc := p.PointCostEquivalent(); pc > 0 && finite(pc) {
							u.Points = addPtr(u.Points, *usdRec/pc)
						}
						u.Percent = addPtr(u.Percent, *usdRec/p.MonthlyPrice*100)
					}
				}
			}
		}
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
			row.UnitTotals = sortedUnits(a.units)
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

	attempts := res.Attempts
	if attempts == nil {
		attempts = []Attempt{}
	}

	rep := &Report{
		GroupBy:          req.GroupBy,
		Since:            since,
		Until:            until,
		Rows:             out,
		RetentionDays:    RetentionDays,
		LifetimeFallback: lifetimeFallback,
		RetentionNote:    retentionNote(lifetimeFallback),
		TotalsByUnit:     sortedUnits(total),
		StartupByProfile: StartupTable(attempts, ProfileKey),
		StartupByRole:    StartupTable(attempts, RoleKey),
	}

	rep.Attribution = CheckAttribution(res.Records, since, until, peer)

	defDays := cfg.EffectiveWindowDays()
	partial := 0
	for i := range rep.Rows {
		r := &rep.Rows[i]
		if r.FirstActivity != nil && r.FirstActivity.Before(since) {
			r.WindowPartial = true
			r.WindowNote = windowRowNote(since, defDays)
			partial++
		}
	}
	if partial > 0 {
		rep.WindowNote = windowReportNote(since, defDays, partial)
	}
	horizon := now.AddDate(0, 0, -RetentionDays)
	if since.Before(horizon) {
		rep.UnknownSpendNote = unknownSpendNote(horizon)
		rep.UnknownBefore = &horizon
		for i := range rep.Rows {
			// Only rows with activity before the horizon (or with
			// unknown activity times) carry unknown spend; rows wholly
			// inside the ledger are known.
			fa := rep.Rows[i].FirstActivity
			if fa == nil || fa.Before(horizon) {
				rep.Rows[i].SpendUnknown = true
				rep.Rows[i].SpendUnknownNote = rep.UnknownSpendNote
			}
		}
	}
	return rep, nil
}

// ProfileKey is the StartupByProfile row key for an attempt: its
// profile, or UnprofiledKey when the attempt ran with none.
func ProfileKey(a Attempt) string {
	if a.Profile == "" {
		return UnprofiledKey
	}
	return a.Profile
}

// RoleKey is the StartupByRole row key for an attempt: its role, or
// NoRoleKey when the attempt ran with none.
func RoleKey(a Attempt) string {
	if a.Role == "" {
		return NoRoleKey
	}
	return a.Role
}

// startupAcc accumulates one key's attempt counts for StartupTable.
type startupAcc struct {
	attempts   int
	tokenSum   int
	tokenCount int
	estimated  int
	reported   int
	noData     int
}

// add folds one attempt into the accumulator: always an attempt; the
// token size into the average only when nonzero; the source into
// estimated / reported when named; and no_data when the attempt has no
// token size or its source is not one the engine names.
func (s *startupAcc) add(a Attempt) {
	s.attempts++
	if a.StartupTokens > 0 {
		s.tokenSum += a.StartupTokens
		s.tokenCount++
	}
	switch a.Source {
	case StartupSourceEstimated:
		s.estimated++
	case StartupSourceReported:
		s.reported++
	}
	if a.StartupTokens == 0 || (a.Source != StartupSourceEstimated && a.Source != StartupSourceReported) {
		s.noData++
	}
}

// StartupTable aggregates the attempts into one StartupRow per key (via
// keyOf), sorted by key, with the average startup size over the attempts
// that carry one. An empty attempt list yields an empty (not nil) row
// slice, so the JSON field is always present as [] or a row list.
func StartupTable(attempts []Attempt, keyOf func(Attempt) string) []StartupRow {
	acc := map[string]*startupAcc{}
	for _, a := range attempts {
		key := keyOf(a)
		s := acc[key]
		if s == nil {
			s = &startupAcc{}
			acc[key] = s
		}
		s.add(a)
	}
	rows := make([]StartupRow, 0, len(acc))
	for key, s := range acc {
		row := StartupRow{Key: key, Attempts: s.attempts, Estimated: s.estimated, Reported: s.reported, NoData: s.noData}
		if s.tokenCount > 0 {
			avg := float64(s.tokenSum) / float64(s.tokenCount)
			row.AvgStartupTokens = &avg
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	return rows
}

// StartupText renders the per-profile and per-role startup tables as text
// (a labeled header line, then one line per row with the average as "-"
// where no attempt for the key carries a size; "no attempts" where a
// table is empty), for the surfaces that print the report as text. It
// renders both tables, profile first.
func (r *Report) StartupText() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("startup tokens by profile:\n")
	b.WriteString(renderStartupTable(r.StartupByProfile))
	b.WriteString("\n")
	b.WriteString("startup tokens by role:\n")
	b.WriteString(renderStartupTable(r.StartupByRole))
	return strings.TrimRight(b.String(), "\n")
}

// renderStartupTable is the text body of one startup table (StartupText):
// the column header plus one line per row, or a single "no attempts"
// line for an empty table.
func renderStartupTable(rows []StartupRow) string {
	if len(rows) == 0 {
		return "  (no attempts)\n"
	}
	var b strings.Builder
	b.WriteString("  key\tattempts\tavg_startup_tokens\testimated\treported\tno_data\n")
	for _, row := range rows {
		avg := "-"
		if row.AvgStartupTokens != nil {
			avg = strconv.FormatFloat(*row.AvgStartupTokens, 'f', -1, 64)
		}
		fmt.Fprintf(&b, "  %s\t%d\t%s\t%d\t%d\t%d\n", row.Key, row.Attempts, avg, row.Estimated, row.Reported, row.NoData)
	}
	return b.String()
}
