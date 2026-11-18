// Plain-text renderers for the task routing read surfaces (the routing
// table, the profile list, the per-Automaton view and the dry run). The CLI
// prints the wide form (aligned columns) and the chat channel the short form
// (one line per row, capped), both from the same REST bodies.

package autonomous

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/dmz006/datawatch/internal/config"
)

// taskRoutingChatMaxLines caps the rows in a chat reply.
const taskRoutingChatMaxLines = 15

func trLLM(backend, model string) string {
	switch {
	case backend == "" && model == "":
		return "-"
	case model == "":
		return backend
	case backend == "":
		return model
	}
	return backend + "/" + model
}

func trDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// trTable renders rows (the first is the header). wide=true aligns columns;
// otherwise each row is a single "a | b | c" line, capped for chat.
func trTable(rows [][]string, wide bool) string {
	if wide {
		var sb strings.Builder
		tw := tabwriter.NewWriter(&sb, 0, 4, 2, ' ', 0)
		for _, r := range rows {
			_, _ = fmt.Fprintln(tw, strings.Join(r, "\t"))
		}
		_ = tw.Flush()
		return strings.TrimRight(sb.String(), "\n")
	}
	lines := make([]string, 0, len(rows))
	for _, r := range rows[1:] {
		lines = append(lines, strings.Join(r, " | "))
	}
	if len(lines) > taskRoutingChatMaxLines {
		more := len(lines) - taskRoutingChatMaxLines
		lines = append(lines[:taskRoutingChatMaxLines], fmt.Sprintf("... and %d more", more))
	}
	return strings.Join(lines, "\n")
}

// RenderTaskRoutingTable renders GET /api/autonomous/task-routing: the
// active profile's rows plus the validation result.
func RenderTaskRoutingTable(body []byte, wide bool) (string, error) {
	var v struct {
		Enabled       bool                                 `json:"enabled"`
		ActiveProfile string                               `json:"active_profile"`
		Profiles      map[string]config.TaskRoutingProfile `json:"profiles"`
		Validation    struct {
			Valid    bool     `json:"valid"`
			Problems []string `json:"problems"`
		} `json:"validation"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("task routing: unreadable reply: %w", err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Task routing: enabled=%v profile=%s", v.Enabled, trDash(v.ActiveProfile))
	if v.Validation.Valid {
		sb.WriteString(" (valid)")
	} else {
		sb.WriteString(" (INVALID: " + strings.Join(v.Validation.Problems, "; ") + ")")
	}
	prof := v.Profiles[v.ActiveProfile]
	if len(prof) == 0 {
		sb.WriteString("\nNo rows in this profile.")
		return sb.String(), nil
	}
	types := make([]string, 0, len(prof))
	for t := range prof {
		types = append(types, t)
	}
	sort.Strings(types)
	rows := [][]string{{"TYPE", "WORKER", "VERIFY", "EFFORT", "SECOND CHOICE"}}
	for _, t := range types {
		r := prof[t]
		second := "-"
		if r.SecondChoice != nil {
			second = trLLM(r.SecondChoice.Backend, r.SecondChoice.Model)
		}
		rows = append(rows, []string{t, trLLM(r.WorkerBackend, r.WorkerModel),
			trLLM(r.VerificationBackend, r.VerificationModel), trDash(r.Effort), second})
	}
	sb.WriteString("\n" + trTable(rows, wide))
	return sb.String(), nil
}

// RenderTaskRoutingProfiles renders GET /api/autonomous/task-routing/profiles.
func RenderTaskRoutingProfiles(body []byte) (string, error) {
	var v struct {
		Active   string   `json:"active_profile"`
		Profiles []string `json:"profiles"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("task routing: unreadable reply: %w", err)
	}
	if len(v.Profiles) == 0 {
		return "No task routing profiles.", nil
	}
	lines := make([]string, 0, len(v.Profiles))
	for _, p := range v.Profiles {
		mark := "  "
		if p == v.Active {
			mark = "* "
		}
		lines = append(lines, mark+p)
	}
	return "Task routing profiles (* = active):\n" + strings.Join(lines, "\n"), nil
}

// RenderTaskRoutingAutomaton renders GET /api/autonomous/prds/{id}/task-routing:
// which row decided each task's LLM.
func RenderTaskRoutingAutomaton(body []byte, wide bool) (string, error) {
	var v struct {
		ID    string            `json:"automaton_id"`
		Tasks []TaskRoutingView `json:"tasks"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("task routing: unreadable reply: %w", err)
	}
	if len(v.Tasks) == 0 {
		return fmt.Sprintf("Automaton %s has no tasks.", v.ID), nil
	}
	rows := [][]string{{"TASK", "TYPE", "ROW", "LLM", "SOURCE", "NOTE"}}
	for _, t := range v.Tasks {
		name := t.Title
		if name == "" || !wide {
			name = t.TaskID
		}
		note := t.Reason
		if t.Applied {
			note = "routed"
		}
		rows = append(rows, []string{name, t.TaskType, trDash(t.Row),
			trLLM(t.Effective.Backend, t.Effective.Model), trDash(t.Effective.Source), trDash(note)})
	}
	return fmt.Sprintf("Task routing for Automaton %s:\n%s", v.ID, trTable(rows, wide)), nil
}

// RenderTaskRoutingDryRun renders POST .../task-routing/dry-run. Nothing was
// written by the call.
func RenderTaskRoutingDryRun(body []byte, wide bool) (string, error) {
	var v struct {
		ID      string                   `json:"automaton_id"`
		Entries []TaskRoutingDryRunEntry `json:"entries"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("task routing: unreadable reply: %w", err)
	}
	if len(v.Entries) == 0 {
		return fmt.Sprintf("Automaton %s has no tasks.", v.ID), nil
	}
	rows := [][]string{{"TASK", "TYPE", "ROW", "WOULD ASSIGN", "NOTE"}}
	for _, e := range v.Entries {
		name := e.Title
		if name == "" || !wide {
			name = e.TaskID
		}
		assign, note := "-", e.Reason
		if e.WouldApply && e.Result != nil {
			assign = trLLM(e.Result.WorkerBackend, e.Result.WorkerModel)
			note = ""
			if e.SecondChoice {
				note = "second choice"
			}
		}
		rows = append(rows, []string{name, e.TaskType, trDash(e.Row), assign, trDash(note)})
	}
	return fmt.Sprintf("Dry run for Automaton %s (nothing written):\n%s", v.ID, trTable(rows, wide)), nil
}
