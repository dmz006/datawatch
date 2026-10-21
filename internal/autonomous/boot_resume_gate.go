// Boot-resume gate (B165 — stale PRDs must not auto-resume after a restart).
//
// Before B165, resumeRunningPRDs (api.go) re-launched the executor for
// every stored PRD in PRDRunning state unconditionally, so a PRD whose
// work went stale while the daemon was down (or whose scratch workspace
// lived in a temp directory that was removed) would be re-run from a dead
// context. decideBootResume is the single decision point: it weighs the
// PRD's newest recorded activity against the daemon's
// resume_max_age_seconds limit and verifies that the project directories
// the work would land in still exist, and returns a plain-English reason
// either way so the decision is auditable from the operator's side
// (PRD.BlockedReason, the daemon log, and LoopStatus.BootResume).

package autonomous

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// bootResumeDefaultMaxAgeSeconds is the daemon-wide default resume-max-age
// (seconds) used by DefaultConfig. 0 is meaningful (never resume) and -1
// is meaningful (no age limit), so an unset value must fall back to this
// 3600 default — the config-load layer already does exactly that
// (internal/config resume_max_age test) and the autonomous DefaultConfig
// mirrors it for in-process consumers (main.go's backfills, tests).
const bootResumeDefaultMaxAgeSeconds = 3600

// BootResumeEntry records one boot-resume decision (resumed or parked)
// for a PRD, with the plain-English reason and the PRD's age in seconds
// at decision time. Surfaced on LoopStatus.BootResume and returned by
// Manager.BootResumeReport so the operator can see WHY a PRD was parked
// without digging through daemon logs.
type BootResumeEntry struct {
	PRDID      string    `json:"prd_id"`
	Outcome    string    `json:"outcome"` // "resumed" | "parked"
	AgeSeconds int64     `json:"age_seconds"`
	Reason     string    `json:"reason"`
	At         time.Time `json:"at"`
}

// bootResumeDecision is the result of decideBootResume.
type bootResumeDecision struct {
	resume bool
	ageSec int64
	reason string
}

// newestActivity returns the newest non-zero activity timestamp on the
// PRD: PRD.UpdatedAt, every story's UpdatedAt, every task's UpdatedAt,
// and the per-task session activity timestamps (Task.StartedAt — the
// session's start, the only session-activity moment the model tracks).
// Falls back to PRD.CreatedAt if the PRD somehow has no activity field
// set at all (freshly-created records); still all-zero if that is
// missing too, in which case the PRD is treated as age 0.
func newestActivity(prd *PRD) time.Time {
	var newest time.Time
	if !prd.CreatedAt.IsZero() {
		newest = prd.CreatedAt
	}
	upd := func(ts time.Time) {
		if ts.After(newest) {
			newest = ts
		}
	}
	upd(prd.UpdatedAt)
	for i := range prd.Story {
		upd(prd.Story[i].UpdatedAt)
		for j := range prd.Story[i].Tasks {
			t := &prd.Story[i].Tasks[j]
			upd(t.UpdatedAt)
			if t.StartedAt != nil {
				upd(*t.StartedAt)
			}
		}
	}
	return newest
}

// projectDirsForBootResume collects the directories a boot-resume would
// actually write into: the PRD's own project dir plus any task-level
// project_dir overrides. Empty dirs are skipped (a PRD running purely
// against project/cluster profiles has none — nothing to verify).
func projectDirsForBootResume(prd *PRD) []string {
	dirs := make([]string, 0, 1)
	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	add(prd.ProjectDir)
	for i := range prd.Story {
		for j := range prd.Story[i].Tasks {
			add(prd.Story[i].Tasks[j].ProjectDir)
		}
	}
	return dirs
}

