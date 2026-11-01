package autonomous

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// lfrNow is the fixed clock of these tests.
var lfrNow = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

func lfrCand(id string, status PRDStatus, updated time.Time) LaneFillCandidate {
	return LaneFillCandidate{
		ID: id, Title: "title " + id, Status: status, Backend: "pi-prompt",
		BackendChangeAllowed: true, CreatedAt: updated, UpdatedAt: updated,
	}
}

// lfrRefused runs the evaluator and stores the result on the candidate, the
// way the snapshot does.
func lfrRefused(cfg LaneFillConfig, c LaneFillCandidate) LaneFillCandidate {
	code, text := EvaluateLaneFillEligibility(cfg, c, lfrNow)
	c.Refusal = LaneFillRefusal{Code: code, Text: text}
	return c
}

func TestLaneFillStaleJustUnderThresholdIsEligible(t *testing.T) {
	cfg := laneFillTestConfig()
	c := lfrCand("aaaa0001", PRDDraft, lfrNow.Add(-72*time.Hour+time.Minute))
	if code, text := EvaluateLaneFillEligibility(cfg, c, lfrNow); code != "" || text != "" {
		t.Fatalf("just under 72 hours must be eligible, got %q %q", code, text)
	}
}

func TestLaneFillStaleJustOverThresholdIsRefused(t *testing.T) {
	cfg := laneFillTestConfig()
	for _, status := range []PRDStatus{PRDDraft, PRDApproved, PRDNeedsReview} {
		c := lfrCand("aaaa0002", status, lfrNow.Add(-72*time.Hour-time.Minute))
		code, text := EvaluateLaneFillEligibility(cfg, c, lfrNow)
		if code != LaneFillReasonStale {
			t.Fatalf("%s: just over 72 hours must be stale, got %q", status, code)
		}
		if !strings.Contains(text, "73 hours") || !strings.Contains(text, "72 hour") {
			t.Errorf("%s: text must name the age and the threshold in hours, got %q", status, text)
		}
		if strings.Contains(text, "PRD") {
			t.Errorf("operator text must say Automaton, got %q", text)
		}
	}
}

func TestLaneFillStaleZeroHoursTurnsTheRuleOff(t *testing.T) {
	zero := 0
	cfg := laneFillTestConfig()
	cfg.StaleAfterHours = &zero
	c := lfrCand("aaaa0003", PRDDraft, lfrNow.Add(-5000*time.Hour))
	if code, _ := EvaluateLaneFillEligibility(cfg, c, lfrNow); code != "" {
		t.Fatalf("0 hours must disable the stale rule, got %q", code)
	}
}

func TestLaneFillStaleUsesCreatedAtWhenNeverUpdated(t *testing.T) {
	cfg := laneFillTestConfig()
	c := lfrCand("aaaa0004", PRDDraft, time.Time{})
	c.CreatedAt = lfrNow.Add(-100 * time.Hour)
	if code, _ := EvaluateLaneFillEligibility(cfg, c, lfrNow); code != LaneFillReasonStale {
		t.Fatalf("an old CreatedAt with no UpdatedAt must be stale, got %q", code)
	}
	// A recent update wins over an old creation.
	c.UpdatedAt = lfrNow.Add(-time.Hour)
	if code, _ := EvaluateLaneFillEligibility(cfg, c, lfrNow); code != "" {
		t.Fatalf("a recent UpdatedAt must keep it eligible, got %q", code)
	}
}

func TestLaneFillStaleWithNoTimestampsIsNotStale(t *testing.T) {
	c := lfrCand("aaaa0005", PRDDraft, time.Time{})
	if code, _ := EvaluateLaneFillEligibility(laneFillTestConfig(), c, lfrNow); code != "" {
		t.Fatalf("no timestamps must not be stale, got %q", code)
	}
}

func TestLaneFillStaleProbeLikeOldDraftIsRefused(t *testing.T) {
	cfg := laneFillTestConfig()
	c := lfrCand("aaaa0006", PRDDraft, lfrNow.Add(-30*24*time.Hour))
	c.Title = "probe: does the daemon answer"
	if code, _ := EvaluateLaneFillEligibility(cfg, c, lfrNow); code != LaneFillReasonStale {
		t.Fatalf("an old probe draft must be refused as stale, got %q", code)
	}
}

