package usagecalc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPolicyIDsAndLabels(t *testing.T) {
	want := map[string]string{"use-all": "Run until used up", "pace": "Pace to the week", "finish-early": "Run flat out"}
	ids := Policies()
	if len(ids) != 3 || ids[0] != "use-all" || ids[1] != "pace" || ids[2] != "finish-early" {
		t.Fatalf("Policies() = %v", ids)
	}
	for id, label := range want {
		if got := PolicyLabel(id); got != label {
			t.Errorf("PolicyLabel(%q) = %q, want %q", id, got, label)
		}
		if normalizePolicy(id) != id {
			t.Errorf("normalizePolicy(%q) changed the id", id)
		}
	}
	if PolicyLabel("hold-everything") != "" {
		t.Errorf("an unknown policy has a label")
	}
	for _, id := range []string{"", "hold-everything", "USE-ALL"} {
		if normalizePolicy(id) != PolicyUseAll {
			t.Errorf("normalizePolicy(%q) = %q, want use-all", id, normalizePolicy(id))
		}
	}
	in := workedExample(t)
	in.Policy = "hold-everything"
	if a := Advise(in); a.Advice.Policy != PolicyUseAll || a.Advice.LanesSuggested != 2 {
		t.Errorf("unknown policy advice = %+v", a.Advice)
	}
}

// withBurn builds a reading whose recent burn per lane is controlled by the points added
// over the last five hours.
func withBurn(t *testing.T, percent, added float64, policy string) Input {
	in := workedExample(t)
	in.Readings[0] = percentReading(at(t, 2026, 10, 23, 17, 8), percent-added)
	in.Readings[1] = percentReading(at(t, 2026, 10, 23, 22, 8), percent)
	in.Policy = policy
	return in
}

func TestPolicyUseAllWithinBudget(t *testing.T) {
	a := Advise(withBurn(t, 40, 2, PolicyUseAll)) // gentle burn: every lane count fits
	if a.Verdict != VerdictWithin {
		t.Fatalf("verdict = %s", a.Verdict)
	}
	pa := a.Advice
	want := "Within budget: 3 lanes end the week at " + formatPoints(a.ByLanes[3].ProjectedEnd) + "%, " +
		formatPoints(100-a.ByLanes[3].ProjectedEnd) + " points unused at the reset."
	if pa.Headline != want || pa.LanesSuggested != 3 {
		t.Errorf("got %d %q, want 3 %q", pa.LanesSuggested, pa.Headline, want)
	}
	if len(pa.Notes) != 2 || pa.Notes[1] != "Unused allowance is lost at the reset; there is room for more work." {
		t.Errorf("notes = %v", pa.Notes)
	}
	if pa.Finish != nil {
		t.Errorf("use-all carries a finish block: %+v", pa.Finish)
	}
}

func TestPolicyUseAllOverBudgetNote(t *testing.T) {
	pa := Advise(workedExample(t)).Advice
	if len(pa.Notes) != 2 || pa.Notes[1] != "Running out early is allowed under this policy; the alternative is 2 lanes to the reset." {
		t.Errorf("notes = %v", pa.Notes)
	}
}

func TestPolicyUseAllFloorsAtOneLane(t *testing.T) {
	// Even one lane overshoots the allowance, yet the advice names one lane, never zero.
	a := Advise(withBurn(t, 95, 5, PolicyUseAll))
	if a.ByLanes[1].ProjectedEnd <= 100 {
		t.Fatalf("setup: one lane projects %v", a.ByLanes[1].ProjectedEnd)
	}
	if a.Advice.LanesSuggested != 1 || !strings.Contains(a.Advice.Headline, "1 lane ends at ") {
		t.Errorf("got %d %q", a.Advice.LanesSuggested, a.Advice.Headline)
	}
}

func TestPolicyPaceWithinAndAheadOfPace(t *testing.T) {
	a := Advise(withBurn(t, 40, 2, PolicyPace))
	want := "Within budget: 3 lanes end the week at " + formatPoints(a.ByLanes[3].ProjectedEnd) + "%, inside the 95% target."
	if a.Advice.Headline != want || a.Advice.LanesSuggested != 3 || len(a.Advice.Notes) != 1 {
		t.Errorf("got %d %q %v", a.Advice.LanesSuggested, a.Advice.Headline, a.Advice.Notes)
	}

	// Ahead of the pace line adds a note, and so does a target that no lane count reaches.
	b := Advise(withBurn(t, 97, 5, PolicyPace))
	if !b.AheadOfPace {
		t.Fatal("setup: the reading should be ahead of pace")
	}
	notes := strings.Join(b.Advice.Notes, "|")
	if !strings.Contains(notes, "Even 1 lane does not reach the target; the allowance is the limit.") ||
		!strings.Contains(notes, "Usage is ahead of the pace line by "+formatPoints(b.Percent-b.PaceLine)+" points.") {
		t.Errorf("notes = %v", b.Advice.Notes)
	}
	if b.Advice.LanesSuggested != 1 {
		t.Errorf("lanes = %d, want 1", b.Advice.LanesSuggested)
	}
}