// decideBootResume decides whether to resume on boot or park the PRD,
// returning the decision along with a plain-English reason and the age
// (seconds) at decision time. maxAgeSec semantics: 0 = always park
// (never resume after a restart), -1 = no age limit (skip the age test
// ONLY — missing project directories still park the PRD), positive N =
// park when the newest activity is older than N seconds.
func decideBootResume(prd *PRD, now time.Time, maxAgeSec int) bootResumeDecision {
	var ageSec int64
	if newest := newestActivity(prd); !newest.IsZero() {
		ageSec = int64(now.Sub(newest).Seconds())
		if ageSec < 0 {
			ageSec = 0 // future-dated timestamps (clock skew) count as fresh
		}
	}

	// Directory rule — applies for every limit value, including -1.
	// A PRD whose worktree/project dir is gone (temp dirs get reaped)
	// cannot safely resume in place; re-running it would just fail or
	// write to a fresh empty dir pretending to be the old context.
	for _, dir := range projectDirsForBootResume(prd) {
		if _, err := os.Stat(dir); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return bootResumeDecision{
					resume: false,
					ageSec: ageSec,
					reason: fmt.Sprintf("project directory %s no longer exists (it may have been a removed temp directory) — work cannot resume in place; re-point the PRD at a live directory or approve manually", dir),
				}
			}
			// Present-but-unreadable (permissions) or a file where a
			// dir should be: the executor would fail with a clear
			// per-task error, so only a definitively missing dir parks.
			_ = err
			continue
		}
	}

	// Age rule.
	switch {
	case maxAgeSec == 0:
		return bootResumeDecision{
			resume: false,
			ageSec: ageSec,
			reason: "boot-resume is disabled for this daemon (resume_max_age_seconds = 0): this PRD was left running before the restart and needs manual review before it runs again",
		}
	case maxAgeSec > 0 && ageSec > int64(maxAgeSec):
		return bootResumeDecision{
			resume: false,
			ageSec: ageSec,
			reason: fmt.Sprintf("last activity was %d seconds ago (%s), older than the %d-second resume limit — work from before the restart may be stale, so it does not auto-resume; approve it manually if it should still run", ageSec, (time.Duration(ageSec) * time.Second).Round(time.Second).String(), maxAgeSec),
		}
	case maxAgeSec < 0:
		return bootResumeDecision{
			resume: true,
			ageSec: ageSec,
			reason: fmt.Sprintf("no age limit applies (resume_max_age_seconds = -1) and its project directory still exists (last activity %d seconds ago)", ageSec),
		}
	default:
		return bootResumeDecision{
			resume: true,
			ageSec: ageSec,
			reason: fmt.Sprintf("last activity %d seconds ago is within the %d-second resume limit and its project directory still exists", ageSec, maxAgeSec),
		}
	}
}

// SetAlertFn wires the operator-alert callback used when boot-resume
// parks a PRD (the PRD is now invisible to the "running" view and the
// operator needs to be told WHY). Wired from main.go to the daemon's
// existing alert store; nil = log-only. Matches the wireCapacity
// title/body convention already used for autonomous alerts.
func (m *Manager) SetAlertFn(fn func(title, body string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alertFn = fn
}

// BootResumeReport returns a copy of every boot-resume decision recorded
// since Manager construction (resumed and parked). Stable order: the order
// PRDs were processed at boot resume.
func (m *Manager) BootResumeReport() []BootResumeEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]BootResumeEntry(nil), m.bootResumeReport...)
}

// recordBootResume appends one decision under mu.
func (m *Manager) recordBootResume(entry BootResumeEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bootResumeReport = append(m.bootResumeReport, entry)
}

// parkPRDForBootResume applies the park outcome: status PRDBlocked +
// reason, persisted, status event emitted, operator alert raised.
func (m *Manager) parkPRDForBootResume(prd *PRD, reason string) error {
	m.mu.Lock()
	prd.Status = PRDBlocked
	prd.BlockedReason = reason
	alert := m.alertFn
	m.mu.Unlock()

	if err := m.store.SavePRD(prd); err != nil {
		return err
	}
	m.emitPRDStatus(prd, PRDRunning, lastDecision(prd, "boot_resume_parked"), nil)
	m.EmitPRDUpdate(prd.ID)
	if alert != nil {
		alert(
			"Automaton parked at boot resume",
			fmt.Sprintf("PRD %s %q was left running before the daemon restarted and did not auto-resume. Reason: %s. Approve it manually if it should still run.", prd.ID, prd.Title, reason),
		)
	}
	return nil
}