func TestLaneFillStaleNeverRefusesWorkThatIsNotQueued(t *testing.T) {
	cfg := laneFillTestConfig()
	for _, status := range []PRDStatus{PRDRunning, PRDCompleted, PRDCancelled} {
		c := lfrCand("aaaa0007", status, lfrNow.Add(-1000*time.Hour))
		if code, _ := EvaluateLaneFillEligibility(cfg, c, lfrNow); code != "" {
			t.Errorf("%s is not queued work and must not be refused, got %q", status, code)
		}
	}
}

func TestLaneFillReasonCodesAreStable(t *testing.T) {
	if LaneFillReasonStale != "stale" || LaneFillReasonHeld != "held" ||
		LaneFillReasonSequencedAfterUnshipped != "sequenced_after_unshipped" {
		t.Fatalf("refusal codes changed: %q %q %q", LaneFillReasonStale, LaneFillReasonHeld, LaneFillReasonSequencedAfterUnshipped)
	}
}

func TestLaneFillReasonHoldCodes(t *testing.T) {
	lookup := func(ref string) (PRDStatus, bool) {
		switch ref {
		case "aaaa1111":
			return PRDCompleted, true
		case "bbbb2222":
			return PRDRunning, true
		}
		return "", false
	}
	cases := []struct {
		name     string
		spec     string
		wantCode string
	}{
		{"hold line", "HOLD until the operator decides", LaneFillReasonHeld},
		{"do not run", "Planned work. Do not run this yet.", LaneFillReasonHeld},
		{"sequenced last", "This one is sequenced last.", LaneFillReasonHeld},
		{"sequenced after an unknown reference", "sequenced after cccc3333", LaneFillReasonSequencedAfterUnshipped},
		{"sequenced after a not completed Automaton", "sequenced after bbbb2222", LaneFillReasonSequencedAfterUnshipped},
		{"sequenced after a completed Automaton", "sequenced after aaaa1111", ""},
		{"plain spec", "Add a config block.", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prd := &PRD{ID: "11112222", Spec: tc.spec}
			code, text := PRDHoldCode(prd, lookup)
			if code != tc.wantCode {
				t.Fatalf("PRDHoldCode code = %q, want %q (text %q)", code, tc.wantCode, text)
			}
			if (text != "") != (tc.wantCode != "") {
				t.Fatalf("PRDHoldCode text = %q for code %q", text, code)
			}
			if got := PRDHoldReason(prd, lookup); got != text {
				t.Errorf("PRDHoldReason = %q, want exactly the PRDHoldCode text %q", got, text)
			}
		})
	}
	if code, text := PRDHoldCode(nil, lookup); code != "" || text != "" {
		t.Errorf("a nil Automaton is never held, got %q %q", code, text)
	}
}

func TestLaneFillReasonJudgeTimeoutHoldIsHeld(t *testing.T) {
	// A candidate carrying the judge time-out text maps to held.
	cfg := laneFillTestConfig()
	c := lfrCand("aaaa0008", PRDNeedsReview, lfrNow)
	c.HoldReason = "its judge timed out on every backend and it needs review"
	code, text := EvaluateLaneFillEligibility(cfg, c, lfrNow)
	if code != LaneFillReasonHeld || text != c.HoldReason {
		t.Fatalf("a judge time-out hold must be held with its text, got %q %q", code, text)
	}
}

func TestLaneFillReasonEvaluatorMapsEachHoldToItsCode(t *testing.T) {
	cfg := laneFillTestConfig()
	lookup := func(ref string) (PRDStatus, bool) {
		if ref == "aaaa1111" {
			return PRDCompleted, true
		}
		return "", false
	}
	cases := []struct {
		name, spec, wantCode string
	}{
		{"hold line", "HOLD: not yet", LaneFillReasonHeld},
		{"do not run", "do not run", LaneFillReasonHeld},
		{"sequenced last", "sequenced last", LaneFillReasonHeld},
		{"sequenced after unknown", "sequenced after dddd4444", LaneFillReasonSequencedAfterUnshipped},
		{"sequenced after completed", "sequenced after aaaa1111", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hc, ht := PRDHoldCode(&PRD{Spec: tc.spec}, lookup)
			c := lfrCand("aaaa0009", PRDApproved, lfrNow)
			c.HoldCode, c.HoldReason = hc, ht
			code, _ := EvaluateLaneFillEligibility(cfg, c, lfrNow)
			if code != tc.wantCode {
				t.Fatalf("code = %q, want %q", code, tc.wantCode)
			}
		})
	}
}