func TestPolicyPaceUsesTheSafetyMargin(t *testing.T) {
	in := workedExample(t)
	in.Policy = PolicyPace
	in.SafetyPoints = f64(3) // the design: a 3-point margin advises 2 lanes instead of 1
	if got := Advise(in).Advice.LanesSuggested; got != 2 {
		t.Errorf("3-point margin: %d lanes, want 2", got)
	}
	in.SafetyPoints = f64(5)
	if got := Advise(in).Advice.LanesSuggested; got != 1 {
		t.Errorf("5-point margin: %d lanes, want 1", got)
	}
}

func TestPolicyFinishEarlyNeverSlowsLanes(t *testing.T) {
	in := workedExample(t)
	in.Policy = PolicyFinishEarly
	pa := Advise(in).Advice
	if len(pa.Notes) != 2 || pa.Notes[1] != "All lanes run; the calculator does not slow them down under this policy." {
		t.Errorf("notes = %v", pa.Notes)
	}
	in.RemainingWork = RemainingWork{}
	pa = Advise(in).Advice
	if pa.Headline != "Enter the remaining work to see whether it finishes before the allowance does." || pa.LanesSuggested != 3 || pa.Finish != nil {
		t.Errorf("without work: %+v", pa)
	}
	// The suggestion follows the configured maximum.
	in.MaxLanes = 5
	if got := Advise(in).Advice.LanesSuggested; got != 5 {
		t.Errorf("max 5 lanes: %d suggested", got)
	}
}

func TestPolicySwitchChangesOnlyTheAdvice(t *testing.T) {
	in := workedExample(t)
	var base Advice
	for i, policy := range Policies() {
		in.Policy = policy
		a := Advise(in)
		a.Advice = PolicyAdvice{}
		if i == 0 {
			base = a
			continue
		}
		if a.Percent != base.Percent || a.ProjectedEndRecent != base.ProjectedEndRecent || a.ProjectedEndAvg != base.ProjectedEndAvg ||
			a.RecentBurnPer24h != base.RecentBurnPer24h || a.Verdict != base.Verdict || a.Chosen.ProjectedEnd != base.Chosen.ProjectedEnd ||
			len(a.ByLanes) != len(base.ByLanes) {
			t.Errorf("%s changed a number of the reading", policy)
		}
		for j := range a.ByLanes {
			if a.ByLanes[j].ProjectedEnd != base.ByLanes[j].ProjectedEnd || a.ByLanes[j].BurnPer24h != base.ByLanes[j].BurnPer24h {
				t.Errorf("%s changed lane row %d", policy, j)
			}
		}
	}
}

// TestPolicyTargetIsTheTargetEachPolicyAimsAt pins the projected end the response names for
// each policy: the ceiling under use-all, the ceiling minus the margin under pace, and none
// under finish-early. Bought extra credit moves the ceiling, so it moves both targets.
func TestPolicyTargetIsTheTargetEachPolicyAimsAt(t *testing.T) {
	for _, extra := range []float64{0, 10} {
		in := workedExample(t)
		in.ExtraCreditPoints = extra
		ceiling := 100 + extra
		cases := []struct {
			policy string
			want   *float64
		}{
			{PolicyUseAll, f64(ceiling)},
			{PolicyPace, f64(ceiling - 5)},
			{PolicyFinishEarly, nil},
		}
		for _, c := range cases {
			in.Policy = c.policy
			got := Advise(in).Advice.PolicyTarget
			switch {
			case c.want == nil && got != nil:
				t.Errorf("extra %v, %s: policy_target = %v, want none", extra, c.policy, *got)
			case c.want != nil && (got == nil || *got != *c.want):
				t.Errorf("extra %v, %s: policy_target = %v, want %v", extra, c.policy, got, *c.want)
			}
		}
	}
}

// TestPolicyTargetSerializesAsNullUnderFinishEarly keeps the field present in the body, so a
// renderer can tell "no target" from a missing field.
func TestPolicyTargetSerializesAsNullUnderFinishEarly(t *testing.T) {
	in := workedExample(t)
	in.Policy = PolicyFinishEarly
	body, err := json.Marshal(Advise(in).Advice)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"policy_target":null`) {
		t.Errorf("finish-early advice JSON lacks policy_target null: %s", body)
	}
	in.Policy = PolicyUseAll
	body, err = json.Marshal(Advise(in).Advice)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"policy_target":100`) {
		t.Errorf("use-all advice JSON lacks policy_target 100: %s", body)
	}
}
