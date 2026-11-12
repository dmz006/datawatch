// Repo CI watch read view — the read-only snapshot served by the GET
// /api/autonomous/repo-ci-watch route.
package autonomous

import "time"

// RepoCIWatchStatus is the read-only view of the repo CI watch: the
// mode in effect (off, shadow or on), the configured repository paths
// (empty means nothing is watched), the alert threshold in effect in
// whole minutes (0 while the watch is off), the instant the
// coordinator last polled (the zero time when it never has), and the
// stored records, newest first.
type RepoCIWatchStatus struct {
	Mode         string
	Repos        []string
	AlertMinutes int
	LastPollAt   time.Time
	Records      []*RepoCIWatchRecord
}

// RepoCIWatchStatus assembles the repo CI watch read view from the
// manager's current config, the coordinator's last-poll instant and
// the record store; every field is read under its own lock, so a
// concurrent poll, save or save-config never tears the view.
func (m *Manager) RepoCIWatchStatus() (s RepoCIWatchStatus) {
	cfg := m.Config().Automation.RepoCIWatch
	s.Mode = cfg.EffectiveMode()
	s.Repos = cfg.EffectiveRepos()
	if s.Mode != RepoCIWatchModeOff {
		s.AlertMinutes = cfg.EffectiveAlertMinutes()
	} // while the watch is off the alert threshold is 0
	m.mu.Lock()
	coord := m.coordinator
	m.mu.Unlock()
	if coord != nil {
		s.LastPollAt = coord.RepoCIWatchLastPoll()
	} // zero when the coordinator is not running or never polled
	s.Records = m.ListRepoCIWatchRecords()
	return s
}
