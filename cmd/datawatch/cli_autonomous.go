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
  status                          Loop status (running, active PRDs, queued/running tasks)
  config-get                      Read current autonomous config
  config-set <json>               Replace autonomous config (full body)
  prd-list                        List all PRDs
  prd-create <spec>               Create a draft PRD (--type, --guided-mode, --skills)
  prd-get <id>                    Fetch one PRD with story+task tree
  prd-decompose <id>              Run the LLM decomposition for a PRD
  prd-run <id>                    Kick the executor for a PRD
  prd-cancel <id>                 Cancel + archive a PRD
  prd-delete <id>                 Hard-delete a PRD + descendants
  prd-edit <id>                   Edit PRD title + spec
  prd-approve <id>                Approve decomposed PRD
  prd-reject <id>                 Reject a PRD decomposition
  prd-request-revision <id>       Request fresh decomposition
  prd-edit-task <id>              Rewrite a task spec before approving
  prd-set-type <id> <type>        Set automaton type (BL221 Phase 4)
  prd-set-guided-mode <id> on|off Enable/disable Guided Mode (BL221 Phase 4)
  prd-set-skills <id> <csv>       Assign skills to PRD (BL221 Phase 4)
  prd-scan <id>                   Trigger security scan (BL221 Phase 3)
  prd-scan-result <id>            Get latest scan result (BL221 Phase 3)
  prd-scan-fix <id>               Create fix child PRD (BL221 Phase 3b)
  prd-scan-rules <id>             Propose AGENT.md rule edits (BL221 Phase 3b)
  scan-config-get                 Read scan config (BL221 Phase 3)
  scan-config-set <k=v,...>       Update scan config fields (BL221 Phase 3)
  types-list                      List automaton type registry (BL221 Phase 4)
  type-register                   Register or update an automaton type (BL221 Phase 4)
  template-list                   List template store (BL221 Phase 2)
  template-create                 Create a template (BL221 Phase 2)
  template-get <id>               Fetch one template (BL221 Phase 2)
  template-update <id>            Update a template (BL221 Phase 2)
  template-delete <id>            Delete a template (BL221 Phase 2)
  template-instantiate <id>       Create PRD from template (BL221 Phase 2)
  template-clone <prd-id>         Clone PRD to template (BL221 Phase 2)
  prd-instantiate <tmpl-id>       Instantiate legacy YAML template (BL191)
  learnings                       List extracted post-task learnings`,
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
		newAutonomousPRDDeleteCmd(),
		newAutonomousPRDEditCmd(),
		// BL221 Phase 3 — scan framework.
		newAutonomousPRDScanCmd(),
		newAutonomousPRDScanResultCmd(),
		newAutonomousPRDScanFixCmd(),
		newAutonomousPRDScanRulesCmd(),
		newAutonomousScanConfigGetCmd(),
		newAutonomousScanConfigSetCmd(),
		// BL221 Phase 4 — type registry, Guided Mode, skills.
		newAutonomousPRDSetTypeCmd(),
		newAutonomousPRDSetGuidedModeCmd(),
		newAutonomousPRDSetSkillsCmd(),
		newAutonomousTypesListCmd(),
		newAutonomousTypeRegisterCmd(),
		// BL221 Phase 2 — template store.
		newAutonomousTemplateListCmd(),
		newAutonomousTemplateCreateCmd(),
		newAutonomousTemplateGetCmd(),
		newAutonomousTemplateUpdateCmd(),
		newAutonomousTemplateDeleteCmd(),
		newAutonomousTemplateInstantiateCmd(),
		newAutonomousTemplateCloneCmd(),
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
	var projectDir, backend, effort, typ, skillsCSV string
	var guidedMode bool
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
			if typ != "" {
				body["type"] = typ
			}
			if guidedMode {
				body["guided_mode"] = true
			}
			if skillsCSV != "" {
				var skills []string
				for _, s := range strings.Split(skillsCSV, ",") {
					if s = strings.TrimSpace(s); s != "" {
						skills = append(skills, s)
					}
				}
				body["skills"] = skills
			}
			return daemonJSON(http.MethodPost, "/api/autonomous/prds", body)
		},
	}
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory (default: cwd)")
	cmd.Flags().StringVar(&backend, "backend", "", "LLM backend override")
	cmd.Flags().StringVar(&effort, "effort", "", "Effort hint (low/medium/high/max)")
	cmd.Flags().StringVar(&typ, "type", "", "Automaton type (software|research|operational|personal or custom)")
	cmd.Flags().BoolVar(&guidedMode, "guided-mode", false, "Enable Guided Mode (step-by-step operator checkpoints)")
	cmd.Flags().StringVar(&skillsCSV, "skills", "", "Comma-separated skill IDs (e.g. git,docker,pytest)")
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

// v5.19.0 — full CRUD: prd-delete (hard) + prd-edit (title/spec).
func newAutonomousPRDDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-delete <id>",
		Short: "Permanently remove a PRD + its SpawnPRD descendants (use prd-cancel for reversible cancel)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/autonomous/prds/"+args[0]+"?hard=true", nil)
		},
	}
}

func newAutonomousPRDEditCmd() *cobra.Command {
	var title, spec string
	cmd := &cobra.Command{
		Use:   "prd-edit <id>",
		Short: "Edit PRD title + spec (non-running PRDs only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{"actor": "operator"}
			if title != "" {
				body["title"] = title
			}
			if spec != "" {
				body["spec"] = spec
			}
			return daemonJSON(http.MethodPatch, "/api/autonomous/prds/"+args[0], body)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "New PRD title")
	cmd.Flags().StringVar(&spec, "spec", "", "New PRD spec (full replacement)")
	return cmd
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

// BL221 (v6.2.0) Phase 3 — scan framework CLI commands.

func newAutonomousPRDScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-scan <id>",
		Short: "Trigger security scan on a PRD (SAST + secrets + deps)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/scan", nil)
		},
	}
}

func newAutonomousPRDScanResultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-scan-result <id>",
		Short: "Get the latest scan result for a PRD",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/autonomous/prds/" + args[0] + "/scan")
		},
	}
}

func newAutonomousPRDScanFixCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-scan-fix <id>",
		Short: "Create a child fix PRD from scan violations",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/scan/fix", nil)
		},
	}
}

func newAutonomousPRDScanRulesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-scan-rules <id>",
		Short: "Propose AGENT.md rule edits to prevent recurrence of scan findings",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/scan/rules", nil)
		},
	}
}

func newAutonomousScanConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan-config-get",
		Short: "Read the autonomous scan configuration",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/scan/config") },
	}
}

func newAutonomousScanConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan-config-set <key=value,...>",
		Short: "Update scan config fields (enabled, sast_enabled, secrets_enabled, deps_enabled, fail_on_severity, max_findings, grader_enabled, fix_loop_enabled, fix_loop_max_retries)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{}
			for _, kv := range args {
				if i := strings.IndexByte(kv, '='); i > 0 {
					k, v := strings.TrimSpace(kv[:i]), strings.TrimSpace(kv[i+1:])
					var parsed any
					if err := json.Unmarshal([]byte(v), &parsed); err == nil {
						body[k] = parsed
					} else {
						body[k] = v
					}
				}
			}
			return daemonJSON(http.MethodPut, "/api/autonomous/scan/config", body)
		},
	}
}

// BL221 (v6.2.0) Phase 4 — type registry, Guided Mode, skills CLI commands.

func newAutonomousPRDSetTypeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-set-type <id> <type>",
		Short: "Set the automaton type on a PRD (software|research|operational|personal or custom)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"type": args[1]})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_type", body)
		},
	}
}

func newAutonomousPRDSetGuidedModeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-set-guided-mode <id> <on|off>",
		Short: "Enable or disable Guided Mode on a PRD",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			enabled := args[1] == "on" || args[1] == "true" || args[1] == "1"
			body, _ := json.Marshal(map[string]bool{"guided_mode": enabled})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_guided_mode", body)
		},
	}
}

func newAutonomousPRDSetSkillsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-set-skills <id> <skill1,skill2,...>",
		Short: "Assign skills to a PRD (passed to spawned task sessions)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			var skills []string
			for _, s := range strings.Split(args[1], ",") {
				if s = strings.TrimSpace(s); s != "" {
					skills = append(skills, s)
				}
			}
			body, _ := json.Marshal(map[string]any{"skills": skills})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_skills", body)
		},
	}
}

func newAutonomousTypesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "types-list",
		Short: "List the automaton type registry (builtins + operator-registered)",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/types") },
	}
}

func newAutonomousTypeRegisterCmd() *cobra.Command {
	var label, description, color string
	cmd := &cobra.Command{
		Use:   "type-register <id>",
		Short: "Register or update an automaton type in the registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{
				"id":          args[0],
				"label":       label,
				"description": description,
				"color":       color,
			})
			return daemonJSON(http.MethodPost, "/api/autonomous/types", body)
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "Human-readable label (required)")
	cmd.Flags().StringVar(&description, "description", "", "Optional description")
	cmd.Flags().StringVar(&color, "color", "", "Optional CSS color for badges (e.g. #6366f1)")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

// BL221 (v6.2.0) Phase 2 — template store CRUD CLI commands.

func newAutonomousTemplateListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "template-list",
		Short: "List all templates in the automaton template store",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/templates") },
	}
}

func newAutonomousTemplateCreateCmd() *cobra.Command {
	var title, spec, description, typ, tagsCSV string
	cmd := &cobra.Command{
		Use:   "template-create",
		Short: "Create a new automaton template",
		RunE: func(_ *cobra.Command, _ []string) error {
			var tags []string
			for _, t := range strings.Split(tagsCSV, ",") {
				if t = strings.TrimSpace(t); t != "" {
					tags = append(tags, t)
				}
			}
			body, _ := json.Marshal(map[string]any{
				"title":       title,
				"spec":        spec,
				"description": description,
				"type":        typ,
				"tags":        tags,
			})
			return daemonJSON(http.MethodPost, "/api/autonomous/templates", body)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "Template title (required)")
	cmd.Flags().StringVar(&spec, "spec", "", "Template spec text (required)")
	cmd.Flags().StringVar(&description, "description", "", "Optional description")
	cmd.Flags().StringVar(&typ, "type", "", "Automaton type")
	cmd.Flags().StringVar(&tagsCSV, "tags", "", "Comma-separated tag list")
	_ = cmd.MarkFlagRequired("title")
	_ = cmd.MarkFlagRequired("spec")
	return cmd
}

func newAutonomousTemplateGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "template-get <id>",
		Short: "Fetch one automaton template",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/autonomous/templates/" + args[0])
		},
	}
}

func newAutonomousTemplateUpdateCmd() *cobra.Command {
	var title, spec, description, typ, tagsCSV string
	cmd := &cobra.Command{
		Use:   "template-update <id>",
		Short: "Update an existing automaton template",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var tags []string
			for _, t := range strings.Split(tagsCSV, ",") {
				if t = strings.TrimSpace(t); t != "" {
					tags = append(tags, t)
				}
			}
			body, _ := json.Marshal(map[string]any{
				"title":       title,
				"spec":        spec,
				"description": description,
				"type":        typ,
				"tags":        tags,
			})
			return daemonJSON(http.MethodPut, "/api/autonomous/templates/"+args[0], body)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "New title (empty = keep existing)")
	cmd.Flags().StringVar(&spec, "spec", "", "New spec (empty = keep existing)")
	cmd.Flags().StringVar(&description, "description", "", "New description")
	cmd.Flags().StringVar(&typ, "type", "", "New automaton type")
	cmd.Flags().StringVar(&tagsCSV, "tags", "", "New comma-separated tag list")
	return cmd
}

func newAutonomousTemplateDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "template-delete <id>",
		Short: "Permanently delete an automaton template",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/autonomous/templates/"+args[0], nil)
		},
	}
}

func newAutonomousTemplateInstantiateCmd() *cobra.Command {
	var projectDir, backend, effort, varsCSV string
	cmd := &cobra.Command{
		Use:   "template-instantiate <id>",
		Short: "Create a new PRD from an automaton template (template store version)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if projectDir == "" {
				projectDir, _ = os.Getwd()
			}
			vars := map[string]string{}
			for _, kv := range strings.Split(varsCSV, ",") {
				if i := strings.IndexByte(kv, '='); i > 0 {
					vars[strings.TrimSpace(kv[:i])] = strings.TrimSpace(kv[i+1:])
				}
			}
			body, _ := json.Marshal(map[string]any{
				"project_dir": projectDir,
				"vars":        vars,
				"backend":     backend,
				"effort":      effort,
				"actor":       "operator",
			})
			return daemonJSON(http.MethodPost, "/api/autonomous/templates/"+args[0]+"/instantiate", body)
		},
	}
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory (default: cwd)")
	cmd.Flags().StringVar(&backend, "backend", "", "LLM backend override")
	cmd.Flags().StringVar(&effort, "effort", "", "Effort hint (low/medium/high/max)")
	cmd.Flags().StringVar(&varsCSV, "vars", "", "Comma-separated k=v variable substitutions")
	return cmd
}

func newAutonomousTemplateCloneCmd() *cobra.Command {
	var description string
	cmd := &cobra.Command{
		Use:   "template-clone <prd-id>",
		Short: "Save a completed PRD as a reusable template in the template store",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"description": description, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/clone_to_template", body)
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "Optional description for the new template")
	return cmd
}
