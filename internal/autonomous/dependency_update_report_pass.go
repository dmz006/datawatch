// dependency_update_report_pass.go — reports a finished weekly run. Called
// by the weekly pass on every poll: it reads the run's results file, records
// the outcome for the Events source, and raises the alert through the
// coordinator's alert path (the same sink repo_ci_watch_alert.go uses).
package autonomous

import (
	"os"
	"path/filepath"
	"time"
)

// dependencyUpdateAlerter returns the coordinator's one alerter, so a run
// alerts once however many polls look at it.
func (c *Coordinator) dependencyUpdateAlerter() *DependencyUpdateAlerter {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dependencyUpdateAlert == nil {
		c.dependencyUpdateAlert = &DependencyUpdateAlerter{Alert: c.alert}
	}
	return c.dependencyUpdateAlert
}

func (c *Coordinator) dependencyUpdateReportPass(now time.Time) {
	c.mu.Lock()
	c.dependencyUpdateLoad()
	st := c.dependencyUpdateRun
	c.mu.Unlock()
	if st.AutomatonID == "" || st.Reported {
		return
	}
	prd, ok := c.m.store.GetPRD(st.AutomatonID)
	if !ok {
		return
	}
	var status PRDStatus
	var dir string
	c.m.readLocked(func() { status, dir = prd.Status, prd.ProjectDir })
	finished, _ := c.dependencyUpdateFinished(st.AutomatonID)
	waiting := status == PRDBlocked || status == PRDNeedsReview

	var data []byte
	if dir != "" {
		data, _ = os.ReadFile(filepath.Join(dir, DependencyUpdateResultsPath()))
	}
	state := DependencyUpdateRunState{
		CouldNotStart: finished && len(data) == 0 && (status == PRDFailed || status == PRDCancelled || status == PRDRejected),
	}
	rep, _ := DependencyUpdateReportFromResults(data, state)
	key := st.AutomatonID
	if !finished {
		if waiting {
			c.dependencyUpdateAlerter().Raise(key, st.Title, rep, true)
		}
		return
	}
	c.dependencyUpdateAlerter().Raise(key, st.Title, rep, false)
	st.Reported = true
	st.History = append(st.History, DependencyUpdateRunReport{Title: st.Title, At: now, Report: rep})
	if n := len(st.History); n > dependencyUpdateHistoryLimit {
		st.History = st.History[n-dependencyUpdateHistoryLimit:]
	}
	c.dependencyUpdateStore(st)
}

// DependencyUpdateReports returns the reported weekly runs for the Events
// source. A nil coordinator reads as none.
func (c *Coordinator) DependencyUpdateReports() []DependencyUpdateRunReport {
	if c == nil || c.m == nil || c.m.store == nil {
		return nil
	}
	c.mu.Lock()
	c.dependencyUpdateLoad()
	out := append([]DependencyUpdateRunReport(nil), c.dependencyUpdateRun.History...)
	c.mu.Unlock()
	return out
}

// DependencyUpdateReports is the manager's read of the coordinator's
// reported weekly runs; empty while no coordinator runs.
func (m *Manager) DependencyUpdateReports() []DependencyUpdateRunReport {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	coord := m.coordinator
	m.mu.Unlock()
	return coord.DependencyUpdateReports()
}
