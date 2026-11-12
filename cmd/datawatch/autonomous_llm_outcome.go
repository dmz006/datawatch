// Package main — explicit outcome lines for the autonomous set-llm family CLI.
//
// Task 6cb90009: when a set-llm / decompose call carries a structured
// outcome (the daemon's top-level `override` and/or `planning` key), the
// CLI prints an explicit line for it — which targets changed vs were left
// alone (running/finished), and whether a planning requeue was requeued,
// deferred to the next run, or a no-op — before the daemon's pretty-printed
// JSON, instead of just handing the operator the JSON.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// daemonJSONWithOutcome is daemonJSON for the set-llm family: when the
// daemon's response carries a structured outcome, the explicit line for it
// (requeued / deferred, and the changed vs left-alone task lists) prints
// first, then the pretty-printed JSON — the operator never has to dig the
// outcome out of the JSON alone.
func daemonJSONWithOutcome(method, path string, body any) error {
	resp, err := daemonJSONBody(method, path, body)
	if err != nil {
		return err
	}
	if line := llmOutcomeReply(resp); line != "" {
		_, _ = fmt.Fprintln(os.Stdout, line)
	}
	if len(resp) > 0 {
		prettyPrint(resp)
	} else {
		fmt.Println("ok")
	}
	return nil
}

// llmOutcomeTask mirrors one LLMOverrideOutcome as serialized by the daemon
// (internal/autonomous.LLMOverrideOutcome).
type llmOutcomeTask struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
}

// llmOutcomeOverride mirrors a queued-mode per-task outcome
// (internal/autonomous.LLMOverrideResult): which unstarted tasks the override
// was applied to (changed) and which were left alone because they are running
// or finished (left_alone, with the reason).
type llmOutcomeOverride struct {
	Changed   []llmOutcomeTask `json:"changed"`
	LeftAlone []llmOutcomeTask `json:"left_alone"`
}

// llmOutcomePlanning mirrors a planning requeue outcome
// (internal/autonomous.PlanningRequeueResult).
type llmOutcomePlanning struct {
	Outcome    string   `json:"outcome"`
	OldBackend string   `json:"old_backend"`
	NewBackend string   `json:"new_backend"`
	OldModel   string   `json:"old_model"`
	NewModel   string   `json:"new_model"`
	OldPools   []string `json:"old_pools"`
	NewPools   []string `json:"new_pools"`
	Reason     string   `json:"reason"`
}

// llmOutcomeTasks renders a changed / left-alone task list — "id" per task,
// or "id (reason)" when the daemon explained why the task was left alone, so
// the operator sees the per-task split without parsing JSON.
func llmOutcomeTasks(ts []llmOutcomeTask) string {
	parts := make([]string, 0, len(ts))
	for _, t := range ts {
		if t.Reason != "" {
			parts = append(parts, t.TaskID+" ("+t.Reason+")")
		} else {
			parts = append(parts, t.TaskID)
		}
	}
	return strings.Join(parts, ", ")
}

// llmOutcomeDesc renders the destination of a planning requeue: the pool list
// when the daemon reports one, otherwise the backend (and model) pair.
func llmOutcomeDesc(pools []string, backend, model string) string {
	if len(pools) > 0 {
		return strings.Join(pools, "+")
	}
	if backend == "" {
		return "(unknown)"
	}
	if model != "" {
		return backend + "/" + model
	}
	return backend
}

// llmOutcomeReply renders the explicit CLI line for a set-llm / decompose
// response's structured outcome. It reads the two top-level keys the daemon
// rides along in that mode:
//   - override: "changed: …" and "left alone: …" (running/finished tasks the
//     override leaves alone; they apply when they next run),
//   - planning: "requeued planner: old -> new", "deferred to next run", or
//     "planner unchanged" — with the reason when the daemon carries one.
//
// It returns "" when the response carries no outcome (a classic accepted call,
// or a plain PRD payload), so the caller prints nothing extra.
func llmOutcomeReply(resp []byte) string {
	var o struct {
		Override *llmOutcomeOverride `json:"override"`
		Planning *llmOutcomePlanning `json:"planning"`
	}
	if err := json.Unmarshal(resp, &o); err != nil {
		return ""
	}
	bits := []string{}
	if ov := o.Override; ov != nil && (len(ov.Changed) > 0 || len(ov.LeftAlone) > 0) {
		if len(ov.Changed) > 0 {
			bits = append(bits, "changed: "+llmOutcomeTasks(ov.Changed))
		} else {
			bits = append(bits, "changed: (none)")
		}
		if len(ov.LeftAlone) > 0 {
			bits = append(bits, "left alone: "+llmOutcomeTasks(ov.LeftAlone))
		} else {
			bits = append(bits, "left alone: (none)")
		}
	}
	if pl := o.Planning; pl != nil && pl.Outcome != "" {
		switch pl.Outcome {
		case "requeued":
			bit := "requeued planner: " + llmOutcomeDesc(pl.OldPools, pl.OldBackend, pl.OldModel) + " -> " + llmOutcomeDesc(pl.NewPools, pl.NewBackend, pl.NewModel)
			if pl.Reason != "" {
				bit += " — " + pl.Reason
			}
			bits = append(bits, bit)
		case "deferred_to_next_run":
			if pl.Reason != "" {
				bits = append(bits, "deferred to next run — "+pl.Reason)
			} else {
				bits = append(bits, "deferred to next run (a planning run is already started)")
			}
		default: // "unchanged"
			if pl.Reason != "" {
				bits = append(bits, "planner unchanged — "+pl.Reason)
			} else {
				bits = append(bits, "planner unchanged")
			}
		}
	}
	return strings.Join(bits, " | ")
}
