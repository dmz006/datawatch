// `datawatch autonomous failure-escalation` and `datawatch autonomous
// resolve-review`: the failure escalation policy from the command line.
//
// failure-escalation set|clear change the override on one Automaton or on one
// of its tasks (--task). failure-escalation global writes the daemon-wide
// default through PUT /api/config, which persists it and applies it live.
// resolve-review is the operator's retry or cancel of a task held for review.
package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// failureEscalationFlags are the policy flags shared by the override and the
// global forms.
type failureEscalationFlags struct {
	enabled     bool
	maxFailures int
	steps       string
}

// addFailureEscalationPolicyFlags registers the policy flags on cmd.
func addFailureEscalationPolicyFlags(cmd *cobra.Command, f *failureEscalationFlags) {
	cmd.Flags().BoolVar(&f.enabled, "enabled", true, "turn failure escalation on (false restores the old retry-then-fail behaviour)")
	cmd.Flags().IntVar(&f.maxFailures, "max-failures", 0, "failed attempts before the task is held for review (1 or more; 0 leaves it unset)")
	cmd.Flags().StringVar(&f.steps, "steps", "", "worker ladder as backend:model[@attempts],... ; - means the task's normal worker; empty leaves it unset")
}

// policyFromFlags builds the override policy from the flags that were set. A
// flag left at its default is not sent, so it inherits from the next level up.
// The steps text is parsed here, so a bad ladder fails before the request.
func policyFromFlags(cmd *cobra.Command, f failureEscalationFlags) (*autonomouspkg.FailureEscalationPolicy, error) {
	p := &autonomouspkg.FailureEscalationPolicy{}
	if cmd.Flags().Changed("enabled") {
		enabled := f.enabled
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
  set <automaton-id> [--task <task-id>] [--enabled=bool] [--max-failures N] [--steps ...]
                                  Override the policy on an Automaton, or on one
                                  task of it. Unset flags inherit from the next
                                  level (task, then Automaton, then the default).
  clear <automaton-id> [--task <task-id>]
                                  Remove the override at that level.
  global [--enabled=bool] [--max-failures N] [--steps ...]
                                  Set the daemon-wide default (saved to config.yaml).

Steps are written backend:model[@attempts], comma separated, for example
  --steps "pi-prompt:ollama/qwen3.8:27b@2,claude-code:claude-opus-5-5"
`,
	}
	cmd.AddCommand(
		newAutonomousFailureEscalationSetCmd(),
		newAutonomousFailureEscalationClearCmd(),
		newAutonomousFailureEscalationGlobalCmd(),
	)
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
				patch["autonomous.failure_escalation.enabled"] = f.enabled
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
cancels the task. A task that is not held answers 409.`,
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
