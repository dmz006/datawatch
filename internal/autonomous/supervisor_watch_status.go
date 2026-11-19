// Supervisor watch read view — the read-only snapshot served by the
// GET /api/autonomous/supervisor-watch route. Mirrors the repo CI
// watch read view (repo_ci_watch_status.go): one manager accessor that
// assembles the view from the config, the coordinator's last-poll
// instant and the finding store, every field read under its own lock
// so a concurrent pass, save or save-config never tears the view.
package autonomous

import "time"

// SupervisorWatchComparisonView is what the supervisor watch read view
// shows of the comparison history: the last recorded comparison (nil
// when no pass recorded one yet), the days-of-full-agreement count from
// the per-day summary, and the last comparison's one-sided differences
// (empty when there is no last comparison).
type SupervisorWatchComparisonView struct {
	// Last is the most recent recorded comparison, or nil when the pass
	// has never recorded one (no script output path configured, or an
	// unavailable script record — unavailability is never recorded).
	Last *SupervisorWatchComparison `json:"last"`
	// AgreedDays is the number of calendar days whose comparisons all
	// agreed (a day with no comparisons does not count).
	AgreedDays int `json:"agreed_days"`
	// OnlyWatch is the last comparison's entries the watch raised that
	// the script did not; empty when there is no last comparison or the
	// two agreed.
	OnlyWatch []string `json:"only_watch"`
	// OnlyScript is the last comparison's entries the script raised
	// that the watch did not; empty when there is no last comparison or
	// the two agreed.
	OnlyScript []string `json:"only_script"`
}

// Priority-band boundaries for the wait stats — the same scale the
// priority-stall rule reads, split so the band the default priority floor
// (80) sits on is its own band. The boundaries are fixed: they are the
// watch's vocabulary (the read surfaces and the operator's words), not a
// setting. High is 80 or more, medium is 50 through 79, low is below 50.
const (
	SupervisorWatchBandHigh           = "high"
	SupervisorWatchBandMedium         = "medium"
	SupervisorWatchBandLow            = "low"
	SupervisorWatchHighPriorityTo     = 100
	SupervisorWatchHighPriorityFrom   = 80 // the default priority floor
	SupervisorWatchMediumPriorityFrom = 50
)

// SupervisorWatchPriorityBand is one row of the per-band wait stats: the
// band name, the priority range the band covers (inclusive), how many
// Automata wait in it, the oldest of their waits in whole minutes and
// how many of those waiters also satisfy the watch's priority-stall rule
// — the same rule the priority-stall finding fires on, scoped to the
// band.
type SupervisorWatchPriorityBand struct {
	Band                 string `json:"band"`
	PriorityFrom         int    `json:"priority_from"`
	PriorityTo           int    `json:"priority_to"`
	Waiting              int    `json:"waiting"`
	OldestWaitingMinutes int    `json:"oldest_waiting_minutes"`
	Stalled              int    `json:"stalled"`
}

// SupervisorWatchStatus is the read-only view of the supervisor watch:
// the mode in effect (off, shadow or on — an unset mode reads shadow),
// the instant the coordinator last polled (the zero time when it never
// has), the stored findings (open first, most recently seen first), the
// comparison view, the Automata counts and the Held group. A held
// Automaton is counted and listed only in Held: the blocked and
// waiting-for-review counts never include it, and it never raises a
// finding.
type SupervisorWatchStatus struct {
	Mode       string                        `json:"mode"`
	LastPollAt time.Time                     `json:"last_poll_at"`
	Findings   []*SupervisorWatchFinding     `json:"findings"`
	Comparison SupervisorWatchComparisonView `json:"comparison"`
	// BlockedAutomata counts Automata whose status is blocked.
	BlockedAutomata int `json:"blocked_automata"`
	// WaitingForReviewAutomata counts Automata whose status is needs_review.
	WaitingForReviewAutomata int `json:"waiting_for_review_automata"`
	// Held is the group of Automata on hold by the operator, each with its
	// reason and look-again line, expired first.
	Held HeldGroup `json:"held"`
	// PriorityBands is the per-band wait stats derived from the same
	// waiting set the priority-stall rule scans: one row per band that
	// waits at least one Automaton, in high, medium, low order. Never
	// nil — an empty list when no band waits anything, so the read
	// surfaces always get a list.
	PriorityBands []SupervisorWatchPriorityBand `json:"priority_bands"`
}

