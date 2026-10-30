package autonomous

import (
	"strings"
	"testing"
)

func mergedAlerts(h *coordHarness) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, a := range h.alerts {
		if strings.Contains(a, MergedMemberAlertTitle) {
			out = append(out, a)
		}
	}
	return out
}

func TestMergedMemberAlert_AdvanceNamesAutomatonBranchAndCommit(t *testing.T) {
	h, a, _ := mergedCoordHarness(t, "", "main")
	h.setStatus(0, PRDCancelled)
	h.event(0, PRDCancelled)

	got := mergedAlerts(h)
	if len(got) != 1 {
		t.Fatalf("merged-member alerts = %v, want exactly one", got)
	}
	al := got[0]
	auto := h.get(a)
	for _, want := range []string{"info|", auto.Name, "automaton/x", "main", proofTipSHA, "Automaton", a.ID} {
		if !strings.Contains(al, want) {
			t.Errorf("alert lacks %q: %q", want, al)
		}
	}
	if strings.Contains(strings.ToLower(al), "prd") {
		t.Errorf("alert must say Automaton or Automata, never PRD: %q", al)
	}
}

func TestMergedMemberAlert_NotRaisedOnBlockedPaths(t *testing.T) {
	for _, tc := range []struct {
		name, policy string
		unmerged     bool
	}{
		{"policy manual", "manual", false},
		{"policy block", "block", false},
		{"unmerged branch", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, a, _ := mergedCoordHarness(t, tc.policy, "main")
			if tc.unmerged {
				git := newMemberProofMergeGit(t, mergedMemberScript("main", "automaton/x", true))
				git.script[joinArgs("cherry", "main", "automaton/x")] = scriptStep{out: "+ " + proofOtherSHA + "\n"}
				h.c.mp.git = git
			}
			h.setStatus(0, PRDCancelled)
			h.event(0, PRDCancelled)

			if got := h.get(a); got.Status != AutomationBlocked {
				t.Fatalf("status = %s, want blocked", got.Status)
			}
			if got := mergedAlerts(h); len(got) != 0 {
				t.Fatalf("blocked path raised the merged-member alert: %v", got)
			}
		})
	}
}

func TestMergedMemberAlert_NotRaisedWhenMemberCompleted(t *testing.T) {
	h, _, _ := mergedCoordHarness(t, "", "main")
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	if got := mergedAlerts(h); len(got) != 0 {
		t.Fatalf("a member that completed on its own raised the alert: %v", got)
	}
}

func TestMergedMemberAlert_Body(t *testing.T) {
	body := MergedMemberAlertBody("Ship", "a1", "alpha", "automaton/x", "main", "abc1234")
	for _, want := range []string{`Automaton "Ship"`, "alpha", "automaton/x", "main", "abc1234", "merged-members a1"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q: %q", want, body)
		}
	}
	if strings.Contains(strings.ToLower(body), "prd") {
		t.Errorf("body names a PRD: %q", body)
	}
}
