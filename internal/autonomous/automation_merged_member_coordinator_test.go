package autonomous

import (
	"strings"
	"testing"
)

const mergedCoordRepo = "/base/repo"

// mergedCoordHarness is a two-member chain whose first member has branch
// automaton/x, with the coordinator reading git through a scripted runner.
func mergedCoordHarness(t *testing.T, policy, base string) (*coordHarness, *Automation, *memberProofMergeGit) {
	t.Helper()
	h := newCoordHarness(t, 2)
	h.m.mu.Lock()
	h.m.cfg.WorktreeBaseRepo = mergedCoordRepo
	h.m.cfg.Automation.MergedMemberPolicy = policy
	h.m.mu.Unlock()
	p, ok := h.m.store.GetPRD(h.prds[0].ID)
	if !ok {
		t.Fatal("prd 0 missing")
	}
	p.Git.Branch = "automaton/x"
	if err := h.m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	git := newMemberProofMergeGit(t, mergedMemberScript(base, "automaton/x", true))
	h.c.mp.git = git
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))
	return h, a, git
}

func mergedCoordDecision(ds []Decision, kind string) *Decision {
	for i := range ds {
		if ds[i].Kind == kind {
			return &ds[i]
		}
	}
	return nil
}

func TestMergedMemberCoordinator_MergedThenCancelledAdvances(t *testing.T) {
	h, a, _ := mergedCoordHarness(t, "", "main")
	h.setStatus(0, PRDCancelled)
	h.event(0, PRDCancelled)

	runs, _ := h.fake.counts()
	if len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("starter runs = %v, want [%s]", runs, h.prds[1].ID)
	}
	got := h.get(a)
	if got.Status != AutomationActive || got.CurrentMemberID != "m1" {
		t.Fatalf("status=%s cursor=%q, want active at m1", got.Status, got.CurrentMemberID)
	}
	m0 := got.Members[0]
	if m0.Status != MemberCompleted || m0.EdgeTaken != "on_complete" || m0.Outcome != "completed" {
		t.Fatalf("member 0 = %+v, want completed via on_complete", m0)
	}
	if !hasCoordDecision(got.Decisions, "advance") {
		t.Fatalf("no advance decision: %+v", got.Decisions)
	}
}

func TestMergedMemberCoordinator_UnmergedCancelledKeepsTodaysBlock(t *testing.T) {
	h, a, _ := mergedCoordHarness(t, "", "main")
	git := newMemberProofMergeGit(t, mergedMemberScript("main", "automaton/x", true))
	// the branch has commits whose patches are not on main
	git.script[joinArgs("cherry", "main", "automaton/x")] = scriptStep{out: "+ " + proofOtherSHA + "\n"}
	h.c.mp.git = git
	h.setStatus(0, PRDCancelled)
	h.event(0, PRDCancelled)

	got := h.get(a)
	if got.Status != AutomationBlocked || got.Members[0].Status != MemberBlocked {
		t.Fatalf("status=%s member=%s, want blocked", got.Status, got.Members[0].Status)
	}
	d := mergedCoordDecision(got.Decisions, "blocked")
	if d == nil || d.Note != "member cancelled outside the Automation" {
		t.Fatalf("blocked decision = %+v, want today's reason", d)
	}
	if hasCoordDecision(got.Decisions, decisionMergedMember) {
		t.Fatalf("unmerged member left a merged_member decision: %+v", got.Decisions)
	}
	if runs, _ := h.fake.counts(); len(runs) != 0 {
		t.Fatalf("starter runs = %v, want none", runs)
	}
}

func TestMergedMemberCoordinator_UnknownStateKeepsTodaysBlock(t *testing.T) {
	h, a, _ := mergedCoordHarness(t, "", "main")
	h.m.mu.Lock()
	h.m.cfg.WorktreeBaseRepo = "" // no repo: the proof cannot answer
	h.m.mu.Unlock()
	h.setStatus(0, PRDCancelled)
	h.event(0, PRDCancelled)

	got := h.get(a)
	d := mergedCoordDecision(got.Decisions, "blocked")
	if got.Status != AutomationBlocked || d == nil || d.Note != "member cancelled outside the Automation" {
		t.Fatalf("status=%s blocked=%+v, want today's block", got.Status, d)
	}
}

