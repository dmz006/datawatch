package usagecalc

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Defaults and limits of the model.
const (
	// WeeklyAllowance is the weekly allowance in points; the ceiling is this plus any
	// extra credit.
	WeeklyAllowance = 100.0
	// MinRecentGapHours is the smallest gap between the previous and the newest reading
	// for the recent burn to be used; a smaller gap falls back to the week average.
	MinRecentGapHours = 2.0

	DefaultMaxLanes       = 3
	DefaultBaselinePer24h = 3.0
	DefaultSafetyPoints   = 5.0

	// maxLaneLimit and maxPoints keep every computed figure finite for any input.
	maxLaneLimit = 100
	maxPoints    = 1e6
)

// Verdict values.
const (
	VerdictWithin = "within"
	VerdictOver   = "over"
)

// Needs values: what the operator must supply before the advice can carry numbers.
const (
	NeedsPercent = "percent"
	NeedsReading = "reading"
	NeedsReset   = "reset"
)

// PointValue source values: where a PointValue came from.
const (
	// PointValueSourceProfile: a claude-code billing profile the operator set.
	PointValueSourceProfile = "profile"
	// PointValueSourceDefault: no profile set, the assumed default profile
	// (zero price) supplies the value.
	PointValueSourceDefault = "default"
)

// A weekly reset that follows the newest reading makes that reading last week's. These are
// the words every surface then shows.
const (
	// ResetPassedReason is the stale reason of the advice.
	ResetPassedReason = "a weekly reset passed since the newest reading"
	// EstimateNoteText marks a percentage estimated from the tokens since the reset.
	EstimateNoteText = "Estimated from tokens since the reset; enter a reading to correct it."
	// noEstimateText is the headline when there is no token data to estimate from.
	noEstimateText = "A weekly reset passed since the newest reading and there is no token data to estimate the new week from; enter a weekly reading."
	// estimateMinDays is the shortest span the week average of an estimate is taken over,
	// so the first minutes of a week do not read as a very high daily burn.
	estimateMinDays = 3.0 / 24
)

// WeekEstimate is what the token data say about a week that has no reading yet: the points
// used since its reset and the burn now. The caller prices the tokens; this package does not
// read them.
type WeekEstimate struct {
	Points        float64 `json:"points"`
	PointsPerHour float64 `json:"points_per_hour"`
}

// ResetPassed reports whether a weekly reset at or before now follows the newest usable
// reading, so that no reading belongs to the week that holds now.
func ResetPassed(readings []Reading, reset ResetSpec, now time.Time) bool {
	rs := validReadings(readings)
	if len(rs) == 0 || now.IsZero() {
		return false
	}
	next, err := reset.NextReset(rs[len(rs)-1].At)
	return err == nil && !now.Before(next)
}

// Fallback reasons for the recent burn, and the notice shown whenever it falls back.
const (
	FallbackNoPrevious  = "no_previous"
	FallbackGapUnder2h  = "gap_under_2h"
	FallbackNoticeText  = "No earlier reading at least 2 hours back; the week average is used, so this figure is smoother than the recent burn."
	SessionLabel        = "secondary"
	WorkInUseTyped      = "typed"
	WorkInUseEstimate   = "estimate"
	WorkInUseNone       = "none"
	operatorChoosesNote = "The operator chooses the number of lanes; this is advice only."
	// autoDaemonNote replaces operatorChoosesNote as the first note of the advice while
	// the auto lane mode is on: the daemon sets the lane count itself, within the
	// operator maximum, and under either wording the calculator never stops a lane.
	autoDaemonNote = "The daemon sets the number of lanes within the operator maximum; this calculator never stops or slows a lane."
)

// LanesSource values: how the keeper arrived at Reading.Lanes.
const (
	// LanesLive: the reading is at the clock, the count is the ledger count now.
	LanesLive = "live"
	// LanesRecorded: an earlier time that the lane history covers.
	LanesRecorded = "recorded"
	// LanesCurrent: an earlier time the history does not cover, so the ledger count now is used.
	LanesCurrent = "current"
	// LanesUnavailable: no ledger is wired; Lanes is nil.
	LanesUnavailable = "unavailable"
	// LanesTyped: a reading stored before the stamp existed that carried an operator-typed
	// lane count. Its value is kept as it is and never feeds the advice.
	LanesTyped = "typed"
)

