// BL6 — cost tracking tests.

package session

import (
	"testing"
	"time"
)

func TestBL6_EstimateCost_BasicMath(t *testing.T) {
	rate := CostRate{InPerK: 0.003, OutPerK: 0.015}
	got := EstimateCost(rate, 1000, 2000)
	want := 0.003 + 2*0.015
	if got != want {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestBL6_EstimateCost_ZeroRateZeroCost(t *testing.T) {
	if EstimateCost(CostRate{}, 100000, 100000) != 0 {
		t.Errorf("zero rate should produce zero cost")
	}
}

func TestBL6_DefaultCostRates_HasClaude(t *testing.T) {
	r, ok := DefaultCostRates()["claude-code"]
	if !ok {
		t.Fatal("missing claude-code rate")
	}
	if r.InPerK <= 0 || r.OutPerK <= 0 {
		t.Errorf("claude-code rates should be positive: %+v", r)
	}
}

func TestBL6_SummaryFor_Aggregates(t *testing.T) {
	sessions := []*Session{
		{ID: "a", LLMBackend: "claude-code", TokensIn: 1000, TokensOut: 500, EstCostUSD: 0.011},
		{ID: "b", LLMBackend: "claude-code", TokensIn: 2000, TokensOut: 1500, EstCostUSD: 0.029},
		{ID: "c", LLMBackend: "ollama", TokensIn: 5000, TokensOut: 5000, EstCostUSD: 0},
	}
	sum := SummaryFor(sessions)
	if sum.Sessions != 3 {
		t.Errorf("sessions=%d want 3", sum.Sessions)
	}
	if sum.TotalIn != 8000 || sum.TotalOut != 7000 {
		t.Errorf("totals wrong: %+v", sum)
	}
	if sum.PerBackend["claude-code"].Sessions != 2 {
		t.Errorf("claude bucket wrong: %+v", sum.PerBackend["claude-code"])
	}
}

func TestBL6_AddUsage_UpdatesSession(t *testing.T) {
	mgr, err := NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	mgr.WithFakeTmux()
	_ = mgr.SaveSession(&Session{
		ID: "aa", FullID: "testhost-aa", Hostname: "testhost",
		LLMBackend: "claude-code", State: StateRunning,
		UpdatedAt: time.Now(),
	})
	if err := mgr.AddUsage("testhost-aa", 1000, 500, CostRate{}); err != nil {
		t.Fatal(err)
	}
	got, _ := mgr.GetSession("testhost-aa")
	if got.TokensIn != 1000 || got.TokensOut != 500 {
		t.Errorf("usage not applied: %+v", got)
	}
	if got.EstCostUSD <= 0 {
		t.Errorf("expected positive est cost for claude rates, got %v", got.EstCostUSD)
	}
}

func TestBL6_CostRate_FamilyFallback(t *testing.T) {
	mgr, _ := NewManager("h", t.TempDir(), "/bin/echo", 0)
	r, ok := mgr.costRate("opencode-acp")
	if !ok {
		t.Fatal("expected family fallback to find opencode-acp directly")
	}
	if r.InPerK <= 0 {
		t.Errorf("rate should be positive: %+v", r)
	}
}

func TestBL6_SetCostRates_Override(t *testing.T) {
	mgr := &Manager{}
	mgr.SetCostRates(map[string]CostRate{"foo": {InPerK: 99}})
	r, ok := mgr.costRate("foo")
	if !ok || r.InPerK != 99 {
		t.Errorf("override failed: %+v ok=%v", r, ok)
	}
}
