// `datawatch budget status [--llm NAME]` and the budget flags of `llm add` /
// `llm update`. Same data and field names as GET /api/budget, the MCP
// budget_status tool and the comm-channel `budget` command.

package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/inference"
)

func newBudgetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "budget",
		Short: "Show per-LLM dollar budgets",
		Long: `A budget caps the dollars an LLM may spend inside a rolling or weekly window.
Set one with the --budget-* flags of "llm add" / "llm update".`,
	}
	var llm string
	status := &cobra.Command{
		Use:   "status",
		Short: "Show spend, ceiling, window end and rollover time for budgeted LLMs",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			path := "/api/budget"
			if name := strings.TrimSpace(llm); name != "" {
				path += "?llm=" + url.QueryEscape(name)
			}
			body, err := daemonJSONBody(http.MethodGet, path, nil)
			if err != nil {
				return err
			}
			out, err := autonomous.FormatBudgetText(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(c.OutOrStdout(), out)
			return err
		},
	}
	status.Flags().StringVar(&llm, "llm", "", "show only this LLM (error when it has no budget)")
	cmd.AddCommand(status)
	return cmd
}

// addLLMBudgetFlags registers the budget flags shared by `llm add` and
// `llm update`.
func addLLMBudgetFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.Float64("budget-ceiling-usd", 0, "dollar ceiling for one budget window (> 0); needs --budget-hours or --budget-weekday")
	f.Int("budget-hours", 0, "rolling budget window of this many trailing hours (1-720); exclusive with --budget-weekday")
	f.String("budget-weekday", "", "weekly budget window anchor day, monday..sunday; exclusive with --budget-hours")
	f.String("budget-time", "", "weekly anchor clock time, 24h HH:MM (default 00:00)")
	f.String("budget-zone", "", "IANA zone the weekly anchor is read in (default UTC)")
	f.Bool("budget-allocation-pct", false, "also report an estimated share of the plan-tier allocation (an estimate that never blocks)")
	f.String("budget-peer-window", "", "comma-separated federation peers whose spend counts toward the ceiling (off by default)")
	f.Bool("clear-budget", false, "remove this LLM's budget")
}

var llmBudgetFlagNames = []string{
	"budget-ceiling-usd", "budget-hours", "budget-weekday", "budget-time",
	"budget-zone", "budget-allocation-pct", "budget-peer-window",
}

// llmBudgetFromFlags builds the "budget" member of an LLM request body from
// the budget flags. set is false when no budget flag was passed, so an update
// keeps the stored budget; --clear-budget yields a nil value with set true,
// which clears it. Any other combination is validated with
// BudgetWindow.Validate before a request is sent.
func llmBudgetFromFlags(f *pflag.FlagSet) (value any, set bool, err error) {
	var changed []string
	for _, name := range llmBudgetFlagNames {
		if f.Changed(name) {
			changed = append(changed, "--"+name)
		}
	}
	if clear, _ := f.GetBool("clear-budget"); clear {
		if len(changed) > 0 {
			return nil, false, fmt.Errorf("--clear-budget cannot be combined with %s", strings.Join(changed, ", "))
		}
		return nil, true, nil
	}
	if len(changed) == 0 {
		return nil, false, nil
	}
	bw := inference.BudgetWindow{}
	bw.CeilingUSD, _ = f.GetFloat64("budget-ceiling-usd")
	bw.Hours, _ = f.GetInt("budget-hours")
	bw.Weekday, _ = f.GetString("budget-weekday")
	bw.Time, _ = f.GetString("budget-time")
	bw.Zone, _ = f.GetString("budget-zone")
	bw.AllocationPct, _ = f.GetBool("budget-allocation-pct")
	if csv, _ := f.GetString("budget-peer-window"); strings.TrimSpace(csv) != "" {
		bw.PeerWindow = strings.Split(csv, ",")
		for i := range bw.PeerWindow {
			bw.PeerWindow[i] = strings.TrimSpace(bw.PeerWindow[i])
		}
	}
	if err := bw.Validate(); err != nil {
		return nil, false, fmt.Errorf("invalid budget flags: %w", err)
	}
	return bw, true, nil
}
