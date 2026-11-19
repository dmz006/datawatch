package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

type fakeLandingGit struct {
	merged bool
	detail string
	calls  int
}

func (f *fakeLandingGit) BranchMerged(_ context.Context, _, _ string) (bool, string) {
	f.calls++
	return f.merged, f.detail
}

func TestCompletionLandingTable(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		merged  bool
		judge   string
		prd     PRD
		commit  string
		allowed bool
		want    string
	}{
		{"pass merged", true, true, "pass", PRD{Git: GitWorkflow{Branch: "b"}}, "", true, "merged"},
		{"warn merged", true, true, "warn", PRD{Git: GitWorkflow{Branch: "b"}}, "", true, "merged"},
		{"warn unmerged refused", true, false, "warn", PRD{Git: GitWorkflow{Branch: "b"}}, "", false, "judge returned a warning"},
		{"pass unmerged refused", true, false, "pass", PRD{Git: GitWorkflow{Branch: "b"}}, "", false, "not merged"},
		{"fail merged refused", true, true, "fail", PRD{Git: GitWorkflow{Branch: "b"}}, "", false, "judge verdict is fail"},
		{"block merged refused", true, true, "block", PRD{Git: GitWorkflow{Branch: "b"}}, "", false, "judge verdict is block"},
		{"warn no-code reason", true, false, "warn", PRD{Git: GitWorkflow{Branch: "b"}, NoCodeReason: "research only"}, "", true, "no-code"},
		{"warn shipped commit", true, false, "warn", PRD{Git: GitWorkflow{Branch: "b"}, MergeCommit: "abc"}, "", true, "shipped"},
		{"post-ship commit", true, false, "pass", PRD{Git: GitWorkflow{Branch: "b"}}, "abc", true, "supplied"},
		{"on main", true, false, "warn", PRD{Git: GitWorkflow{Branch: "main"}}, "", true, "default branch"},
		{"no branch", true, false, "warn", PRD{}, "", true, "nothing to land"},
		{"landing kept", true, false, "pass", PRD{Git: GitWorkflow{Branch: "b"}, Landing: &Landing{State: LandingKept}}, "", true, "kept"},
		{"landing pending", true, false, "pass", PRD{Git: GitWorkflow{Branch: "b"}, Landing: &Landing{State: LandingPending}}, "", false, "not merged"},
		{"disabled", false, false, "warn", PRD{Git: GitWorkflow{Branch: "b"}}, "", true, "off"},
		{"git detail shown", true, false, "pass", PRD{Git: GitWorkflow{Branch: "b"}}, "", false, "boom"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := &fakeLandingGit{merged: c.merged, detail: "boom"}
			p := c.prd
			d := EvaluateCompletionLanding(context.Background(), c.enabled, g, "/repo", &p, c.judge, c.commit)
			if d.Allowed != c.allowed {
				t.Fatalf("allowed=%v want %v (%s)", d.Allowed, c.allowed, d.Reason)
			}
			if !strings.Contains(d.Reason, c.want) {
				t.Fatalf("reason %q lacks %q", d.Reason, c.want)
			}
		})
	}
	if d := EvaluateCompletionLanding(context.Background(), true, nil, "", nil, "pass", ""); d.Allowed {
		t.Fatal("nil record must be refused")
	}
	p := PRD{Git: GitWorkflow{Branch: "b"}}
	if d := EvaluateCompletionLanding(context.Background(), true, nil, "", &p, "pass", ""); d.Allowed || !strings.Contains(d.Reason, "no ancestry check") {
		t.Fatalf("nil git: %+v", d)
	}
	g := &fakeLandingGit{}
	EvaluateCompletionLanding(context.Background(), true, g, "", &PRD{Git: GitWorkflow{Branch: "b"}}, "fail", "")
	if g.calls != 0 {
		t.Fatal("a failed judge must not reach git")
	}
}

