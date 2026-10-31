// service.go — the daemon-level usage calculator service (docs/plans/2026-10-23-usage-calculator-design.md,
// decisions docs/plans/2026-10-25-usage-calculator-decisions.md).
//
// Service composes three already-built parts into one advice call for the daemon, the REST
// surface and the other access methods on the design's list:
//
//   - the shared readings series (shared.go), where this account's readings live across
//     federated daemons; the service reads from its Snapshot, keeping the one-store and
//     one-reading rules of doc.go;
//   - this daemon's local store (store.go), which holds the operator's typed
//     remaining-work figure, a daemon-local value that is not part of the shared record
//     (decision 6: both figures, the typed one replaces the estimate);
//   - the operator's live settings (Settings), read through a getter on every call so a
//     setting change takes effect without a restart, and the daemon's queued-task count
//     (QueueCounter), which prices the finish-early estimate (decision 6).
//
// Advice(ctx, Options) is the one call; Options lets a single request override the variant,
// the policy and the lane count against the stored settings (variant and policy are
// independent: the variant picks the rendering, the policy picks the target, both from one
// set of numbers). The service is pure computation plus the reads and writes of the parts
// above — it holds no state of its own.

package usagecalc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// Display variants. The variant picks the rendering of one advice; it does not change the
// numbers.
const (
	// VariantPaceGauge is the gauge form: the verdict, the pace line and the burn.
	VariantPaceGauge = "pace-gauge"
	// VariantLanePlanner is the lane table: one row per lane count with its projected end.
	VariantLanePlanner = "lane-planner"
	// VariantBurnLedger is the day-by-day ledger of points used since the reset.
	VariantBurnLedger = "burn-ledger"
	// VariantAll shows all three forms side by side.
	VariantAll = "all"
)

// Variants lists the display variants, in display order.
func Variants() []string {
	return []string{VariantPaceGauge, VariantLanePlanner, VariantBurnLedger, VariantAll}
}

func validVariant(v string) bool {
	for _, a := range Variants() {
		if v == a {
			return true
		}
	}
	return false
}

// Settings is the operator's live calculator settings, as they are at the moment of one
// advice call. The daemon fills it from its config on every call; the zero value keeps the
// package defaults.
type Settings struct {
	// Reset is the weekly reset: a weekday, an HH:MM clock time and an IANA zone.
	Reset ResetSpec
	// Variant is the stored display variant; empty means VariantAll.
	Variant string
	// Policy is the stored policy; empty means use-all.
	Policy string
	// BaselinePer24h is the idle burn in points per 24 hours; nil means the default.
	BaselinePer24h *float64
	// SafetyPoints is the pace policy's margin under the pace line; nil means the default.
	SafetyPoints *float64
	// CostPerTask is the weekly points one queued task costs; 0 disables the estimate.
	CostPerTask float64
	// MaxLanes caps the planner's lane counts; 0 means the default.
	MaxLanes int
}

// SettingsGetter returns the live settings at call time. It must be safe for concurrent
// calls; the service reads it once per Advice call.
type SettingsGetter func() Settings

// QueueCounter counts this daemon's queued automation tasks — the tasks that are not
// finished yet. The count prices the finish-early estimate as QueueTasks times CostPerTask
// (decision 6). A nil counter leaves the estimate out.
type QueueCounter interface {
	QueuedTaskCount() int
}

// Options overrides the stored settings for one Advice call. Empty fields keep the stored
// value; a non-nil Lanes is a what-if that replaces the stamped, stored and default lane
// counts for this call only, and a non-nil CostPerTask replaces the stored cost for the
// estimate of this call.
type Options struct {
	// Variant is this call's display variant; empty uses the stored one.
	Variant string
	// Policy is this call's policy; empty uses the stored one.
	Policy string
	// Lanes is the what-if lane count this call plans with. Nil means not set and uses the
	// stamped chain; 0 is a valid what-if (zero lanes). It is never saved.
	Lanes *int
	// CostPerTask is this call's cost per queued task; nil uses the stored one.
	CostPerTask *float64
}

// AdviceResult is one advice with the resolved selections and the series provenance.
// Stale and StaleReason carry the shared-series markers when this daemon reads the last
// copy while the keeper is unreachable (decision 8); CopiedAt is when that copy was taken.
// Text carries the two text forms of this advice, so every surface shows the same words.
type AdviceResult struct {
	Variant     string     `json:"variant"`
	Policy      string     `json:"policy"`
	Stale       bool       `json:"stale"`
	StaleReason string     `json:"stale_reason,omitempty"`
	CopiedAt    *time.Time `json:"copied_at,omitempty"`
	Advice      Advice     `json:"advice"`
	Text        TextForms  `json:"text"`
}

// TextForms are the CLI and comm-channel renderings of one advice, built by FormatCLI and
// FormatComm from the same fields in the same order.
type TextForms struct {
	CLI  string `json:"cli"`
	Comm string `json:"comm"`
}

