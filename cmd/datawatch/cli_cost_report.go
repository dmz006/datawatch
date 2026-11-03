// `datawatch cost report` — Sprint Sx parity CLI subcommand for
// GET /api/cost/report (internal/costreport): the grouped token + USD
// cost report. Like the sibling Sx commands in cli_sx_parity.go this is
// a thin REST proxy: it locates the running daemon, forwards the
// --group-by / --since / --until flags as query parameters, and renders
// the report. The report engine itself lives in the daemon — nothing
// here duplicates that logic.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newCostReportCmd builds `datawatch cost report`.
func newCostReportCmd() *cobra.Command {
	var groupBy, since, until, format string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Grouped token + USD cost report",
		Long: "Asks the daemon for the grouped cost report (GET /api/cost/report).\n\n" +
			"  --group-by  automaton | automation | backend | week (default: cost_report.default_group_by config)\n" +
			"  --since     window lower bound, RFC3339 or YYYY-MM-DD\n" +
			"  --until     window upper bound, RFC3339 or YYYY-MM-DD\n" +
			"  --format    text (table, default) or json",
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
	cmd.Flags().StringVar(&groupBy, "group-by", "", "group by automaton, automation, backend or week")
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
	RetentionDays    int             `json:"retention_days"`
	LifetimeFallback bool            `json:"lifetime_fallback"`
}

type costReportRow struct {
	Key         string   `json:"key"`
	Sessions    int      `json:"sessions"`
	TokensIn    int      `json:"tokens_in"`
	TokensOut   int      `json:"tokens_out"`
	USDEstimate *float64 `json:"usd_estimate"`
}

// printCostReport renders the report as a table:
// key | sessions | tokens_in | tokens_out | usd — the usd column only
// appears when at least one row carries a dollar estimate, and a row
// prints "-" there when the server omitted it (no rates configured).
// The footer shows the retention days and whether the server fell back
// to lifetime totals for the window.
func printCostReport(body []byte) error {
	var rep costReportWire
	if err := json.Unmarshal(body, &rep); err != nil {
		return fmt.Errorf("parse cost report: %w", err)
	}

	anyUSD := false
	for _, r := range rep.Rows {
		if r.USDEstimate != nil {
			anyUSD = true
			break
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if anyUSD {
		fmt.Fprintln(w, "key\t sessions\t tokens_in\t tokens_out\t usd")
	} else {
		fmt.Fprintln(w, "key\t sessions\t tokens_in\t tokens_out")
	}
	for _, r := range rep.Rows {
		if anyUSD {
			usd := "-"
			if r.USDEstimate != nil {
				usd = fmt.Sprintf("$%.2f", *r.USDEstimate)
			}
			fmt.Fprintf(w, "%s\t %d\t %d\t %d\t %s\n", r.Key, r.Sessions, r.TokensIn, r.TokensOut, usd)
		} else {
			fmt.Fprintf(w, "%s\t %d\t %d\t %d\n", r.Key, r.Sessions, r.TokensIn, r.TokensOut)
		}
	}
	//nolint:errcheck
	w.Flush()

	fmt.Printf("retention: %d days\n", rep.RetentionDays)
	if rep.LifetimeFallback {
		fmt.Println("lifetime fallback: yes (window reaches beyond the retention horizon)")
	} else {
		fmt.Println("lifetime fallback: no")
	}
	return nil
}
