package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/costreport"
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
	LocalTokens          int      `json:"local_tokens"`
	TokensNotReported    int      `json:"tokens_not_reported_sessions"`
	WindowNote           string   `json:"window_note"`
	USDEstimate          *float64 `json:"usd_estimate"`
	APIEquivalentUSD     *float64 `json:"api_equivalent_usd"`
	EstimatedUsagePoints *float64 `json:"estimated_usage_points"`
}

// costModelRow mirrors the wire shape of a costreport.ModelRow — one row
// of the by-model breakdown (see costreport.Report.ModelRows).
type costModelRow struct {
	Model            string   `json:"model"`
	Sessions         int      `json:"sessions"`
	TokensIn         int      `json:"tokens_in"`
	TokensOut        int      `json:"tokens_out"`
	CacheRead        int      `json:"cache_read"`
	CacheCreation    int      `json:"cache_creation"`
	ToolCalls        int      `json:"tool_calls"`
	APIEquivalentUSD *float64 `json:"api_equivalent_usd"`
	USDLabel         string   `json:"usd_label"`
}

type costReportReply struct {
	GroupBy     string         `json:"group_by"`
	Rows        []costRow      `json:"rows"`
	ModelRows   []costModelRow `json:"model_rows"`
	WindowNote  string         `json:"window_note"`
	Attribution *struct {
		AutomatonLaunched int    `json:"automaton_launched"`
		Unattributed      int    `json:"unattributed"`
		NoAutomatonNoRole int    `json:"no_automaton_no_role"`
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
	line := fmt.Sprintf("%s: %d sessions, in/out/cache %d/%d/%d+%d, %d tools, span %s, active %s, %s",
		r.Key, r.Sessions, r.TokensIn, r.TokensOut, r.CacheRead, r.CacheCreation,
		r.ToolCalls, costSeconds(r.SpanSeconds), costSeconds(r.ActiveSeconds), costUSD(r))
	// Local tokens are their own unit, never summed with Claude tokens or priced.
	if r.LocalTokens > 0 || r.TokensNotReported > 0 {
		line += fmt.Sprintf(", local tokens %d", r.LocalTokens)
		if r.TokensNotReported > 0 {
			line += fmt.Sprintf(" (%d local sessions: tokens not reported)", r.TokensNotReported)
		}
	}
	return line
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
	if len(rep.ModelRows) > 0 {
		lines = append(lines, "by model:")
		anyModelUSD := false
		for _, r := range rep.ModelRows {
			if r.APIEquivalentUSD != nil {
				anyModelUSD = true
			}
			lines = append(lines, fmt.Sprintf("model %s: %d sessions, in/out/cache %d/%d/%d+%d, %d tools, %s",
				r.Model, r.Sessions, r.TokensIn, r.TokensOut, r.CacheRead, r.CacheCreation,
				r.ToolCalls, costUSD(costRow{APIEquivalentUSD: r.APIEquivalentUSD})))
		}
		if anyModelUSD {
			lines = append(lines, "model dollars are API-equivalent (each model's own rate), not a bill")
		}
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
		l += fmt.Sprintf("; %d with no Automaton and no role", a.NoAutomatonNoRole)
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
		LocalTokens: c.LocalTokens, TokensNotReported: c.TokensNotReported,
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

// handleCostChartsCmd serves "cost charts [group_by=<g>] [window=<d>]": the
// combined usage report. It reads the cost-over-time, tool-usage and failures
// REST views through the loopback and joins them with the shared costreport
// helpers, the same ones the CLI and the MCP tool use; the handler only
// translates the chat tokens into a request and relays the text back.
func (r *Router) handleCostChartsCmd(cmd Command) {
	const usage = "usage: cost charts [group_by=<automaton|project|model|automation>] [window=<36h|7d|2w>]"
	var req costreport.ChartsRequest
	for _, tok := range strings.Fields(cmd.Text) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			r.reply("cost charts failed", usage)
			return
		}
		switch k {
		case "group_by":
			req.GroupBy = v
		case "window":
			req.Window = v
		default:
			r.reply("cost charts failed", usage)
			return
		}
	}
	charts, err := costreport.FetchCharts(req, func(path string, q url.Values) ([]byte, error) {
		body, err := r.commGet(path, q)
		return []byte(body), err
	})
	if err != nil {
		r.reply("cost charts failed", err.Error())
		return
	}
	r.reply("cost charts", charts.Text())
}
