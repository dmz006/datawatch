// usage_calculator.go — the six usage-calculator MCP tools
// (docs/plans/2026-10-23-usage-calculator-design.md, the "access" section;
// decisions, docs/plans/2026-10-25-usage-calculator-decisions.md). They
// proxy the loopback REST surface (/api/usage/...), so argument names and
// validation match REST exactly:
//
//	usage_advice            → GET    /api/usage/advice            (query: variant, policy, lanes, cost)
//	usage_readings          → GET    /api/usage/readings
//	usage_reading_add       → POST   /api/usage/readings          (exactly one figure; source defaults to typed)
//	usage_reading_delete    → DELETE /api/usage/readings/<id>
//	usage_extra_credit_set  → PUT    /api/usage/extra-credit      ({"points": n}, n >= 0)
//	usage_remaining_work_set→ PUT    /api/usage/remaining-work    ({"points": n} to set, {"points": null} to clear)
//
// The calculator advises only: it never stops or slows lanes (decision 9),
// so none of these tools gates anything. The planned sources (pasted,
// account) are not built yet, so a reading added through here always
// carries no source at REST, which means the built (typed) kind.

package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// usagePercentReading bounds a 0-100 percent figure (internal/usagecalc's
// MaxPercentReading, mirrored here so MCP clients get the same guidance).
const usagePercentReading = 100.0

func (s *Server) toolUsageAdvice() mcpsdk.Tool {
	return mcpsdk.NewTool("usage_advice",
		mcpsdk.WithDescription("One advice result from the account usage calculator: the latest reading of the shared readings series (percent and/or spend, when, session percent, lanes), extra credit and ceiling, lane choice (recent lanes and the max), the pace line and headroom after the safety margin, the one burn figure (recent, with the weekly average and the fallback notice when the recent window fell back to 48h), the projected end of cycle for each burn line, run-out warnings, the finish-early estimate (queue × per-task cost vs the typed remaining work) when the run-flat-out policy is active, and the plain-English advice block naming the formulas used. Advice only — the calculator never stops or slows lanes. variant and policy are independent: the variant picks the answer shape, the policy picks the spend target. Each optional argument overrides the stored settings for this one call only; omitting it keeps the stored value."),
		mcpsdk.WithString("variant", mcpsdk.Description("Answer shape to ask for (for example pace-gauge); independent of policy")),
		mcpsdk.WithString("policy", mcpsdk.Description("Spend target to ask for (for example steady or run-flat-out); independent of variant")),
		mcpsdk.WithNumber("lanes", mcpsdk.Description("Parallel lanes to assume (integer, >= 0), overriding the stored setting for this call")),
		mcpsdk.WithNumber("cost", mcpsdk.Description("Per-task cost in points (>= 0) for the finish-early estimate, overriding the stored setting for this call")),
	)
}

