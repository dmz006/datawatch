package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestClosedWorkRefusalCodes(t *testing.T) {
	shipped := func(status PRDStatus) *PRD {
		return &PRD{ID: "aaaa0001", Status: status, MergedInRelease: shippedRelease, MergeCommit: shippedCommit}
	}
	cases := []struct {
		name string
		p    *PRD
		want string
	}{
		{"queued with shipped record", shipped(PRDNeedsReview), LaneFillReasonAlreadyShipped},
		{"failed with shipped record", shipped(PRDFailed), LaneFillReasonAlreadyShipped},
		{"blocked with shipped record", shipped(PRDBlocked), LaneFillReasonAlreadyShipped},
		{"archived", &PRD{Status: PRDArchived}, LaneFillReasonWorkClosed},
		{"cancelled with shipped record", shipped(PRDCancelled), LaneFillReasonWorkClosed},
		{"completed", &PRD{Status: PRDCompleted}, LaneFillReasonWorkClosed},
		{"rejected", &PRD{Status: PRDRejected}, LaneFillReasonWorkClosed},
		{"running with shipped record", shipped(PRDRunning), ""},
		{"held with shipped record", shipped(PRDHeld), ""},
		{"planning with shipped record", shipped(PRDPlanning), ""},
		{"commit without release", &PRD{Status: PRDApproved, MergeCommit: shippedCommit}, ""},
		{"release without commit", &PRD{Status: PRDApproved, MergedInRelease: shippedRelease}, ""},
		{"nothing set", &PRD{Status: PRDApproved}, ""},
	}
	for _, tc := range cases {
		if code, text := closedWorkRefusal(tc.p); code != tc.want || (code != "" && text == "") {
			t.Errorf("%s: code %q text %q, want code %q with a text", tc.name, code, text, tc.want)
		}
	}
	if LaneFillReasonAlreadyShipped == LaneFillReasonWorkClosed || LaneFillReasonAlreadyShipped == LaneFillReasonHeld {
		t.Fatal("the new codes must be distinct")
	}
}

func TestEligibilityRefusesAShippedCandidateBeforeOtherRules(t *testing.T) {
	cfg := laneFillTestConfig()
	c := LaneFillCandidate{
		ID: "bbbb0001", Status: PRDApproved, ShippedRelease: shippedRelease, ShippedCommit: shippedCommit,
		HoldReason: "a line starts with HOLD", UpdatedAt: time.Now().Add(-1000 * time.Hour),
	}
	code, text := EvaluateLaneFillEligibility(cfg, c, time.Now())
	if code != LaneFillReasonAlreadyShipped {
		t.Fatalf("code = %q, want %q ahead of a hold line and the stale rule", code, LaneFillReasonAlreadyShipped)
	}
	for _, want := range []string{shippedRelease, shortCommit(shippedCommit)} {
		if !strings.Contains(text, want) {
			t.Errorf("text %q must name %q", text, want)
		}
	}
	c.ShippedCommit = ""
	if code, _ := EvaluateLaneFillEligibility(cfg, c, time.Now()); code == LaneFillReasonAlreadyShipped {
		t.Fatal("a candidate with no shipped record must not be refused as shipped")
	}
	held := LaneFillCandidate{ID: "bbbb0002", Status: PRDHeld, ShippedRelease: shippedRelease, ShippedCommit: shippedCommit}
	if code, _ := EvaluateLaneFillEligibility(cfg, held, time.Now()); code != LaneFillReasonOperatorHold {
		t.Fatalf("code = %q, an operator hold is named first", code)
	}
}

func TestShippedRefusalModes(t *testing.T) {
	for _, mode := range []string{ShippedRefusalOff, ShippedRefusalShadow} {
		t.Run(mode, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.ShippedRefusal = mode
			m := newEmitManager(t, cfg, nil)
			prd := newShippedPRD(t, m, "shipped", PRDApproved, shippedRelease, shippedCommit)
			if err := (&API{M: m}).Run(prd.ID); err != nil {
				t.Fatalf("mode %s must not refuse: %v", mode, err)
			}
			if ids := m.DeliverShippedQueued(); len(ids) != 0 {
				t.Fatalf("mode %s sweep changed %v", mode, ids)
			}
			got, _ := m.Store().GetPRD(prd.ID)
			if got.Status != PRDRunning || got.ShippedRefusal != nil {
				t.Fatalf("mode %s: status %s record %+v, nothing may change", mode, got.Status, got.ShippedRefusal)
			}
		})
	}
	if (Config{}).EffectiveShippedRefusal() != ShippedRefusalOn || (Config{ShippedRefusal: " ON "}).EffectiveShippedRefusal() != ShippedRefusalOn ||
		(Config{ShippedRefusal: "bogus"}).EffectiveShippedRefusal() != ShippedRefusalOn {
		t.Fatal("empty, upper-case and unknown values must read as on")
	}
}