const (
	day  = 24 * time.Hour
	hour = time.Hour
)

// Reading is one operator-reported usage record. A record carries either a weekly
// percentage or a spend figure. Advise turns percentage readings into a verdict; spend
// readings are stored and listed but not yet turned into one.
type Reading struct {
	ID             string    `json:"id"`
	At             time.Time `json:"at"`
	Percent        *float64  `json:"percent,omitempty"`
	SpendUSD       *float64  `json:"spend_usd,omitempty"`
	SessionPercent *float64  `json:"session_percent,omitempty"`
	// Lanes is the Claude lane count the keeper stamped on the reading; LanesSource says
	// where that count came from (the LanesSource values below).
	Lanes       *int   `json:"lanes,omitempty"`
	LanesSource string `json:"lanes_source,omitempty"`
	Source      string `json:"source"`
	// LLM names the paid LLM entry the reading measures. Empty means the first entry
	// (the reading series predates per-entry readings, so an unnamed reading keeps
	// meaning what it always did).
	LLM string `json:"llm,omitempty"`
}

// defaultReadingEntry is the name of the first paid LLM entry, the one an unnamed
// reading belongs to. The daemon sets it from usage.burn_guard; empty means only
// unnamed readings are the first entry's.
var defaultReadingEntry atomic.Value

// SetDefaultReadingEntry records the name of the first paid LLM entry.
func SetDefaultReadingEntry(name string) { defaultReadingEntry.Store(strings.TrimSpace(name)) }

// DefaultReadingEntry is the name SetDefaultReadingEntry recorded ("" when none).
func DefaultReadingEntry() string {
	v, _ := defaultReadingEntry.Load().(string)
	return v
}

// ReadingsForEntry returns the readings that measure the named entry, in order. first
// is true for the first entry, which also owns every reading that names no entry.
func ReadingsForEntry(rs []Reading, entry string, first bool) []Reading {
	entry = strings.TrimSpace(entry)
	out := make([]Reading, 0, len(rs))
	for _, r := range rs {
		name := strings.TrimSpace(r.LLM)
		if (name == "" && first) || (name != "" && name == entry) {
			out = append(out, r)
		}
	}
	return out
}

// DefaultEntryReadings is ReadingsForEntry for the first entry: the readings the
// account-wide calculator advice reads.
func DefaultEntryReadings(rs []Reading) []Reading {
	return ReadingsForEntry(rs, DefaultReadingEntry(), true)
}

// RemainingWork is the work still to run under the run-flat-out policy, in points of the
// weekly allowance. The estimate is QueueTasks times CostPerTask; a typed figure replaces it.
type RemainingWork struct {
	QueueTasks  int      `json:"queue_tasks"`
	CostPerTask float64  `json:"cost_per_task"`
	Typed       *float64 `json:"typed,omitempty"`
}

// Input is everything Advise reads. Pointer fields are optional settings whose zero value
// is meaningful, so nil selects the default.
// PointValue prices one usage point and a week of the allowance in
// cost-equivalent money, from the billing profile's monthly price
// (PointCostEquivalent): a point is 1/100 of a week, so a week is
// WeeklyAllowance times the per-point value. It is an estimate (Estimate is
// always true) and never changes the numbers the readings drive.
type PointValue struct {
	Estimate bool    `json:"estimate"`
	Source   string  `json:"source"`
	PerPoint float64 `json:"per_point"`
	PerWeek  float64 `json:"per_week"`
	// Currency is the ISO 4217 code of the values (USD, EUR, …); empty when
	// the profile does not name one.
	Currency string `json:"currency,omitempty"`
}

