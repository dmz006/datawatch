// BL24+BL25 (v3.10.0) — `datawatch autonomous ...` CLI subcommand
// surface. Thin REST proxies via daemonGet/daemonJSON.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newAutonomousCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autonomous",
		Short: "Autonomous PRD decomposition + verification",
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
		newAutonomousPRDApproveCmd(),
		newAutonomousPRDRejectCmd(),
		newAutonomousPRDRequestRevisionCmd(),
		newAutonomousPRDEditTaskCmd(),
		newAutonomousPRDInstantiateCmd(),
		newAutonomousPRDSetLLMCmd(),
		newAutonomousPRDSetTaskLLMCmd(),
		newAutonomousLearningsCmd(),
		newAutonomousPRDChildrenCmd(),
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

// BL191 Q4 (v5.9.0) — recursion: list child PRDs spawned from a parent's
// SpawnPRD tasks. Genealogy tree visibility from CLI without crawling
// the full PRD list.
func newAutonomousPRDChildrenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-children <id>",
		Short: "List child PRDs spawned from a parent PRD's SpawnPRD tasks",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/autonomous/prds/" + args[0] + "/children")
		},
	}
}

// BL191 (v5.2.0) — review/approve/reject/edit-task/instantiate-template.

func newAutonomousPRDApproveCmd() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "prd-approve <id>",
		Short: "Approve a decomposed PRD so Run is allowed",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"actor": "operator", "note": note})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/approve", body)
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "free-form note saved on the Decision row")
	return cmd
}

func newAutonomousPRDRejectCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "prd-reject <id>",
		Short: "Reject a PRD; the decomposition stays for inspection but never runs",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"actor": "operator", "reason": reason})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/reject", body)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "operator-supplied rejection reason")
	return cmd
}

func newAutonomousPRDRequestRevisionCmd() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "prd-request-revision <id>",
		Short: "Ask for a fresh decomposition; status moves back to revisions_asked",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"actor": "operator", "note": note})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/request_revision", body)
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "what's wrong with the current decomposition")
	return cmd
}

func newAutonomousPRDEditTaskCmd() *cobra.Command {
	var taskID, newSpec string
	cmd := &cobra.Command{
		Use:   "prd-edit-task <prd-id> --task <task-id> --spec <new-spec>",
		Short: "Rewrite a task's spec before approving",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"task_id": taskID, "new_spec": newSpec, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/edit_task", body)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID to rewrite (required)")
	cmd.Flags().StringVar(&newSpec, "spec", "", "new task spec text (required)")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("spec")
	return cmd
}

func newAutonomousPRDSetLLMCmd() *cobra.Command {
	var backend, effort, model string
	cmd := &cobra.Command{
		Use:   "prd-set-llm <id>",
		Short: "Set the PRD-level worker LLM (backend / effort / model). Tasks inherit unless overridden.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"backend": backend, "effort": effort, "model": model, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_llm", body)
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "", "LLM backend name (claude / claude-code / ollama / openai / etc.) — empty = inherit global default")
	cmd.Flags().StringVar(&effort, "effort", "", "effort level (low / medium / high / max / quick / normal / thorough) — empty = inherit")
	cmd.Flags().StringVar(&model, "model", "", "specific model name (e.g. claude-3-5-sonnet) — empty = backend default")
	return cmd
}

func newAutonomousPRDSetTaskLLMCmd() *cobra.Command {
	var taskID, backend, effort, model string
	cmd := &cobra.Command{
		Use:   "prd-set-task-llm <prd-id> --task <task-id> [--backend B --effort E --model M]",
		Short: "Override the worker LLM for one task. Empty value clears the override (falls back to PRD then global).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"task_id": taskID, "backend": backend, "effort": effort, "model": model, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_task_llm", body)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID to override (required)")
	cmd.Flags().StringVar(&backend, "backend", "", "task-specific LLM backend; empty = inherit PRD then global")
	cmd.Flags().StringVar(&effort, "effort", "", "effort level for this task; empty = inherit")
	cmd.Flags().StringVar(&model, "model", "", "specific model name for this task; empty = backend default")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newAutonomousPRDInstantiateCmd() *cobra.Command {
	var varsCSV string
	cmd := &cobra.Command{
		Use:   "prd-instantiate <template-id>",
		Short: "Create a fresh PRD from a template (--vars k=v,k=v)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			vars := map[string]string{}
			if varsCSV != "" {
				for _, kv := range strings.Split(varsCSV, ",") {
					if i := strings.IndexByte(kv, '='); i > 0 {
						vars[strings.TrimSpace(kv[:i])] = strings.TrimSpace(kv[i+1:])
					}
				}
			}
			body, _ := json.Marshal(map[string]any{"vars": vars, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/instantiate", body)
		},
	}
	cmd.Flags().StringVar(&varsCSV, "vars", "", "comma-separated k=v list of template variable values")
	return cmd
}