func TestShippedRefusalRecordsReasonOnTheAutomaton(t *testing.T) {
	m, clk := newHoldManager(t)
	prd := newShippedPRD(t, m, "Land X onto main", PRDNeedsReview, shippedRelease, shippedCommit)
	if ids := m.DeliverShippedQueued(); len(ids) != 1 || ids[0] != prd.ID {
		t.Fatalf("sweep = %v, want [%s]", ids, prd.ID)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	r := got.ShippedRefusal
	if r == nil || r.From != PRDNeedsReview || r.Release != shippedRelease || r.Commit != shippedCommit || !r.At.Equal(clk.t) {
		t.Fatalf("ShippedRefusal = %+v", r)
	}
	if got.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed (delivered)", got.Status)
	}
	if ids := m.DeliverShippedQueued(); len(ids) != 0 {
		t.Fatalf("a second sweep changed %v", ids)
	}
}

func TestForceLiftsTheRuleAuditedAndRestoresDroppedWork(t *testing.T) {
	m, _ := newHoldManager(t)
	prd := newShippedPRD(t, m, "Land X onto main", PRDApproved, shippedRelease, shippedCommit)
	a := &API{M: m}

	if _, err := m.ForceClosedWork(prd.ID, "operator", "  "); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("force with no reason = %v, want a reason demanded", err)
	}
	// Dropped first: the daemon marked it delivered before the operator acted.
	if err := a.Run(prd.ID); err == nil {
		t.Fatal("first Run must be refused")
	}
	if got, _ := m.Store().GetPRD(prd.ID); got.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed", got.Status)
	}

	out, err := m.ForceClosedWork(prd.ID, "alice", "the release was reverted")
	if err != nil {
		t.Fatalf("ForceClosedWork: %v", err)
	}
	if out.Status != PRDApproved || out.ShippedRefusal != nil {
		t.Fatalf("status %s record %+v, want the dropped Automaton back to approved", out.Status, out.ShippedRefusal)
	}
	o := out.ClosedWorkOverride
	if o == nil || o.Actor != "alice" || o.Commit != shippedCommit || o.Reason != "the release was reverted" {
		t.Fatalf("override = %+v", o)
	}
	ds := shippedDecisions(out, DecisionClosedWorkOverride)
	if len(ds) != 1 || ds[0].Actor != "alice" || !strings.Contains(ds[0].Note, "the release was reverted") || !strings.Contains(ds[0].Note, shortCommit(shippedCommit)) {
		t.Fatalf("override decision = %+v", ds)
	}
	if err := a.Run(prd.ID); err != nil {
		t.Fatalf("Run after force: %v", err)
	}
	if got, _ := m.Store().GetPRD(prd.ID); got.Status != PRDRunning {
		t.Fatalf("status = %s, want running", got.Status)
	}
	if ids := m.DeliverShippedQueued(); len(ids) != 0 {
		t.Fatalf("sweep dropped forced work: %v", ids)
	}
	if _, err := m.ForceClosedWork(prd.ID, "alice", "again"); err == nil {
		t.Fatal("a second force for the same commit must be refused")
	}
}

func TestForceCoversOnlyTheNamedCommit(t *testing.T) {
	m, _ := newHoldManager(t)
	prd := newShippedPRD(t, m, "shipped", PRDApproved, shippedRelease, shippedCommit)
	if _, err := m.ForceClosedWork(prd.ID, "alice", "on purpose"); err != nil {
		t.Fatal(err)
	}
	m.Store().MutatePRD(prd.ID, func(p *PRD) bool { p.MergeCommit = "ffffffffffffffff"; return true })
	if err := (&API{M: m}).Run(prd.ID); err == nil || !strings.Contains(err.Error(), "already shipped") {
		t.Fatalf("Run with a new shipped commit = %v, want refused again", err)
	}
}

func TestForceRefusedWhereThereIsNothingToForce(t *testing.T) {
	m, _ := newHoldManager(t)
	plain := newShippedPRD(t, m, "plain", PRDApproved, "", "")
	if _, err := m.ForceClosedWork(plain.ID, "operator", "why"); err == nil || !strings.Contains(err.Error(), "no shipped record") {
		t.Fatalf("force without a record = %v", err)
	}
	cancelled := newShippedPRD(t, m, "cancelled", PRDCancelled, shippedRelease, shippedCommit)
	if _, err := m.ForceClosedWork(cancelled.ID, "operator", "why"); err == nil || !strings.Contains(err.Error(), "closed work") {
		t.Fatalf("force on a cancelled Automaton = %v, want closed work refused", err)
	}
	if _, err := m.ForceClosedWork("nope0001", "operator", "why"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("force on a missing Automaton = %v", err)
	}
}
