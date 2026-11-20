package main

import (
	"context"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

func scalingCfg(perThousand int) autonomouspkg.Config {
	return autonomouspkg.Config{JudgeTimeoutSeconds: 1800, JudgeSizeThresholdLines: 400, JudgeTimeoutPerThousandLinesSeconds: perThousand}
}

func TestJudgeLimitScalesWithLines(t *testing.T) {
	base := 1800 * time.Second
	cfg := scalingCfg(600)
	for _, lines := range []int{0, 50, 400} {
		if got := judgeAttemptLimit(cfg, lines); got != base {
			t.Errorf("%d lines: limit %s, want base %s", lines, got, base)
		}
	}
	l1 := judgeAttemptLimit(cfg, 1000)
	l2 := judgeAttemptLimit(cfg, 1556)
	if !(l1 > base && l2 > l1) {
		t.Errorf("limit must grow with lines: base=%s 1000=%s 1556=%s", base, l1, l2)
	}
	if want := base + 600*1556/1000*time.Second; l2 != want {
		t.Errorf("1556 lines: %s, want %s", l2, want)
	}
	if got := judgeAttemptLimit(cfg, 1_000_000); got != 4*base {
		t.Errorf("cap: %s, want %s", got, 4*base)
	}
	if got := judgeAttemptLimit(scalingCfg(0), 5000); got != base {
		t.Errorf("zero setting: %s, want base", got)
	}
	off := cfg
	off.JudgeSizeThresholdLines = 0
	if got := judgeAttemptLimit(off, 5000); got != base {
		t.Errorf("threshold off: %s, want base", got)
	}
}

func TestJudgeSplitLargeDiffRunsPerGroup(t *testing.T) {
	cfg := scalingCfg(600)
	files := []autonomouspkg.JudgeFileLines{
		{Path: "cmd/a.go", Lines: 300}, {Path: "internal/x/big.go", Lines: 900},
		{Path: "internal/y/y.go", Lines: 200}, {Path: "docs/d.md", Lines: 156},
	}
	groups := autonomouspkg.SplitJudgeFiles(files, 400)
	if len(groups) < 3 {
		t.Fatalf("groups = %+v", groups)
	}
	req := autonomouspkg.GuardrailInvocation{PRDID: "p1", UnitID: "p1", Guardrail: "rules-review", UnitSpec: "spec"}

	var calls []string
	limits := map[string]time.Duration{}
	v, err := runJudgeSplit(req, groups, cfg, func(gr autonomouspkg.GuardrailInvocation, g autonomouspkg.JudgeGroup, limit time.Duration) (autonomouspkg.GuardrailVerdict, error) {
		calls = append(calls, g.Name)
		limits[g.Name] = limit
		for _, f := range g.Files {
			if !strings.Contains(gr.UnitSpec, f) {
				t.Errorf("group %s prompt lacks its file %s", g.Name, f)
			}
		}
		for _, other := range groups {
			if other.Name == g.Name {
				continue
			}
			for _, f := range other.Files {
				if strings.Contains(gr.UnitSpec, f) {
					t.Errorf("group %s prompt names %s of group %s", g.Name, f, other.Name)
				}
			}
		}
		if g.Name == "internal/x/big.go" {
			return autonomouspkg.GuardrailVerdict{Outcome: "block", Severity: "high", Summary: "bad", Issues: []string{"broken rule"}}, nil
		}
		return autonomouspkg.GuardrailVerdict{Outcome: "pass", Summary: "ok"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != len(groups) {
		t.Fatalf("judge called %d times for %d groups", len(calls), len(groups))
	}
	for _, g := range groups {
		if limits[g.Name] != judgeAttemptLimit(cfg, g.Lines) {
			t.Errorf("group %s limit %s, want %s", g.Name, limits[g.Name], judgeAttemptLimit(cfg, g.Lines))
		}
	}
	if limits["internal/x/big.go"] <= 1800*time.Second {
		t.Errorf("oversized group limit %s must be scaled", limits["internal/x/big.go"])
	}
	if v.Outcome != "block" || len(v.Issues) != 1 || v.Issues[0] != "[internal/x/big.go] broken rule" {
		t.Errorf("merged verdict = %+v", v)
	}

	// A group the judge never answers on yields a timeout hold naming it.
	slow := groups[1].Name
	v, err = runJudgeSplit(req, groups, cfg, func(gr autonomouspkg.GuardrailInvocation, g autonomouspkg.JudgeGroup, limit time.Duration) (autonomouspkg.GuardrailVerdict, error) {
		if g.Name == slow {
			att := []judgeAttempt{{Backend: "fake", Limit: limit, Elapsed: limit, TimedOut: true}}
			return judgeHoldVerdict(gr, att), nil
		}
		return autonomouspkg.GuardrailVerdict{Outcome: "pass", Summary: "ok"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !autonomouspkg.IsJudgeTimeoutHold(v) || !strings.Contains(v.Summary, slow) {
		t.Errorf("timeout merge = %+v", v)
	}

	// An infra failure is handed back so the infra retry can run the judge again.
	v, _ = runJudgeSplit(req, groups, cfg, func(autonomouspkg.GuardrailInvocation, autonomouspkg.JudgeGroup, time.Duration) (autonomouspkg.GuardrailVerdict, error) {
		return autonomouspkg.GuardrailVerdict{Outcome: "block", Category: "infra", Summary: "no slot"}, nil
	})
	if v.Category != "infra" {
		t.Errorf("infra verdict lost: %+v", v)
	}
}

func TestJudgeSplitGroupsFor(t *testing.T) {
	old := judgeDiffFiles
	defer func() { judgeDiffFiles = old }()
	judgeDiffFiles = func(context.Context, *autonomouspkg.PRD, string) ([]autonomouspkg.JudgeFileLines, error) {
		return []autonomouspkg.JudgeFileLines{{Path: "a/x.go", Lines: 500}, {Path: "b/y.go", Lines: 500}}, nil
	}
	find := func(string) (*autonomouspkg.PRD, bool) { return &autonomouspkg.PRD{ID: "p"}, true }
	req := autonomouspkg.GuardrailInvocation{PRDID: "p", Guardrail: "rules-review"}
	if g := judgeSplitGroupsFor(context.Background(), judgeSize{Threshold: 400, Lines: 1000}, req, find); len(g) != 2 {
		t.Errorf("above threshold: %d groups, want 2", len(g))
	}
	if g := judgeSplitGroupsFor(context.Background(), judgeSize{Threshold: 400, Lines: 400}, req, find); g != nil {
		t.Errorf("at threshold must not split: %v", g)
	}
	if g := judgeSplitGroupsFor(context.Background(), judgeSize{Threshold: 0, Lines: 5000}, req, find); g != nil {
		t.Errorf("size rule off must not split: %v", g)
	}
	other := req
	other.Guardrail = "security"
	if g := judgeSplitGroupsFor(context.Background(), judgeSize{Threshold: 400, Lines: 1000}, other, find); g != nil {
		t.Errorf("other guardrail must not split: %v", g)
	}
}
