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

// SupervisorWatchStatus is the read-only view of the supervisor watch:
// the mode in effect (off, shadow or on — an unset mode reads shadow),
// the instant the coordinator last polled (the zero time when it never
// has), the stored findings (open first, most recently seen first) and
// the comparison view.
type SupervisorWatchStatus struct {
	Mode       string                        `json:"mode"`
	LastPollAt time.Time                     `json:"last_poll_at"`
	Findings   []*SupervisorWatchFinding     `json:"findings"`
	Comparison SupervisorWatchComparisonView `json:"comparison"`
}

// SupervisorWatchStatus assembles the supervisor watch read view from
// the manager's current config, the coordinator's last-poll instant and
// the finding store; the store is nil-safe (a nil store reads as no
// findings and an empty comparison view), as is a missing comparison
// history.
func (m *Manager) SupervisorWatchStatus() (s SupervisorWatchStatus) {
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
	return s
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
}

// Stats derives the numeric monitoring view from the read view; now is
// the instant the poll age is measured against.
func (s SupervisorWatchStatus) Stats(now time.Time) SupervisorWatchStats {
	out := SupervisorWatchStats{
		Mode:               s.Mode,
		LastPollAt:         s.LastPollAt,
		LastPollAgeSeconds: -1,
		AgreedDays:         s.Comparison.AgreedDays,
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