func TestLaneFillReasonHeldWinsOverStale(t *testing.T) {
	c := lfrCand("aaaa0010", PRDDraft, lfrNow.Add(-500*time.Hour))
	c.HoldReason = "a line starts with HOLD"
	if code, _ := EvaluateLaneFillEligibility(laneFillTestConfig(), c, lfrNow); code != LaneFillReasonHeld {
		t.Fatalf("held is checked before stale, got %q", code)
	}
}

func TestLaneFillReasonHoldReasonAloneMapsToHeld(t *testing.T) {
	// A candidate built with only HoldReason, as before this change, is held.
	c := LaneFillCandidate{ID: "aaaa0011", Status: PRDApproved, HoldReason: "the text says do not run"}
	src, why := ClassifyLaneFillCandidate(c)
	if src != LaneFillSourceNone || !strings.Contains(why, "held") || !strings.Contains(why, "do not run") {
		t.Fatalf("classify = %v %q, want excluded as held", src, why)
	}
	ds := plan(laneFillTestConfig(), testLanes(1, 0), []LaneFillCandidate{c})
	var found bool
	for _, d := range ds {
		if d.PRDID == "aaaa0011" {
			found = true
			if d.EligibilityReason != LaneFillReasonHeld {
				t.Errorf("eligibility_reason = %q, want held", d.EligibilityReason)
			}
		}
	}
	if !found {
		t.Fatalf("no blocked decision for the held Automaton: %+v", ds)
	}
}

func TestLaneFillEligibRefusedIsNeverStartedOrDecomposed(t *testing.T) {
	cfg := laneFillTestConfig()
	cfg.DecomposeAheadMin = 5 // decompose-ahead would love a draft
	staleReady := lfrRefused(cfg, lfrCand("aaaa0020", PRDApproved, lfrNow.Add(-200*time.Hour)))
	staleReady.Priority = 99
	staleDraft := lfrRefused(cfg, lfrCand("aaaa0021", PRDDraft, lfrNow.Add(-200*time.Hour)))
	staleDraft.Priority = 99
	fresh := lfrCand("aaaa0022", PRDApproved, lfrNow)
	fresh.Priority = 1
	for _, c := range []LaneFillCandidate{staleReady, staleDraft} {
		if !c.Refusal.Refused() {
			t.Fatalf("setup: %s must be refused", c.ID)
		}
	}

	lanes := testLanes(2, 2)
	ds := PlanLaneFill(LaneFillInput{
		Config: cfg, Lanes: lanes, Now: lfrNow,
		Candidates: []LaneFillCandidate{staleReady, staleDraft, fresh},
	})
	for _, d := range ds {
		switch d.Action {
		case LaneFillActionStartPRD, LaneFillActionDecompose, LaneFillActionDecomposeAhead:
			if d.PRDID == staleReady.ID || d.PRDID == staleDraft.ID {
				t.Errorf("a refused Automaton was planned for %s: %+v", d.Action, d)
			}
		}
	}
	// The fresh one still gets a lane.
	var started bool
	for _, d := range ds {
		if d.Action == LaneFillActionStartPRD && d.PRDID == fresh.ID {
			started = true
		}
	}
	if !started {
		t.Errorf("the eligible Automaton must still start: %+v", ds)
	}
}

func TestLaneFillEligibOnlyRefusedDraftIsNotDecomposedAhead(t *testing.T) {
	cfg := laneFillTestConfig()
	cfg.DecomposeAheadMin = 3
	c := lfrRefused(cfg, lfrCand("aaaa0023", PRDDraft, lfrNow.Add(-1000*time.Hour)))
	ds := PlanLaneFill(LaneFillInput{Config: cfg, Lanes: testLanes(1, 1), Now: lfrNow, Candidates: []LaneFillCandidate{c}})
	for _, d := range ds {
		if d.Action == LaneFillActionDecompose || d.Action == LaneFillActionDecomposeAhead {
			t.Fatalf("a refused draft must not be decomposed: %+v", d)
		}
	}
}

