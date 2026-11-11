package router

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// costRow mirrors the wire shape of a costreport.Row; only the fields the
// compact chat text needs are named.
type costRow struct {
	Key                  string   `json:"key"`
	Sessions             int      `json:"sessions"`
	TokensIn             int      `json:"tokens_in"`
	TokensOut            int      `json:"tokens_out"`
	CacheRead            int      `json:"cache_read"`
	CacheCreation        int      `json:"cache_creation"`
	ToolCalls            int      `json:"tool_calls"`
	SpanSeconds          float64  `json:"span_seconds"`
	ActiveSeconds        float64  `json:"active_seconds"`
	WindowNote           string   `json:"window_note"`
	USDEstimate          *float64 `json:"usd_estimate"`
	APIEquivalentUSD     *float64 `json:"api_equivalent_usd"`
	EstimatedUsagePoints *float64 `json:"estimated_usage_points"`
}

type costReportReply struct {
	GroupBy     string    `json:"group_by"`
	Rows        []costRow `json:"rows"`
	WindowNote  string    `json:"window_note"`
	Attribution *struct {
		AutomatonLaunched int    `json:"automaton_launched"`
		Unattributed      int    `json:"unattributed"`
		OK                bool   `json:"ok"`
		Note              string `json:"note"`
	} `json:"attribution"`
}

func costSeconds(sec float64) string {
	if sec <= 0 {
		return "0s"
	}
	return (time.Duration(sec) * time.Second).String()
}

func costUSD(r costRow) string {
	est := r.APIEquivalentUSD
	if est == nil {
		est = r.USDEstimate
	}
	if est == nil {
		return "-"
	}
	return fmt.Sprintf("$%.2f", *est)
}

// costRowLine renders one row compactly.
func costRowLine(r costRow) string {
	return fmt.Sprintf("%s: %d sessions, in/out/cache %d/%d/%d+%d, %d tools, span %s, active %s, %s",
		r.Key, r.Sessions, r.TokensIn, r.TokensOut, r.CacheRead, r.CacheCreation,
		r.ToolCalls, costSeconds(r.SpanSeconds), costSeconds(r.ActiveSeconds), costUSD(r))
}

// formatCostReportText renders a cost report JSON body as compact chat text:
// one line per row with the new columns, then window notes and the
// attribution count. Returns "" when body is not a report.
func formatCostReportText(body string) string {
	var rep costReportReply
	if err := json.Unmarshal([]byte(body), &rep); err != nil || rep.Rows == nil && rep.GroupBy == "" {
		return ""
	}
	var lines []string
	for _, r := range rep.Rows {
		lines = append(lines, costRowLine(r))
	}
	if len(rep.Rows) == 0 {
		lines = append(lines, "no cost rows")
	}
	for _, r := range rep.Rows {
		if r.WindowNote != "" {
			lines = append(lines, fmt.Sprintf("window note (%s): %s", r.Key, r.WindowNote))
		}
	}
	if rep.WindowNote != "" {
		lines = append(lines, "window note: "+rep.WindowNote)
	}
	if a := rep.Attribution; a != nil {
		state := "ok"
		if !a.OK {
			state = "UNATTRIBUTED"
		}
		l := fmt.Sprintf("attribution: %s, %d of %d Automaton-launched sessions unattributed", state, a.Unattributed, a.AutomatonLaunched)
		if a.Note != "" {
			l += " (" + a.Note + ")"
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n")
}

// automatonCostStatusLines renders the `cost` object of an Automaton detail
// response for the status reply; nil when it carries none.
func automatonCostStatusLines(prdJSON string) []string {
	var v struct {
		Cost *struct {
			costRow
			ReportWindowNote string `json:"report_window_note"`
		} `json:"cost"`
	}
	if err := json.Unmarshal([]byte(prdJSON), &v); err != nil || v.Cost == nil {
		return nil
	}
	c := v.Cost
	lines := []string{"cost: " + costRowLine(costRow{
		Key: "automaton", Sessions: c.Sessions, TokensIn: c.TokensIn, TokensOut: c.TokensOut,
		CacheRead: c.CacheRead, CacheCreation: c.CacheCreation, ToolCalls: c.ToolCalls,
		SpanSeconds: c.SpanSeconds, ActiveSeconds: c.ActiveSeconds,
		USDEstimate: c.USDEstimate, APIEquivalentUSD: c.APIEquivalentUSD,
	}) + " (API-equivalent, not a bill)"}
	if c.WindowNote != "" {
		lines = append(lines, "cost window note: "+c.WindowNote)
	}
	if c.ReportWindowNote != "" {
		lines = append(lines, "cost window note: "+c.ReportWindowNote)
	}
	return lines
}
