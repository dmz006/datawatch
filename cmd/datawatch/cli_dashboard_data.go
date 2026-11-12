// cli_dashboard_data.go — read-only CLI for the dashboard data endpoints:
//
//	datawatch dashboard work-map                       GET /api/dashboard/work-map
//	datawatch dashboard lane-pulse [--window 6h]       GET /api/dashboard/lane-pulse
//	datawatch dashboard series <name> [--window|--days|--limit]
//	                                                   GET /api/dashboard/series/<name>
//
// Each prints the endpoint JSON, the same body the REST route returns.

package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// dashboardSeriesNames are the six series endpoints.
var dashboardSeriesNames = []string{
	"lane-utilization", "throughput-forecast", "verifier-health",
	"release-train", "review-backlog", "defects-per-day",
}

func printDashboardData(path string, q url.Values) error {
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	body, err := daemonJSONBody(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	prettyPrint(body)
	return nil
}

func newDashboardWorkMapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "work-map",
		Short: "Work map of projects, Automata, tasks and sessions (GET /api/dashboard/work-map)",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return printDashboardData("/api/dashboard/work-map", nil) },
	}
}

func newDashboardLanePulseCmd() *cobra.Command {
	var window string
	cmd := &cobra.Command{
		Use:   "lane-pulse",
		Short: "Lane pulse traces and burn panel (GET /api/dashboard/lane-pulse)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			q := url.Values{}
			if window != "" {
				q.Set("window", window)
			}
			return printDashboardData("/api/dashboard/lane-pulse", q)
		},
	}
	cmd.Flags().StringVar(&window, "window", "", "time window, e.g. 6h (default) up to 7 days")
	return cmd
}

func newDashboardSeriesCmd() *cobra.Command {
	var window string
	var days, limit int
	cmd := &cobra.Command{
		Use:   "series <name>",
		Short: "One dashboard data series (" + strings.Join(dashboardSeriesNames, ", ") + ")",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			ok := false
			for _, n := range dashboardSeriesNames {
				ok = ok || n == name
			}
			if !ok {
				return fmt.Errorf("unknown series %q: use one of %s", name, strings.Join(dashboardSeriesNames, ", "))
			}
			q := url.Values{}
			if window != "" {
				q.Set("window", window)
			}
			if days > 0 {
				q.Set("days", fmt.Sprint(days))
			}
			if limit > 0 {
				q.Set("limit", fmt.Sprint(limit))
			}
			return printDashboardData("/api/dashboard/series/"+name, q)
		},
	}
	cmd.Flags().StringVar(&window, "window", "", "duration for lane-utilization and verifier-health, e.g. 24h")
	cmd.Flags().IntVar(&days, "days", 0, "whole days for throughput-forecast, release-train and defects-per-day")
	cmd.Flags().IntVar(&limit, "limit", 0, "row limit for review-backlog")
	return cmd
}
