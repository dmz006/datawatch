package autonomous

import (
	"strings"
	"testing"
)

func TestLaneFillEligibHoldReason(t *testing.T) {
	lookup := func(table map[string]PRDStatus) PRDRefLookup {
		return func(ref string) (PRDStatus, bool) {
			s, ok := table[ref]
			return s, ok
		}
	}
	known := lookup(map[string]PRDStatus{
		"aaaa1111":         PRDCompleted,
		"bbbb2222":         PRDRunning,
		"lane fill config": PRDCompleted,
		"capacity card":    PRDApproved,
	})

	cases := []struct {
		name    string
		title   string
		spec    string
		lookup  PRDRefLookup
		wantHit bool
		wantSub string
	}{
		{name: "plain spec", spec: "Add a config block.\nWrite tests.", lookup: known},
		{name: "empty", lookup: known},
		{name: "hold line", spec: "HOLD until the operator decides\nrest", lookup: known, wantHit: true, wantSub: "HOLD"},
		{name: "hold line lower case", spec: "intro\nhold: waiting", lookup: known, wantHit: true, wantSub: "HOLD"},
		{name: "hold line indented bullet", spec: "- Hold for review", lookup: known, wantHit: true, wantSub: "HOLD"},
		{name: "hold in title", title: "HOLD: lane fill", spec: "body", lookup: known, wantHit: true, wantSub: "HOLD"},
		{name: "hold mid line is not a hold line", spec: "Do not withhold the result and do hold hands", lookup: known},
		{name: "holder is not hold", spec: "Holder of the lease", lookup: known},
		{name: "do not run", spec: "Planned work. Do Not Run this yet.", lookup: known, wantHit: true, wantSub: "do not run"},
		{name: "do not run split by newline", spec: "do not\nrun", lookup: known, wantHit: true, wantSub: "do not run"},
		{name: "sequenced last", spec: "This Automaton is Sequenced Last.", lookup: known, wantHit: true, wantSub: "sequenced last"},
		{name: "sequenced after completed id", spec: "Sequenced after aaaa1111.", lookup: known},
		{name: "sequenced after completed id upper case", spec: "sequenced after AAAA1111", lookup: known},
		{name: "sequenced after unshipped id", spec: "Sequenced after bbbb2222.", lookup: known, wantHit: true, wantSub: "bbbb2222"},
		{name: "sequenced after unknown id", spec: "sequenced after cccc3333", lookup: known, wantHit: true, wantSub: "cccc3333"},
		{name: "sequenced after completed title", spec: "sequenced after the lane fill config", lookup: known},
		{name: "sequenced after quoted completed title", spec: `sequenced after "lane fill config"`, lookup: known},
		{name: "sequenced after unshipped title", spec: "sequenced after the capacity card, then go", lookup: known, wantHit: true, wantSub: "capacity card"},
		{name: "sequenced after unknown title", spec: "sequenced after the mystery work", lookup: known, wantHit: true, wantSub: "mystery work"},
		{name: "second ref fails", spec: "sequenced after aaaa1111\nsequenced after bbbb2222", lookup: known, wantHit: true, wantSub: "bbbb2222"},
		{name: "nil lookup resolves nothing", spec: "sequenced after aaaa1111", lookup: nil, wantHit: true, wantSub: "aaaa1111"},
		{name: "spec text is data", spec: "$(rm -rf /); `touch x`; sequenced after aaaa1111", lookup: known},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PRDHoldReason(&PRD{ID: "11112222", Title: tc.title, Spec: tc.spec}, tc.lookup)
			if (got != "") != tc.wantHit {
				t.Fatalf("PRDHoldReason = %q, want hit=%v", got, tc.wantHit)
			}
			if tc.wantSub != "" && !strings.Contains(strings.ToLower(got), strings.ToLower(tc.wantSub)) {
				t.Fatalf("PRDHoldReason = %q, want it to contain %q", got, tc.wantSub)
			}
		})
	}

	if got := PRDHoldReason(nil, known); got != "" {
		t.Fatalf("nil PRD gave %q", got)
	}
}