// SupervisorWatchStatus assembles the supervisor watch read view from
// the manager's current config, the coordinator's last-poll instant and
// the finding store; the store is nil-safe (a nil store reads as no
// findings and an empty comparison view), as is a missing comparison
// history.
func (m *Manager) SupervisorWatchStatus() (s SupervisorWatchStatus) {
	s.Held = emptyHeldGroup()
	s.PriorityBands = []SupervisorWatchPriorityBand{}
	if m == nil {
		return s
	}
	s.Mode = m.Config().Automation.SupervisorWatch.EffectiveMode()

	m.mu.Lock()
	coord := m.coordinator
	st := m.supervisorWatch
	m.mu.Unlock()

	if coord != nil {
		s.LastPollAt = coord.SupervisorWatchLastPoll()
	} // zero when the coordinator is not running or never polled

	if st != nil {
		if fs := st.List(); fs != nil {
			s.Findings = fs
		}
		comps := st.ListComparisons()
		view := SupervisorWatchComparisonView{OnlyWatch: []string{}, OnlyScript: []string{}}
		if n := len(comps); n > 0 {
			view.Last = &comps[n-1]
			view.OnlyWatch = comps[n-1].OnlyWatch
			view.OnlyScript = comps[n-1].OnlyScript
			if view.OnlyWatch == nil {
				view.OnlyWatch = []string{}
			}
			if view.OnlyScript == nil {
				view.OnlyScript = []string{}
			}
		}
		if days := st.DayAgreement(); days.Days != nil {
			view.AgreedDays = days.FullAgreementDays()
		}
		s.Comparison = view
	}
	s.fillAutomataCounts(m)
	return s
}

// fillAutomataCounts sets the blocked and waiting-for-review counts, the
// Held group and the per-band wait stats from the store as of the
// manager's clock. A held Automaton is counted in neither count.
func (s *SupervisorWatchStatus) fillAutomataCounts(m *Manager) {
	if m.store == nil {
		return
	}
	prds := m.store.ListPRDs()
	for _, p := range prds {
		switch p.Status {
		case PRDBlocked:
			s.BlockedAutomata++
		case PRDNeedsReview:
			s.WaitingForReviewAutomata++
		}
	}
	now := m.holdNow()
	s.Held = BuildHeldGroup(prds, now)
	s.PriorityBands = supervisorWatchPriorityBands(prds, m.Config().Automation.SupervisorWatch, now)
}

// supervisorWatchPriorityBands derives the per-band wait stats from the
// same waiting set the priority-stall rule scans: an Automaton counts in
// a band when it is in a waiting status (draft, planning, needs-review,
// approved or revisions-asked) as of now and carries a created instant —
// a zero created instant reads as not yet waiting, like
// supervisorWatchWaitingAge — and its priority places it in the band
// (80 or more high, 50 through 79 medium, below 50 low). Stalled counts
// the band's waiting Automata that additionally satisfy the watch's
// priority-stall rule (at or above the configured priority floor and
// waiting strictly longer than the configured stall threshold) — so a
// band's stalled row is the share of its waiters the priority-stall
// finding fires on. The rows appear in high, medium, low order and the
// result is an empty slice, never nil, when no band waits anything.
func supervisorWatchPriorityBands(prds []*PRD, cfg SupervisorWatchConfig, now time.Time) []SupervisorWatchPriorityBand {
	bands := map[string]*SupervisorWatchPriorityBand{}
	get := func(name string, from, to int) *SupervisorWatchPriorityBand {
		if b := bands[name]; b != nil {
			return b
		}
		b := &SupervisorWatchPriorityBand{Band: name, PriorityFrom: from, PriorityTo: to}
		bands[name] = b
		return b
	}

	floor := cfg.EffectivePriorityFloor()
	threshold := cfg.EffectiveStallThresholdMinutes()
	for _, p := range prds {
		age, waiting := supervisorWatchWaitingAge(p, now)
		if !waiting {
			continue
		}
		var name string
		var from, to int
		switch {
		case p.Priority >= SupervisorWatchHighPriorityFrom:
			name, from, to = SupervisorWatchBandHigh, SupervisorWatchHighPriorityFrom, SupervisorWatchHighPriorityTo
		case p.Priority >= SupervisorWatchMediumPriorityFrom:
			name, from, to = SupervisorWatchBandMedium, SupervisorWatchMediumPriorityFrom, SupervisorWatchHighPriorityFrom-1
		default:
			name, from, to = SupervisorWatchBandLow, 0, SupervisorWatchMediumPriorityFrom-1
		}
		band := get(name, from, to)
		band.Waiting++
		if age > band.OldestWaitingMinutes {
			band.OldestWaitingMinutes = age
		}
		if p.Priority >= floor && age > threshold {
			band.Stalled++
		}
	}
	out := []SupervisorWatchPriorityBand{}
	for _, name := range []string{SupervisorWatchBandHigh, SupervisorWatchBandMedium, SupervisorWatchBandLow} {
		if b := bands[name]; b != nil {
			out = append(out, *b)
		}
	}
	return out
}

