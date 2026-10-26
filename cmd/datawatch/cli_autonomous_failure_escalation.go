// `datawatch autonomous failure-escalation` and `datawatch autonomous
// resolve-review`: the failure escalation policy from the command line.
//
// failure-escalation show|set|clear read and change the override on one
// Automaton or on one of its tasks (--task). failure-escalation global writes
// the daemon-wide default through PUT /api/config, which persists it and
// applies it live. resolve-review is the operator's retry or cancel of a task
// held for review.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// failureEscalationFlags are the policy flags shared by the override and the
// global forms. enabled is text ("true" or "false") so that both
// `--enabled false` and `--enabled=false` work.
type failureEscalationFlags struct {
	enabled     string
	maxFailures int
	steps       string
}

// parseEnabledFlag reads the --enabled value. It must be true or false.
func parseEnabledFlag(v string) (bool, error) {
	enabled, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return false, fmt.Errorf("--enabled must be true or false (got %q)", v)
	}
	return enabled, nil
}

// addFailureEscalationPolicyFlags registers the policy flags on cmd.
func addFailureEscalationPolicyFlags(cmd *cobra.Command, f *failureEscalationFlags) {
	cmd.Flags().StringVar(&f.enabled, "enabled", "", "true or false: turn failure escalation on (false restores the old retry-then-fail behaviour); empty leaves it unset")
	cmd.Flags().IntVar(&f.maxFailures, "max-failures", 0, "failed attempts before the task is held for review (1 or more; 0 leaves it unset)")
	cmd.Flags().StringVar(&f.steps, "steps", "", "worker ladder as backend:model[@attempts],... ; - means the task's normal worker; empty leaves it unset")
}

// policyFromFlags builds the override policy from the flags that were set. A
// flag left at its default is not sent, so it inherits from the next level up.
// The steps text is parsed here, so a bad ladder fails before the request.
func policyFromFlags(cmd *cobra.Command, f failureEscalationFlags) (*autonomouspkg.FailureEscalationPolicy, error) {
	p := &autonomouspkg.FailureEscalationPolicy{}
	if cmd.Flags().Changed("enabled") {
		enabled, err := parseEnabledFlag(f.enabled)
		if err != nil {
			return nil, err
		}
		p.Enabled = &enabled
	}
	if cmd.Flags().Changed("max-failures") {
		p.MaxFailures = f.maxFailures
	}
	if cmd.Flags().Changed("steps") {
		steps, err := autonomouspkg.ParseFailureEscalationSteps(f.steps)
		if err != nil {
			return nil, err
		}
		p.Steps = steps
	}
	return p, nil
}

func newAutonomousFailureEscalationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "failure-escalation",
		Short: "Set or clear the failure escalation policy (retry ladder and review threshold)",
		Long: `Failure escalation decides what happens when a task keeps failing: each
failed attempt moves to the next worker on the ladder, and after max-failures
failed attempts the task is held for operator review.

Subcommands:
  show <automaton-id> [--task <task-id>]
                                  Print the override stored on the Automaton (or
                                  on one task of it) and the daemon-wide default.
  set <automaton-id> [--task <task-id>] [--enabled true|false] [--max-failures N] [--steps ...]
                                  Override the policy on an Automaton, or on one
                                  task of it. Unset flags inherit from the next
                                  level (task, then Automaton, then the default).
  clear <automaton-id> [--task <task-id>]
                                  Remove the override at that level.
  global [--enabled true|false] [--max-failures N] [--steps ...]
                                  Set the daemon-wide default (saved to config.yaml).

Steps are written backend:model[@attempts], comma separated, for example
  --steps "pi-prompt:ollama/qwen3.8:27b@2,claude-code:claude-opus-5-5"
`,
	}
	cmd.AddCommand(
		newAutonomousFailureEscalationShowCmd(),
		newAutonomousFailureEscalationSetCmd(),
		newAutonomousFailureEscalationClearCmd(),
		newAutonomousFailureEscalationGlobalCmd(),
	)
	return cmd
}

// failureEscalationShowPRD is the part of an Automaton detail that show reads.
type failureEscalationShowPRD struct {
	ID                string          `json:"id"`
	Title             string          `json:"title"`
	FailureEscalation json.RawMessage `json:"failure_escalation"`
	Story             []struct {
		Tasks []struct {
			ID                 string          `json:"id"`
			Title              string          `json:"title"`
			Status             string          `json:"status"`
			EscalationFailures int             `json:"escalation_failures"`
			FailureEscalation  json.RawMessage `json:"failure_escalation"`
		} `json:"tasks"`
	} `json:"stories"`
}

// failureEscalationShowView builds what show prints from the Automaton detail
// and the daemon config. Each override is the stored one, or null when it is not
// set at that level; the daemon default is read from the autonomous config.
func failureEscalationShowView(prd failureEscalationShowPRD, taskID string, cfg []byte) (map[string]any, error) {
	var global struct {
		Autonomous struct {
			FailureEscalation json.RawMessage `json:"failure_escalation"`
		} `json:"autonomous"`
	}
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &global); err != nil {
			return nil, fmt.Errorf("daemon config is not JSON: %w", err)
		}
	}
	view := map[string]any{
		"automaton": map[string]any{
			"id":                 prd.ID,
			"title":              prd.Title,
			"failure_escalation": rawOrNull(prd.FailureEscalation),
		},
		"daemon_default": rawOrNull(global.Autonomous.FailureEscalation),
		"rule":           "each field resolves task, then Automaton, then the daemon default",
	}
	if taskID == "" {
		return view, nil
	}
	for _, s := range prd.Story {
		for _, t := range s.Tasks {
			if t.ID != taskID {
				continue
			}
			view["task"] = map[string]any{
				"id":                  t.ID,
				"title":               t.Title,
				"status":              t.Status,
				"escalation_failures": t.EscalationFailures,
				"failure_escalation":  rawOrNull(t.FailureEscalation),
			}
			return view, nil
		}
	}
	return nil, fmt.Errorf("task %q is not on Automaton %q", taskID, prd.ID)
}

