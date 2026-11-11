// `datawatch cost report` — Sprint Sx parity CLI subcommand for
// GET /api/cost/report (internal/costreport): the grouped token + USD
// cost report. Like the sibling Sx commands in cli_sx_parity.go this is
// a thin REST proxy: it locates the running daemon, forwards the
// --group-by / --since / --until flags as query parameters, and renders
// the report. The report engine itself lives in the daemon — nothing
// here duplicates that logic. Rows may also carry the owning provider's
// billing mode (usage.billing): bundled-plan rows show an API-equivalent
// estimate (not a bill) with the usage points it represents and their
// share of the monthly price; group_by release carries shipped-unit
// counts.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// newCostReportCmd builds `datawatch cost report`.
func newCostReportCmd() *cobra.Command {
	var groupBy, since, until, format string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Grouped token + USD cost report",
		Long: "Asks the daemon for the grouped cost report (GET /api/cost/report).\n\n" +
			"  --group-by  automaton | automation | backend | release | week (default: cost_report.default_group_by config)\n" +
			"  --since     window lower bound, RFC3339 or YYYY-MM-DD\n" +
			"  --until     window upper bound, RFC3339 or YYYY-MM-DD\n" +
			"  --format    text (table, default) or json\n\n" +
			"Rows carry the owning provider's billing mode where usage.billing is\n" +
			"configured: bundled plans read as an API-equivalent estimate (a bundled\n" +
			"plan bills the subscription, not this usage), with the usage points it\n" +
			"represents and its share of the monthly price; unset providers are\n" +
			"shown as assumed. group_by release adds shipped-unit counts per\n" +
			"release; work whose owner has not shipped lands under 'unreleased'.",
		RunE: func(*cobra.Command, []string) error {
			q := url.Values{}
			if groupBy != "" {
				q.Set("group_by", groupBy)
			}
			if since != "" {
				q.Set("since", since)
			}
			if until != "" {
				q.Set("until", until)
			}
			path := "/api/cost/report"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			if format == "json" {
				return daemonGet(path)
			}
			body, err := daemonJSONBody(http.MethodGet, path, nil)
			if err != nil {
				return err
			}
			return printCostReport(body)
		},
	}
	cmd.Flags().StringVar(&groupBy, "group-by", "", "group by automaton, automation, backend, release or week")
	cmd.Flags().StringVar(&since, "since", "", "window lower bound (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().StringVar(&until, "until", "", "window upper bound (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text (table) or json")
	return cmd
}

// costReportWire mirrors the JSON shape of costreport.Report / Row
// (see internal/costreport/report.go). Kept local to the CLI so the
// proxy does not pull the engine package into the binary; only the wire
// fields the table + footer need are named here.
type costReportWire struct {
	GroupBy          string          `json:"group_by"`
	Rows             []costReportRow `json:"rows"`
	TotalsByUnit     []costUnitTotal `json:"totals_by_unit"`
	RetentionDays    int             `json:"retention_days"`
	LifetimeFallback bool            `json:"lifetime_fallback"`
	RetentionNote    string          `json:"retention_note"`
	WindowNote       string          `json:"window_note"`
	Attribution      *costAttribWire `json:"attribution"`
}

// costAttribWire mirrors costreport.AttributionCheck.
type costAttribWire struct {
	AutomatonLaunched int      `json:"automaton_launched"`
	Unattributed      int      `json:"unattributed"`
	SessionIDs        []string `json:"session_ids"`
	OK                bool     `json:"ok"`
	Note              string   `json:"note"`
}

// costUnitTotal mirrors costreport.UnitTotal.
type costUnitTotal struct {
	Unit          string   `json:"unit"`
	BillingMode   string   `json:"billing_mode"`
	Sessions      int      `json:"sessions"`
	TokensIn      int      `json:"tokens_in"`
	TokensOut     int      `json:"tokens_out"`
	ActiveSeconds float64  `json:"active_seconds"`
	Dollars       *float64 `json:"dollars"`
	Points        *float64 `json:"points"`
	Percent       *float64 `json:"percent"`
}

// printUnitTotals renders one line per billing unit; dollars, points and
// local usage are never added together.
func printUnitTotals(totals []costUnitTotal) {
	if len(totals) == 0 {
		return
	}
	fmt.Println("totals by unit:")
	for _, t := range totals {
		fig := "tokens and time only"
		switch t.Unit {
		case "dollars":
			fig = "dollars " + moneyOrDash(t.Dollars)
		case "points":
			fig = "points " + numOrDash(t.Points)
			if t.Percent != nil {
				fig += fmt.Sprintf(", %.1f%% of the allowance", *t.Percent)
			}
		}
		fmt.Printf("  %s (%s): %s; sessions %d, tokens in %d, tokens out %d, active %s\n",
			t.Unit, t.BillingMode, fig, t.Sessions, t.TokensIn, t.TokensOut, formatSeconds(t.ActiveSeconds))
	}
}

type costReportRow struct {
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
	ShareOfMonthlyPrice  *float64 `json:"share_of_monthly_price"`
	Units                int      `json:"units"`
	BillingMode          string   `json:"billing_mode"`
	BillingLabel         string   `json:"billing_label"`
}

// printCostReport renders the report as a table. The base columns are
// always key | sessions | tokens_in | tokens_out; the usd column appears
// when at least one row carries a dollar estimate; the api_equiv_usd /
// usage_pts / share_of_price columns appear when any row is a bundled
// plan (they hold the estimate, its usage points and its share of the
// monthly price — an equivalence, not a charge); units appears for
// group_by release; billing carries the row's billing-mode label (e.g.
// "bundled plan — API-equivalent, not billed", "api_metered (assumed —
// no usage.billing entry)"). A missing value prints "-". The footer
// shows the retention explanation (the server's retention_note, else
// the retention days) and the lifetime-fallback flag.
func printCostReport(body []byte) error {
	var rep costReportWire
	if err := json.Unmarshal(body, &rep); err != nil {
		return fmt.Errorf("parse cost report: %w", err)
	}

	var anyUSD, anyAPI, anyUnits, anyBilling bool
	for _, r := range rep.Rows {
		if r.USDEstimate != nil {
			anyUSD = true
		}
		if r.APIEquivalentUSD != nil {
			anyAPI = true
		}
		if r.Units > 0 {
			anyUnits = true
		}
		if r.BillingLabel != "" {
			anyBilling = true
		}
	}
	if rep.GroupBy == "release" {
		anyUnits = true // ships the units column even when every count is 0
	}

	header := "key\tsessions\ttokens_in\ttokens_out\tcache_read\tcache_creation\ttool_calls\tspan\tactive"
	rowFns := []func(costReportRow) string{
		func(r costReportRow) string { return r.Key },
		func(r costReportRow) string { return strconv.Itoa(r.Sessions) },
		func(r costReportRow) string { return strconv.Itoa(r.TokensIn) },
		func(r costReportRow) string { return strconv.Itoa(r.TokensOut) },
		func(r costReportRow) string { return strconv.Itoa(r.CacheRead) },
		func(r costReportRow) string { return strconv.Itoa(r.CacheCreation) },
		func(r costReportRow) string { return strconv.Itoa(r.ToolCalls) },
		func(r costReportRow) string { return formatSeconds(r.SpanSeconds) },
		func(r costReportRow) string { return formatSeconds(r.ActiveSeconds) },
	}
	if anyUSD {
		header += "\tusd"
		rowFns = append(rowFns, func(r costReportRow) string { return moneyOrDash(r.USDEstimate) })
	}
	if anyAPI {
		header += "\tapi_equiv_usd\tusage_pts\tshare_of_price"
		rowFns = append(rowFns,
			func(r costReportRow) string { return moneyOrDash(r.APIEquivalentUSD) },
			func(r costReportRow) string { return numOrDash(r.EstimatedUsagePoints) },
			func(r costReportRow) string { return shareOrDash(r.ShareOfMonthlyPrice) },
		)
	}
	if anyUnits {
		header += "\tunits"
		rowFns = append(rowFns, func(r costReportRow) string { return strconv.Itoa(r.Units) })
	}
	if anyBilling {
		header += "\tbilling"
		rowFns = append(rowFns, func(r costReportRow) string {
			if r.BillingLabel == "" {
				return "-"
			}
			return r.BillingLabel
		})
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, header)
	for _, r := range rep.Rows {
		line := make([]string, 0, len(rowFns))
		for _, f := range rowFns {
			line = append(line, f(r))
		}
		_, _ = fmt.Fprintln(w, strings.Join(line, "\t"))
	}
	//nolint:errcheck
	w.Flush()

	printUnitTotals(rep.TotalsByUnit)
	if anyAPI {
		fmt.Println("api_equiv_usd is an API-equivalent estimate, not a bill")
	}
	for _, r := range rep.Rows {
		if r.WindowNote != "" {
			fmt.Printf("window note (%s): %s\n", r.Key, r.WindowNote)
		}
	}
	if rep.WindowNote != "" {
		fmt.Println("window note:", rep.WindowNote)
	}
	if line := attributionLine(rep.Attribution); line != "" {
		fmt.Println(line)
	}
	if rep.RetentionNote != "" {
		fmt.Println("retention:", rep.RetentionNote)
	} else {
		fmt.Printf("retention: %d days\n", rep.RetentionDays)
	}
	if rep.LifetimeFallback {
		fmt.Println("lifetime fallback: yes (window reaches beyond the retention horizon)")
	} else {
		fmt.Println("lifetime fallback: no")
	}
	return nil
}

func moneyOrDash(f *float64) string {
	if f == nil {
		return "-"
	}
	return fmt.Sprintf("$%.2f", *f)
}

func numOrDash(f *float64) string {
	if f == nil {
		return "-"
	}
	return strconv.FormatFloat(*f, 'f', -1, 64)
}

// shareOrDash renders the row's share of the plan's monthly price as a
// percentage (the wire value is a fraction: 0.05 = 5% of the price).
func shareOrDash(f *float64) string {
	if f == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", *f*100)
}

// formatSeconds renders a duration in seconds as a compact h/m/s string.
func formatSeconds(sec float64) string {
	if sec <= 0 {
		return "0s"
	}
	return (time.Duration(sec) * time.Second).String()
}

// attributionLine renders the attribution check: how many
// Automaton-launched sessions had no automaton id.
func attributionLine(a *costAttribWire) string {
	if a == nil {
		return ""
	}
	state := "ok"
	if !a.OK {
		state = "UNATTRIBUTED"
	}
	line := fmt.Sprintf("attribution check: %s — %d of %d Automaton-launched sessions unattributed", state, a.Unattributed, a.AutomatonLaunched)
	if a.Note != "" {
		line += " (" + a.Note + ")"
	}
	return line
}

// automatonCostLines renders the `cost` object of an Automaton detail
// response for prd-get: sessions, tokens in/out/cache, tool calls, span,
// active time, the API-equivalent estimate and the window note. Returns
// nil when the response carries no cost.
func automatonCostLines(prdJSON []byte) []string {
	var v struct {
		Cost *struct {
			costReportRow
			ReportWindowNote string `json:"report_window_note"`
		} `json:"cost"`
	}
	if err := json.Unmarshal(prdJSON, &v); err != nil || v.Cost == nil {
		return nil
	}
	c := v.Cost
	lines := []string{
		fmt.Sprintf("cost: %d sessions, tokens in/out/cache %d/%d/%d+%d, %d tool calls, span %s, active %s",
			c.Sessions, c.TokensIn, c.TokensOut, c.CacheRead, c.CacheCreation, c.ToolCalls,
			formatSeconds(c.SpanSeconds), formatSeconds(c.ActiveSeconds)),
	}
	est := c.APIEquivalentUSD
	if est == nil {
		est = c.USDEstimate
	}
	if est != nil {
		lines = append(lines, "cost estimate: "+moneyOrDash(est)+" (API-equivalent, not a bill)")
	}
	if c.WindowNote != "" {
		lines = append(lines, "cost window note: "+c.WindowNote)
	}
	if c.ReportWindowNote != "" {
		lines = append(lines, "cost window note: "+c.ReportWindowNote)
	}
	return lines
}