func TestCompletionLandingManagerRecordsRefusal(t *testing.T) {
	m, _ := NewManager(t.TempDir(), Config{}, nil)
	g := &fakeLandingGit{detail: "not an ancestor"}
	m.SetCompletionLandingGit(g)
	p := &PRD{ID: "p1", Status: PRDRunning, Git: GitWorkflow{Branch: "automaton/x"},
		Verdicts: []GuardrailVerdict{{Outcome: "warn", Severity: "low"}}}
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	ok, why := m.checkCompletionLanding(context.Background(), p, CompletionPathRollup, "", "")
	if ok || p.Status != PRDRunning || p.CompletionRefusal != why {
		t.Fatalf("ok=%v status=%s refusal=%q", ok, p.Status, p.CompletionRefusal)
	}
	if lastDecision(p, DecisionCompletionRefused) == nil {
		t.Fatal("no refusal decision")
	}
	n := len(p.Decisions)
	m.checkCompletionLanding(context.Background(), p, CompletionPathRollup, "", "")
	if len(p.Decisions) != n {
		t.Fatal("same refusal must not append twice")
	}
	g.merged = true
	if ok, _ := m.checkCompletionLanding(context.Background(), p, CompletionPathRollup, "", ""); !ok || p.CompletionRefusal != "" {
		t.Fatal("merged branch must pass and clear the refusal")
	}
}

func TestCompletionLandingMarkPRDCompletedAndRejudgePaths(t *testing.T) {
	m, _ := NewManager(t.TempDir(), Config{}, nil)
	m.SetCompletionLandingGit(&fakeLandingGit{})
	p := &PRD{ID: "p2", Status: PRDBlocked, Git: GitWorkflow{Branch: "automaton/y"}}
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	// The post-ship path with a commit is allowed.
	if _, err := m.MarkPRDCompleted("p2", "", time.Now(), "abc123", "op", "r"); err != nil {
		t.Fatal(err)
	}
	// Without a commit and with no merge, it is refused and the status stays.
	q := &PRD{ID: "p3", Status: PRDBlocked, Git: GitWorkflow{Branch: "automaton/z"}}
	if err := m.store.SavePRD(q); err != nil {
		t.Fatal(err)
	}
	if _, err := m.MarkPRDCompleted("p3", "", time.Now(), "", "op", "r"); err == nil {
		t.Fatal("unmerged post-ship completion must be refused")
	}
	if got, _ := m.store.GetPRD("p3"); got.Status != PRDBlocked {
		t.Fatalf("status changed to %s", got.Status)
	}
	// Disabled gate lets it through.
	f := false
	m.SetCompletionLandingConfig(config.CompletionLandingConfig{Enabled: &f})
	if _, err := m.MarkPRDCompleted("p3", "", time.Now(), "", "op", "r"); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionLandingArchiveUsesRecordedEvidence(t *testing.T) {
	if recordedLandingEvidence(&PRD{Git: GitWorkflow{Branch: "b"}}) != "" {
		t.Fatal("unmerged branch has no recorded evidence")
	}
	if recordedLandingEvidence(&PRD{Git: GitWorkflow{Branch: "b"}, NoCodeReason: "docs"}) == "" {
		t.Fatal("no-code reason is evidence")
	}
}

func TestCompletionLandingLatestJudgeOutcome(t *testing.T) {
	if latestJudgeOutcome(nil) != "pass" || latestJudgeOutcome(&PRD{}) != "pass" {
		t.Fatal("no verdict reads pass")
	}
	warn := &PRD{Verdicts: []GuardrailVerdict{{Outcome: "block"}, {Outcome: "warn"}}}
	if latestJudgeOutcome(warn) != "warn" {
		t.Fatal("newest warn reads warn")
	}
	stale := &PRD{Verdicts: []GuardrailVerdict{{Outcome: "warn"}, {Outcome: "block"}}}
	if latestJudgeOutcome(stale) != "pass" {
		t.Fatal("a stale block is not a verdict on the current work")
	}
}
