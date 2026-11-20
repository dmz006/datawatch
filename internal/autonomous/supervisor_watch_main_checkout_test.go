package autonomous

// Supervisor watch main checkout finding tests (main_checkout_automaton):
// one finding per open Automaton (any non-terminal status) whose project
// directory is the daemon's own main checkout; none for a terminal
// Automaton, none for an Automaton without a project dir, none for an
// Automaton whose dir is a worktree. The mainCheckoutCheck seam carries
// fakes — no real git.

import (
	"strings"
	"testing"
	"time"
)

// mainCheckoutWatchPRD returns an Automaton in status with ProjectDir
// saved in the manager's store, keyed under its own id.
func mainCheckoutWatchPRD(t *testing.T, m *Manager, status PRDStatus, projectDir string) *PRD {
	t.Helper()
	prd := newEmitPRD(t, m, PRDDraft)
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatalf("GetPRD: not found")
	}
	got.Status = status
	got.ProjectDir = projectDir
	if err := m.Store().SavePRD(got); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return got
}

func mainCheckoutWatchSubjects(t *testing.T, prds []SupervisorWatchFinding) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	for i := range prds {
		f := &prds[i]
		if f == nil || f.Kind != SupervisorWatchMainCheckoutAutomaton {
			continue
		}
		got[f.Subject] = true
	}
	return got
}

// TestSupervisorWatchMainCheckoutAutomatonRaisesForOpenAutomata — a
// draft and a running Automaton whose project directory the check calls
// the main checkout each raise exactly one main_checkout_automaton
// finding, in stable id order, each with the one-line remedy text.
func TestSupervisorWatchMainCheckoutAutomatonRaisesForOpenAutomata(t *testing.T) {
	m := newEmitManager(t, DefaultConfig(), nil)
	m.SetMainCheckoutCheck(func(prd *PRD) bool { return prd != nil && prd.ProjectDir == "/repo/main" })

	draft := mainCheckoutWatchPRD(t, m, PRDDraft, "/repo/main")
	running := mainCheckoutWatchPRD(t, m, PRDRunning, "/repo/main")

	got := m.supervisorWatchMainCheckoutAutomaton(m.supervisorWatchPRDs())
	subjects := mainCheckoutWatchSubjects(t, got)
	if len(got) != 2 {
		t.Fatalf("findings = %d, want 2: %+v", len(got), got)
	}
	if !subjects[draft.ID] || !subjects[running.ID] {
		t.Fatalf("subjects = %v, want the draft and the running Automata", subjects)
	}
	if got[0].Subject > got[1].Subject {
		t.Fatalf("findings are not in stable id order: %+v", got)
	}
	for _, f := range got {
		if want := strings.TrimSpace(f.Text); want == "" {
			t.Fatalf("finding %s has no text", f.Subject)
		}
	}
}

// TestSupervisorWatchMainCheckoutAutomatonTerminalNone — a terminal
// Automaton (completed) on the main checkout never raises: it is done,
// so there is nothing stuck to name.
func TestSupervisorWatchMainCheckoutAutomatonTerminalNone(t *testing.T) {
	m := newEmitManager(t, DefaultConfig(), nil)
	m.SetMainCheckoutCheck(func(*PRD) bool { return true })

	for _, status := range []PRDStatus{PRDCompleted, PRDFailed, PRDCancelled, PRDArchived, PRDRejected} {
		prd := mainCheckoutWatchPRD(t, m, status, "/repo/main")
		got := mainCheckoutWatchSubjects(t, m.supervisorWatchMainCheckoutAutomaton(m.supervisorWatchPRDs()))
		if got[prd.ID] {
			t.Fatalf("status %s is terminal; it must not raise main_checkout_automaton", status)
		}
	}
}

// TestSupervisorWatchMainCheckoutAutomatonEmptyDirNone — an Automaton
// without a project dir can never be the main checkout, even when the
// check seam says yes for everything.
func TestSupervisorWatchMainCheckoutAutomatonEmptyDirNone(t *testing.T) {
	m := newEmitManager(t, DefaultConfig(), nil)
	m.SetMainCheckoutCheck(func(*PRD) bool { return true })

	prd := mainCheckoutWatchPRD(t, m, PRDRunning, "")
	got := mainCheckoutWatchSubjects(t, m.supervisorWatchMainCheckoutAutomaton(m.supervisorWatchPRDs()))
	if got[prd.ID] {
		t.Fatalf("an empty project dir must never raise main_checkout_automaton")
	}
}

// TestSupervisorWatchMainCheckoutAutomatonWorktreeNone — an Automaton
// whose project directory the check calls a worktree (not the main
// checkout) never raises: the guard allows it, so nothing is stuck.
func TestSupervisorWatchMainCheckoutAutomatonWorktreeNone(t *testing.T) {
	m := newEmitManager(t, DefaultConfig(), nil)
	m.SetMainCheckoutCheck(func(prd *PRD) bool { return prd != nil && prd.ProjectDir == "/repo/main" })

	for _, status := range []PRDStatus{PRDDraft, PRDApproved, PRDRunning, PRDHeld, PRDBlocked} {
		prd := mainCheckoutWatchPRD(t, m, status, "/elsewhere/worktree")
		got := mainCheckoutWatchSubjects(t, m.supervisorWatchMainCheckoutAutomaton(m.supervisorWatchPRDs()))
		if got[prd.ID] {
			t.Fatalf("status %s on a worktree must not raise main_checkout_automaton", status)
		}
	}
}

// TestSupervisorWatchMainCheckoutAutomatonInRawFindings — the pass's
// raw findings carry the main_checkout_automaton finding for an open
// Automaton on the main checkout and nothing of that kind for the others.
func TestSupervisorWatchMainCheckoutAutomatonInRawFindings(t *testing.T) {
	m := newEmitManager(t, DefaultConfig(), nil)
	m.SetMainCheckoutCheck(func(prd *PRD) bool { return prd != nil && prd.ProjectDir == "/repo/main" })

	stuck := mainCheckoutWatchPRD(t, m, PRDDraft, "/repo/main")
	worktree := mainCheckoutWatchPRD(t, m, PRDRunning, "/elsewhere/worktree")

	raw := m.supervisorWatchRawFindings(time.Now(), SupervisorWatchConfig{}, m.supervisorWatchPRDs())
	subjects := mainCheckoutWatchSubjects(t, raw)
	if !subjects[stuck.ID] {
		t.Fatalf("the raw findings do not carry the main_checkout_automaton finding for the stuck Automaton: %+v", raw)
	}
	if subjects[worktree.ID] {
		t.Fatalf("the worktree Automaton must not carry a main_checkout_automaton finding: %+v", raw)
	}
}