// rawOrNull returns the JSON value raw, or nil when it is empty.
func rawOrNull(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}

func newAutonomousFailureEscalationShowCmd() *cobra.Command {
	var taskID string
	cmd := &cobra.Command{
		Use:   "show <automaton-id>",
		Short: "Show the failure escalation override on an Automaton or one task, and the daemon default",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prdBody, err := daemonJSONBody(http.MethodGet, "/api/autonomous/prds/"+args[0], nil)
			if err != nil {
				return err
			}
			var prd failureEscalationShowPRD
			if err := json.Unmarshal(prdBody, &prd); err != nil {
				return fmt.Errorf("automaton detail is not JSON: %w", err)
			}
			cfgBody, err := daemonJSONBody(http.MethodGet, "/api/config", nil)
			if err != nil {
				return err
			}
			view, err := failureEscalationShowView(prd, taskID, cfgBody)
			if err != nil {
				return err
			}
			out, err := json.MarshalIndent(view, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID; empty shows the Automaton override")
	return cmd
}

func newAutonomousFailureEscalationSetCmd() *cobra.Command {
	var taskID string
	var f failureEscalationFlags
	cmd := &cobra.Command{
		Use:   "set <automaton-id>",
		Short: "Override the failure escalation policy on an Automaton or one task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := policyFromFlags(cmd, f)
			if err != nil {
				return err
			}
			if p.Enabled == nil && p.MaxFailures == 0 && p.Steps == nil {
				return fmt.Errorf("nothing to set: pass at least one of --enabled, --max-failures or --steps (use clear to remove the override)")
			}
			body := map[string]any{"task_id": taskID, "policy": p, "actor": "operator"}
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_failure_escalation", body)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID; empty sets the override on the Automaton")
	addFailureEscalationPolicyFlags(cmd, &f)
	return cmd
}

func newAutonomousFailureEscalationClearCmd() *cobra.Command {
	var taskID string
	cmd := &cobra.Command{
		Use:   "clear <automaton-id>",
		Short: "Remove the failure escalation override on an Automaton or one task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{"task_id": taskID, "policy": nil, "actor": "operator"}
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_failure_escalation", body)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID; empty clears the Automaton override")
	return cmd
}

func newAutonomousFailureEscalationGlobalCmd() *cobra.Command {
	var f failureEscalationFlags
	cmd := &cobra.Command{
		Use:   "global",
		Short: "Set the daemon-wide failure escalation default (autonomous.failure_escalation)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			patch := map[string]any{}
			if cmd.Flags().Changed("enabled") {
				enabled, err := parseEnabledFlag(f.enabled)
				if err != nil {
					return err
				}
				patch["autonomous.failure_escalation.enabled"] = enabled
			}
			if cmd.Flags().Changed("max-failures") {
				patch["autonomous.failure_escalation.max_failures"] = f.maxFailures
			}
			if cmd.Flags().Changed("steps") {
				patch["autonomous.failure_escalation.steps"] = f.steps
			}
			if len(patch) == 0 {
				return fmt.Errorf("nothing to set: pass at least one of --enabled, --max-failures or --steps")
			}
			return daemonJSON(http.MethodPut, "/api/config", patch)
		},
	}
	addFailureEscalationPolicyFlags(cmd, &f)
	return cmd
}

func newAutonomousResolveReviewCmd() *cobra.Command {
	var taskID, action, spec, backend, model, effort, files, note string
	cmd := &cobra.Command{
		Use:   "resolve-review <automaton-id> --task <task-id> --action retry|cancel [--spec ...] [--backend B --model M --effort E] [--files a,b] [--note ...]",
		Short: "Retry or cancel a task held for operator review",
		Long: `The operator's decision on a task held for review (status review_needed).
retry requeues the task. It may carry a replaced spec, a worker (backend,
model, effort) for the task, and a narrowed file scope (--files). cancel
cancels the task. It works while the Automaton is running and other tasks are
live. A task that is not held answers 409.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if action != "retry" && action != "cancel" {
				return fmt.Errorf("--action must be retry or cancel (got %q)", action)
			}
			body := map[string]any{
				"task_id": taskID,
				"action":  action,
				"spec":    spec,
				"backend": backend,
				"model":   model,
				"effort":  effort,
				"files":   splitReviewFiles(files),
				"note":    note,
				"actor":   "operator",
			}
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/resolve_task_review", body)
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID held for review (required)")
	cmd.Flags().StringVar(&action, "action", "", "retry or cancel (required)")
	cmd.Flags().StringVar(&spec, "spec", "", "retry only: replacement task spec")
	cmd.Flags().StringVar(&backend, "backend", "", "retry only: worker backend for the task")
	cmd.Flags().StringVar(&model, "model", "", "retry only: worker model for the task")
	cmd.Flags().StringVar(&effort, "effort", "", "retry only: worker effort for the task")
	cmd.Flags().StringVar(&files, "files", "", "retry only: comma-separated narrowed list of files the task may change")
	cmd.Flags().StringVar(&note, "note", "", "operator note, recorded on the decision log and given to the next attempt")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("action")
	return cmd
}

// splitReviewFiles splits a comma-separated file list, dropping blanks.
func splitReviewFiles(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}
