// Supervisor watch main checkout — one finding per open Automaton whose
// project directory is the daemon's own main working tree
// (main_checkout_automaton).
//
// The launch guard (main_checkout_guard.go, refuseMainCheckout) already
// refuses approve and run for such an Automaton, so a draft, approved,
// queued, running, held or blocked Automaton pointed at the main checkout
// can never run: it is stuck until the operator recreates it without a
// project dir. This check surfaces that state in the supervisor watch
// findings: one finding per such Automaton, raised on every pass while
// it stays open, resolved by the pass once the Automaton reaches a
// terminal status, loses its project dir, or its dir stops being the
// main checkout.
//
// The decision reuses the guard's own helper (isMainCheckout, the
// symlink-resolved path equality plus the .git-is-a-directory rule that a
// linked worktree never matches) and runs through the Manager's
// injectable mainCheckoutCheck seam (SetMainCheckoutCheck): with a seam
// wired the checks — and their tests — never touch a real checkout, and
// with none wired it is exactly the guard's stat-based fallback against
// Config.WorktreeBaseRepo. A fact the views cannot read raises nothing
// (unknown, no finding), and the check never acts.

package autonomous

import (
	"strings"
)

// supervisorWatchMainCheckoutOpenStatus reports whether the Automaton
// status is open — any non-terminal status: the draft, planning,
// needs-review, approved, revisions-asked, paused, running, held and
// blocked states (the legacy "active" alias of running included). The
// terminal statuses (completed, failed, rejected, cancelled, archived)
// never raise: an Automaton that is done, dead or closed cannot be the
// one stuck on the main checkout.
func supervisorWatchMainCheckoutOpenStatus(s PRDStatus) bool {
	switch s {
	case PRDDraft, PRDPlanning, PRDNeedsReview, PRDApproved, PRDRevisionsAsked,
		PRDActive, PRDRunning, PRDPaused, PRDHeld, PRDBlocked:
		return true
	}
	return false
}

// supervisorWatchMainCheckoutAutomaton returns one
// main_checkout_automaton finding per open Automaton (any non-terminal
// status) whose project directory is the daemon's own main checkout, in
// stable Automaton id order. The decision runs through the
// Manager's mainCheckoutCheck seam (SetMainCheckoutCheck) with the
// guard's real stat-based check (isMainCheckout against
// Config.WorktreeBaseRepo) as the fallback, so tests carry fakes and no
// real git is touched. An Automaton with an empty project dir can never
// be the main checkout and never raises; a nil manager reads as no
// findings.
func (m *Manager) supervisorWatchMainCheckoutAutomaton(prds []*PRD) []SupervisorWatchFinding {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	check := m.mainCheckoutCheck
	baseRepo := m.cfg.WorktreeBaseRepo
	m.mu.Unlock()
	if check == nil {
		check = func(prd *PRD) bool { return isMainCheckout(prd.ProjectDir, baseRepo) }
	}
	seen := map[string]bool{}
	var out []SupervisorWatchFinding
	for _, p := range prds {
		if p == nil {
			continue
		}
		id := strings.TrimSpace(p.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if !supervisorWatchMainCheckoutOpenStatus(p.Status) {
			continue // terminal: the Automaton is done, dead or closed
		}
		if strings.TrimSpace(p.ProjectDir) == "" {
			continue // no project dir: it is not the main checkout
		}
		if !check(p) {
			continue // worktree or elsewhere: the guard allows it
		}
		text, _ := SupervisorWatchText(SupervisorWatchMainCheckoutAutomaton, id)
		out = append(out, SupervisorWatchFinding{
			Kind:    SupervisorWatchMainCheckoutAutomaton,
			Subject: id,
			Text:    text,
		})
	}
	sortSupervisorWatchFindings(out)
	return out
}
