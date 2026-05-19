// S14b — `datawatch alert-rules ...` CLI subcommand.

package main

import (
	"net/http"
	"strconv"

	"github.com/spf13/cobra"
)

func newAlertRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alert-rules",
		Short: "Per-pod alert rules and observer-driven autoscaling",
		Long: `Manage per-pod alert rules.

Rules evaluate observer envelope metrics every 30 s and fire an action
when the configured condition is met and the cooldown has elapsed.

Supported metrics:
  cpu_pct        CPU utilisation (0–100)
  gpu_pct        GPU utilisation (0–100)
  rss_bytes      Resident set size in bytes
  net_rx_bps     Network receive bytes/sec
  net_tx_bps     Network transmit bytes/sec

Supported operators: > < >= <=

Supported actions: alert | scale_up | scale_down

Subcommands:
  list                   List all rules
  get <name>             Fetch one rule
  add                    Create a new rule (see flags)
  update <name>          Replace a rule (see flags)
  delete <name>          Remove a rule
  enable <name>          Enable a rule
  disable <name>         Disable a rule
  firings                Show the last 100 firings`,
	}
	cmd.AddCommand(
		newAlertRulesListCmd(),
		newAlertRulesGetCmd(),
		newAlertRulesAddCmd(),
		newAlertRulesUpdateCmd(),
		newAlertRulesDeleteCmd(),
		newAlertRulesEnableCmd(),
		newAlertRulesDisableCmd(),
		newAlertRulesFiringsCmd(),
	)
	return cmd
}

func newAlertRulesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all alert rules",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/alert-rules") },
	}
}

func newAlertRulesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Fetch one alert rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/alert-rules/" + args[0])
		},
	}
}

func newAlertRulesAddCmd() *cobra.Command {
	var (
		metric          string
		operator        string
		threshold       float64
		sourceFilter    string
		window          int
		actionKind      string
		scaleTarget     string
		scaleAmount     int
		cooldown        int
		description     string
		enabled         bool
	)
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a new alert rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{
				"name":        args[0],
				"description": description,
				"enabled":     enabled,
				"condition": map[string]any{
					"metric":    metric,
					"operator":  operator,
					"threshold": threshold,
				},
				"action": map[string]any{
					"kind":         actionKind,
					"scale_target": scaleTarget,
					"scale_amount": scaleAmount,
				},
				"source_filter":    sourceFilter,
				"window_seconds":   window,
				"cooldown_seconds": cooldown,
			}
			return daemonJSON(http.MethodPost, "/api/alert-rules", body)
		},
	}
	cmd.Flags().StringVar(&metric, "condition-metric", "cpu_pct", "Metric to monitor (cpu_pct|mem_pct|gpu_pct|rss_bytes|net_rx_bps|net_tx_bps)")
	cmd.Flags().StringVar(&operator, "condition-operator", ">", "Comparison operator (>|<|>=|<=)")
	cmd.Flags().Float64Var(&threshold, "condition-threshold", 80, "Threshold value")
	cmd.Flags().StringVar(&sourceFilter, "source-filter", "", "Only evaluate envelopes with this Source (empty=all)")
	cmd.Flags().IntVar(&window, "window", 60, "Evaluation window in seconds")
	cmd.Flags().StringVar(&actionKind, "action-kind", "alert", "Action to fire (alert|scale_up|scale_down)")
	cmd.Flags().StringVar(&scaleTarget, "scale-target", "", "Compute node name for scale actions")
	cmd.Flags().IntVar(&scaleAmount, "scale-amount", 1, "Number of instances to add/remove")
	cmd.Flags().IntVar(&cooldown, "cooldown", 300, "Minimum seconds between firings")
	cmd.Flags().StringVar(&description, "description", "", "Human-readable description")
	cmd.Flags().BoolVar(&enabled, "enabled", true, "Whether the rule starts enabled")
	return cmd
}

func newAlertRulesUpdateCmd() *cobra.Command {
	var (
		metric       string
		operator     string
		threshold    string
		sourceFilter string
		window       int
		actionKind   string
		scaleTarget  string
		scaleAmount  int
		cooldown     int
		description  string
		enabled      string
	)
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Replace an existing alert rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{
				"name":        args[0],
				"description": description,
				"condition": map[string]any{
					"metric":   metric,
					"operator": operator,
				},
				"action": map[string]any{
					"kind":         actionKind,
					"scale_target": scaleTarget,
					"scale_amount": scaleAmount,
				},
				"source_filter":    sourceFilter,
				"window_seconds":   window,
				"cooldown_seconds": cooldown,
			}
			if threshold != "" {
				if v, err := strconv.ParseFloat(threshold, 64); err == nil {
					body["condition"].(map[string]any)["threshold"] = v
				}
			}
			if enabled != "" {
				body["enabled"] = enabled == "true" || enabled == "1" || enabled == "yes"
			}
			return daemonJSON(http.MethodPut, "/api/alert-rules/"+args[0], body)
		},
	}
	cmd.Flags().StringVar(&metric, "condition-metric", "", "Metric to monitor")
	cmd.Flags().StringVar(&operator, "condition-operator", "", "Comparison operator")
	cmd.Flags().StringVar(&threshold, "condition-threshold", "", "Threshold value")
	cmd.Flags().StringVar(&sourceFilter, "source-filter", "", "Source filter")
	cmd.Flags().IntVar(&window, "window", 0, "Evaluation window in seconds")
	cmd.Flags().StringVar(&actionKind, "action-kind", "", "Action kind")
	cmd.Flags().StringVar(&scaleTarget, "scale-target", "", "Compute node name")
	cmd.Flags().IntVar(&scaleAmount, "scale-amount", 0, "Scale amount")
	cmd.Flags().IntVar(&cooldown, "cooldown", 0, "Cooldown seconds")
	cmd.Flags().StringVar(&description, "description", "", "Description")
	cmd.Flags().StringVar(&enabled, "enabled", "", "true/false")
	return cmd
}

func newAlertRulesDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an alert rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/alert-rules/"+args[0], nil)
		},
	}
}

func newAlertRulesEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <name>",
		Short: "Enable an alert rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/alert-rules/"+args[0]+"/enable", nil)
		},
	}
}

func newAlertRulesDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <name>",
		Short: "Disable an alert rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/alert-rules/"+args[0]+"/disable", nil)
		},
	}
}

func newAlertRulesFiringsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "firings",
		Short: "Show the last 100 rule firings",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/alert-rules/firings") },
	}
}