func TestMergedMemberCoordinator_PolicyManualBlocksWithProof(t *testing.T) {
	h, a, _ := mergedCoordHarness(t, "manual", "main")
	h.setStatus(0, PRDCancelled)
	h.event(0, PRDCancelled)

	got := h.get(a)
	if got.Status != AutomationBlocked || got.Members[0].Status != MemberBlocked {
		t.Fatalf("status=%s member=%s, want blocked", got.Status, got.Members[0].Status)
	}
	d := mergedCoordDecision(got.Decisions, "blocked")
	if d == nil || !strings.HasPrefix(d.Note, "member cancelled outside the Automation") ||
		!strings.Contains(d.Note, "automaton/x") || !strings.Contains(d.Note, proofTipSHA) {
		t.Fatalf("blocked decision = %+v, want today's reason plus the proof", d)
	}
	if !hasCoordDecision(got.Decisions, decisionMergedMember) {
		t.Fatalf("no merged_member decision: %+v", got.Decisions)
	}
	if runs, _ := h.fake.counts(); len(runs) != 0 {
		t.Fatalf("starter runs = %v, want none", runs)
	}
}

func TestMergedMemberCoordinator_PolicyBlockIsTodaysBehaviourWithNoGit(t *testing.T) {
	h, a, git := mergedCoordHarness(t, "block", "main")
	h.setStatus(0, PRDCancelled)
	h.event(0, PRDCancelled)

	got := h.get(a)
	d := mergedCoordDecision(got.Decisions, "blocked")
	if got.Status != AutomationBlocked || d == nil || d.Note != "member cancelled outside the Automation" {
		t.Fatalf("status=%s blocked=%+v, want today's block", got.Status, d)
	}
	if hasCoordDecision(got.Decisions, decisionMergedMember) {
		t.Fatalf("policy block recorded a merged_member decision: %+v", got.Decisions)
	}
	if len(git.seen) != 0 {
		t.Fatalf("policy block ran git: %v", git.seen)
	}
}

func TestMergedMemberCoordinator_DecisionCarriesMemberBranchAndCommit(t *testing.T) {
	h, a, _ := mergedCoordHarness(t, "", "main")
	h.setStatus(0, PRDCancelled)
	h.event(0, PRDCancelled)

	got := h.get(a)
	d := mergedCoordDecision(got.Decisions, decisionMergedMember)
	if d == nil {
		t.Fatalf("no merged_member decision: %+v", got.Decisions)
	}
	if d.Actor != coordinatorActor {
		t.Errorf("actor = %q, want %q", d.Actor, coordinatorActor)
	}
	for _, want := range []string{"alpha", "automaton/x", "main", proofTipSHA} {
		if !strings.Contains(d.Note, want) {
			t.Errorf("decision note %q lacks %q", d.Note, want)
		}
	}
	if !h.hasAudit("automation_merged_member") {
		t.Fatalf("audit = %v, want automation_merged_member", h.audits)
	}
	var det map[string]any
	h.mu.Lock()
	for i, act := range h.audits {
		if act == "automation_merged_member" {
			det = h.auditD[i]
		}
	}
	h.mu.Unlock()
	if det["branch"] != "automaton/x" || det["commit"] != proofTipSHA || det["default_branch"] != "main" {
		t.Fatalf("audit details = %v", det)
	}
}

func TestMergedMemberCoordinator_NonMainDefaultBranchStillAdvances(t *testing.T) {
	h, a, git := mergedCoordHarness(t, "", "develop")
	h.setStatus(0, PRDCancelled)
	h.event(0, PRDCancelled)

	got := h.get(a)
	if got.Status != AutomationActive || got.CurrentMemberID != "m1" {
		t.Fatalf("status=%s cursor=%q, want active at m1", got.Status, got.CurrentMemberID)
	}
	d := mergedCoordDecision(got.Decisions, decisionMergedMember)
	if d == nil || !strings.Contains(d.Note, "develop") {
		t.Fatalf("merged_member decision = %+v, want it to name develop", d)
	}
	if !git.saw("rev-list", "--count", "develop..automaton/x") {
		t.Fatalf("comparison did not run against develop: %v", git.seen)
	}
}

func TestMergedMemberCoordinator_CompletedMemberRecordsProof(t *testing.T) {
	h, a, _ := mergedCoordHarness(t, "", "main")
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	got := h.get(a)
	if got.Status != AutomationActive || got.CurrentMemberID != "m1" || got.Members[0].Status != MemberCompleted {
		t.Fatalf("status=%s cursor=%q member=%s, want completed and advanced", got.Status, got.CurrentMemberID, got.Members[0].Status)
	}
	if !hasCoordDecision(got.Decisions, decisionMergedMember) {
		t.Fatalf("no merged_member decision: %+v", got.Decisions)
	}
}
