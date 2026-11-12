// Comm-channel budget surface: the `budget status [llm]` command and the
// budget_* key=value pairs of `llm add` / `llm update`.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParse_BudgetStatus(t *testing.T) {
	for in, want := range map[string]string{
		"budget":                 "",
		"budget status":          "",
		"BUDGET Status":          "",
		"budget status claude":   "claude",
		"budget claude":          "claude",
		"budget   status   pi  ": "pi",
	} {
		c := Parse(in)
		if c.Type != CmdBudget || c.BudgetLLM != want || c.BudgetVerb != "" {
			t.Errorf("Parse(%q) = %+v, want budget for %q", in, c, want)
		}
	}
	if c := Parse("budget status a b"); c.Type != CmdBudget || c.BudgetVerb != "invalid" {
		t.Errorf("extra argument not flagged invalid: %+v", c)
	}
	if c := Parse("budgets"); c.Type == CmdBudget {
		t.Errorf("a longer word must not parse as budget: %+v", c)
	}
}

func TestParse_BudgetSet(t *testing.T) {
	// weekly window with every optional member
	got, set, err := parseLLMBudgetKV("kind=claude-code budget_ceiling_usd=40 budget_weekday=friday budget_time=17:00 budget_zone=America/New_York budget_allocation_pct=true budget_peer_window=a,b")
	if err != nil || !set {
		t.Fatalf("set=%v err=%v", set, err)
	}
	raw, _ := json.Marshal(got)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["ceiling_usd"] != float64(40) || m["weekday"] != "friday" || m["time"] != "17:00" ||
		m["zone"] != "America/New_York" || m["allocation_pct"] != true {
		t.Errorf("budget = %v", m)
	}
	if peers, _ := m["peer_window"].([]any); len(peers) != 2 || peers[0] != "a" || peers[1] != "b" {
		t.Errorf("peer_window = %v", m["peer_window"])
	}

	// rolling window
	got, set, err = parseLLMBudgetKV("budget_ceiling_usd=5.5 budget_hours=24")
	if err != nil || !set {
		t.Fatalf("rolling: set=%v err=%v", set, err)
	}
	raw, _ = json.Marshal(got)
	if !strings.Contains(string(raw), `"hours":24`) || strings.Contains(string(raw), "weekday") {
		t.Errorf("rolling budget = %s", raw)
	}

	// no budget key leaves the stored budget alone
	if _, set, err := parseLLMBudgetKV("kind=ollama model=m"); set || err != nil {
		t.Errorf("no budget keys: set=%v err=%v", set, err)
	}
	// clear
	if v, set, err := parseLLMBudgetKV("budget=clear"); !set || v != nil || err != nil {
		t.Errorf("clear: v=%v set=%v err=%v", v, set, err)
	}
	// invalid combinations are rejected with a reason
	for _, in := range []string{
		"budget_ceiling_usd=5",
		"budget_hours=5",
		"budget_ceiling_usd=5 budget_hours=5 budget_weekday=monday",
		"budget_ceiling_usd=abc budget_hours=5",
		"budget_ceiling_usd=5 budget_hours=x",
		"budget_ceiling_usd=5 budget_weekday=funday",
		"budget_ceiling_usd=5 budget_hours=5 budget_time=09:00",
		"budget_ceiling_usd=5 budget_hours=5 budget_peer_window=a,,b",
		"budget=clear budget_hours=5",
		"budget=5",
	} {
		if _, set, err := parseLLMBudgetKV(in); err == nil || set {
			t.Errorf("parseLLMBudgetKV(%q) accepted (set=%v)", in, set)
		}
	}
	// the flat kv parser must not leak budget_* keys into the request body
	body := parseLLMKVPairs("kind=ollama budget_ceiling_usd=5 budget_hours=5 budget=clear")
	for k := range body {
		if strings.HasPrefix(k, "budget") {
			t.Errorf("parseLLMKVPairs leaked %q", k)
		}
	}
}

func TestBudgetCommRoundTrip(t *testing.T) {
	var statusQueries []string
	var llmBodies []map[string]any
	var llmMethods []string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/budget", func(w http.ResponseWriter, r *http.Request) {
		statusQueries = append(statusQueries, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("llm") == "nobudget" {
			http.Error(w, "no budget for llm nobudget", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"budgets":[{"llm":"claude-code","ceiling_usd":50,"spend_usd":12.5,"peer_spend_usd":0,"remaining_usd":37.5,
"window":"rolling","window_hours":5,"window_start":"2026-10-22T08:00:00Z","window_end":"2026-10-22T13:00:00Z",
"rolls_over_at":"2026-10-22T10:30:00Z","full":false,
"allocation":{"allocation_pct":25,"estimated":true,"allocation_usd":50,"plan_tier":"max_5x","basis":"x"}}]}`))
	})
	mux.HandleFunc("/api/llms/", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		llmBodies = append(llmBodies, body)
		llmMethods = append(llmMethods, r.Method)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r := loopbackRouter(t, srv)
	send := func(msg string) string { return strings.Join(r.HandleTestMessage(msg), "\n") }

	out := send("budget status")
	for _, want := range []string{
		"budget status",
		"claude-code: spend $12.50 of $50.00 ceiling (rolling 5h window)",
		"window end: 2026-10-22T13:00:00Z",
		"rolls over: 2026-10-22T10:30:00Z",
		"allocation (estimate): ~25% of $50.00 (max_5x)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("reply missing %q:\n%s", want, out)
		}
	}
	if len(statusQueries) != 1 || statusQueries[0] != "" {
		t.Errorf("status query = %v, want one request with no llm filter", statusQueries)
	}

	send("budget status claude-code")
	if statusQueries[len(statusQueries)-1] != "llm=claude-code" {
		t.Errorf("per-llm query = %v", statusQueries)
	}

	if out := send("budget status nobudget"); !strings.Contains(out, "budget status failed") || !strings.Contains(out, "no budget for llm nobudget") {
		t.Errorf("404 reply = %q", out)
	}
	if out := send("budget a b"); !strings.Contains(out, "usage: budget") {
		t.Errorf("usage reply = %q", out)
	}

	// llm update carries the budget object to the REST body
	send("llm update cc budget_ceiling_usd=40 budget_hours=24")
	if len(llmBodies) != 1 || llmMethods[0] != http.MethodPut {
		t.Fatalf("llm update requests = %v %v", llmMethods, llmBodies)
	}
	b, _ := llmBodies[0]["budget"].(map[string]any)
	if b["ceiling_usd"] != float64(40) || b["hours"] != float64(24) {
		t.Errorf("budget body = %v", llmBodies[0])
	}
	for k := range llmBodies[0] {
		if k != "budget" && k != "name" {
			t.Errorf("unexpected body member %q", k)
		}
	}

	// clearing sends an explicit null; an invalid budget never reaches REST
	send("llm update cc budget=clear")
	if v, has := llmBodies[1]["budget"]; !has || v != nil {
		t.Errorf("clear body = %v", llmBodies[1])
	}
	if out := send("llm update cc budget_ceiling_usd=5"); !strings.Contains(out, "window style") || len(llmBodies) != 2 {
		t.Errorf("invalid budget: reply %q, requests %d", out, len(llmBodies))
	}
	if out := send("llm help"); !strings.Contains(out, "budget_ceiling_usd") || !strings.Contains(out, "budget_peer_window") {
		t.Errorf("llm usage lacks the budget keys:\n%s", out)
	}
	if out := send("help"); !strings.Contains(out, "budget [status]") {
		t.Errorf("help lacks the budget command")
	}
}