type Input struct {
	Readings []Reading `json:"readings"`
	Reset    ResetSpec `json:"reset"`
	// Now is used for Advice.LatestAgeHours, and to tell whether a weekly reset has passed
	// since the newest reading. The zero value means no reset has.
	Now time.Time `json:"now"`
	// SinceReset is the token estimate of the week that holds Now. It is read only when a
	// reset has passed since the newest reading; nil then leaves the advice needing one.
	SinceReset *WeekEstimate `json:"since_reset,omitempty"`
	// PointValue is the billing profile's estimated value of a point and of a
	// week of the allowance; nil leaves the advice without one.
	PointValue *PointValue `json:"point_value,omitempty"`
	// ResetFromProfile marks that Reset's weekday and clock came from the
	// billing profile's reset schedule; the zone is still the calculator's.
	ResetFromProfile bool `json:"reset_from_profile,omitempty"`
	// ExtraCreditPoints is bought extra credit, 0 or more; the ceiling is 100 plus these.
	ExtraCreditPoints float64 `json:"extra_credit_points"`
	// Lanes is the lane count the advice plans with.
	Lanes int `json:"lanes"`
	// RecentLanes is the lane count that divides the recent burn into a burn per lane: the
	// lanes stamped on the newest reading. Nil means Lanes.
	RecentLanes *int `json:"recent_lanes,omitempty"`
	// MaxLanes defaults to 3 when 0 or less, and is capped at 100.
	MaxLanes int `json:"max_lanes"`
	// BaselinePer24h is the idle burn in points per 24 hours; nil means 3.
	BaselinePer24h *float64 `json:"baseline_per_24h,omitempty"`
	// SafetyPoints is the margin of the pace policy; nil means 5, and 0 is allowed.
	SafetyPoints *float64 `json:"safety_points,omitempty"`
	// Policy is use-all, pace or finish-early; empty or unknown means use-all.
	Policy string `json:"policy"`
	// AutoLanes switches the auto lane mode on: the first note of the advice then says
	// the daemon sets the lane count within the operator maximum, and the calculator
	// still never stops a lane.
	AutoLanes     bool          `json:"auto_lanes"`
	RemainingWork RemainingWork `json:"remaining_work"`
}

// LaneRow is the projection for one lane count. BurnPer24h is measured from the recent
// readings; PlanningBurnPer24h is the planning figure from the queue alone (see
// planningPerLane). The two can disagree, and the planning figure is not a forecast.
type LaneRow struct {
	Lanes              int        `json:"lanes"`
	BurnPer24h         float64    `json:"burn_per_24h"`
	PlanningBurnPer24h float64    `json:"planning_burn_per_24h"`
	ProjectedEnd       float64    `json:"projected_end"`
	RunOutAt           *time.Time `json:"run_out_at"`
	HoursEarly         float64    `json:"hours_early"`
}

// Finish is the run-flat-out check of whether the remaining work ends before the allowance.
// When no lane burns above the idle baseline, NoBurn is true and the numbers are left out.
type Finish struct {
	Hours           *float64   `json:"hours,omitempty"`
	At              *time.Time `json:"at,omitempty"`
	EndPercent      *float64   `json:"end_percent,omitempty"`
	SparePoints     *float64   `json:"spare_points,omitempty"`
	BeforeAllowance bool       `json:"before_allowance"`
	NoBurn          bool       `json:"no_burn"`
}

// PolicyAdvice is the part of the result that depends on the policy. PolicyTarget is the
// projected end at the reset that a lane count must not pass to fit the policy: the ceiling
// under use-all, the ceiling minus the safety margin under pace, and nil under finish-early,
// where no row has a target.
type PolicyAdvice struct {
	Policy         string   `json:"policy"`
	LanesSuggested int      `json:"lanes_suggested"`
	Headline       string   `json:"headline"`
	Notes          []string `json:"notes"`
	Finish         *Finish  `json:"finish"`
	PolicyTarget   *float64 `json:"policy_target"`
}

// WorkResult reports which remaining-work figure is in use.
type WorkResult struct {
	InUse          string   `json:"in_use"`
	Points         float64  `json:"points"`
	EstimatePoints *float64 `json:"estimate_points,omitempty"`
	TypedPoints    *float64 `json:"typed_points,omitempty"`
	QueueTasks     int      `json:"queue_tasks"`
	CostPerTask    float64  `json:"cost_per_task"`
}

// Session is the session percentage of the newest reading. It is secondary: it has no
// verdict and never feeds a calculation.
type Session struct {
	Percent float64   `json:"percent"`
	ReadAt  time.Time `json:"read_at"`
	Label   string    `json:"label"`
}

