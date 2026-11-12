// Branch refresh numbers for monitoring: how many Automata have been
// refreshed cleanly, hit a conflict or failed, and how many are stopped. The
// numbers are read from the Automata's own refresh state, so they survive a
// daemon restart and need no counter of their own.
package autonomous

// BranchRefreshStats is the daemon-wide branch refresh summary.
type BranchRefreshStats struct {
	// Enabled is the daemon setting autonomous.branch_refresh_enabled.
	Enabled bool `json:"enabled"`
	// BehindLimit is the commits-behind limit in effect (0 reads as 5).
	BehindLimit int `json:"behind_limit"`
	// Tracked counts the Automata that have a recorded refresh.
	Tracked int `json:"tracked"`
	// Clean, Conflict, Skipped and Error count the Automata by the result of
	// their latest refresh.
	Clean    int `json:"clean"`
	Conflict int `json:"conflict"`
	Skipped  int `json:"skipped"`
	Error    int `json:"error"`
	// Stopped counts the Automata whose refreshing is stopped.
	Stopped int `json:"stopped"`
	// OverrideOn and OverrideOff count the Automata with a per-Automaton
	// override of true and false.
	OverrideOn  int `json:"override_on"`
	OverrideOff int `json:"override_off"`
}

// SummarizeBranchRefresh builds the summary from the Automata and the daemon
// setting. A nil Automaton in the list is ignored.
func SummarizeBranchRefresh(prds []*PRD, daemonEnabled bool, behindLimit int) BranchRefreshStats {
	if behindLimit == 0 {
		behindLimit = 5
	}
	out := BranchRefreshStats{Enabled: daemonEnabled, BehindLimit: behindLimit}
	for _, p := range prds {
		if p == nil {
			continue
		}
		if p.BranchRefreshOverride != nil {
			if *p.BranchRefreshOverride {
				out.OverrideOn++
			} else {
				out.OverrideOff++
			}
		}
		br := p.BranchRefresh
		if br == nil {
			continue
		}
		out.Tracked++
		switch br.LastResult {
		case BranchRefreshClean:
			out.Clean++
		case BranchRefreshConflict:
			out.Conflict++
		case BranchRefreshSkipped:
			out.Skipped++
		case BranchRefreshError:
			out.Error++
		}
		if br.Stopped {
			out.Stopped++
		}
	}
	return out
}

// BranchRefreshStats reports the branch refresh numbers over every Automaton.
func (m *Manager) BranchRefreshStats() BranchRefreshStats {
	cfg := m.Config()
	return SummarizeBranchRefresh(m.store.ListPRDs(), cfg.BranchRefreshEnabled, cfg.BranchRefreshBehindLimit)
}
