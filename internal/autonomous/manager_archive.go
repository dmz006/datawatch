// Manager side of the finished-Automaton archive: the settings accessor, the
// read/plan/run calls the API, CLI, MCP and comm surfaces share, and the
// background pass. The pass only reads and rewrites Automaton record files; it
// never touches a session.

package autonomous

import (
	"context"
	"errors"
	"log"
	"time"
)

var errArchiveNoSessionCheck = errors.New("the archive cannot tell whether a session is open yet; try again once the daemon has finished starting")

const (
	archivePassFirstDelay = 10 * time.Minute
	archivePassInterval   = 6 * time.Hour
)

// SetArchiveSettings wires the accessor for autonomous.archive.enabled and
// autonomous.archive.retention_days. It is read at every use, so a config
// change applies without a restart.
func (m *Manager) SetArchiveSettings(fn func() (enabled bool, retentionDays int)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.archiveSettings = fn
}

// archiveRetention returns the settings in effect: the daemon default (enabled,
// 30 days) when nothing is wired.
func (m *Manager) archiveRetention() (enabled bool, retention time.Duration) {
	m.mu.Lock()
	fn := m.archiveSettings
	m.mu.Unlock()
	enabled, days := true, 30
	if fn != nil {
		enabled, days = fn()
	}
	if days <= 0 {
		days = 30
	}
	return enabled, time.Duration(days) * 24 * time.Hour
}

// wireArchiveSessionCheck hands the store the live-session check. It reports
// false when no check is wired: the caller then must not archive, since a
// session could still be open.
func (m *Manager) wireArchiveSessionCheck() bool {
	m.mu.Lock()
	fn := m.sessionAliveFn
	m.mu.Unlock()
	if fn == nil {
		return false
	}
	m.store.SetArchiveSessionCheck(fn)
	return true
}

// ReadAutomatonArchive loads the archive of one Automaton on request.
func (m *Manager) ReadAutomatonArchive(id string) (*PRDArchive, error) {
	return m.store.ReadArchive(id)
}

// PlanAutomatonArchive is the dry-run report. It writes nothing.
func (m *Manager) PlanAutomatonArchive() ArchivePlan {
	_, retention := m.archiveRetention()
	m.wireArchiveSessionCheck()
	return m.store.PlanArchive(time.Now(), retention)
}

// RunAutomatonArchive archives every eligible finished Automaton after a
// verified backup. It refuses to run when no live-session check is wired.
func (m *Manager) RunAutomatonArchive() (ArchiveReport, error) {
	_, retention := m.archiveRetention()
	if !m.wireArchiveSessionCheck() {
		return ArchiveReport{}, errArchiveNoSessionCheck
	}
	return m.store.RunArchive(time.Now(), retention)
}

// RestoreAutomatonArchive puts one Automaton's archived parts back.
func (m *Manager) RestoreAutomatonArchive(id string) error {
	return m.store.RestoreFromArchive(id)
}

// runArchivePass is one background pass: nothing happens while archiving is
// off, no session check is wired, or an Automaton write is hot.
func (m *Manager) runArchivePass() {
	enabled, retention := m.archiveRetention()
	if !enabled || !m.wireArchiveSessionCheck() {
		return
	}
	rep, err := m.store.runArchive(time.Now(), retention, ArchiveQuietWindow)
	if err != nil {
		log.Printf("[autonomous] archive pass: %v", err)
		return
	}
	if len(rep.Archived) > 0 || len(rep.Failed) > 0 {
		log.Printf("[autonomous] archive pass: archived %d finished Automata, %d failed, backup %s", len(rep.Archived), len(rep.Failed), rep.BackupDir)
	}
}

// startArchivePass runs the background pass at low frequency until ctx ends.
func (m *Manager) startArchivePass(ctx context.Context) {
	go func() {
		t := time.NewTimer(archivePassFirstDelay)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.runArchivePass()
				t.Reset(archivePassInterval)
			}
		}
	}()
}
