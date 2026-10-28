// BL24+BL25 (v3.10.0) — `datawatch autonomous ...` CLI subcommand
// surface. Thin REST proxies via daemonGet/daemonJSON.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newAutonomousCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autonomous",
		Short: "Autonomous Automata planning + verification",
		Long: `LLM-driven Product Requirements Document → Stories → Tasks planning,
each Task spawned as a worker session with independent verification.

Subcommands:
  status                          Loop status (running, active PRDs, queued/running tasks)
  config-get                      Read current autonomous config
  config-set <json>               Merge fields into autonomous config, e.g.
                                  config-set '{"max_concurrent_tasks":2}'
                                  config-set '{"max_engines_per_node":2}'
  prd-list                        List all PRDs
  prd-create <spec>               Create a draft PRD (--type, --guided-mode, --skills)
  prd-get <id>                    Fetch one PRD with story+task tree
  prd-plan <id>                   Run the LLM planning phase for a PRD (alias: prd-decompose)
  prd-decompose <id>              Run the LLM planning phase for a PRD (alias: prd-plan)
  prd-run <id>                    Kick the executor for a PRD
  prd-cancel <id>                 Cancel + archive a PRD
  prd-delete <id>                 Hard-delete a PRD + descendants
  prd-edit <id>                   Edit PRD title + spec
  prd-approve <id>                Approve planned PRD
  prd-reject <id>                 Reject a PRD plan
  prd-request-revision <id>       Request fresh planning run
  prd-edit-task <id>              Rewrite a task spec before approving
  prd-edit-story <id>             Rewrite a story's title/description before approving
  prd-add-story <id>              Add a new story without re-running decompose
  prd-remove-story <id>           Remove a story (and its tasks) without re-running decompose
  prd-add-task <id>               Add a new task to an existing story without re-running decompose
  prd-remove-task <id>            Remove one task from a story without re-running decompose
  prd-set-type <id> <type>        Set automaton type (BL221 Phase 4)
  prd-set-guided-mode <id> on|off Enable/disable Guided Mode (BL221 Phase 4)
  prd-set-git <id> on|off         Set AutoPR (+--base-branch override) for a PRD (BL407 Phase 5)
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
		newAutonomousPRDPlanCmd(),
		newAutonomousPRDRunCmd(),
		newAutonomousPRDCancelCmd(),
		newAutonomousPRDApproveCmd(),
		newAutonomousPRDRejectCmd(),
		newAutonomousPRDRequestRevisionCmd(),
		newAutonomousPRDEditTaskCmd(),
		newAutonomousPRDEditStoryCmd(),
		newAutonomousPRDAddStoryCmd(),
		newAutonomousPRDRemoveStoryCmd(),
		newAutonomousPRDAddTaskCmd(),
		newAutonomousPRDRemoveTaskCmd(),
		newAutonomousPRDInstantiateCmd(),
		newAutonomousPRDSetLLMCmd(),
		newAutonomousPRDSetStoryLLMCmd(),
		newAutonomousPRDSetTaskLLMCmd(),
		newAutonomousPRDSetThinkingLevelCmd(),
		newAutonomousPRDSetStoryThinkingLevelCmd(),
		newAutonomousPRDSetTaskThinkingLevelCmd(),
		newAutonomousPRDSetTaskProjectDirCmd(),
		newAutonomousPRDSetTaskVerificationLLMCmd(),
		newAutonomousPRDSetStoryVerificationLLMCmd(),
		newAutonomousPRDSetVerificationLLMCmd(),
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
		newAutonomousPRDSetGitCmd(),
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
		Long: `Read the current autonomous config.

The concurrency keys are max_concurrent_tasks (tasks of one Automaton that
run at once; 1 = sequential) and max_engines_per_node (engine sessions one
compute node runs at once; 1 = one engine per node, the default).`,
		RunE: func(*cobra.Command, []string) error { return daemonGet("/api/autonomous/config") },
	}
}

func newAutonomousConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config-set <json-or-@file>",
		Short: "Merge fields into autonomous config",
		Long: `Merge the given JSON fields onto the current autonomous config; fields
not given are left unchanged.

