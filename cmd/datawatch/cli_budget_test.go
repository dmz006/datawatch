package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const budgetListFixture = `{"budgets":[{"llm":"claude-code","ceiling_usd":50,"spend_usd":12.5,"peer_spend_usd":0,"remaining_usd":37.5,
"window":"rolling","window_hours":5,"window_start":"2026-10-22T08:00:00Z","window_end":"2026-10-22T13:00:00Z",
"rolls_over_at":"2026-10-22T10:30:00Z","full":false,
"allocation":{"allocation_pct":25,"estimated":true,"allocation_usd":50,"plan_tier":"max_5x","basis":"x"}},
{"llm":"pi","ceiling_usd":10,"spend_usd":10,"peer_spend_usd":0,"remaining_usd":0,"window":"weekly",
"window_start":"2026-10-19T00:00:00Z","window_end":"2026-10-26T00:00:00Z","rolls_over_at":"2026-10-26T00:00:00Z","full":true}]}`

func TestCLIBudget(t *testing.T) {
	t.Run("status prints every budgeted llm", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.RequestURI()
			_, _ = w.Write([]byte(budgetListFixture))
		}))
		defer srv.Close()
		prev := serverURL
		serverURL = srv.URL
		defer func() { serverURL = prev }()

		var out bytes.Buffer
		c := newBudgetCmd()
		c.SetOut(&out)
		c.SetArgs([]string{"status"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		if gotPath != "/api/budget" {
			t.Errorf("requested %q, want /api/budget", gotPath)
		}
		text := out.String()
		for _, want := range []string{
			"claude-code: spend $12.50 of $50.00 ceiling (rolling 5h window)",
			"window end: 2026-10-22T13:00:00Z",
			"rolls over: 2026-10-22T10:30:00Z",
			"allocation (estimate): ~25% of $50.00 (max_5x)",
			"pi: spend $10.00 of $10.00 ceiling (weekly window) - FULL",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("missing %q in:\n%s", want, text)
			}
		}
		if strings.Count(text, "allocation") != 1 {
			t.Errorf("allocation should print only for the LLM that has one:\n%s", text)
		}
	})

	t.Run("status --llm asks for one llm", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.RequestURI()
			_, _ = w.Write([]byte(`{"llm":"pi","ceiling_usd":10,"spend_usd":3,"remaining_usd":7,"window":"weekly","window_start":"2026-10-19T00:00:00Z","window_end":"2026-10-26T00:00:00Z","rolls_over_at":"2026-10-26T00:00:00Z","full":false}`))
		}))
		defer srv.Close()
		prev := serverURL
		serverURL = srv.URL
		defer func() { serverURL = prev }()

		var out bytes.Buffer
		c := newBudgetCmd()
		c.SetOut(&out)
		c.SetArgs([]string{"status", "--llm", "pi"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		if gotPath != "/api/budget?llm=pi" {
			t.Errorf("requested %q, want /api/budget?llm=pi", gotPath)
		}
		if !strings.Contains(out.String(), "pi: spend $3.00 of $10.00 ceiling") || strings.Contains(out.String(), "allocation") {
			t.Errorf("output:\n%s", out.String())
		}
	})

	t.Run("status with no budgets", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"budgets":[]}`))
		}))
		defer srv.Close()
		prev := serverURL
		serverURL = srv.URL
		defer func() { serverURL = prev }()
		var out bytes.Buffer
		c := newBudgetCmd()
		c.SetOut(&out)
		c.SetArgs([]string{"status"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "No LLM has a budget") {
			t.Errorf("output: %q", out.String())
		}
	})

	t.Run("llm add sends the budget object", func(t *testing.T) {
		seen := llmDaemon(t)
		c := newLLMAddCmd(false)
		c.SetArgs([]string{"cc", "--kind", "claude-code", "--budget-ceiling-usd", "40", "--budget-weekday", "friday",
			"--budget-time", "17:00", "--budget-zone", "America/New_York", "--budget-allocation-pct",
			"--budget-peer-window", "peer-a, peer-b"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		calls := seen()
		if len(calls) != 1 {
			t.Fatalf("calls = %d", len(calls))
		}
		b, _ := calls[0].body["budget"].(map[string]any)
		if b["ceiling_usd"] != float64(40) || b["weekday"] != "friday" || b["time"] != "17:00" ||
			b["zone"] != "America/New_York" || b["allocation_pct"] != true {
			t.Errorf("budget = %v", b)
		}
		peers, _ := b["peer_window"].([]any)
		if len(peers) != 2 || peers[0] != "peer-a" || peers[1] != "peer-b" {
			t.Errorf("peer_window = %v", b["peer_window"])
		}
		if _, has := b["hours"]; has {
			t.Errorf("hours should be omitted for a weekly window: %v", b)
		}
	})

	t.Run("llm update without budget flags keeps the stored budget", func(t *testing.T) {
		seen := llmDaemon(t)
		c := newLLMAddCmd(true)
		c.SetArgs([]string{"cc", "--model", "m"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		if _, has := seen()[0].body["budget"]; has {
			t.Errorf("budget sent without a budget flag: %v", seen()[0].body)
		}
	})

	t.Run("invalid combinations are rejected before any request", func(t *testing.T) {
		for name, args := range map[string][]string{
			"no window style":  {"--budget-ceiling-usd", "5"},
			"both styles":      {"--budget-ceiling-usd", "5", "--budget-hours", "5", "--budget-weekday", "monday"},
			"no ceiling":       {"--budget-hours", "5"},
			"hours too large":  {"--budget-ceiling-usd", "5", "--budget-hours", "721"},
			"time on rolling":  {"--budget-ceiling-usd", "5", "--budget-hours", "5", "--budget-time", "09:00"},
			"unknown weekday":  {"--budget-ceiling-usd", "5", "--budget-weekday", "funday"},
			"unknown zone":     {"--budget-ceiling-usd", "5", "--budget-weekday", "monday", "--budget-zone", "Mars/Olympus"},
			"empty peer":       {"--budget-ceiling-usd", "5", "--budget-hours", "5", "--budget-peer-window", "a,,b"},
			"clear plus other": {"--clear-budget", "--budget-hours", "5"},
		} {
			for _, update := range []bool{false, true} {
				seen := llmDaemon(t)
				c := newLLMAddCmd(update)
				c.SetArgs(append([]string{"cc"}, args...))
				if err := runRunE(t, c); err == nil {
					t.Errorf("%s (update=%v): accepted, want error", name, update)
				}
				if n := len(seen()); n != 0 {
					t.Errorf("%s (update=%v): daemon hit %d times, want 0", name, update, n)
				}
			}
		}
	})

	t.Run("clear-budget sends null", func(t *testing.T) {
		seen := llmDaemon(t)
		c := newLLMAddCmd(true)
		c.SetArgs([]string{"cc", "--clear-budget"})
		if err := runRunE(t, c); err != nil {
			t.Fatal(err)
		}
		calls := seen()
		if len(calls) != 1 || calls[0].method != http.MethodPut {
			t.Fatalf("calls = %+v", calls)
		}
		v, has := calls[0].body["budget"]
		if !has || v != nil {
			t.Errorf("budget = %v (present=%v), want explicit null", v, has)
		}
	})
}
