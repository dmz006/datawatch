package autonomous

import (
	"strings"
	"testing"
	"time"
)

func fakeJudgeFiles() []JudgeFileLines {
	// 1556 changed lines across several directories, plus a lock file.
	return []JudgeFileLines{
		{"cmd/app/main.go", 300},
		{"cmd/app/flags.go", 120},
		{"internal/config/config.go", 450},
		{"internal/config/judge.go", 90},
		{"internal/server/api.go", 200},
		{"internal/server/web/app.js", 80},
		{"docs/guide.md", 40},
		{"docs/ref.md", 276},
		{"go.sum", 5000},
	}
}

func TestJudgeSplitLargeDiff(t *testing.T) {
	files := fakeJudgeFiles()
	const threshold = 400
	groups := SplitJudgeFiles(files, threshold)
	if len(groups) < 2 {
		t.Fatalf("1556 lines split into %d groups", len(groups))
	}
	seen := map[string]int{}
	total := 0
	for _, g := range groups {
		if g.Lines > threshold && len(g.Files) > 1 {
			t.Errorf("group %q has %d lines over the threshold with %d files", g.Name, g.Lines, len(g.Files))
		}
		for _, f := range g.Files {
			seen[f]++
		}
		total += g.Lines
	}
	for _, f := range files {
		if f.Path == "go.sum" {
			if seen[f.Path] != 0 {
				t.Errorf("lock file %s must be left out", f.Path)
			}
			continue
		}
		if seen[f.Path] != 1 {
			t.Errorf("file %s in %d groups, want exactly 1", f.Path, seen[f.Path])
		}
	}
	if total != 1556 {
		t.Errorf("group lines total %d, want 1556", total)
	}
	// A file larger than the threshold is its own group.
	for _, g := range groups {
		for _, f := range g.Files {
			if f == "internal/config/config.go" && len(g.Files) != 1 {
				t.Errorf("oversized file shares group %q", g.Name)
			}
		}
	}
	// Deterministic.
	again := SplitJudgeFiles(files, threshold)
	for i := range groups {
		if groups[i].Name != again[i].Name || len(groups[i].Files) != len(again[i].Files) {
			t.Fatal("split is not deterministic")
		}
	}
	// A diff within the threshold is one group.
	one := SplitJudgeFiles([]JudgeFileLines{{"a/x.go", 100}, {"b/y.go", 200}}, threshold)
	if len(one) != 1 || len(one[0].Files) != 2 {
		t.Fatalf("small diff groups = %+v", one)
	}
	if SplitJudgeFiles(nil, threshold) != nil {
		t.Error("no files must give no groups")
	}
}

func TestJudgeSplitParseNumstat(t *testing.T) {
	fs, err := ParseJudgeNumstatFiles("10\t5\ta/b.go\n-\t-\timg.png\n3\t1\tx/{old => new}/f.go\n")
	if err != nil || len(fs) != 3 || fs[0].Lines != 15 || fs[1].Lines != 0 || fs[2].Path != "x/new/f.go" {
		t.Fatalf("got %+v err=%v", fs, err)
	}
}

func TestJudgeSplitMergeVerdicts(t *testing.T) {
	at := time.Now()
	pass := GuardrailVerdict{Outcome: "pass", Summary: "ok"}
	block := GuardrailVerdict{Outcome: "block", Severity: "high", Summary: "bad", Issues: []string{"rule one", "rule two"}}
	hold := GuardrailVerdict{Outcome: "block", Category: JudgeTimeoutCategory, Summary: "timed out", Issues: []string{"claude: timed out"}}

	m := MergeJudgeVerdicts("rules-review", []JudgeGroupResult{{Group: "a", Verdict: pass}, {Group: "b", Verdict: pass}}, at)
	if m.Outcome != "pass" || m.Guardrail != "rules-review" {
		t.Errorf("all pass = %+v", m)
	}

	m = MergeJudgeVerdicts("rules-review", []JudgeGroupResult{{Group: "a", Verdict: pass}, {Group: "b", Verdict: block}, {Group: "c", Verdict: block}}, at)
	if m.Outcome != "block" || m.Category == JudgeTimeoutCategory || len(m.Issues) != 4 || !strings.HasPrefix(m.Issues[0], "[b] ") || !strings.HasPrefix(m.Issues[2], "[c] ") {
		t.Errorf("block merge = %+v", m)
	}

	m = MergeJudgeVerdicts("rules-review", []JudgeGroupResult{{Group: "a", Verdict: pass}, {Group: "slow", Verdict: hold, TimedOut: true}}, at)
	if !IsJudgeTimeoutHold(m) || !strings.Contains(m.Summary, "slow") {
		t.Errorf("timeout merge = %+v", m)
	}

	// A judged problem is never hidden by another group timing out.
	m = MergeJudgeVerdicts("rules-review", []JudgeGroupResult{{Group: "slow", Verdict: hold, TimedOut: true}, {Group: "b", Verdict: block}}, at)
	if m.Outcome != "block" || IsJudgeTimeoutHold(m) {
		t.Fatalf("block hidden by timeout: %+v", m)
	}
	joined := strings.Join(m.Issues, "\n")
	if !strings.Contains(joined, "[b] rule one") || !strings.Contains(joined, "[slow]") {
		t.Errorf("issues = %v", m.Issues)
	}
}