// SeriesPoint is one percentage reading since the last reset.
type SeriesPoint struct {
	At              time.Time `json:"at"`
	Percent         float64   `json:"percent"`
	HoursSinceReset float64   `json:"hours_since_reset"`
}

// Advice is the full result of Advise. When Needs is set, the operator must supply
// something first and the numbers of the reading are left at zero; the explanation is in
// Advice.Headline.
type Advice struct {
	Needs string `json:"needs,omitempty"`

	ReadAt  *time.Time `json:"read_at,omitempty"`
	Percent float64    `json:"percent"`
	// Estimated is true when Percent is not a reading: a reset has passed since the newest
	// one (ReadAt), and Percent is estimated from the tokens since that reset.
	Estimated      bool       `json:"estimated,omitempty"`
	EstimateNote   string     `json:"estimate_note,omitempty"`
	LatestAgeHours *float64   `json:"latest_age_hours,omitempty"`
	LastResetAt    *time.Time `json:"last_reset_at,omitempty"`
	NextResetAt    *time.Time `json:"next_reset_at,omitempty"`

	// Zone is the IANA zone the weekly reset runs in (effective value): the
	// operator's set zone, or the daemon's local zone when the operator left
	// it unset. ServerZone is true in that latter case — the zone follows
	// the server, so the text forms carry the "(server zone)" marker and the
	// settings UI shows it as unset rather than saving the server's zone.
	// ResetFromProfile marks that the reset's weekday and clock came from the
	// claude-code billing profile's reset schedule; the zone is still the
	// calculator's, because a profile names when, not in whose clock.
	Zone             string `json:"zone,omitempty"`
	ServerZone       bool   `json:"server_zone"`
	ResetFromProfile bool   `json:"reset_from_profile,omitempty"`

	// PointValue is the billing profile's estimated cost-equivalent of a usage
	// point and of a week of the allowance (see PointValue); nil when nothing
	// names one. It is always an estimate and changes no number the readings
	// drive.
	PointValue *PointValue `json:"point_value,omitempty"`

	ExtraCreditPoints float64 `json:"extra_credit_points"`
	Ceiling           float64 `json:"ceiling"`
	Lanes             int     `json:"lanes"`
	RecentLanes       int     `json:"recent_lanes"`
	MaxLanes          int     `json:"max_lanes"`
	SafetyPoints      float64 `json:"safety_points"`

	// The three lane numbers, kept apart (the service fills them; Advise leaves them nil).
	// LanesRunningNow is the Claude lane count in the capacity ledger at the moment of the
	// call, null when no ledger is wired. LanesStamped is the lane count stamped on the
	// newest reading whose source is live, recorded or current, null when no reading has
	// one. WhatIfLanes is the lane count this one call plans with when the caller set it;
	// it is never saved.
	LanesRunningNow *int `json:"lanes_running_now"`
	LanesStamped    *int `json:"lanes_stamped"`
	WhatIfLanes     *int `json:"what_if_lanes,omitempty"`

	DaysSinceReset  float64 `json:"days_since_reset"`
	DaysLeft        float64 `json:"days_left"`
	AllowancePer24h float64 `json:"allowance_per_24h"`
	PaceLine        float64 `json:"pace_line"`
	Headroom        float64 `json:"headroom"`

	RecentBurnPer24h   float64 `json:"recent_burn_per_24h"`
	WeekAvgBurnPer24h  float64 `json:"week_avg_burn_per_24h"`
	RecentFallback     bool    `json:"recent_fallback"`
	FallbackReason     string  `json:"fallback_reason,omitempty"`
	FallbackNotice     string  `json:"fallback_notice,omitempty"`
	ProjectedEndRecent float64 `json:"projected_end_recent"`
	ProjectedEndAvg    float64 `json:"projected_end_avg"`

	RunOutAtRecent   *time.Time `json:"run_out_at_recent"`
	RunOutAtAvg      *time.Time `json:"run_out_at_avg"`
	HoursEarlyRecent float64    `json:"hours_early_recent"`
	HoursEarlyAvg    float64    `json:"hours_early_avg"`

	Verdict     string `json:"verdict,omitempty"`
	VerdictAvg  string `json:"verdict_avg,omitempty"`
	AheadOfPace bool   `json:"ahead_of_pace"`

	PerLanePer24h  float64            `json:"per_lane_per_24h"`
	ByModel        map[string]float64 `json:"by_model,omitempty"`
	BaselinePer24h float64            `json:"baseline_per_24h"`
	ByLanes        []LaneRow          `json:"by_lanes"`
	Chosen         *LaneRow           `json:"chosen,omitempty"`

	RemainingWork WorkResult    `json:"remaining_work"`
	Session       *Session      `json:"session,omitempty"`
	Series        []SeriesPoint `json:"series"`
	Daily         []DayUse      `json:"daily"`

	Advice PolicyAdvice `json:"advice"`
}