// SupervisorWatchStats is the numeric view of the supervisor watch for
// the monitoring surfaces (the stats collector, GET
// /api/autonomous/supervisor-watch/stats, the supervisor_watch_stats
// MCP tool and the Prometheus gauges): the mode, how long ago the
// coordinator last polled, the finding counts by standing and the days
// of full agreement with the script.
type SupervisorWatchStats struct {
	// Mode is the watch mode in effect (off, shadow or on).
	Mode string `json:"mode"`
	// LastPollAt is the coordinator's last poll instant; the zero time
	// when it never has.
	LastPollAt time.Time `json:"last_poll_at"`
	// LastPollAgeSeconds is the whole seconds since LastPollAt, or -1
	// when the coordinator never polled.
	LastPollAgeSeconds int64 `json:"last_poll_age_seconds"`
	// FindingsOpen counts the findings not yet resolved (it includes
	// the would-alert and alerted ones).
	FindingsOpen int `json:"findings_open"`
	// FindingsWouldAlert counts the open findings shadow marked
	// alert-due without raising an alert.
	FindingsWouldAlert int `json:"findings_would_alert"`
	// FindingsAlerted counts the open findings an alert was raised for.
	FindingsAlerted int `json:"findings_alerted"`
	// FindingsResolved counts the stored findings whose check stopped
	// reporting them.
	FindingsResolved int `json:"findings_resolved"`
	// AgreedDays is the number of calendar days whose comparisons with
	// the script all agreed.
	AgreedDays int `json:"agreed_days"`
	// PriorityBands is the per-band wait stats carried through from the
	// read view unchanged: one row per band that waits at least one
	// Automaton, in high, medium, low order; an empty list when no band
	// waits anything.
	PriorityBands []SupervisorWatchPriorityBand `json:"priority_bands"`
}

// Stats derives the numeric monitoring view from the read view; now is
// the instant the poll age is measured against.
func (s SupervisorWatchStatus) Stats(now time.Time) SupervisorWatchStats {
	out := SupervisorWatchStats{
		Mode:               s.Mode,
		LastPollAt:         s.LastPollAt,
		LastPollAgeSeconds: -1,
		AgreedDays:         s.Comparison.AgreedDays,
		PriorityBands:      s.PriorityBands,
	}
	if out.PriorityBands == nil {
		out.PriorityBands = []SupervisorWatchPriorityBand{}
	}
	if !s.LastPollAt.IsZero() {
		out.LastPollAgeSeconds = int64(now.Sub(s.LastPollAt).Seconds())
	}
	for _, f := range s.Findings {
		if f == nil {
			continue
		}
		switch SupervisorWatchFindingState(f) {
		case "resolved":
			out.FindingsResolved++
			continue
		case "alerted":
			out.FindingsAlerted++
		case "would alert":
			out.FindingsWouldAlert++
		}
		out.FindingsOpen++
	}
	return out
}
