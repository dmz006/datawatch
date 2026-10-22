// Per-LLM dollar budget MCP surface. The read tool proxies the loopback REST
// endpoint (/api/budget) so field names and bodies match REST exactly; the
// helpers here also carry the budget object on llm_add / llm_update and the
// plan-tier allocation table on autonomous_config_set.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/inference"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// planTierAllocationConfigKey is the REST config key of the notional dollar
// allocation table; the single-tier pair form appends ".<tier>" to it.
const planTierAllocationConfigKey = "autonomous.plan_tier_allocation_usd"

func (s *Server) toolBudgetStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("budget_status",
		mcpsdk.WithDescription("Read the per-LLM dollar budget state (same body as GET /api/budget): {\"budgets\": [...]} with one entry for every LLM that has a budget, each carrying its window, ceiling, spend and state. Pass llm to get that one LLM's entry instead (an error when it has no budget). allocation_pct is only an estimate of the share of the plan-tier allocation used; it is informational and never blocks anything. Spend from federation peers counts toward a ceiling only when the LLM lists them in peer_window, which is off by default."),
		mcpsdk.WithString("llm", mcpsdk.Description("Optional LLM registry name; omit to list every budgeted LLM")),
	)
}

func (s *Server) handleBudgetStatus(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	var q url.Values
	if name := strings.TrimSpace(req.GetString("llm", "")); name != "" {
		q = url.Values{"llm": {name}}
	}
	out, err := s.proxyGet("/api/budget", q)
	if err != nil {
		return toolError("budget_status", err), nil
	}
	return textOK(string(out)), nil
}

// llmBudgetOption declares the budget object argument shared by llm_add and
// llm_update. The members are the REST field names; the REST side validates
// them (one window style, positive ceiling) and rejects unknown members.
func llmBudgetOption() mcpsdk.ToolOption {
	return mcpsdk.WithObject("budget",
		mcpsdk.Description("Optional dollar budget for this LLM, replaced as a whole on llm_update (omit to keep the stored one; null or {} clears it). "+
			"ceiling_usd (> 0) is required with exactly one window style: hours (rolling trailing window, 1-720) or weekday (monday..sunday, a weekly window) with optional time (24h HH:MM, default 00:00) and zone (IANA zone, default UTC). "+
			"allocation_pct (bool) adds an estimated share of the plan-tier allocation to the status; it is an estimate that never blocks. "+
			"peer_window (list of federation peer names) counts those peers' spend toward the ceiling; it is off by default, so peers are not counted unless named."),
		mcpsdk.Properties(map[string]any{
			"ceiling_usd":    map[string]any{"type": "number", "description": "dollar ceiling for one window (> 0)"},
			"hours":          map[string]any{"type": "integer", "description": "rolling window length in hours (1-720); exclusive with weekday"},
			"weekday":        map[string]any{"type": "string", "description": "weekly window anchor day, monday..sunday; exclusive with hours"},
			"time":           map[string]any{"type": "string", "description": "weekly anchor clock time, 24h HH:MM (default 00:00)"},
			"zone":           map[string]any{"type": "string", "description": "IANA zone the weekly anchor is read in (default UTC)"},
			"allocation_pct": map[string]any{"type": "boolean", "description": "report an estimated share of the plan-tier allocation; an estimate that never blocks"},
			"peer_window":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "federation peers whose spend counts toward the ceiling (off by default)"},
		}),
	)
}

// budgetFromReq returns the budget object to forward to REST and whether the
// caller supplied one. Presence is what matters: an explicit null or empty
// object must reach REST so it can clear the stored budget.
func budgetFromReq(req mcpsdk.CallToolRequest) (any, bool, error) {
	args := req.GetArguments()
	if args == nil {
		return nil, false, nil
	}
	raw, ok := args["budget"]
	if !ok {
		return nil, false, nil
	}
	switch v := raw.(type) {
	case nil, map[string]any:
		return v, true, nil
	case string:
		// Some clients hand an object argument over as its JSON text.
		text := strings.TrimSpace(v)
		if text == "" || text == "null" {
			return nil, true, nil
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(text), &obj); err != nil {
			return nil, false, fmt.Errorf("budget: expected an object: %v", err)
		}
		return obj, true, nil
	default:
		return nil, false, fmt.Errorf("budget: expected an object, got %T", raw)
	}
}

// attachBudget copies the caller's budget object, when one was given, into
// the LLM request body under the REST field name.
func attachBudget(body map[string]any, req mcpsdk.CallToolRequest) error {
	budget, ok, err := budgetFromReq(req)
	if err != nil {
		return err
	}
	if ok {
		body["budget"] = budget
	}
	return nil
}

// parsePlanTierAllocationArg parses the plan_tier_allocation_usd tool
// argument into the config PUT entries that realise it. A JSON object
// replaces the whole table under autonomous.plan_tier_allocation_usd; a
// "tier=usd,tier=usd" list sets just those tiers through the single-tier pair
// keys autonomous.plan_tier_allocation_usd.<tier>, leaving the others alone.
// Tiers must be known and dollar values finite numbers >= 0 (0 = not
// declared); the REST side enforces the same contract.
func parsePlanTierAllocationArg(v string) (map[string]any, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, fmt.Errorf("plan_tier_allocation_usd: empty value; omit the argument to leave the stored table unchanged")
	}
	if strings.HasPrefix(v, "{") {
		var raw map[string]json.Number
		if err := json.Unmarshal([]byte(v), &raw); err != nil {
			return nil, fmt.Errorf("plan_tier_allocation_usd: JSON object parse: %v", err)
		}
		table := make(map[string]float64, len(raw))
		for tier, n := range raw {
			usd, err := checkAllocationEntry(tier, n.String())
			if err != nil {
				return nil, err
			}
			table[tier] = usd
		}
		return map[string]any{planTierAllocationConfigKey: table}, nil
	}
	entries := map[string]any{}
	for _, pair := range strings.Split(v, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		tier, val, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("plan_tier_allocation_usd: %q is not tier=usd nor a JSON object", pair)
		}
		tier = strings.TrimSpace(tier)
		usd, err := checkAllocationEntry(tier, strings.TrimSpace(val))
		if err != nil {
			return nil, err
		}
		entries[planTierAllocationConfigKey+"."+tier] = usd
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("plan_tier_allocation_usd: no tier=usd pairs parsed from %q", v)
	}
	return entries, nil
}

func checkAllocationEntry(tier, val string) (float64, error) {
	if tier == "" || !inference.ValidPlanTier(inference.PlanTier(tier)) {
		return 0, fmt.Errorf("plan_tier_allocation_usd: unknown plan tier %q (expected pro, max_5x, max_20x, or api)", tier)
	}
	usd, err := strconv.ParseFloat(val, 64)
	if err != nil || usd < 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
		return 0, fmt.Errorf("plan_tier_allocation_usd: value for tier %q must be a finite number >= 0", tier)
	}
	return usd, nil
}

// putPlanTierAllocation writes the allocation entries through the config
// endpoint, the only REST route that carries this table.
func (s *Server) putPlanTierAllocation(entries map[string]any) ([]byte, error) {
	return s.proxyJSON(http.MethodPut, "/api/config", entries)
}
