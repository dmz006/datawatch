// `datawatch cost charts` — the combined usage report: the cost-over-time
// series, the tool-usage series and the retries-and-failure-categories series
// in one payload and one text rendering. Like `cost report` this is a thin
// REST proxy: it reads GET /api/cost/series, GET /api/cost/tool-usage and
// GET /api/cost/failures from the running daemon and joins them with the
// shared costreport helpers, the same ones the comm channel verb and the MCP
// tool cost_charts use. Nothing here computes a series.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/costreport"
)

// newCostChartsCmd builds `datawatch cost charts`.
func newCostChartsCmd() *cobra.Command {
	var groupBy, window string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "charts",
		Short: "Combined usage report: cost over time, tool usage, retries and failures",
		Long: "Reads the three usage series from the daemon and prints them as one report.\n\n" +
			"  --group-by  " + strings.Join(costreport.ChartsGroupBys, " | ") + " (default: each view's own default)\n" +
			"  --window    how far back from now: 36h, 7d, 2w (default: the cost_report default window)\n" +
			"  --json      print the combined payload as JSON instead of text\n\n" +
			"Cost over time and tool usage group by automaton, project or model; retries and\n" +
			"failures group by project, automation or automaton. A grouping a view does not\n" +
			"know leaves that view on its default, and the report says so.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			charts, err := fetchCostCharts(costreport.ChartsRequest{GroupBy: groupBy, Window: window})
			if err != nil {
				return err
			}
			if asJSON {
				out, err := json.MarshalIndent(charts, "", "  ")
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), charts.Text())
			return nil
		},
	}
	cmd.Flags().StringVar(&groupBy, "group-by", "", "group by "+strings.Join(costreport.ChartsGroupBys, ", "))
	cmd.Flags().StringVar(&window, "window", "", "how far back from now (36h, 7d, 2w)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the combined payload as JSON")
	return cmd
}

// fetchCostCharts reads the three REST views from the daemon and joins them.
func fetchCostCharts(req costreport.ChartsRequest) (*costreport.Charts, error) {
	return costreport.FetchCharts(req, func(path string, q url.Values) ([]byte, error) {
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		return daemonJSONBody(http.MethodGet, path, nil)
	})
}
