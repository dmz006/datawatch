package autonomous

import (
	"strings"
	"testing"
	"time"
)

// laneFillTextReasonsStatus is a status with a refusal decision, a decision
// that is not a refusal, lane counts from both sources and per-kind modes.
func laneFillTextReasonsStatus() LaneFillStatus {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return LaneFillStatus{
		Mode:  "shadow",
		Modes: LaneFillKindModes{GPU: "shadow", Claude: "shadow"},
		Lanes: []LaneFillLaneStatus{
			{ID: "gpu-1", Type: LaneTypeGPU, State: "idle"},
		},
		LaneCounts: LaneFillLaneCounts{
			GPU:    LaneFillLaneCount{Configured: 2, Effective: 2, Source: LaneCountSourceConfig},
			Claude: LaneFillLaneCount{Configured: 3, Effective: 1, Source: LaneCountSourceCaps},
		},
		Decisions: []LaneFillDecision{
			{
				At: at, LaneID: "gpu-1", Action: LaneFillActionBlocked, PRDID: "p1",
				Reason: "queued longer than 72 hours", Blocker: "queued longer than 72 hours",
				EligibilityReason: LaneFillReasonStale,
			},
			{
				At: at, LaneID: "gpu-1", Action: LaneFillActionIdle,
				Reason: "no eligible work",
			},
		},
	}
}

// TestLaneFillTextReasonCodeComesBeforeTheReasonText checks that a refusal
// shows its code in brackets in front of the reason text.
func TestLaneFillTextReasonCodeComesBeforeTheReasonText(t *testing.T) {
	line := laneFillDecisionText(laneFillTextReasonsStatus().Decisions[0])
	want := "[stale] queued longer than 72 hours"
	if !strings.Contains(line, want) {
		t.Fatalf("decision line lacks %q:\n%s", want, line)
	}
	if strings.Index(line, "[stale]") > strings.Index(line, "queued longer") {
		t.Errorf("the code must come before the reason text:\n%s", line)
	}
}

// TestLaneFillTextReasonCodeAbsentOnNonRefusal checks that a decision that is
// not a refusal shows no bracketed code.
func TestLaneFillTextReasonCodeAbsentOnNonRefusal(t *testing.T) {
	line := laneFillDecisionText(laneFillTextReasonsStatus().Decisions[1])
	if strings.Contains(line, ": [") {
		t.Errorf("a non-refusal decision must show no reason code:\n%s", line)
	}
	if !strings.Contains(line, ": no eligible work") {
		t.Errorf("the reason text is missing:\n%s", line)
	}
}

// TestLaneFillTextReasonCodeWithoutReasonText keeps the code when a refusal
// has no reason text.
func TestLaneFillTextReasonCodeWithoutReasonText(t *testing.T) {
	d := LaneFillDecision{LaneID: "gpu-1", Action: LaneFillActionBlocked, EligibilityReason: LaneFillReasonHeld}
	if line := laneFillDecisionText(d); !strings.Contains(line, ": [held]") {
		t.Errorf("the code is missing:\n%s", line)
	}
}

// TestLaneFillTextEveryReasonCodeShows checks that all seven codes render.
func TestLaneFillTextEveryReasonCodeShows(t *testing.T) {
	codes := []string{
		LaneFillReasonStale, LaneFillReasonHeld, LaneFillReasonSequencedAfterUnshipped,
		LaneFillReasonProposalNotConfirmed, LaneFillReasonProposalMissing,
		LaneFillReasonMainCarriesWork, LaneFillReasonWorktreeInvalid,
	}
	for _, c := range codes {
		d := LaneFillDecision{LaneID: "gpu-1", Action: LaneFillActionBlocked, Reason: "r", EligibilityReason: c}
		if line := laneFillDecisionText(d); !strings.Contains(line, "["+c+"] r") {
			t.Errorf("code %s not shown:\n%s", c, line)
		}
	}
}

// TestLaneFillTextLaneCountsLine checks the lane counts line: effective,
// configured and source for each lane kind, under the lane lines.
func TestLaneFillTextLaneCountsLine(t *testing.T) {
	out := RenderLaneFillText(laneFillTextReasonsStatus(), 0)
	want := "Lane counts: gpu 2 of 2 (config), claude 1 of 3 (LLM caps)"
	if !strings.Contains(out, want) {
		t.Fatalf("missing %q:\n%s", want, out)
	}
	lane, counts, queued := strings.Index(out, "gpu-1:"), strings.Index(out, "Lane counts:"), strings.Index(out, "Queued eligible:")
	if lane >= counts || counts >= queued {
		t.Errorf("the lane counts line must sit under the lane lines:\n%s", out)
	}
}

// TestLaneFillTextNoLaneCountsLineFromAnOlderDaemon checks that a status
// without lane counts adds no line and no zeroed counts.
func TestLaneFillTextNoLaneCountsLineFromAnOlderDaemon(t *testing.T) {
	st := laneFillTextReasonsStatus()
	st.LaneCounts = LaneFillLaneCounts{}
	if out := RenderLaneFillText(st, 0); strings.Contains(out, "Lane counts:") {
		t.Errorf("no lane counts line expected:\n%s", out)
	}
}

// TestLaneFillTextModesLineOnlyWhenTheyDiffer checks the per-kind modes line.
func TestLaneFillTextModesLineOnlyWhenTheyDiffer(t *testing.T) {
	st := laneFillTextReasonsStatus()
	if out := RenderLaneFillText(st, 0); strings.Contains(out, "Mode per lane kind") {
		t.Errorf("modes equal to the base mode must add no line:\n%s", out)
	}
	st.Modes = LaneFillKindModes{GPU: "on", Claude: "shadow"}
	out := RenderLaneFillText(st, 0)
	if !strings.Contains(out, "Mode per lane kind: gpu on, claude shadow") {
		t.Errorf("missing the per-kind modes line:\n%s", out)
	}
}