// Service is the daemon-level usage calculator. It composes the shared readings series,
// the daemon's local store, the live settings and the queued-task count; see the package
// documentation. It holds no state beyond those parts and may be shared by the handlers
// that serve it.
type Service struct {
	series    *SharedSeries
	local     *Store
	settingsF SettingsGetter
	queue     QueueCounter
	live      LaneCounter
	now       func() time.Time
}

// ServiceOption tunes a Service at construction time.
type ServiceOption func(*Service)

// WithServiceNow injects the clock the service uses for the reading age and the finish
// line; the zero value of a nil function means the system clock. Tests and daemons that
// need a stable clock set it here.
func WithServiceNow(now func() time.Time) ServiceOption {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithLiveLanes wires the counter of the Claude lanes running now, the lane-history unit's
// LaneCounter, so Advice can report LanesRunningNow. Without it, or when the counter reports
// that no ledger is wired, LanesRunningNow is null.
func WithLiveLanes(c LaneCounter) ServiceOption {
	return func(s *Service) { s.live = c }
}

// NewService builds a calculator service over the shared readings series. local is this
// daemon's store, which holds the typed remaining-work figure; pass nil when the daemon
// keeps no typed figure of its own. settings returns the live settings per call; nil keeps
// the package defaults. queue counts the daemon's queued tasks for the estimate; nil
// disables the estimate.
func NewService(series *SharedSeries, local *Store, settings SettingsGetter, queue QueueCounter, opts ...ServiceOption) *Service {
	if settings == nil {
		settings = func() Settings { return Settings{} }
	}
	s := &Service{series: series, local: local, settingsF: settings, queue: queue, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) liveSettings() Settings {
	cfg := s.settingsF()
	cfg.Variant = strings.ToLower(strings.TrimSpace(cfg.Variant))
	cfg.Policy = strings.ToLower(strings.TrimSpace(cfg.Policy))
	return cfg
}

func (s *Service) requireSeries() error {
	if s.series == nil {
		return errors.New("usage service: no shared readings series is wired")
	}
	return nil
}

// Advice computes the usage advice for this account. It reads the shared series through
// its snapshot, applies the stored settings with any per-call overrides, and keeps the
// numbers in every field of the reference model: the variant picks the rendering, the
// policy picks the target, and both read one set of numbers from the same newest
// percentage reading.
func (s *Service) Advice(ctx context.Context, o Options) (*AdviceResult, error) {
	if err := s.requireSeries(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg := s.liveSettings()

	snap, err := s.series.Snapshot(ctx)
	if err != nil {
		return nil, err
	}

	variant := strings.ToLower(strings.TrimSpace(o.Variant))
	if variant == "" {
		if variant = cfg.Variant; variant == "" {
			variant = VariantAll
		}
		if !validVariant(variant) {
			// The design's retiring rule: a saved setting that names a value removed
			// from the allowed list falls back to the full form; the numbers never
			// change, and one log line says so.
			log.Printf("usage calculator: stored variant %q is retired; serving %q", variant, VariantAll)
			variant = VariantAll
		}
	}
	if !validVariant(variant) {
		return nil, fmt.Errorf("unknown usage calculator variant %q: use one of %s", variant, strings.Join(Variants(), ", "))
	}

	policy := strings.ToLower(strings.TrimSpace(o.Policy))
	if policy != "" && !ValidPolicy(policy) {
		return nil, fmt.Errorf("unknown usage calculator policy %q: use one of %s", strings.TrimSpace(o.Policy), strings.Join(Policies(), ", "))
	}
	if policy == "" {
		policy = cfg.Policy
	}

	stamped := newestStampedLanes(snap.Record.Readings)
	in := Input{
		Readings:          nonNilReadings(snap.Record.Readings),
		Reset:             cfg.Reset,
		Now:               s.now(),
		ExtraCreditPoints: snap.Record.ExtraCreditPoints,
		Lanes:             lanesFrom(o, cfg, stamped),
		RecentLanes:       stamped,
		MaxLanes:          cfg.MaxLanes,
		BaselinePer24h:    cfg.BaselinePer24h,
		SafetyPoints:      cfg.SafetyPoints,
		Policy:            policy,
		RemainingWork:     s.remainingWork(o, cfg),
	}

	advice := Advise(in)
	advice.LanesRunningNow = s.lanesRunningNow()
	advice.LanesStamped = stamped
	if o.Lanes != nil {
		n := *o.Lanes
		advice.WhatIfLanes = &n
	}
	res := &AdviceResult{
		Variant:     variant,
		Policy:      advice.Advice.Policy,
		Stale:       snap.Stale,
		StaleReason: snap.StaleReason,
		Advice:      advice,
	}
	if !snap.CopiedAt.IsZero() {
		at := snap.CopiedAt
		res.CopiedAt = &at
	}
	res.Text = TextForms{CLI: FormatCLI(*res), Comm: FormatComm(*res)}
	return res, nil
}

// newestStampedLanes returns the lane count stamped on the newest reading whose source is
// live, recorded or current, or nil when no reading has a usable stamp. Typed and
// unavailable readings never count: a typed count was entered by hand and an unavailable one
// has no count. The same count divides the recent burn into a burn per lane and picks the
// default chosen lane count.
func newestStampedLanes(readings []Reading) *int {
	var best *Reading
	for i := range readings {
		r := &readings[i]
		if r.Lanes == nil || *r.Lanes < 0 || !stampedSource(r.LanesSource) {
			continue
		}
		if best == nil || !r.At.Before(best.At) {
			best = r
		}
	}
	if best == nil {
		return nil
	}
	n := *best.Lanes
	return &n
}

func stampedSource(src string) bool {
	switch src {
	case LanesLive, LanesRecorded, LanesCurrent:
		return true
	}
	return false
}

// lanesFrom resolves the lane count this advice plans with: the what-if when set, then the
// lanes stamped on the newest usable reading, then the stored maximum, then the package
// default. The model clamps the value against the maximum, so a larger choice stays
// possible without breaking the planner's table.
func lanesFrom(o Options, cfg Settings, stamped *int) int {
	if o.Lanes != nil {
		return *o.Lanes
	}
	if stamped != nil {
		return *stamped
	}
	if cfg.MaxLanes > 0 {
		return cfg.MaxLanes
	}
	return DefaultMaxLanes
}

// lanesRunningNow is the Claude lane count in the ledger now, or nil when no counter is
// wired or the counter has no ledger.
func (s *Service) lanesRunningNow() *int {
	if s.live == nil {
		return nil
	}
	n, ok := s.live.Lanes()
	if !ok {
		return nil
	}
	return &n
}

// remainingWork resolves the finish-early figure (decision 6): the estimate is the daemon's
// queued tasks times the stored cost per task, the operator's typed figure replaces it, and
// the queue's count stays visible beside both.
func (s *Service) remainingWork(o Options, cfg Settings) RemainingWork {
	cost := cfg.CostPerTask
	if o.CostPerTask != nil {
		cost = *o.CostPerTask
	}
	if cost < 0 {
		cost = 0
	}
	w := RemainingWork{CostPerTask: cost}
	if s.queue != nil {
		n := s.queue.QueuedTaskCount()
		if n < 0 {
			n = 0
		}
		w.QueueTasks = n
	}
	if s.local != nil {
		w.Typed = s.typedRemainingWorkLocked()
	}
	return w
}

// TypedRemainingWork returns the operator's typed remaining-work figure, or nil when none
// is set or the service keeps no local store.
func (s *Service) TypedRemainingWork() *float64 {
	if s.local == nil {
		return nil
	}
	return s.typedRemainingWorkLocked()
}

func (s *Service) typedRemainingWorkLocked() *float64 {
	v := s.local.Snapshot().Local.RemainingWork
	if v == nil || !finite(*v) || *v < 0 {
		return nil
	}
	out := *v
	return &out
}

// Readings returns this account's readings, oldest first, as the shared series serves
// them. Stale snapshots carry their copy's readings, and the callers see the same
// provenance through Advice.
func (s *Service) Readings(ctx context.Context) ([]Reading, error) {
	if err := s.requireSeries(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snap, err := s.series.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	out := nonNilReadings(snap.Record.Readings)
	if out == nil {
		out = []Reading{}
	}
	return out, nil
}

// AddReading forwards one reading to the shared series: into this daemon's store when this
// daemon is the keeper, and to the keeper otherwise (shared.go). The reading is validated
// and stored by the keeper's store; the stored reading comes back with the id assigned
// (by the local store on the keeper path, by the keeper's daemon on the peer path).
func (s *Service) AddReading(ctx context.Context, r Reading) (Reading, error) {
	if err := s.requireSeries(); err != nil {
		return Reading{}, err
	}
	if err := ctx.Err(); err != nil {
		return Reading{}, err
	}
	return s.series.AddReading(ctx, r)
}

// DeleteReading removes one reading by its ID through the shared series.
func (s *Service) DeleteReading(ctx context.Context, id string) error {
	if err := s.requireSeries(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.series.DeleteReading(ctx, id)
}

// SetExtraCredit sets the bought extra credit in points, through the shared series.
func (s *Service) SetExtraCredit(ctx context.Context, points float64) error {
	if err := s.requireSeries(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.series.SetExtraCredit(ctx, points)
}

// SetRemainingWork sets the operator's typed remaining-work figure for this daemon; points
// of nil removes it. The figure is daemon-local: it stays in this daemon's store and does
// not travel with the shared account record.
func (s *Service) SetRemainingWork(ctx context.Context, points *float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.local == nil {
		return errors.New("usage service: no local store is wired")
	}
	if points != nil && (!finite(*points) || *points < 0) {
		return fmt.Errorf("invalid remaining work %v: a finite number of 0 or more points is needed", *points)
	}
	return s.local.SetRemainingWork(points)
}