func TestLaneFillEligibClassifySource(t *testing.T) {
	cases := []struct {
		name       string
		cand       LaneFillCandidate
		wantSrc    LaneFillSource
		wantReason string
	}{
		{name: "approved", cand: LaneFillCandidate{ID: "a1", Status: PRDApproved}, wantSrc: LaneFillSourceReady},
		{name: "needs review", cand: LaneFillCandidate{ID: "a2", Status: PRDNeedsReview}, wantSrc: LaneFillSourceReady},
		{name: "draft", cand: LaneFillCandidate{ID: "a3", Status: PRDDraft}, wantSrc: LaneFillSourceDraft},
		{name: "running", cand: LaneFillCandidate{ID: "a4", Status: PRDRunning}, wantSrc: LaneFillSourceNone},
		{name: "completed", cand: LaneFillCandidate{ID: "a5", Status: PRDCompleted}, wantSrc: LaneFillSourceNone},
		{name: "cancelled", cand: LaneFillCandidate{ID: "a6", Status: PRDCancelled}, wantSrc: LaneFillSourceNone},
		{name: "merge flag", cand: LaneFillCandidate{ID: "a7", Status: PRDCompleted, AllTasksDone: true, NeedsMergeFlag: true}, wantSrc: LaneFillSourceMerge},
		{name: "tasks done without merge flag", cand: LaneFillCandidate{ID: "a8", Status: PRDApproved, AllTasksDone: true}, wantSrc: LaneFillSourceReady},
		{name: "merge flag without all tasks done", cand: LaneFillCandidate{ID: "a9", Status: PRDApproved, NeedsMergeFlag: true}, wantSrc: LaneFillSourceReady},
		{name: "held approved", cand: LaneFillCandidate{ID: "b1", Status: PRDApproved, HoldReason: "a line starts with HOLD"}, wantSrc: LaneFillSourceNone, wantReason: "held"},
		{name: "held draft", cand: LaneFillCandidate{ID: "b2", Status: PRDDraft, HoldReason: "do not run"}, wantSrc: LaneFillSourceNone, wantReason: "held"},
		{name: "automation member approved", cand: LaneFillCandidate{ID: "b3", Status: PRDApproved, InAutomation: true}, wantSrc: LaneFillSourceNone, wantReason: "Automation"},
		{name: "automation member draft", cand: LaneFillCandidate{ID: "b4", Status: PRDDraft, InAutomation: true}, wantSrc: LaneFillSourceNone, wantReason: "Automation"},
		{name: "legacy decomposing status is not queued", cand: LaneFillCandidate{ID: "b5", Status: "decomposing"}, wantSrc: LaneFillSourceNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, reason := ClassifyLaneFillCandidate(tc.cand)
			if src != tc.wantSrc {
				t.Fatalf("source = %d, want %d", src, tc.wantSrc)
			}
			if tc.wantReason == "" && reason != "" {
				t.Fatalf("unexpected exclusion reason %q", reason)
			}
			if tc.wantReason != "" && !strings.Contains(reason, tc.wantReason) {
				t.Fatalf("reason = %q, want it to contain %q", reason, tc.wantReason)
			}
			if tc.wantReason != "" && !strings.Contains(reason, tc.cand.ID) {
				t.Fatalf("reason = %q, want it to name %s", reason, tc.cand.ID)
			}
		})
	}
}

func TestLaneFillEligibQueuedCounts(t *testing.T) {
	cands := []LaneFillCandidate{
		{ID: "g1", Status: PRDApproved, LanePreference: "gpu"},
		{ID: "g2", Status: PRDNeedsReview, LanePreference: "gpu"},
		{ID: "c1", Status: PRDApproved, LanePreference: "claude"},
		{ID: "n1", Status: PRDApproved},
		{ID: "n2", Status: PRDApproved, LanePreference: "any"},
		{ID: "held", Status: PRDApproved, HoldReason: "x"},
		{ID: "chained", Status: PRDApproved, InAutomation: true},
		{ID: "draft", Status: PRDDraft},
		{ID: "running", Status: PRDRunning},
		{ID: "merge", Status: PRDCompleted, AllTasksDone: true, NeedsMergeFlag: true},
	}
	got := QueuedEligible(cands)
	if got[LaneTypeGPU] != 4 || got[LaneTypeClaude] != 3 {
		t.Fatalf("QueuedEligible = %v, want gpu 4 (2 gpu + 2 any) and claude 3 (1 claude + 2 any)", got)
	}
	empty := QueuedEligible(nil)
	if empty[LaneTypeGPU] != 0 || empty[LaneTypeClaude] != 0 {
		t.Fatalf("QueuedEligible(nil) = %v", empty)
	}
}

func TestLaneFillEligibNormalizedPreference(t *testing.T) {
	cases := []struct {
		pref string
		lane LaneType
		want bool
	}{
		{"", LaneTypeGPU, true},
		{"", LaneTypeClaude, true},
		{"any", LaneTypeGPU, true},
		{"GPU", LaneTypeGPU, true},
		{"gpu", LaneTypeClaude, false},
		{"claude", LaneTypeGPU, false},
		{" claude ", LaneTypeClaude, true},
		{"bogus", LaneTypeGPU, false},
	}
	for _, tc := range cases {
		if got := preferenceFitsLane(tc.pref, tc.lane); got != tc.want {
			t.Errorf("preferenceFitsLane(%q, %s) = %v, want %v", tc.pref, tc.lane, got, tc.want)
		}
	}
}