func TestLaneFillEligibQueuedEligibleExcludesRefused(t *testing.T) {
	cfg := laneFillTestConfig()
	ok := lfrCand("aaaa0030", PRDApproved, lfrNow)
	stale := lfrRefused(cfg, lfrCand("aaaa0031", PRDApproved, lfrNow.Add(-100*time.Hour)))
	held := lfrCand("aaaa0032", PRDApproved, lfrNow)
	held.HoldReason = "a line starts with HOLD"
	got := QueuedEligible([]LaneFillCandidate{ok, stale, held})
	if got[LaneTypeGPU] != 1 || got[LaneTypeClaude] != 1 {
		t.Fatalf("QueuedEligible = %v, want 1 and 1 (only the eligible one)", got)
	}
	if src, _ := ClassifyLaneFillCandidate(stale); src != LaneFillSourceNone {
		t.Errorf("a stale candidate must classify as none, got %v", src)
	}
}

func TestLaneFillReasonRefusalIsABlockedDecision(t *testing.T) {
	cfg := laneFillTestConfig()
	stale := lfrRefused(cfg, lfrCand("aaaa0040", PRDDraft, lfrNow.Add(-100*time.Hour)))
	ds := PlanLaneFill(LaneFillInput{Config: cfg, Lanes: testLanes(1, 0), Now: lfrNow, Candidates: []LaneFillCandidate{stale}})
	var got *LaneFillDecision
	for i := range ds {
		if ds[i].PRDID == stale.ID && ds[i].Action == LaneFillActionBlocked {
			got = &ds[i]
		}
	}
	if got == nil {
		t.Fatalf("no blocked decision for the refused Automaton: %+v", ds)
	}
	if got.EligibilityReason != LaneFillReasonStale {
		t.Errorf("eligibility_reason = %q, want stale", got.EligibilityReason)
	}
	if got.Reason != stale.Refusal.Text || got.Blocker != stale.Refusal.Text || got.Title != stale.Title {
		t.Errorf("reason/blocker/title = %q / %q / %q, want the refusal text %q", got.Reason, got.Blocker, got.Title, stale.Refusal.Text)
	}
	// Decisions without a refusal carry no code.
	for _, d := range ds {
		if d.PRDID != stale.ID && d.EligibilityReason != "" {
			t.Errorf("unrelated decision carries a code: %+v", d)
		}
	}
}

func TestLaneFillReasonRefusalsComeAfterLaneDecisionsInFixedOrder(t *testing.T) {
	cfg := laneFillTestConfig()
	var cands []LaneFillCandidate
	for _, id := range []string{"aaaa0053", "aaaa0051", "aaaa0052"} {
		cands = append(cands, lfrRefused(cfg, lfrCand(id, PRDDraft, lfrNow.Add(-100*time.Hour))))
	}
	cands = append(cands, lfrCand("aaaa0050", PRDApproved, lfrNow))

	first := PlanLaneFill(LaneFillInput{Config: cfg, Lanes: testLanes(1, 1), Now: lfrNow, Candidates: cands})
	reversed := append([]LaneFillCandidate(nil), cands...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	second := PlanLaneFill(LaneFillInput{Config: cfg, Lanes: testLanes(1, 1), Now: lfrNow, Candidates: reversed})
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("the same input gave different decisions:\n%+v\n%+v", first, second)
	}
	again := PlanLaneFill(LaneFillInput{Config: cfg, Lanes: testLanes(1, 1), Now: lfrNow, Candidates: cands})
	if !reflect.DeepEqual(first, again) {
		t.Fatalf("a second run differs from the first")
	}

	laneEnd, refusalStart := -1, -1
	var refusalIDs []string
	for i, d := range first {
		if d.LaneID != "" {
			laneEnd = i
		}
		if d.EligibilityReason != "" {
			if refusalStart < 0 {
				refusalStart = i
			}
			refusalIDs = append(refusalIDs, d.PRDID)
		}
	}
	if refusalStart <= laneEnd {
		t.Errorf("refusals must come after the lane decisions (lanes end %d, refusals start %d)", laneEnd, refusalStart)
	}
	want := []string{"aaaa0051", "aaaa0052", "aaaa0053"}
	if !reflect.DeepEqual(refusalIDs, want) {
		t.Errorf("refusal order = %v, want %v", refusalIDs, want)
	}
}

