package costreport

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/spendledger"
)

func localEntries(now time.Time) []spendledger.Entry {
	at := now.Add(-time.Hour)
	return []spendledger.Entry{
		{SessionID: "c1", AutomatonID: "a", Backend: "claude-code", StartedAt: at,
			TokensIn: 1000, TokensOut: 2000, ToolCalls: 5, ActiveSeconds: 60},
		{SessionID: "l1", AutomatonID: "a", Backend: "pi", StartedAt: at, Local: true, TokensReported: true,
			LocalTokensIn: 300, LocalTokensOut: 200, ToolCalls: 7, ActiveSeconds: 30},
		{SessionID: "l2", AutomatonID: "a", Backend: "pi", StartedAt: at, Local: true, TokensReported: false,
			ToolCalls: 0, ActiveSeconds: 45},
	}
}

func TestLocalSeriesTokensOwnUnitNeverSummedNeverPriced(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	s, err := BuildSeries(SeriesRequest{Window: 24 * time.Hour}, localEntries(now), SeriesOptions{
		Now: now, RateFor: testRate,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Groups) != 1 {
		t.Fatalf("groups = %+v", s.Groups)
	}
	g := s.Groups[0]
	if g.TokensIn != 1000 || g.TokensOut != 2000 || g.Tokens != 3000 {
		t.Errorf("Claude totals changed: %+v", g)
	}
	if g.LocalTokens != 500 || s.LocalTokens != 500 {
		t.Errorf("local tokens = %d / %d, want 500", g.LocalTokens, s.LocalTokens)
	}
	if g.TokensNotReportedSessions != 1 || s.TokensNotReportedSessions != 1 {
		t.Errorf("not reported = %d / %d, want 1", g.TokensNotReportedSessions, s.TokensNotReportedSessions)
	}
	if g.ActiveSeconds != 135 {
		t.Errorf("active seconds = %v, want 135", g.ActiveSeconds)
	}
}

func TestLocalReportLocalTokensZeroUSDClaudeUnchanged(t *testing.T) {
	at := fixedDay(time.October, 6, 10)
	claude := makeRecord("c1", "x", "a", "claude-code", at, 1000, 2000, false)
	base := buildUnits(t, []Record{claude})
	rep := buildUnits(t, []Record{
		claude,
		{SessionID: "l1", AutomatonID: "a", Backend: "ollama", At: at, Local: true, TokensReported: true,
			LocalTokensIn: 300, LocalTokensOut: 200, ActiveSeconds: 30},
		{SessionID: "l2", AutomatonID: "a", Backend: "ollama", At: at, Local: true, ActiveSeconds: 45},
	})
	if len(rep.Rows) != 1 {
		t.Fatalf("rows = %+v", rep.Rows)
	}
	r, b := rep.Rows[0], base.Rows[0]
	if r.TokensIn != b.TokensIn || r.TokensOut != b.TokensOut {
		t.Errorf("Claude tokens changed: %+v vs %+v", r, b)
	}
	pr, pb := unitOf(rep, UnitPoints), unitOf(base, UnitPoints)
	if pr == nil || pb == nil || !samePtr(pr.Points, pb.Points) || pr.TokensIn != pb.TokensIn || pr.Sessions != pb.Sessions {
		t.Errorf("Claude unit changed by local: %+v vs %+v", pr, pb)
	}
	if r.LocalTokens != 500 || rep.Totals.LocalTokens != 500 {
		t.Errorf("local tokens = %d / %d", r.LocalTokens, rep.Totals.LocalTokens)
	}
	if r.TokensNotReportedSessions != 1 || rep.Totals.TokensNotReportedSessions != 1 {
		t.Errorf("not reported = %d", r.TokensNotReportedSessions)
	}
	if r.ActiveSeconds != 75 {
		t.Errorf("active = %v", r.ActiveSeconds)
	}
	if u := unitOf(rep, UnitNone); u == nil || u.Dollars != nil {
		t.Errorf("local unit must carry no dollars: %+v", u)
	}
	_ = config.BillingModeLocal
}

func TestLocalToolUsageCountsNotReportedSessions(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	tu, err := BuildToolUsage(SeriesRequest{Window: 24 * time.Hour}, localEntries(now), SeriesOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if tu.TotalToolCalls != 12 || tu.TotalSessions != 3 {
		t.Errorf("calls/sessions = %d/%d, want 12/3", tu.TotalToolCalls, tu.TotalSessions)
	}
	if tu.TotalToolCallsNotReportedSessions != 1 {
		t.Errorf("not reported = %d, want 1", tu.TotalToolCallsNotReportedSessions)
	}
	if g := tu.Groups[0]; g.ToolCallsNotReportedSessions != 1 || g.ToolCalls != 12 {
		t.Errorf("group = %+v", g)
	}
}

func samePtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
