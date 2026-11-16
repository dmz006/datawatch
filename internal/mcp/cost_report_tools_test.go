// cost_report — MCP tool tests: the cost report engine
// (internal/costreport) over the daemon's recorded sessions, with every
// default sourced from the cost_report config block (Server.costReportCfg).

package mcp

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/costreport"
	"github.com/dmz006/datawatch/internal/session"
)

// costReportSession records one session with usage in the manager's
// store so managerCostReportSource (and costreport.Build) can read it.
func costReportSession(t *testing.T, s *Server, id, backend string, tin, tout int, at time.Time) {
	t.Helper()
	sess := &session.Session{
		ID:            id,
		FullID:        "testhost-" + id,
		Hostname:      "testhost",
		BackendFamily: backend,
		UpdatedAt:     at,
		TokensIn:      tin,
		TokensOut:     tout,
	}
	if err := s.manager.Store().Save(sess); err != nil {
		t.Fatalf("Store().Save: %v", err)
	}
}

func costReportRes(t *testing.T, s *Server, args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	res, err := s.handleCostReport(context.Background(), call(args))
	if err != nil {
		t.Fatalf("handleCostReport: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	return res
}

func costReportBody(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if res.IsError {
		t.Fatalf("unexpected error result: %s", costReportText(res))
	}
	return costReportText(res)
}

func costReportText(res *mcpsdk.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return "<nil>"
	}
	if tc, ok := res.Content[0].(mcpsdk.TextContent); ok {
		return tc.Text
	}
	return "<non-text>"
}

func costReportReport(t *testing.T, s *Server, args map[string]any) *costreport.Report {
	t.Helper()
	res := costReportRes(t, s, args)
	text := costReportBody(t, res)
	rep := &costreport.Report{}
	if err := json.Unmarshal([]byte(text), rep); err != nil {
		t.Fatalf("result is not a costreport.Report: %v (%s)", err, text)
	}
	return rep
}

func TestCostReportToolIsListedOptionalParams(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	allowTestTemp(mgr)
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	var found *ToolDescriptor
	for i := range tools {
		if tools[i].Name == "cost_report" {
			found = &tools[i]
		}
	}
	if found == nil {
		t.Fatal("cost_report is not registered")
	}
	var schema struct {
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(found.InputSchema, &schema); err != nil {
		t.Fatalf("input schema: %v", err)
	}
	for _, p := range []string{"group_by", "since", "until"} {
		if _, ok := schema.Properties[p]; !ok {
			t.Errorf("cost_report lacks the %s parameter", p)
		}
	}
	if required := strings.Join(schema.Required, ","); required != "" {
		t.Errorf("group_by, since, until must all be optional; required = %q", required)
	}
}

func TestCostReportDefaultsGroupAutomatonForZeroConfig(t *testing.T) {
	s := bl91Server(t) // zero costReportCfg → engine defaults (automaton, 7d)
	rep := costReportReport(t, s, nil)
	if rep.GroupBy != costreport.GroupByAutomaton {
		t.Errorf("default group_by = %q, want automaton (config default)", rep.GroupBy)
	}
	if got := rep.GroupBy.Valid(); !got {
		t.Errorf("returned group_by %q is not in the vocabulary", rep.GroupBy)
	}
}

func TestCostReportWindowingAndTokensPerBackendGroup(t *testing.T) {
	s := bl91Server(t)
	now := time.Now().UTC().Truncate(time.Minute)
	// In-window session on a rate-configured backend.
	costReportSession(t, s, "c001", "claude-code", 20_000, 4_000, now.Add(-24*time.Hour))
	// Out-of-window session: excluded from the default 7-day window.
	costReportSession(t, s, "c002", "claude-code", 90_000, 9_000, now.Add(-30*24*time.Hour))

	rep := costReportReport(t, s, map[string]any{"group_by": "backend"})
	if len(rep.Rows) != 1 {
		t.Fatalf("rows = %+v, want one claude-code row (second session is out of window)", rep.Rows)
	}
	row := rep.Rows[0]
	if row.Key != "claude-code" {
		t.Errorf("row key = %q, want claude-code", row.Key)
	}
	if row.Sessions != 1 || row.TokensIn != 20_000 || row.TokensOut != 4_000 {
		t.Errorf("row = %+v, want the in-window session only", row)
	}
	if !row.RateConfigured {
		t.Error("claude-code has a default rate; RateConfigured must be true")
	}
	if row.USDEstimate == nil {
		t.Error("USD estimate absent although rate + default show_usd_estimates=true")
	} else if want := 20_000/1000*0.003 + 4_000/1000*0.015; *row.USDEstimate < want-1e-9 || *row.USDEstimate > want+1e-9 {
		t.Errorf("usd_estimate = %v, want %v", *row.USDEstimate, want)
	}

	// Extending the window with since/until brings the older session in.
	wide := costReportReport(t, s, map[string]any{"group_by": "backend", "since": "2000-01-01"})
	if len(wide.Rows) != 1 || wide.Rows[0].Sessions != 2 || wide.Rows[0].TokensIn != 110_000 {
		t.Errorf("wide window rows = %+v, want both sessions on claude-code", wide.Rows)
	}
}

func TestCostReportConfigDefaultsDriveGroupBy(t *testing.T) {
	s := bl91Server(t)
	weekPtr, days365 := "week", 365
	s.costReportCfg = config.CostReportConfig{DefaultGroupBy: &weekPtr, DefaultWindowDays: &days365}
	// Thursday 2026-10-29 UTC → Monday-anchored week key "2026-10-26".
	costReportSession(t, s, "c001", "claude-code", 1_000, 1_000, time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC))

	rep := costReportReport(t, s, nil) // no group_by argument
	if rep.GroupBy != costreport.GroupByWeek {
		t.Errorf("group_by = %q, want the cost_report config's default (week)", rep.GroupBy)
	}
	if len(rep.Rows) != 1 || rep.Rows[0].Key != "2026-10-26" {
		t.Errorf("rows = %+v, want one bucket keyed 2026-10-26", rep.Rows)
	}
}

func TestCostReportRejectsBadArguments(t *testing.T) {
	s := bl91Server(t)
	for name, args := range map[string]map[string]any{
		"bad group_by": {"group_by": "bogus"},
		"bad since":    {"since": "not-a-time"},
		"bad until":    {"until": "not-a-time"},
	} {
		res := costReportRes(t, s, args)
		if !res.IsError {
			t.Errorf("%s: expected an error result, got %q", name, costReportText(res))
			continue
		}
		text := costReportText(res)
		if !strings.Contains(text, "cost_report") {
			t.Errorf("%s: error text should name the tool: %q", name, text)
		}
	}
}

func TestCostReportWithoutAManagerIsAFlaggedError(t *testing.T) {
	s := &Server{}
	res := costReportRes(t, s, nil)
	if !res.IsError {
		t.Fatalf("expected an error result without a manager, got %q", costReportText(res))
	}
	if text := costReportText(res); !strings.Contains(text, errCostReportUnavailable.Error()) {
		t.Errorf("error text = %q, want the missing-manager sentinel", text)
	}
}

// costReportModelSession is costReportSession with a model name, so the
// by-model section has a finer key than the backend family.
func costReportModelSession(t *testing.T, s *Server, id, backend, model string, tin, tout int, at time.Time) {
	t.Helper()
	sess := &session.Session{
		ID:            id,
		FullID:        "testhost-" + id,
		Hostname:      "testhost",
		BackendFamily: backend,
		Model:         model,
		UpdatedAt:     at,
		TokensIn:      tin,
		TokensOut:     tout,
	}
	if err := s.manager.Store().Save(sess); err != nil {
		t.Fatalf("Store().Save: %v", err)
	}
}

// TestCostReportModelRowsTwoRates — the tool's by-model section reports
// each model at its own rate from the daemon's rate table (two models,
// two different rates), each row labelled API-equivalent; the model
// rows carry the same tokens as the backend rows they refine.
func TestCostReportModelRowsTwoRates(t *testing.T) {
	s := bl91Server(t)
	now := time.Now().UTC().Truncate(time.Minute)
	// Two sessions, two models, each with its own entry in the rate
	// table and a different one from the other's.
	costReportModelSession(t, s, "mm01", "claude-code", "claude-sonnet-4", 20_000, 4_000, now.Add(-24*time.Hour))
	costReportModelSession(t, s, "mm02", "gpt", "gpt-5", 10_000, 2_000, now.Add(-24*time.Hour))
	s.manager.SetCostRates(map[string]session.CostRate{
		"claude-code":     {InPerK: 0.003, OutPerK: 0.015}, // backend fallback
		"gpt":             {InPerK: 0.0025, OutPerK: 0.01}, // backend fallback
		"claude-sonnet-4": {InPerK: 0.003, OutPerK: 0.015}, // the model's own rate
		"gpt-5":           {InPerK: 0.005, OutPerK: 0.02},  // a different one
	})

	rep := costReportReport(t, s, map[string]any{"group_by": "backend"})
	if len(rep.ModelRows) != 2 {
		t.Fatalf("model_rows = %+v, want two rows (one per model)", rep.ModelRows)
	}
	byModel := map[string]*costreport.ModelRow{}
	for i := range rep.ModelRows {
		byModel[rep.ModelRows[i].Model] = &rep.ModelRows[i]
	}
	sonnet := byModel["claude-sonnet-4"]
	if sonnet == nil || sonnet.TokensIn != 20_000 || sonnet.TokensOut != 4_000 {
		t.Fatalf("claude-sonnet-4 row = %+v, want the mm01 session's tokens", sonnet)
	}
	if sonnet.APIEquivalentUSD == nil {
		t.Fatal("claude-sonnet-4: want USD (rate table has a model-named entry)")
	}
	if want := 20_000/1000*0.003 + 4_000/1000*0.015; math.Abs(*sonnet.APIEquivalentUSD-want) > 1e-9 {
		t.Errorf("claude-sonnet-4 usd = %v, want %v (its own rate)", *sonnet.APIEquivalentUSD, want)
	}
	if sonnet.USDLabel == "" {
		t.Error("claude-sonnet-4: priced rows must carry the API-equivalent label")
	}

	gpt := byModel["gpt-5"]
	if gpt == nil || gpt.TokensIn != 10_000 || gpt.TokensOut != 2_000 {
		t.Fatalf("gpt-5 row = %+v, want the mm02 session's tokens", gpt)
	}
	if gpt.APIEquivalentUSD == nil {
		t.Fatal("gpt-5: want USD (rate table has a model-named entry)")
	}
	if want := 10_000/1000*0.005 + 2_000/1000*0.02; math.Abs(*gpt.APIEquivalentUSD-want) > 1e-9 {
		t.Errorf("gpt-5 usd = %v, want %v (its own rate)", *gpt.APIEquivalentUSD, want)
	}
	// Different rates must give different figures, not a shared one.
	if *sonnet.APIEquivalentUSD == *gpt.APIEquivalentUSD {
		t.Fatal("the two models share a USD figure; per-model rates were not applied")
	}
}
