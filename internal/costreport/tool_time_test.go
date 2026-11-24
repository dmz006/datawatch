package costreport

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/spendledger"
)

// Tool time reaches the cost report row of its group, summed over sessions.
func TestCostReportRowSumsToolSeconds(t *testing.T) {
	r1 := makeRecord("s1", "a1", "auto1", "claude-code", fixedDay(time.October, 6, 10), 100, 200, false)
	r2 := makeRecord("s2", "a2", "auto1", "claude-code", fixedDay(time.October, 6, 11), 10, 20, false)
	r1.ToolSeconds, r2.ToolSeconds = 12.5, 7.5
	src := &fakeSource{result: &SourceResult{Records: []Record{r1, r2}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 1 || rep.Rows[0].ToolSeconds != 20 {
		t.Fatalf("rows %+v, want one row with tool_seconds 20", rep.Rows)
	}
	if len(rep.ModelRows) != 1 || rep.ModelRows[0].ToolSeconds != 20 {
		t.Fatalf("model rows %+v, want tool_seconds 20", rep.ModelRows)
	}
}

// Tool time reaches the tool usage series per bucket, group and in total.
func TestBuildToolUsageSumsToolSeconds(t *testing.T) {
	now := outcomesNow
	entries := []spendledger.Entry{
		{SessionID: "s1", AutomatonID: "a1", Backend: "claude-code", StartedAt: now.Add(-3 * time.Hour), ToolCalls: 4, ToolSeconds: 30},
		{SessionID: "s2", AutomatonID: "a1", Backend: "claude-code", StartedAt: now.Add(-2 * time.Hour), ToolCalls: 2, ToolSeconds: 12.5},
	}
	tu, err := BuildToolUsage(SeriesRequest{Window: 24 * time.Hour}, entries, SeriesOptions{Now: outcomesNow})
	if err != nil {
		t.Fatal(err)
	}
	g := toolGroup(t, tu, "a1")
	if g.ToolSeconds != 42.5 || tu.TotalToolSeconds != 42.5 {
		t.Fatalf("group %v total %v, want 42.5", g.ToolSeconds, tu.TotalToolSeconds)
	}
	var sum float64
	for _, p := range g.Points {
		sum += p.ToolSeconds
	}
	if sum != 42.5 {
		t.Fatalf("points sum %v, want 42.5", sum)
	}
	if tu.ToolNamesNote == "" {
		t.Fatal("the note must still say no per-tool-name breakdown exists")
	}
}

func TestChartsTextShowsToolTimeOnlyWhenRecorded(t *testing.T) {
	with := (&Charts{ToolUsage: &ToolUsage{
		Groups:         []ToolUsageGroup{{Key: "a1", ToolCalls: 6, Sessions: 2, ToolSeconds: 450}},
		TotalToolCalls: 6, TotalSessions: 2, TotalToolSeconds: 450,
	}}).Text()
	for _, want := range []string{"a1: 6 tool calls over 2 sessions, 7m30s in tools", "total: 6 tool calls over 2 sessions, 7m30s in tools"} {
		if !strings.Contains(with, want) {
			t.Errorf("text lacks %q:\n%s", want, with)
		}
	}
	without := (&Charts{ToolUsage: &ToolUsage{
		Groups:         []ToolUsageGroup{{Key: "a1", ToolCalls: 6, Sessions: 2}},
		TotalToolCalls: 6, TotalSessions: 2,
	}}).Text()
	if strings.Contains(without, "in tools") {
		t.Errorf("a line with no tool time must read as before:\n%s", without)
	}
}