// settings holds Input after defaults and limits are applied.
type settings struct {
	extra       float64
	ceiling     float64
	lanes       int
	recentLanes int
	maxLanes    int
	baseline    float64
	safety      float64
	policy      string
	autoLanes   bool
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// normalizePointValue keeps a PointValue when it names a known source and
// carries finite, non-negative figures, and returns nil for a malformed one
// (unknown source, a non-finite or negative figure), so a bad value carries
// nothing rather than wrong money. Estimate is forced on: the figure is
// always an estimate of profile price, never a metered amount.
func normalizePointValue(p *PointValue) *PointValue {
	if p == nil {
		return nil
	}
	if p.Source != PointValueSourceProfile && p.Source != PointValueSourceDefault {
		return nil
	}
	if !finite(p.PerPoint) || p.PerPoint < 0 || !finite(p.PerWeek) || p.PerWeek < 0 {
		return nil
	}
	return &PointValue{
		Estimate: true,
		Source:   p.Source,
		PerPoint: p.PerPoint,
		PerWeek:  p.PerWeek,
		Currency: strings.ToUpper(strings.TrimSpace(p.Currency)),
	}
}

// clampPoints returns v limited to 0..maxPoints, and fallback when v is not a number.
func clampPoints(v, fallback float64) float64 {
	if !finite(v) {
		return fallback
	}
	return math.Min(math.Max(v, 0), maxPoints)
}

func newSettings(in Input) settings {
	s := settings{
		extra:     clampPoints(in.ExtraCreditPoints, 0),
		maxLanes:  in.MaxLanes,
		baseline:  DefaultBaselinePer24h,
		safety:    DefaultSafetyPoints,
		policy:    normalizePolicy(in.Policy),
		autoLanes: in.AutoLanes,
	}
	if s.maxLanes <= 0 {
		s.maxLanes = DefaultMaxLanes
	}
	if s.maxLanes > maxLaneLimit {
		s.maxLanes = maxLaneLimit
	}
	s.ceiling = WeeklyAllowance + s.extra
	s.lanes = clampInt(in.Lanes, 0, s.maxLanes)
	s.recentLanes = s.lanes
	if in.RecentLanes != nil {
		s.recentLanes = clampInt(*in.RecentLanes, 0, maxLaneLimit)
	}
	if in.BaselinePer24h != nil {
		s.baseline = clampPoints(*in.BaselinePer24h, DefaultBaselinePer24h)
	}
	if in.SafetyPoints != nil {
		s.safety = clampPoints(*in.SafetyPoints, DefaultSafetyPoints)
	}
	return s
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// validReadings keeps the readings that carry a usable moment and figure, oldest first.
func validReadings(in []Reading) []Reading {
	out := make([]Reading, 0, len(in))
	for _, r := range in {
		if r.At.IsZero() {
			continue
		}
		if r.Percent != nil && (!finite(*r.Percent) || *r.Percent < 0 || *r.Percent > maxPoints) {
			r.Percent = nil
		}
		if r.SpendUSD != nil && (!finite(*r.SpendUSD) || *r.SpendUSD < 0) {
			r.SpendUSD = nil
		}
		if r.Percent == nil && r.SpendUSD == nil {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// round1 rounds to one decimal place, half up, as the reference model does.
func round1(v float64) float64 { return math.Floor(v*10+0.5) / 10 }

// formatPoints prints a figure rounded to one decimal place without a trailing ".0", as the
// reference model's headlines do: 106, 96.1, 0.
func formatPoints(v float64) string {
	r := round1(v)
	if r == 0 {
		return "0"
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

func lanesWord(n int) string {
	if n == 1 {
		return "1 lane"
	}
	return strconv.Itoa(n) + " lanes"
}

func lanesEnd(n int) string {
	if n == 1 {
		return "1 lane ends"
	}
	return strconv.Itoa(n) + " lanes end"
}

func verdictFor(projectedEnd, ceiling float64) string {
	if projectedEnd > ceiling {
		return VerdictOver
	}
	return VerdictWithin
}

// Advise computes the usage advice. It is pure: it reads no clock and does no I/O.
func Advise(in Input) Advice {
	cfg := newSettings(in)
	out := Advice{
		ExtraCreditPoints: cfg.extra,
		Ceiling:           cfg.ceiling,
		Lanes:             cfg.lanes,
		RecentLanes:       cfg.recentLanes,
		MaxLanes:          cfg.maxLanes,
		SafetyPoints:      cfg.safety,
		AllowancePer24h:   WeeklyAllowance / daysPerWeek,
		BaselinePer24h:    cfg.baseline,
		RemainingWork:     resolveWork(in.RemainingWork),
		ByLanes:           []LaneRow{},
		Series:            []SeriesPoint{},
		Daily:             []DayUse{},
		Advice:            PolicyAdvice{Policy: cfg.policy, Notes: []string{leadNote(cfg)}},
	}

	out.Zone = in.Reset.Zone
	out.ServerZone = in.Reset.ServerZone
	out.ResetFromProfile = in.ResetFromProfile
	out.PointValue = normalizePointValue(in.PointValue)

	if err := in.Reset.Validate(); err != nil {
		return out.needing(NeedsReset, "Set the weekly reset as a weekday, an HH:MM time and a time zone name.")
	}
	readings := validReadings(in.Readings)
	if len(readings) == 0 {
		return out.needing(NeedsReading, "Enter a weekly reading to see advice.")
	}
	newest := readings[len(readings)-1]
	// at is the moment the week's figures are measured at: the newest reading, or now when
	// a reset has passed since it and the week that holds now has to be estimated.
	at, rolled := newest.At, ResetPassed(readings, in.Reset, in.Now)
	if rolled {
		at = in.Now
	}
	anchor, _ := in.Reset.anchor(at)
	last, next := anchor.last, anchor.at(daysPerWeek)
	out.setWeek(anchor, newest.At)
	if !in.Now.IsZero() {
		age := math.Max(in.Now.Sub(newest.At).Hours(), 0)
		out.LatestAgeHours = &age
	}
	if !rolled && newest.SessionPercent != nil && finite(*newest.SessionPercent) {
		out.Session = &Session{Percent: *newest.SessionPercent, ReadAt: newest.At.In(anchor.loc), Label: SessionLabel}
	}
	out.Series = seriesSince(readings, last, anchor.loc)

	var percent float64
	switch {
	case rolled && (in.SinceReset == nil || !finite(in.SinceReset.Points)):
		// Last week's reading is never shown as this week's.
		out.ReadAt, out.LatestAgeHours = nil, nil
		return out.needing(NeedsReading, noEstimateText)
	case rolled:
		percent = clampPoints(in.SinceReset.Points, 0)
		out.Estimated, out.EstimateNote = true, EstimateNoteText
	case newest.Percent == nil:
		return out.needing(NeedsPercent, "A weekly percentage is needed: the newest reading carries only a spend figure, and spend readings are stored but not yet turned into a verdict.")
	default:
		percent = *newest.Percent
	}
	out.Percent = percent

	// Position in the week. The pace line and the allowance per 24 hours stay on the
	// 100-point allowance and follow the real length of the week, so on a daylight saving
	// week (167 or 169 hours) the pace line still reaches 100 at the next reset.
	weekDays := next.Sub(last).Hours() / 24
	out.DaysSinceReset = at.Sub(last).Hours() / 24
	out.DaysLeft = math.Max(next.Sub(at).Hours()/24, 0)
	out.AllowancePer24h = WeeklyAllowance / weekDays
	out.PaceLine = out.DaysSinceReset / weekDays * 100
	out.Headroom = out.PaceLine - percent
	out.AheadOfPace = percent > out.PaceLine

	if rolled {
		// No daily split: there is no reading in the week to split.
		out.setEstimatedBurns(percent, in.SinceReset.PointsPerHour)
	} else {
		out.setBurns(readings, percent, at, last)
		out.Daily = dailyUse(anchor, out.Series, at)
	}
	out.setProjections(cfg, percent, at, anchor.loc)
	out.setLanes(cfg, percent, at, anchor.loc)

	out.Advice = adviseByPolicy(cfg, &out, at, anchor.loc)
	if in.Reset.FellBack {
		// After the policy notes: the daemon's local zone could not be resolved
		// and the reset ran in UTC, so the operator must pin the zone.
		out.Advice.Notes = append(out.Advice.Notes,
			"the reset zone fell back to UTC because the daemon's local zone could not be resolved; set usage.calculator.reset_zone explicitly to pin it")
	}
	return out
}

// needing marks the result as waiting for the operator and sets the headline.
func (a Advice) needing(needs, headline string) Advice {
	a.Needs = needs
	a.Advice.LanesSuggested = 0
	a.Advice.Headline = headline
	return a
}

func (a *Advice) setWeek(anchor resetAnchor, readAt time.Time) {
	at, last, next := readAt.In(anchor.loc), anchor.last.In(anchor.loc), anchor.at(daysPerWeek).In(anchor.loc)
	a.ReadAt, a.LastResetAt, a.NextResetAt = &at, &last, &next
}

// seriesSince lists the percentage readings at or after the last reset.
func seriesSince(readings []Reading, last time.Time, loc *time.Location) []SeriesPoint {
	series := []SeriesPoint{}
	for _, r := range readings {
		if r.Percent == nil || r.At.Before(last) {
			continue
		}
		series = append(series, SeriesPoint{At: r.At.In(loc), Percent: *r.Percent, HoursSinceReset: r.At.Sub(last).Hours()})
	}
	return series
}

// setBurns fills the week average burn and the recent burn. The recent burn needs a
// previous percentage reading in the same week at least MinRecentGapHours before the
// newest one; otherwise the week average stands in and the result says why.
func (a *Advice) setBurns(readings []Reading, percent float64, readAt, last time.Time) {
	if a.DaysSinceReset > 0 {
		a.WeekAvgBurnPer24h = percent / a.DaysSinceReset
	}
	a.RecentBurnPer24h = a.WeekAvgBurnPer24h
	a.RecentFallback = true
	a.FallbackReason = FallbackNoPrevious
	if prev := previousPercentReading(readings); prev != nil && !prev.At.Before(last) {
		gap := readAt.Sub(prev.At).Hours()
		if gap >= MinRecentGapHours {
			a.RecentBurnPer24h = math.Max((percent-*prev.Percent)/gap*24, 0)
			a.RecentFallback = false
			a.FallbackReason = ""
		} else {
			a.FallbackReason = FallbackGapUnder2h
		}
	}
	if a.RecentFallback {
		a.FallbackNotice = FallbackNoticeText
	}
}

// setEstimatedBurns fills the burns of an estimated week: the week average from the estimate
// since the reset, and the recent burn from the token burn now.
func (a *Advice) setEstimatedBurns(percent, pointsPerHour float64) {
	a.WeekAvgBurnPer24h = percent / math.Max(a.DaysSinceReset, estimateMinDays)
	a.RecentBurnPer24h = clampPoints(pointsPerHour, 0) * 24
}

// previousPercentReading is the newest percentage reading before the newest reading; spend
// readings in between are skipped.
func previousPercentReading(readings []Reading) *Reading {
	for i := len(readings) - 2; i >= 0; i-- {
		if readings[i].Percent != nil {
			return &readings[i]
		}
	}
	return nil
}

// runOut returns the moment the ceiling would be reached at the given burn, and how many
// hours before the reset that is. There is none when the burn is zero or the ceiling would
// last until the reset.
func (a *Advice) runOut(burn, percent float64, readAt time.Time, loc *time.Location) (*time.Time, float64) {
	if burn <= 0 {
		return nil, 0
	}
	days := math.Max((a.Ceiling-percent)/burn, 0)
	if days >= a.DaysLeft {
		return nil, 0
	}
	at := readAt.Add(time.Duration(days * float64(day))).In(loc)
	return &at, (a.DaysLeft - days) * 24
}

func (a *Advice) setProjections(cfg settings, percent float64, readAt time.Time, loc *time.Location) {
	a.ProjectedEndRecent = percent + a.RecentBurnPer24h*a.DaysLeft
	a.ProjectedEndAvg = percent + a.WeekAvgBurnPer24h*a.DaysLeft
	a.RunOutAtRecent, a.HoursEarlyRecent = a.runOut(a.RecentBurnPer24h, percent, readAt, loc)
	a.RunOutAtAvg, a.HoursEarlyAvg = a.runOut(a.WeekAvgBurnPer24h, percent, readAt, loc)
	a.Verdict = verdictFor(a.ProjectedEndRecent, a.Ceiling)
	a.VerdictAvg = verdictFor(a.ProjectedEndAvg, a.Ceiling)
}

// setLanes infers the burn per lane from the recent burn above the idle baseline and the
// lanes that ran in that window, then projects every lane count from 0 to the maximum.
// One figure serves all lanes; ByModel is kept empty so a per-model split can fill it later
// without a change of contract.
func (a *Advice) setLanes(cfg settings, percent float64, readAt time.Time, loc *time.Location) {
	if cfg.recentLanes > 0 {
		a.PerLanePer24h = math.Max((a.RecentBurnPer24h-cfg.baseline)/float64(cfg.recentLanes), 0)
	}
	planLane := a.planningPerLane(cfg.maxLanes)
	a.ByLanes = make([]LaneRow, 0, cfg.maxLanes+1)
	for l := 0; l <= cfg.maxLanes; l++ {
		burn := cfg.baseline + float64(l)*a.PerLanePer24h
		at, early := a.runOut(burn, percent, readAt, loc)
		a.ByLanes = append(a.ByLanes, LaneRow{
			Lanes: l, BurnPer24h: burn, PlanningBurnPer24h: cfg.baseline + float64(l)*planLane,
			ProjectedEnd: percent + burn*a.DaysLeft, RunOutAt: at, HoursEarly: early,
		})
	}
	chosen := a.ByLanes[cfg.lanes]
	a.Chosen = &chosen
}

// planningPerLane is the points per lane per 24 hours that would finish the queue by the
// reset if the queue were shared evenly over maxLanes lanes: the queue estimate (queued tasks
// times the cost per task) divided by the lanes and by the days left. It reads the queue and
// the reset only, never a measured burn, and it is 0 when there is no estimate, no lane or
// no time left.
func (a *Advice) planningPerLane(maxLanes int) float64 {
	est := a.RemainingWork.EstimatePoints
	if est == nil || maxLanes <= 0 || a.DaysLeft <= 0 {
		return 0
	}
	per := *est / (float64(maxLanes) * a.DaysLeft)
	if !finite(per) {
		return 0
	}
	return per
}

// resolveWork picks the remaining-work figure in use: the typed figure when it is valid,
// otherwise the queue estimate when a cost per task is set, otherwise none.
func resolveWork(w RemainingWork) WorkResult {
	res := WorkResult{InUse: WorkInUseNone, QueueTasks: maxInt(w.QueueTasks, 0), CostPerTask: clampPoints(w.CostPerTask, 0)}
	if res.CostPerTask > 0 {
		if est := float64(res.QueueTasks) * res.CostPerTask; est <= maxPoints {
			res.EstimatePoints = &est
			res.InUse, res.Points = WorkInUseEstimate, est
		}
	}
	if w.Typed != nil && finite(*w.Typed) && *w.Typed >= 0 && *w.Typed <= maxPoints {
		typed := *w.Typed
		res.TypedPoints = &typed
		res.InUse, res.Points = WorkInUseTyped, typed
	}
	return res
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