func TestLaneFillReasonKeyCollapsesSameRefusalAndSplitsOnChange(t *testing.T) {
	cfg := laneFillTestConfig()
	r := newLaneFillRecords(t.TempDir())
	stale := lfrCand("aaaa0060", PRDDraft, lfrNow.Add(-100*time.Hour))
	poll := func(c LaneFillCandidate, now time.Time) {
		c.Refusal.Code, c.Refusal.Text = EvaluateLaneFillEligibility(cfg, c, now)
		for _, d := range PlanLaneFill(LaneFillInput{Config: cfg, Lanes: testLanes(1, 0), Now: now, Candidates: []LaneFillCandidate{c}}) {
			if d.EligibilityReason != "" {
				r.observe(d, now)
			}
		}
		r.endPoll()
	}
	poll(stale, lfrNow)
	poll(stale, lfrNow.Add(time.Minute))
	// An hour later the age in the text has moved on; still the same refusal.
	poll(stale, lfrNow.Add(2*time.Hour))
	list := r.list(0)
	if len(list) != 1 || list[0].RepeatCount != 3 {
		t.Fatalf("the same refusal must collapse into one record with a repeat count of 3, got %+v", list)
	}
	if !strings.Contains(list[0].Blocker, "102 hours") {
		t.Errorf("the record keeps the latest wording, got %q", list[0].Blocker)
	}

	// A changed reason (now held) is a new record.
	held := stale
	held.HoldReason = "a line starts with HOLD"
	poll(held, lfrNow.Add(3*time.Hour))
	list = r.list(0)
	if len(list) != 2 || list[0].EligibilityReason != LaneFillReasonHeld || list[1].EligibilityReason != LaneFillReasonStale {
		t.Fatalf("a changed reason must add a record, got %+v", list)
	}
}

func TestLaneFillReasonRecordsStayBounded(t *testing.T) {
	r := newLaneFillRecords(t.TempDir())
	for i := 0; i < laneFillRecordLimit+50; i++ {
		d := LaneFillDecision{
			Action: LaneFillActionBlocked, PRDID: strings.Repeat("a", 4) + string(rune('0'+i%10)) + time.Duration(i).String(),
			EligibilityReason: LaneFillReasonStale, Blocker: "old",
		}
		r.observe(d, lfrNow)
	}
	r.endPoll()
	if n := len(r.list(0)); n != laneFillRecordLimit {
		t.Fatalf("records = %d, want the retention limit %d", n, laneFillRecordLimit)
	}
}

func TestLaneFillReasonAuditCarriesEligibilityReason(t *testing.T) {
	h := newLaneFillHarness(t, "shadow")
	h.c.SetReady()
	// Make the clock far enough ahead that a freshly saved Automaton is stale.
	h.now = time.Now().Add(200 * time.Hour)
	h.c.SetLaneFillEvidenceSource(&laneFillEvidenceFake{ev: PoolEvidence{CheckedAt: h.now}})
	p := h.addPRD("old draft", PRDDraft, "", "")

	h.poll()

	var found bool
	for _, a := range h.audits() {
		if a["prd_id"] == p.ID && a["action"] == string(LaneFillActionBlocked) {
			found = true
			if a["eligibility_reason"] != LaneFillReasonStale {
				t.Errorf("audit eligibility_reason = %v, want stale: %+v", a["eligibility_reason"], a)
			}
		}
	}
	if !found {
		t.Fatalf("no audit entry for the refusal: %+v", h.audits())
	}
	for _, a := range h.audits() {
		if a["action"] != string(LaneFillActionBlocked) {
			if _, has := a["eligibility_reason"]; has {
				t.Errorf("an entry without a refusal must not carry the key: %+v", a)
			}
		}
	}
	st := h.status()
	if st.QueuedEligible.GPU != 0 || st.QueuedEligible.Claude != 0 {
		t.Errorf("a stale Automaton must not count as eligible: %+v", st.QueuedEligible)
	}
}

func TestLaneFillStaleOnModeNeverStartsOrDecomposesIt(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	h.now = time.Now().Add(200 * time.Hour)
	h.c.SetLaneFillEvidenceSource(&laneFillEvidenceFake{ev: PoolEvidence{CheckedAt: h.now}})
	h.addPRD("old approved", PRDApproved, "", "pi-prompt")
	h.addPRD("old draft", PRDDraft, "", "")

	h.poll()

	if calls := h.fake.all(); len(calls) != 0 {
		t.Fatalf("stale Automata must not be started or decomposed even in on mode: %v", calls)
	}
}
