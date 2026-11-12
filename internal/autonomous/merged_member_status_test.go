package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestMergedMemberStatus_AdvancedRowCarriesTheProof(t *testing.T) {
	h, a, _ := mergedCoordHarness(t, "", "main")
	h.setStatus(0, PRDCancelled)
	h.event(0, PRDCancelled)

	rows, err := h.m.MergedMembers(a.ID)
	if err != nil {
		t.Fatalf("MergedMembers: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one", rows)
	}
	r := rows[0]
	if r.AutomationID != a.ID || r.Member != "m0" || r.PRDID != h.prds[0].ID {
		t.Errorf("identity = %+v", r)
	}
	if r.Branch != "automaton/x" || r.DefaultBranch != "main" || r.ProvingCommit != proofTipSHA {
		t.Errorf("proof = branch %q default %q commit %q", r.Branch, r.DefaultBranch, r.ProvingCommit)
	}
	if r.Policy != "completed" || r.Outcome != MergedMemberAdvanced {
		t.Errorf("policy %q outcome %q, want completed/advanced", r.Policy, r.Outcome)
	}
	if r.At.IsZero() || time.Since(r.At) > time.Minute || r.Note == "" {
		t.Errorf("at %v note %q", r.At, r.Note)
	}
}

func TestMergedMemberStatus_ManualPolicyIsBlocked(t *testing.T) {
	h, a, _ := mergedCoordHarness(t, "manual", "main")
	h.setStatus(0, PRDCancelled)
	h.event(0, PRDCancelled)

	rows, err := h.m.MergedMembers(a.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if rows[0].Policy != "manual" || rows[0].Outcome != MergedMemberBlocked || rows[0].ProvingCommit != proofTipSHA {
		t.Errorf("row = %+v, want manual/blocked with the proof", rows[0])
	}
}

func TestMergedMemberStatus_NoDecisionIsAnEmptyList(t *testing.T) {
	// policy block runs no git and records nothing
	h, a, _ := mergedCoordHarness(t, "block", "main")
	h.setStatus(0, PRDCancelled)
	h.event(0, PRDCancelled)

	rows, err := h.m.MergedMembers(a.ID)
	if err != nil {
		t.Fatalf("MergedMembers: %v", err)
	}
	if rows == nil || len(rows) != 0 {
		t.Fatalf("rows = %#v, want an empty non-nil list", rows)
	}
}

func TestMergedMemberStatus_UnknownAutomationIs404(t *testing.T) {
	h, _, _ := mergedCoordHarness(t, "", "main")
	_, err := h.m.MergedMembers("nope0000")
	if err == nil || !strings.HasPrefix(err.Error(), "404: ") {
		t.Fatalf("err = %v, want a 404", err)
	}
}

func TestMergedMemberStatus_AllAutomataListsEveryRowOldestFirst(t *testing.T) {
	h, a, _ := mergedCoordHarness(t, "", "main")
	h.setStatus(0, PRDCancelled)
	h.event(0, PRDCancelled)

	rows, err := h.m.MergedMembers("")
	if err != nil {
		t.Fatalf("MergedMembers: %v", err)
	}
	if len(rows) != 1 || rows[0].AutomationID != a.ID {
		t.Fatalf("all-Automata rows = %+v, want the one for %s", rows, a.ID)
	}

	// ordering and the fallback for an entry without the structured record
	later := time.Now()
	b := &Automation{ID: "b", Name: "b", Decisions: []Decision{
		{At: later, Kind: decisionMergedMember, Note: "second", MergedMember: &MergedMemberDecision{Member: "m2", Outcome: MergedMemberBlocked}},
		{At: later.Add(-time.Hour), Kind: decisionMergedMember, Note: "first"},
		{At: later, Kind: "advance", Note: "not a merged-member entry"},
	}}
	got := MergedMemberRows(b)
	if len(got) != 2 || got[0].Note != "second" || got[0].Member != "m2" || got[1].Note != "first" || got[1].Member != "" {
		t.Fatalf("rows = %+v", got)
	}
	if rows := MergedMemberRows(nil); rows == nil || len(rows) != 0 {
		t.Fatalf("nil Automation rows = %#v", rows)
	}
}
