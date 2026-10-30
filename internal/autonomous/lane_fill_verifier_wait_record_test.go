package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The evidence record carries the verifier-wait reason behind a decision.

// TestVerifierWaitStartDecisionCarriesWaitReason: a start planned on a lane
// the verifier-wait rule counted available carries the waiting occupant's
// reason, so the record shows not only what the planner would start but why
// it thought the lane was free.
func TestVerifierWaitStartDecisionCarriesWaitReason(t *testing.T) {
	cfg := laneFillTestConfig()
	cfg.GPULanes, cfg.ClaudeLanes = 0, 1
	lanes := testLanes(0, 1)
	lanes[0].Occupant = waitingOccupant("wa")

	ds := plan(cfg, lanes, []LaneFillCandidate{readyCand("r1", "", 0, 0)})
	d := byLane(t, ds, "claude-1")
	if d.Action != LaneFillActionStartPRD {
		t.Fatalf("claude-1 decision = %+v, want a start", d)
	}
	if d.WaitReason != "waiting for a capacity slot to finish verification" {
		t.Fatalf("start decision WaitReason = %q, want the waiting occupant's reason (empty means the record hides the basis)", d.WaitReason)
	}
}

// TestVerifierWaitWorkingLaneDecisionHasNoWaitReason: a working lane gets
// no decision at all — it stays busy — and no decision anywhere carries a
// wait reason when nothing waits.
func TestVerifierWaitWorkingLaneDecisionHasNoWaitReason(t *testing.T) {
	cfg := laneFillTestConfig()
	cfg.GPULanes, cfg.ClaudeLanes = 0, 1
	lanes := testLanes(0, 1)
	lanes[0].Occupant = &LaneOccupant{PRDID: "wk", Title: "wk", Backend: "claude-code"}

	ds := plan(cfg, lanes, []LaneFillCandidate{readyCand("r1", "", 0, 0)})
	for _, d := range ds {
		if d.LaneID == "claude-1" {
			t.Fatalf("a decision on the working lane: %+v", d)
		}
		if d.WaitReason != "" {
			t.Fatalf("decision on %s carries WaitReason %q, want none", d.LaneID, d.WaitReason)
		}
	}
}

// TestVerifierWaitWorkingToWaitingIsSecondRecord: on the same lane a move
// from a working occupant (no wait reason on the decision) to a waiting
// occupant (wait reason on the decision) produces a second record rather
// than folding over the first — the dedupe key includes the wait reason.
func TestVerifierWaitWorkingToWaitingIsSecondRecord(t *testing.T) {
	cfg := laneFillTestConfig()
	cfg.GPULanes, cfg.ClaudeLanes = 0, 1
	rec := newLaneFillRecords(t.TempDir())

	// The same start on the same lane, first with the lane working (no wait
	// reason on the decision), then with the lane waiting (wait reason on
	// it). Both get planned: the record must keep both.
	lanes := testLanes(0, 1)
	d1 := byLane(t, plan(cfg, lanes, []LaneFillCandidate{readyCand("r1", "", 0, 0)}), "claude-1")
	if d1.WaitReason != "" {
		t.Fatalf("no occupant, wait reason = %q", d1.WaitReason)
	}
	e1, ok := rec.observe(d1, laneFillTestNow)
	if !ok {
		t.Fatalf("first decision not recorded")
	}
	rec.endPoll()

	lanes[0].Occupant = waitingOccupant("wa")
	d2 := byLane(t, plan(cfg, lanes, []LaneFillCandidate{readyCand("r1", "", 0, 0)}), "claude-1")
	if d2.WaitReason == "" {
		t.Fatalf("start on a waiting lane carries no WaitReason: %+v", d2)
	}
	if d2.LaneID != d1.LaneID || d2.Action != d1.Action || d2.PRDID != d1.PRDID {
		t.Fatalf("different decisions: %+v vs %+v", d1, d2)
	}
	e2, ok := rec.observe(d2, laneFillTestNow.Add(time.Minute))
	if !ok {
		t.Fatalf("working-to-waiting move folded into the earlier record instead of starting a new one")
	}
	if e2 == e1 {
		t.Fatalf("second decision recorded in the first record's entry")
	}
	if got := len(rec.list(0)); got != 2 {
		t.Fatalf("records = %d, want 2 (working → waiting is a new decision on the same lane)", got)
	}

	// The move repeated on the next poll does NOT add another record: same
	// lane, action, Automaton and wait reason folds into the waiting record.
	rec.endPoll()
	if _, ok := rec.observe(d2, laneFillTestNow.Add(2*time.Minute)); ok {
		t.Fatalf("identical wait decision started a third record")
	}
	rec.endPoll()

	var older, newer LaneFillDecision
	rec.read(e1, func(d LaneFillDecision, _ time.Time) { older = d })
	rec.read(e2, func(d LaneFillDecision, _ time.Time) { newer = d })
	if older.WaitReason != "" || older.RepeatCount != 1 {
		t.Fatalf("earlier record after the move: %+v", older)
	}
	if newer.WaitReason == "" || newer.RepeatCount != 2 {
		t.Fatalf("waiting record after the repeat: %+v", newer)
	}
}

