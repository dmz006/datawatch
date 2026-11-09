// Release CI fall-back — when a release's CI run comes back red soon after
// the push, the release size's approval mode drops to "ask" for that
// project, an operator alert is raised, and a defect is filed through the
// normal filing path (CreatePRD, which runs defect intake).
//
// The window bounds how long after the push a red outcome still triggers
// the fall-back. A red outcome after the window, and a green or pending
// outcome, change nothing.
//
// Approval mode storage: the release approval setting already exists as
// autonomous.merge_pipeline.projects.<project>.release_approval.<size>
// (config.MergePipelineOverride.ReleaseApproval). This package has no
// handle on the daemon's config store, so the fall-back is stored as a
// per-project release size mode override on the Manager (releaseSizeModes).
// The override is in-memory: it is not persisted across a restart, and the
// merge pipeline does not read it yet. Wiring the daemon's config write
// into this path is the follow-up that makes it durable.
package autonomous

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// releaseSizeModeKey is the key for one project's release size override.
func releaseSizeModeKey(project, size string) string {
	return project + "\x00" + size
}

// ReleaseSizeModeOverride returns the fall-back approval mode stored for a
// project's release size, and whether one is set.
func (m *Manager) ReleaseSizeModeOverride(project, size string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mode, ok := m.releaseSizeModes[releaseSizeModeKey(project, size)]
	return mode, ok
}

// setReleaseSizeModeOverride stores the approval mode for a project's
// release size. Guarded by mu.
func (m *Manager) setReleaseSizeModeOverride(project, size, mode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.releaseSizeModes == nil {
		m.releaseSizeModes = map[string]string{}
	}
	m.releaseSizeModes[releaseSizeModeKey(project, size)] = mode
}

// applyReleaseCIFallback applies the release CI fall-back to rec as of now.
// It returns true when the fall-back fired. It fires only when all hold:
// the CI outcome is red, the release size is patch, minor or major, and
// now is within windowMinutes of the push (inclusive). When it fires it
// sets the release size's approval mode to "ask" for the project (stored
// as a per-project override, see the file comment), raises an alert
// through the alert sink, and files a defect through CreatePRD, which runs
// the defect intake.
func (m *Manager) applyReleaseCIFallback(rec *ReleaseRecord, now time.Time, windowMinutes int) bool {
	if rec == nil || rec.CIOutcome != ReleaseCIRed || windowMinutes <= 0 {
		return false
	}
	elapsed := now.Sub(rec.PushedAt)
	if elapsed < 0 || elapsed > time.Duration(windowMinutes)*time.Minute {
		return false
	}
	size := strings.TrimSpace(rec.ReleaseSize)
	switch size {
	case config.ReleaseSizePatch, config.ReleaseSizeMinor, config.ReleaseSizeMajor:
	default:
		return false
	}

	m.setReleaseSizeModeOverride(rec.Project, size, config.ReleaseApprovalAsk)

	m.mu.Lock()
	alert := m.alertSink
	m.mu.Unlock()
	if alert != nil {
		alert("error",
			fmt.Sprintf("Release CI red: %s %s (%s)", rec.Project, rec.Tag, size),
			fmt.Sprintf("Release %s for project %s went red %d min after the push (commit %s). "+
				"The %s release approval is now ask for this project.",
				rec.Tag, rec.Project, int(elapsed.Minutes()), rec.PushedCommit, size))
	}

	spec := fmt.Sprintf("fix the failing release CI run for %s tag %s: the release CI outcome is red",
		rec.Project, rec.Tag)
	if _, err := m.CreatePRD(spec, rec.ReleasePath, "", "", EffortNormal); err != nil {
		log.Printf("[autonomous] release CI fall-back: defect filing for %s %s failed: %v", rec.Project, rec.Tag, err)
	}
	return true
}