Example — let up to 2 independent tasks of one Automaton run at once
(daemon-wide default; an Automaton's own setting overrides it; 1 = sequential):

  datawatch autonomous config-set '{"max_concurrent_tasks":2}'

Example — let each compute node run up to 2 engine sessions at once
(default 1; values below 1 are clamped to 1):

  datawatch autonomous config-set '{"max_engines_per_node":2}'

max_parallel_tasks is deprecated and ignored; use max_concurrent_tasks.`,
		Args: cobra.ExactArgs(1),
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
			resp, err := daemonJSONBody(http.MethodPut, "/api/autonomous/config", body)
			if err != nil {
				return err
			}
			prettyPrint(resp)
			printDeprecatedFields(resp)
			return nil
		},
	}
}

// printDeprecatedFields writes any deprecated_fields note from a config
// PUT response to stderr so a deprecated key never fails silently.
func printDeprecatedFields(resp []byte) {
	var r struct {
		DeprecatedFields map[string]string `json:"deprecated_fields"`
	}
	if json.Unmarshal(resp, &r) != nil {
		return
	}
	for k, note := range r.DeprecatedFields {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", k, note)
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
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory; omit to let the daemon apply its default (worktree mode) instead of assuming the current directory")
	cmd.Flags().StringVar(&backend, "backend", "", "LLM backend override")
	cmd.Flags().StringVar(&effort, "effort", "", "Effort hint (low/medium/high/max)")
	cmd.Flags().StringVar(&typ, "type", "", "Automaton type (software|research|operational|personal or custom)")
	cmd.Flags().BoolVar(&guidedMode, "guided-mode", false, "Enable Guided Mode (step-by-step operator checkpoints)")
	cmd.Flags().StringVar(&skillsCSV, "skills", "", "Comma-separated skill IDs (e.g. git,docker,pytest)")
	return cmd
}

func newAutonomousPRDGetCmd() *cobra.Command {
	var detail string
	cmd := &cobra.Command{
		Use:   "prd-get <id>",
		Short: "Fetch one PRD with its story+task tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path := "/api/autonomous/prds/" + args[0]
			if detail != "" {
				path += "?" + url.Values{"detail": {detail}}.Encode()
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().StringVar(&detail, "detail", "", "Detail level: summary, tasks or full (default: the daemon's full view)")
	return cmd
}

func newAutonomousPRDPlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "prd-plan <id>",
		Short:   "Run the LLM planning phase for a PRD (alias: prd-decompose)",
		Aliases: []string{"prd-decompose"},
		Args:    cobra.ExactArgs(1),
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
		Short: "Approve a planned PRD so Run is allowed",
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
		Short: "Reject a PRD; the plan stays for inspection but never runs",
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
		Short: "Ask for a fresh planning run; status moves back to revisions_asked",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"actor": "operator", "note": note})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/request_revision", body)
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "what's wrong with the current plan")
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

func newAutonomousPRDEditStoryCmd() *cobra.Command {
	var storyID, newTitle, newDescription string
	cmd := &cobra.Command{
		Use:   "prd-edit-story <prd-id> --story <story-id> [--title <t>] [--description <d>]",
		Short: "Rewrite a story's title/description before approving",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"story_id": storyID, "new_title": newTitle, "new_description": newDescription, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/edit_story", body)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID to rewrite (required)")
	cmd.Flags().StringVar(&newTitle, "title", "", "new title (empty = keep unchanged)")
	cmd.Flags().StringVar(&newDescription, "description", "", "new description (empty = keep unchanged)")
	_ = cmd.MarkFlagRequired("story")
	return cmd
}

// Structural edits (operator-requested: add/remove a story or task
// without re-running decompose and handing the structure back to the
// LLM). Only allowed pre-approval (needs_review/revisions_asked).
func newAutonomousPRDAddStoryCmd() *cobra.Command {
	var title, description string
	cmd := &cobra.Command{
		Use:   "prd-add-story <prd-id> --title <t> [--description <d>]",
		Short: "Add a new story without re-running decompose",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"title": title, "description": description, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/add_story", body)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "story title (required)")
	cmd.Flags().StringVar(&description, "description", "", "story description")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newAutonomousPRDRemoveStoryCmd() *cobra.Command {
	var storyID string
	cmd := &cobra.Command{
		Use:   "prd-remove-story <prd-id> --story <story-id>",
		Short: "Remove a story (and its tasks) without re-running decompose",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"story_id": storyID, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/remove_story", body)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID to remove (required)")
	_ = cmd.MarkFlagRequired("story")
	return cmd
}

func newAutonomousPRDAddTaskCmd() *cobra.Command {
	var storyID, title, spec, dependsOnCSV string
	cmd := &cobra.Command{
		Use:   "prd-add-task <prd-id> --story <story-id> --title <t> [--spec <s>] [--depends-on <task-id,...>]",
		Short: "Add a new task to an existing story without re-running decompose",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{"story_id": storyID, "title": title, "spec": spec, "actor": "operator"}
			if dependsOnCSV != "" {
				body["depends_on"] = splitCSV(dependsOnCSV)
			}
			bodyJSON, _ := json.Marshal(body)
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/add_task", bodyJSON)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID to add the task to (required)")
	cmd.Flags().StringVar(&title, "title", "", "task title (required)")
	cmd.Flags().StringVar(&spec, "spec", "", "task spec — concrete instructions for the worker session")
	cmd.Flags().StringVar(&dependsOnCSV, "depends-on", "", "comma-separated task IDs (within the same PRD) that must complete before this one becomes eligible")
	_ = cmd.MarkFlagRequired("story")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newAutonomousPRDRemoveTaskCmd() *cobra.Command {
	var storyID, taskID string
	cmd := &cobra.Command{
		Use:   "prd-remove-task <prd-id> --story <story-id> --task <task-id>",
		Short: "Remove one task from a story without re-running decompose (distinct from cancel-task, which stops a running/queued task without removing it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"story_id": storyID, "task_id": taskID, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/remove_task", body)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID the task belongs to (required)")
	cmd.Flags().StringVar(&taskID, "task", "", "task ID to remove (required)")
	_ = cmd.MarkFlagRequired("story")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newAutonomousPRDSetLLMCmd() *cobra.Command {
	var backend, effort, model, decompositionProfile, guardrailBackend, guardrailModel string
	cmd := &cobra.Command{
		Use:   "prd-set-llm <id>",
		Short: "Set the PRD-level worker LLM (backend / effort / model / decomposition-profile) and/or guardrail judge LLM. Tasks inherit unless overridden.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(prdSetLLMCLIBody(cmd))
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_llm", body)
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "", "Worker LLM name (empty value clears to fallback; omit the flag to leave unchanged)")
	cmd.Flags().StringVar(&effort, "effort", "", "Worker LLM effort (empty value clears to fallback; omit the flag to leave unchanged)")
	cmd.Flags().StringVar(&model, "model", "", "Worker LLM model (empty value clears to fallback; omit the flag to leave unchanged)")
	cmd.Flags().StringVar(&decompositionProfile, "decomposition-profile", "", "Worker LLM decomposition profile (empty value clears to fallback; omit the flag to leave unchanged)")
	addGuardrailLLMFlags(cmd, &guardrailBackend, &guardrailModel)
	return cmd
}

func newAutonomousPRDSetStoryLLMCmd() *cobra.Command {
	var storyID, backend, effort, model string
	cmd := &cobra.Command{
		Use:   "prd-set-story-llm <prd-id> --story <story-id> [--backend B --effort E --model M]",
		Short: "Override the worker LLM for all tasks in one story. Empty value clears the override (falls back to PRD then global).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"story_id": storyID, "backend": backend, "effort": effort, "model": model, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_story_llm", body)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID to override (required)")
	cmd.Flags().StringVar(&backend, "backend", "", "story-level LLM backend; empty = inherit PRD then global")
	cmd.Flags().StringVar(&effort, "effort", "", "effort level for all tasks in this story; empty = inherit")
	cmd.Flags().StringVar(&model, "model", "", "specific model name for this story; empty = backend default")
	_ = cmd.MarkFlagRequired("story")
	return cmd
}

func newAutonomousPRDSetTaskLLMCmd() *cobra.Command {
	var taskID, backend, effort, model, guardrailBackend, guardrailModel string
	cmd := &cobra.Command{
		Use:   "prd-set-task-llm <prd-id> --task <task-id> [--backend B --effort E --model M] [--guardrail-backend GB --guardrail-model GM]",
		Short: "Override the worker LLM and/or guardrail judge LLM for one task. Empty value clears the override (falls back to PRD then global).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			legacy := map[string]string{"backend": backend, "effort": effort, "model": model}
			body, _ := json.Marshal(setLLMCLIBody(cmd, legacy, map[string]string{"task_id": taskID, "actor": "operator"}, guardrailBackend, guardrailModel, "backend", "effort", "model"))
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_task_llm", body)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID to override (required)")
	cmd.Flags().StringVar(&backend, "backend", "", "task-specific LLM backend; empty = inherit PRD then global")
	cmd.Flags().StringVar(&effort, "effort", "", "effort level for this task; empty = inherit")
	cmd.Flags().StringVar(&model, "model", "", "specific model name for this task; empty = backend default")
	addGuardrailLLMFlags(cmd, &guardrailBackend, &guardrailModel)
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

// newAutonomousPRDSetThinkingLevelCmd — set the PRD-level pi --thinking
// level override (off|minimal|low|medium|high; empty clears back to the
// global pi.thinking_level config). Tasks inherit unless overridden.
func newAutonomousPRDSetThinkingLevelCmd() *cobra.Command {
	var level string
	cmd := &cobra.Command{
		Use:   "prd-set-thinking-level <id> --level <off|minimal|low|medium|high|>",
		Short: "Set the PRD-level pi --thinking level override. Tasks inherit unless overridden.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"thinking_level": level, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_thinking_level", body)
		},
	}
	cmd.Flags().StringVar(&level, "level", "", "thinking level: off|minimal|low|medium|high (empty clears to the global pi.thinking_level config)")
	_ = cmd.MarkFlagRequired("level")
	return cmd
}

func newAutonomousPRDSetStoryThinkingLevelCmd() *cobra.Command {
	var storyID, level string
	cmd := &cobra.Command{
		Use:   "prd-set-story-thinking-level <prd-id> --story <story-id> --level L",
		Short: "Override the pi --thinking level for one story. Empty level clears (falls back to PRD then global).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"story_id": storyID, "thinking_level": level, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_story_thinking_level", body)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID to override (required)")
	cmd.Flags().StringVar(&level, "level", "", "thinking level; empty = inherit PRD then global")
	_ = cmd.MarkFlagRequired("story")
	_ = cmd.MarkFlagRequired("level")
	return cmd
}

func newAutonomousPRDSetTaskThinkingLevelCmd() *cobra.Command {
	var taskID, level string
	cmd := &cobra.Command{
		Use:   "prd-set-task-thinking-level <prd-id> --task <task-id> --level L",
		Short: "Override the pi --thinking level for one task. Empty level clears (falls back to story, PRD, then global).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"task_id": taskID, "thinking_level": level, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_task_thinking_level", body)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID to override (required)")
	cmd.Flags().StringVar(&level, "level", "", "thinking level; empty = inherit story, PRD then global")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("level")
	return cmd
}

func newAutonomousPRDSetTaskProjectDirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-set-task-project-dir <prd-id> <task-id> [dir]",
		Short: "Override the project directory for one task (task > Automaton). Omitting dir clears the override (inherits the Automaton's project_dir).",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ""
			if len(args) == 3 {
				dir = args[2]
			}
			body, _ := json.Marshal(map[string]string{"task_id": args[1], "project_dir": dir, "actor": "operator"})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_task_project_dir", body)
		},
	}
}

func newAutonomousPRDSetTaskVerificationLLMCmd() *cobra.Command {
	var taskID string
	cmd := &cobra.Command{
		Use:   "prd-set-task-verification-llm <prd-id> --task <task-id> [--backend B --model M --effort E]",
		Short: "Override the LLM that verifies one task of an Automaton. Precedence: task > story > Automaton > global verification_backend. Omitted flags are unchanged; an empty value clears.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(verificationLLMCLIBody(cmd, map[string]string{"task_id": taskID, "actor": "operator"}))
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_task_verification_llm", body)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID to override (required)")
	addVerificationLLMFlags(cmd, "this task")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newAutonomousPRDSetStoryVerificationLLMCmd() *cobra.Command {
	var storyID string
	cmd := &cobra.Command{
		Use:   "prd-set-story-verification-llm <prd-id> --story <story-id> [--backend B --model M --effort E]",
		Short: "Override the LLM that verifies every task in one story of an Automaton. Precedence: task > story > Automaton > global verification_backend. Omitted flags are unchanged; an empty value clears.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(verificationLLMCLIBody(cmd, map[string]string{"story_id": storyID, "actor": "operator"}))
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_story_verification_llm", body)
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID to override (required)")
	addVerificationLLMFlags(cmd, "this story")
	_ = cmd.MarkFlagRequired("story")
	return cmd
}

func newAutonomousPRDSetVerificationLLMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prd-set-verification-llm <prd-id> [--backend B --model M --effort E]",
		Short: "Override the LLM that verifies all tasks of an Automaton. Precedence: task > story > Automaton > global verification_backend. Omitted flags are unchanged; an empty value clears.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(verificationLLMCLIBody(cmd, map[string]string{"actor": "operator"}))
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_verification_llm", body)
		},
	}
	addVerificationLLMFlags(cmd, "this Automaton")
	return cmd
}

// addVerificationLLMFlags registers --backend / --model / --effort on the
// prd-set-*-verification-llm commands. Values are read back via
// cmd.Flags().GetString in verificationLLMCLIBody.
func addVerificationLLMFlags(cmd *cobra.Command, scope string) {
	cmd.Flags().String("backend", "", "verification LLM backend for "+scope+"; empty = clear, omit = unchanged")
	cmd.Flags().String("model", "", "verification model for "+scope+"; empty = clear, omit = unchanged")
	cmd.Flags().String("effort", "", "verification effort for "+scope+"; empty = clear, omit = unchanged")
}

// verificationLLMCLIBody builds a set_*_verification_llm body. A
// verification_* key is sent only when its flag was set, so an unset flag
// leaves the field unchanged and an explicit "" clears it.
func verificationLLMCLIBody(cmd *cobra.Command, base map[string]string) map[string]string {
	body := map[string]string{}
	for k, v := range base {
		body[k] = v
	}
	for _, f := range []string{"backend", "model", "effort"} {
		if cmd.Flags().Changed(f) {
			v, _ := cmd.Flags().GetString(f)
			body["verification_"+f] = v
		}
	}
	return body
}

// addGuardrailLLMFlags (BL416) registers --guardrail-backend /
// --guardrail-model on prd-set-llm and prd-set-task-llm.
func addGuardrailLLMFlags(cmd *cobra.Command, backend, model *string) {
	cmd.Flags().StringVar(backend, "guardrail-backend", "", "guardrail/verification judge LLM override; inherits task -> automaton -> global autonomous.verification_backend; empty = clear, omit = unchanged")
	cmd.Flags().StringVar(model, "guardrail-model", "", "guardrail/verification judge model override; empty = clear, omit = unchanged")
}

// prdSetLLMCLIBody builds the set_llm body for prd-set-llm. Each legacy
// (backend / effort / model / decomposition-profile) and guardrail key is
// included only when its flag was Changed, so an omitted flag leaves the
// setting unchanged while an explicit --flag "" clears it. actor is always
// sent.
func prdSetLLMCLIBody(cmd *cobra.Command) map[string]string {
	body := map[string]string{"actor": "operator"}
	for flag, key := range map[string]string{
		"backend":               "backend",
		"effort":                "effort",
		"model":                 "model",
		"decomposition-profile": "decomposition_profile",
		"guardrail-backend":     "guardrail_backend",
		"guardrail-model":       "guardrail_model",
	} {
		if cmd.Flags().Changed(flag) {
			v, _ := cmd.Flags().GetString(flag)
			body[key] = v
		}
	}
	return body
}

// setLLMCLIBody (BL416) builds a set_llm / set_task_llm body. Guardrail
// keys are sent only when their flag was set ("" clears). When only
// guardrail flags were set the legacy keys are omitted so the REST
// guardrail-only path runs; otherwise the legacy keys are sent as before.
func setLLMCLIBody(cmd *cobra.Command, legacy, base map[string]string, guardrailBackend, guardrailModel string, legacyFlags ...string) map[string]string {
	body := map[string]string{}
	for k, v := range base {
		body[k] = v
	}
	hasGuardrail := false
	if cmd.Flags().Changed("guardrail-backend") {
		body["guardrail_backend"] = guardrailBackend
		hasGuardrail = true
	}
	if cmd.Flags().Changed("guardrail-model") {
		body["guardrail_model"] = guardrailModel
		hasGuardrail = true
	}
	hasLegacy := false
	for _, f := range legacyFlags {
		if cmd.Flags().Changed(f) {
			hasLegacy = true
			break
		}
	}
	if hasLegacy || !hasGuardrail {
		for k, v := range legacy {
			body[k] = v
		}
	}
	return body
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

func newAutonomousPRDSetGitCmd() *cobra.Command {
	var baseBranch string
	cmd := &cobra.Command{
		Use:   "prd-set-git <id> <on|off>",
		Short: "Set AutoPR (push+open a PR on completion) and base-branch override for a PRD (BL407)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			autoPR := args[1] == "on" || args[1] == "true" || args[1] == "1"
			body, _ := json.Marshal(map[string]any{"auto_pr": autoPR, "base_branch": baseBranch})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_git", body)
		},
	}
	cmd.Flags().StringVar(&baseBranch, "base-branch", "", "PR base branch override (empty = repo's own default branch)")
	return cmd
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
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory; omit to let the daemon apply its default (worktree mode) instead of assuming the current directory")
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
