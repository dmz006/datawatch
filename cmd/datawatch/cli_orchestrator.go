// BL117 (v4.0.0) — `datawatch orchestrator ...` CLI subcommand.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

func newOrchestratorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "PRD-DAG orchestrator with guardrail sub-agents",
		Long: `Compose autonomous PRDs into a graph and run each under a
guardrail sub-agent overlay (rules, security, release-readiness,
docs-diagrams-architecture). See docs/api/orchestrator.md.

Subcommands:
  config-get
  config-set <json>
  graph-list
  graph-create <title> <prd_ids_json> [<deps_json>]
  graph-get <id>
  graph-plan <id> [<deps_json>]
  graph-run <id>
  graph-cancel <id>
  verdicts`,
	}
	cmd.AddCommand(
		newOrchestratorConfigGetCmd(),
		newOrchestratorConfigSetCmd(),
		newOrchestratorGraphListCmd(),
		newOrchestratorGraphCreateCmd(),
		newOrchestratorGraphGetCmd(),
		newOrchestratorGraphPlanCmd(),
		newOrchestratorGraphRunCmd(),
		newOrchestratorGraphCancelCmd(),
		newOrchestratorVerdictsCmd(),
	)
	return cmd
}

func newOrchestratorConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use: "config-get", Short: "Read orchestrator config",
		RunE: func(*cobra.Command, []string) error { return daemonGet("/api/orchestrator/config") },
	}
}

func newOrchestratorConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use: "config-set <json>", Short: "Replace orchestrator config (full body)",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var body any
			if err := json.Unmarshal([]byte(args[0]), &body); err != nil {
				return fmt.Errorf("parse JSON: %w", err)
			}
			return daemonJSON(http.MethodPut, "/api/orchestrator/config", body)
		},
	}
}

func newOrchestratorGraphListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "graph-list", Short: "List all graphs newest-first",
		RunE: func(*cobra.Command, []string) error { return daemonGet("/api/orchestrator/graphs") },
	}
}

func newOrchestratorGraphCreateCmd() *cobra.Command {
	var projectDir string
	cmd := &cobra.Command{
		Use:   "graph-create <title> <prd_ids_json> [<deps_json>]",
		Short: "Create a PRD-DAG graph",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(_ *cobra.Command, args []string) error {
			var prdIDs []string
			if err := json.Unmarshal([]byte(args[1]), &prdIDs); err != nil {
				return fmt.Errorf("parse prd_ids JSON: %w", err)
			}
			body := map[string]any{
				"title":       args[0],
				"project_dir": projectDir,
				"prd_ids":     prdIDs,
			}
			if len(args) == 3 {
				var deps map[string][]string
				if err := json.Unmarshal([]byte(args[2]), &deps); err != nil {
					return fmt.Errorf("parse deps JSON: %w", err)
				}
				body["deps"] = deps
			}
			return daemonJSON(http.MethodPost, "/api/orchestrator/graphs", body)
		},
	}
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory")
	return cmd
}

func newOrchestratorGraphGetCmd() *cobra.Command {
	return &cobra.Command{
		Use: "graph-get <id>", Short: "Fetch one graph with nodes + verdicts",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/orchestrator/graphs/" + args[0])
		},
	}
}

func newOrchestratorGraphPlanCmd() *cobra.Command {
	return &cobra.Command{
		Use: "graph-plan <id> [<deps_json>]", Short: "Rebuild node tree for a graph",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{}
			if len(args) == 2 {
				var deps map[string][]string
				if err := json.Unmarshal([]byte(args[1]), &deps); err != nil {
					return fmt.Errorf("parse deps JSON: %w", err)
				}
				body["deps"] = deps
			}
			return daemonJSON(http.MethodPost, "/api/orchestrator/graphs/"+args[0]+"/plan", body)
		},
	}
}

func newOrchestratorGraphRunCmd() *cobra.Command {
	return &cobra.Command{
		Use: "graph-run <id>", Short: "Kick the runner (fire-and-forget)",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/orchestrator/graphs/"+args[0]+"/run", nil)
		},
	}
}

func newOrchestratorGraphCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use: "graph-cancel <id>", Short: "Cancel + archive a graph",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/orchestrator/graphs/"+args[0], nil)
		},
	}
}

func newOrchestratorVerdictsCmd() *cobra.Command {
	return &cobra.Command{
		Use: "verdicts", Short: "List guardrail verdicts across all graphs",
		RunE: func(*cobra.Command, []string) error { return daemonGet("/api/orchestrator/verdicts") },
	}
}