// TestVerifierWaitLaneStatusShowsWaitingVerifier: the lane status separates
// a lane that works from one that waits: a verifier-wait lane counted free
// by the rule is waiting_verifier with the occupant's reason, a working
// lane stays occupied, an empty lane stays idle.
func TestVerifierWaitLaneStatusShowsWaitingVerifier(t *testing.T) {
	cfg := laneFillTestConfig()
	lane := Lane{ID: "claude-1", Type: LaneTypeClaude}

	lane.Occupant = waitingOccupant("wa")
	ls := laneFillLaneStatusView(cfg, lane, 1)
	if ls.State != "waiting_verifier" {
		t.Fatalf("state = %q, want waiting_verifier", ls.State)
	}
	if !ls.WaitingOnVerifier {
		t.Fatalf("WaitingOnVerifier = false, want true")
	}
	if ls.WaitReason != "waiting for a capacity slot to finish verification" {
		t.Fatalf("WaitReason = %q, want the occupant's reason", ls.WaitReason)
	}

	// Rule off: the waiting lane counts busy again and keeps its meaning.
	cfg.CountVerifierWait = boolPtr(false)
	ls = laneFillLaneStatusView(cfg, lane, 1)
	if ls.State != "occupied" {
		t.Fatalf("state with the rule off = %q, want occupied", ls.State)
	}
	if !ls.WaitingOnVerifier || ls.WaitReason == "" {
		t.Fatalf("rule off still hides the wait: %+v", ls)
	}

	// A working occupant on a Claude lane stays occupied with no wait.
	cfg.CountVerifierWait = nil
	lane.Occupant = &LaneOccupant{PRDID: "wk", Title: "wk", Backend: "claude-code"}
	ls = laneFillLaneStatusView(cfg, lane, 0)
	if ls.State != "occupied" || ls.WaitingOnVerifier || ls.WaitReason != "" {
		t.Fatalf("working lane: %+v", ls)
	}

	// A waiting occupant on a GPU lane stays occupied — the rule counts
	// Claude lanes — but the wait itself is still shown.
	gpu := Lane{ID: "gpu-1", Type: LaneTypeGPU}
	gpu.Occupant = waitingOccupant("wg")
	ls = laneFillLaneStatusView(cfg, gpu, 0)
	if ls.State != "occupied" {
		t.Fatalf("gpu lane state = %q, want occupied", ls.State)
	}
	if !ls.WaitingOnVerifier || ls.WaitReason == "" {
		t.Fatalf("gpu lane hides the wait: %+v", ls)
	}

	// An empty lane is idle.
	lane.Occupant = nil
	ls = laneFillLaneStatusView(cfg, lane, 3)
	if ls.State != "idle" || ls.WaitingOnVerifier || ls.WaitReason != "" {
		t.Fatalf("empty lane: %+v", ls)
	}
	if ls.EligibleWaiting != 3 {
		t.Fatalf("EligibleWaiting = %d, want 3", ls.EligibleWaiting)
	}
}

// TestVerifierWaitJSONNamesAreExact: the two status fields and the decision
// field go over the wire under exactly these names, and only when set.
func TestVerifierWaitJSONNamesAreExact(t *testing.T) {
	ls := LaneFillLaneStatus{
		ID: "claude-1", Type: LaneTypeClaude, State: "waiting_verifier",
		WaitingOnVerifier: true, WaitReason: "waiting for a capacity slot to finish verification",
	}
	b, err := json.Marshal(ls)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"waiting_on_verifier":true`) {
		t.Errorf("marshalled status lacks an exact \"waiting_on_verifier\":true field: %s", b)
	}
	if !strings.Contains(string(b), `"wait_reason":"waiting for a capacity slot to finish verification"`) {
		t.Errorf("marshalled status lacks an exact \"wait_reason\" field with the reason: %s", b)
	}
	v := map[string]any{}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if _, ok := v["waiting_on_verifier"]; !ok {
		t.Errorf("decoded status has no waiting_on_verifier key: %v", v)
	}
	if _, ok := v["wait_reason"]; !ok {
		t.Errorf("decoded status has no wait_reason key: %v", v)
	}

	d := LaneFillDecision{LaneID: "claude-1", Action: LaneFillActionStartPRD, WaitReason: "waiting for a capacity slot to finish verification"}
	b, err = json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"wait_reason":"waiting for a capacity slot to finish verification"`) {
		t.Errorf("marshalled decision lacks an exact \"wait_reason\" field: %s", b)
	}

	// Unset means absent, not the empty string.
	b, _ = json.Marshal(LaneFillDecision{})
	if strings.Contains(string(b), "wait_reason") {
		t.Errorf("unset decision wait_reason should be omitted: %s", b)
	}
	b, _ = json.Marshal(LaneFillLaneStatus{ID: "gpu-1", Type: LaneTypeGPU, State: "occupied"})
	if strings.Contains(string(b), "waiting_on_verifier") || strings.Contains(string(b), "wait_reason") {
		t.Errorf("unset lane wait fields should be omitted: %s", b)
	}
}