func (s *Server) handleUsageAdvice(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := req.GetString("variant", ""); v != "" {
		q.Set("variant", v)
	}
	if v := req.GetString("policy", ""); v != "" {
		q.Set("policy", v)
	}
	if n := req.GetInt("lanes", -1); n >= 0 {
		q.Set("lanes", strconv.Itoa(n))
	}
	if c := req.GetFloat("cost", -1); c >= 0 {
		q.Set("cost", strconv.FormatFloat(c, 'f', -1, 64))
	}
	out, err := s.proxyGet("/api/usage/advice", q)
	if err != nil {
		return toolError("usage_advice", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolUsageReadings() mcpsdk.Tool {
	return mcpsdk.NewTool("usage_readings",
		mcpsdk.WithDescription("The shared usage readings series: the readings the account has been measured at (percent and/or spend, when, session percent, lanes, source), the bought extra credit, and the per-policy remaining-work figure. The series is shared across federated daemons through a keeper; a read served from the last on-disk copy because the keeper could not be reached carries the stale markers (is_keeper, stale, stale_reason, copied_at). Advice only — the calculator behind these tools never stops or slows lanes."),
	)
}

func (s *Server) handleUsageReadings(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/usage/readings", nil)
	if err != nil {
		return toolError("usage_readings", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolUsageReadingAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("usage_reading_add",
		mcpsdk.WithDescription("Append one usage reading to the shared series. Carry exactly one figure: percent (0-100, the account's own number) or spend_usd (>= 0); both together is rejected, as is neither. Optional at (RFC3339 time, now when omitted), session_percent (0-100, the account's session number, the secondary reading) and lanes (>= 0). The source defaults to the built kind (typed); the planned pasted and account sources are not built yet and answer 501 at REST. When this daemon is a peer the write is forwarded to the keeper and stored once. Advice only — the calculator behind these tools never stops or slows lanes."),
		mcpsdk.WithNumber("percent", mcpsdk.Description("Account percentage, 0-100; exclusive with spend_usd")),
		mcpsdk.WithNumber("spend_usd", mcpsdk.Description("Spend in account dollars, >= 0; exclusive with percent")),
		mcpsdk.WithString("at", mcpsdk.Description("Reading time, RFC3339 (default: now)")),
		mcpsdk.WithNumber("session_percent", mcpsdk.Description("Session percentage, 0-100 (optional secondary)")),
		mcpsdk.WithNumber("lanes", mcpsdk.Description("Lanes active when this reading was taken (integer, >= 0, optional)")),
	)
}

func (s *Server) handleUsageReadingAdd(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	args := req.GetArguments()
	_, hasPercent := args["percent"]
	_, hasSpend := args["spend_usd"]
	if hasPercent == hasSpend {
		return toolErrorf("usage_reading_add", "carry exactly one figure, percent or spend_usd"), nil
	}
	if hasPercent {
		if v := req.GetFloat("percent", -1); v < 0 || v > usagePercentReading {
			return toolErrorf("usage_reading_add", "percent must be from 0 to 100"), nil
		}
	}
	if hasSpend && req.GetFloat("spend_usd", -1) < 0 {
		return toolErrorf("usage_reading_add", "spend_usd must be 0 or more"), nil
	}
	if args["session_percent"] != nil {
		if v := req.GetFloat("session_percent", -1); v < 0 || v > usagePercentReading {
			return toolErrorf("usage_reading_add", "session_percent must be from 0 to 100"), nil
		}
	}
	if args["lanes"] != nil && req.GetInt("lanes", 0) < 0 {
		return toolErrorf("usage_reading_add", "lanes must be 0 or more"), nil
	}
	body := map[string]any{}
	if hasPercent {
		body["percent"] = req.GetFloat("percent", 0)
	}
	if hasSpend {
		body["spend_usd"] = req.GetFloat("spend_usd", 0)
	}
	if at := req.GetString("at", ""); at != "" {
		body["at"] = at
	}
	if args["session_percent"] != nil {
		body["session_percent"] = req.GetFloat("session_percent", 0)
	}
	if args["lanes"] != nil {
		body["lanes"] = req.GetInt("lanes", 0)
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/usage/readings", body)
	if err != nil {
		return toolError("usage_reading_add", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolUsageReadingDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("usage_reading_delete",
		mcpsdk.WithDescription("Delete one reading from the shared usage readings series by its id (see usage_readings for the ids). When this daemon is a peer the delete is forwarded to the keeper; an unknown id answers 404, and the storing daemon audits the operation. Advice only — the calculator behind these tools never stops or slows lanes."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Reading id to delete")),
	)
}

func (s *Server) handleUsageReadingDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	if id == "" {
		return toolErrorf("usage_reading_delete", "a reading id is required"), nil
	}
	// Path-escape the id so one carrying a slash or question mark stays a
	// single path segment on the loopback route instead of retargeting it.
	out, err := s.proxyJSON(http.MethodDelete, "/api/usage/readings/"+url.PathEscape(id), nil)
	if err != nil {
		return toolError("usage_reading_delete", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolUsageExtraCreditSet() mcpsdk.Tool {
	return mcpsdk.NewTool("usage_extra_credit_set",
		mcpsdk.WithDescription("Set the bought extra credit, in points of the weekly allowance (>= 0), on the shared series: the calculator's effective ceiling is the weekly allowance plus this figure, per policy. It is a setting, never a reading — it stays out of the readings series and is shown only as its own setting. A 0 is a valid setting (nothing extra bought) and the operator's call to make, not the calculator's. When this daemon is a peer the write is forwarded to the keeper and stored once. Advice only — the calculator behind these tools never stops or slows lanes."),
		mcpsdk.WithNumber("points", mcpsdk.Required(), mcpsdk.Description("Extra credit, points of the weekly allowance, >= 0")),
	)
}

func (s *Server) handleUsageExtraCreditSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	points := req.GetFloat("points", -1)
	if points < 0 {
		return toolErrorf("usage_extra_credit_set", "points must be 0 or more"), nil
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/usage/extra-credit", map[string]any{"points": points})
	if err != nil {
		return toolError("usage_extra_credit_set", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolUsageRemainingWorkSet() mcpsdk.Tool {
	return mcpsdk.NewTool("usage_remaining_work_set",
		mcpsdk.WithDescription("Set the operator's typed remaining work, in points of the weekly allowance (>= 0), under the run-flat-out policy: the finish-early estimate compares the queue estimate (this daemon's unfinished tasks × the per-task cost) against this typed figure, and the typed figure replaces the queue estimate once present. Omit points, or pass it as null, to clear the typed figure: the estimate then falls back to the queue estimate. It is a setting, never a reading: it is daemon-local, stays out of the shared readings series, and is never sent to another daemon even through the keeper. It advises only — the calculator never stops or slows lanes."),
		mcpsdk.WithNumber("points", mcpsdk.Description("Remaining work, points of the weekly allowance, >= 0; omit or pass null to clear the typed figure")),
	)
}

func (s *Server) handleUsageRemainingWorkSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	// Points is optional: omitted or null clears the typed figure, which
	// REST accepts as a null PUT. A nil *float64 marshals to JSON null, so
	// the clear and the set both travel as PUT {"points": …}.
	var points any = (*float64)(nil)
	if raw, present := req.GetArguments()["points"]; present && raw != nil {
		if v := req.GetFloat("points", 0); v < 0 {
			return toolErrorf("usage_remaining_work_set", "points must be 0 or more"), nil
		}
		points = req.GetFloat("points", 0)
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/usage/remaining-work", map[string]any{"points": points})
	if err != nil {
		return toolError("usage_remaining_work_set", err), nil
	}
	return textOK(string(out)), nil
}
