package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// laneFillReasonsStatus is a status with a refusal decision, a decision that
// is not a refusal, lane counts and per-kind modes.
func laneFillReasonsStatus() autonomous.LaneFillStatus {
	st := laneFillFixtureStatus()
	st.Modes = autonomous.LaneFillKindModes{GPU: "on", Claude: "shadow"}
	st.LaneCounts = autonomous.LaneFillLaneCounts{
		GPU:    autonomous.LaneFillLaneCount{Configured: 2, Effective: 2, Source: autonomous.LaneCountSourceConfig},
		Claude: autonomous.LaneFillLaneCount{Configured: 3, Effective: 1, Source: autonomous.LaneCountSourceCaps},
	}
	st.Decisions = []autonomous.LaneFillDecision{
		{
			Mode: "shadow", LaneID: "gpu-1", LaneType: "gpu", Action: autonomous.LaneFillActionBlocked,
			PRDID: "old1", Title: "Probe", Reason: "queued longer than 72 hours",
			Blocker: "queued longer than 72 hours", EligibilityReason: autonomous.LaneFillReasonStale,
			RepeatCount: 1,
		},
		{
			Mode: "shadow", LaneID: "gpu-1", LaneType: "gpu", Action: autonomous.LaneFillActionStartPRD,
			PRDID: "abc", Title: "Parser", Reason: "lane idle", RepeatCount: 1,
		},
	}
	return st
}

func laneFillReasonsBody(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	spy := newLaneSpy()
	spy.status = laneFillReasonsStatus()
	rec := getLaneFill(&Server{autonomousMgr: spy}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET lane-fill: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return got
}

// TestLaneFillReasonsRefusalCarriesEligibilityReason checks that a refusal
// decision returns its eligibility_reason and a non-refusal omits the key.
func TestLaneFillReasonsRefusalCarriesEligibilityReason(t *testing.T) {
	got := laneFillReasonsBody(t)
	var decisions []map[string]json.RawMessage
	if err := json.Unmarshal(got["decisions"], &decisions); err != nil || len(decisions) != 2 {
		t.Fatalf("decisions: %v %s", err, got["decisions"])
	}
	var code string
	raw, ok := decisions[0]["eligibility_reason"]
	if !ok {
		t.Fatalf("the refusal decision lacks eligibility_reason: %v", decisions[0])
	}
	if err := json.Unmarshal(raw, &code); err != nil || code != autonomous.LaneFillReasonStale {
		t.Errorf("eligibility_reason = %q (%v), want %q", code, err, autonomous.LaneFillReasonStale)
	}
	if _, has := decisions[1]["eligibility_reason"]; has {
		t.Errorf("a decision that is not a refusal must omit eligibility_reason: %v", decisions[1])
	}
}

// TestLaneFillReasonsEveryCodeSurvivesTheRoute checks all seven codes pass
// through the route unchanged.
func TestLaneFillReasonsEveryCodeSurvivesTheRoute(t *testing.T) {
	codes := []string{
		autonomous.LaneFillReasonStale, autonomous.LaneFillReasonHeld,
		autonomous.LaneFillReasonSequencedAfterUnshipped, autonomous.LaneFillReasonProposalNotConfirmed,
		autonomous.LaneFillReasonProposalMissing, autonomous.LaneFillReasonMainCarriesWork,
		autonomous.LaneFillReasonWorktreeInvalid,
	}
	spy := newLaneSpy()
	for _, c := range codes {
		spy.status.Decisions = append(spy.status.Decisions, autonomous.LaneFillDecision{
			LaneID: "gpu-1", Action: autonomous.LaneFillActionBlocked, PRDID: c, EligibilityReason: c,
		})
	}
	rec := getLaneFill(&Server{autonomousMgr: spy}, "")
	var body autonomous.LaneFillStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Decisions) != len(codes) {
		t.Fatalf("got %d decisions, want %d", len(body.Decisions), len(codes))
	}
	for _, d := range body.Decisions {
		if d.EligibilityReason != d.PRDID {
			t.Errorf("code %q came back as %q", d.PRDID, d.EligibilityReason)
		}
	}
}

// TestLaneFillReasonsCarryLaneCountsAndModes checks that the route returns the
// lane_counts and modes objects unchanged.
func TestLaneFillReasonsCarryLaneCountsAndModes(t *testing.T) {
	got := laneFillReasonsBody(t)
	var counts struct {
		GPU struct {
			Configured, Effective int
			Source                string
		} `json:"gpu"`
		Claude struct {
			Configured, Effective int
			Source                string
		} `json:"claude"`
	}
	if err := json.Unmarshal(got["lane_counts"], &counts); err != nil {
		t.Fatalf("lane_counts: %v %s", err, got["lane_counts"])
	}
	if counts.GPU.Configured != 2 || counts.GPU.Effective != 2 || counts.GPU.Source != "config" {
		t.Errorf("gpu count = %+v", counts.GPU)
	}
	if counts.Claude.Configured != 3 || counts.Claude.Effective != 1 || counts.Claude.Source != "llm_caps" {
		t.Errorf("claude count = %+v", counts.Claude)
	}
	var modes map[string]string
	if err := json.Unmarshal(got["modes"], &modes); err != nil {
		t.Fatalf("modes: %v %s", err, got["modes"])
	}
	if modes["gpu"] != "on" || modes["claude"] != "shadow" {
		t.Errorf("modes = %v, want gpu on and claude shadow", modes)
	}
}

// TestLaneFillReasonsOpenAPINames checks that both OpenAPI copies document the
// new status fields, the seven reason codes and the lane fill settings.
func TestLaneFillReasonsOpenAPINames(t *testing.T) {
	root := repoRootOf(t, 1)
	names := []string{
		"eligibility_reason", "absent when the decision is not a refusal", "lane_counts", "modes:",
		"stale", "held", "sequenced_after_unshipped", "proposal_not_confirmed", "proposal_missing",
		"main_carries_work", "worktree_invalid",
		"autonomous.lane_fill.stale_after_hours", "autonomous.lane_fill.require_confirmed_proposal",
		"autonomous.lane_fill.check_main_carries", "autonomous.lane_fill.require_worktree",
		"autonomous.lane_fill.lane_counts_from_caps", "autonomous.lane_fill.gpu_mode",
		"autonomous.lane_fill.claude_mode",
	}
	for _, rel := range verifierWaitOpenAPICopies {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for _, n := range names {
			if !strings.Contains(string(data), n) {
				t.Errorf("%s lacks %q", rel, n)
			}
		}
	}
}
