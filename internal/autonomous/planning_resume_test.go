package autonomous

import (
	"strings"
	"testing"
)

// planningResumeOracle states the restart rules directly, independent of the
// implementation, so the exhaustive test checks every combination against it.
func planningResumeOracle(policy string, in PlanningResumeInput) PlanningResumeAction {
	switch policy {
	case "draft":
		return ActionResetToDraft
	case "restart":
		return ActionRestartPlanning
	}
	// reattach, and unset/unknown values which read as reattach
	if in.HasPlannerRecord && in.SessionAlive {
		return ActionAttachReader
	}
	if in.HasPlannerRecord && in.OutputState == OutputCompleteParsable {
		return ActionIngestOutput
	}
	if in.AlreadyRestartedOnce {
		return ActionResetToDraft
	}
	return ActionRestartPlanning
}

var (
	planningResumePolicies = []string{"reattach", "restart", "draft", "", "bogus"}
	planningResumeStates   = []OutputState{OutputNone, OutputPartial, OutputCompleteParsable, OutputCompleteUnparsable}
)

func TestPlanningResumeDecisionEveryPolicyAndInputCombination(t *testing.T) {
	count := 0
	for _, policy := range planningResumePolicies {
		for _, hasRecord := range []bool{false, true} {
			for _, alive := range []bool{false, true} {
				for _, state := range planningResumeStates {
					for _, restarted := range []bool{false, true} {
						in := PlanningResumeInput{
							Policy:               policy,
							HasPlannerRecord:     hasRecord,
							SessionAlive:         alive,
							OutputState:          state,
							AlreadyRestartedOnce: restarted,
						}
						want := planningResumeOracle(policy, in)
						got, reason := DecidePlanningResumeWithReason(in)
						if got != want {
							t.Errorf("policy=%q record=%v alive=%v output=%s restarted=%v: got %s, want %s (reason: %s)",
								policy, hasRecord, alive, state, restarted, got, want, reason)
						}
						if DecidePlanningResume(in) != got {
							t.Errorf("DecidePlanningResume disagrees with DecidePlanningResumeWithReason for %+v", in)
						}
						if strings.TrimSpace(reason) == "" {
							t.Errorf("empty reason for %+v", in)
						}
						count++
					}
				}
			}
		}
	}
	if want := 5 * 2 * 2 * 4 * 2; count != want {
		t.Fatalf("covered %d combinations, want %d", count, want)
	}
}

func TestPlanningResumeDecisionNamedCases(t *testing.T) {
	cases := []struct {
		name string
		in   PlanningResumeInput
		want PlanningResumeAction
	}{
		{"reattach live session attaches reader", PlanningResumeInput{Policy: "reattach", HasPlannerRecord: true, SessionAlive: true, OutputState: OutputNone}, ActionAttachReader},
		{"reattach live session wins over complete output", PlanningResumeInput{Policy: "reattach", HasPlannerRecord: true, SessionAlive: true, OutputState: OutputCompleteParsable}, ActionAttachReader},
		{"reattach ended session with parsable output ingests", PlanningResumeInput{Policy: "reattach", HasPlannerRecord: true, OutputState: OutputCompleteParsable}, ActionIngestOutput},
		{"reattach ended session with partial output restarts once", PlanningResumeInput{Policy: "reattach", HasPlannerRecord: true, OutputState: OutputPartial}, ActionRestartPlanning},
		{"reattach ended session with unparsable output restarts once", PlanningResumeInput{Policy: "reattach", HasPlannerRecord: true, OutputState: OutputCompleteUnparsable}, ActionRestartPlanning},
		{"reattach ended session with no output restarts once", PlanningResumeInput{Policy: "reattach", HasPlannerRecord: true, OutputState: OutputNone}, ActionRestartPlanning},
		{"reattach after the one restart resets to draft", PlanningResumeInput{Policy: "reattach", HasPlannerRecord: true, OutputState: OutputNone, AlreadyRestartedOnce: true}, ActionResetToDraft},
		{"reattach without a planner record restarts once", PlanningResumeInput{Policy: "reattach", SessionAlive: true, OutputState: OutputCompleteParsable}, ActionRestartPlanning},
		{"restart policy always restarts", PlanningResumeInput{Policy: "restart", HasPlannerRecord: true, SessionAlive: true, OutputState: OutputCompleteParsable}, ActionRestartPlanning},
		{"restart policy restarts even after a restart", PlanningResumeInput{Policy: "restart", AlreadyRestartedOnce: true}, ActionRestartPlanning},
		{"draft policy always resets", PlanningResumeInput{Policy: "draft", HasPlannerRecord: true, SessionAlive: true, OutputState: OutputCompleteParsable}, ActionResetToDraft},
		{"unset policy reads as reattach", PlanningResumeInput{Policy: "", HasPlannerRecord: true, SessionAlive: true}, ActionAttachReader},
		{"unknown policy reads as reattach", PlanningResumeInput{Policy: "bogus", HasPlannerRecord: true, OutputState: OutputCompleteParsable}, ActionIngestOutput},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DecidePlanningResume(c.in); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestPlanningResumeDecisionReasonsNameTheRule(t *testing.T) {
	_, reason := DecidePlanningResumeWithReason(PlanningResumeInput{Policy: "draft"})
	if !strings.Contains(reason, "draft") {
		t.Errorf("draft reason should name the policy, got %q", reason)
	}
	_, reason = DecidePlanningResumeWithReason(PlanningResumeInput{Policy: "reattach", HasPlannerRecord: true, OutputState: OutputPartial, AlreadyRestartedOnce: true})
	if !strings.Contains(reason, "already used") || !strings.Contains(reason, "partial") {
		t.Errorf("reset reason should name the spent restart and the output state, got %q", reason)
	}
}

const validPlannerOutput = `{"title":"Plan","stories":[{"title":"S1","description":"d","tasks":[{"title":"T1","spec":"do it"}]}]}`

func TestPlanningResumeDecisionClassifyPlannerOutput(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want OutputState
	}{
		{"empty", "", OutputNone},
		{"whitespace only", " \n\t ", OutputNone},
		{"valid plan", validPlannerOutput, OutputCompleteParsable},
		{"valid plan in json fence", "```json\n" + validPlannerOutput + "\n```", OutputCompleteParsable},
		{"valid JSON with no stories", `{"title":"Plan","stories":[]}`, OutputCompleteUnparsable},
		{"stories without tasks", `{"title":"Plan","stories":[{"title":"S1","tasks":[]}]}`, OutputCompleteUnparsable},
		{"whole document of wrong shape", `{"hello":"world"}`, OutputCompleteUnparsable},
		{"truncated mid document", validPlannerOutput[:len(validPlannerOutput)/2], OutputPartial},
		{"plain text", "I am still thinking about the plan", OutputPartial},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyPlannerOutput([]byte(c.raw)); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}
