// BL24+BL25 (v3.10.0) — `datawatch autonomous ...` CLI subcommand
// surface. Thin REST proxies via daemonGet/daemonJSON.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

func newAutonomousCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autonomous",
		Short: "Autonomous PRD decomposition (BL24) + verification (BL25)",
		Long: `LLM-driven Product Requirements Document → Stories → Tasks decomposition,
each Task spawned as a worker session with independent verification.

Subcommands:
  status                    Loop status (running, active PRDs, queued/running tasks)
  config-get                Read current autonomous config
  config-set <json>         Replace autonomous config (full body)
  prd-list                  List all PRDs
  prd-create <spec>         Create a draft PRD
  prd-get <id>              Fetch one PRD with story+task tree
  prd-decompose <id>        Run the LLM decomposition for a PRD
  prd-run <id>              Kick the executor for a PRD
  prd-cancel <id>           Cancel + archive a PRD
  learnings                 List extracted post-task learnings`,
	}
	cmd.AddCommand(
		newAutonomousStatusCmd(),
		newAutonomousConfigGetCmd(),
		newAutonomousConfigSetCmd(),
		newAutonomousPRDListCmd(),
		newAutonomousPRDCreateCmd(),
		newAutonomousPRDGetCmd(),
		newAutonomousPRDDecomposeCmd(),
		newAutonomousPRDRunCmd(),
		newAutonomousPRDCancelCmd(),
		newAutonomousLearningsCmd(),
	)
	return cmd
}

func newAutonomousStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Loop status snapshot",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/status") },
	}
}

func newAutonomousConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config-get",
		Short: "Read autonomous config",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/config") },
	}
}

func newAutonomousConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config-set <json-or-@file>",
		Short: "Replace autonomous config (full body)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			raw := args[0]
			if len(raw) > 1 && raw[0] == '@' {
				b, err := os.ReadFile(raw[1:])
				if err != nil {
					return err
				}
				raw = string(b)
			}
			var body any
			if err := json.Unmarshal([]byte(raw), &body); err != nil {
				return fmt.Errorf("parse JSON: %w", err)
			}
			return daemonJSON(http.MethodPut, "/api/autonomous/config", body)
		},
	}
}

func newAutonomousPRDListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-list",
		Short: "List all PRDs (newest first)",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/prds") },
	}
}

func newAutonomousPRDCreateCmd() *cobra.Command {
	var projectDir, backend, effort string
	cmd := &cobra.Command{
		Use:   "prd-create <spec>",
		Short: "Create a draft PRD from a feature description",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			spec := joinArgs(args)
			if projectDir == "" {
				projectDir, _ = os.Getwd()
			}
			body := map[string]any{
				"spec":        spec,
				"project_dir": projectDir,
				"backend":     backend,
				"effort":      effort,
			}
			return daemonJSON(http.MethodPost, "/api/autonomous/prds", body)
		},
	}
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory (default: cwd)")
	cmd.Flags().StringVar(&backend, "backend", "", "LLM backend override")
	cmd.Flags().StringVar(&effort, "effort", "", "Effort hint (low/medium/high/max)")
	return cmd
}

func newAutonomousPRDGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-get <id>",
		Short: "Fetch one PRD with its story+task tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/autonomous/prds/" + args[0])
		},
	}
}

func newAutonomousPRDDecomposeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-decompose <id>",
		Short: "Run the LLM decomposition for a PRD",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/decompose", nil)
		},
	}
}

func newAutonomousPRDRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-run <id>",
		Short: "Kick the executor for a PRD",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/run", nil)
		},
	}
}

func newAutonomousPRDCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-cancel <id>",
		Short: "Cancel + archive a PRD",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/autonomous/prds/"+args[0], nil)
		},
	}
}

func newAutonomousLearningsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "learnings",
		Short: "List extracted post-task learnings",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/learnings") },
	}
}
