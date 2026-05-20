// BL303 S1 — comm-channel verb for session telemetry.
//
//	"telemetry <id>"      → GET /api/sessions/<id>/telemetry
//	"telemetry list"      → structured telemetry summary for all sessions

package router

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (r *Router) handleTelemetryCmd(cmd Command) {
	if cmd.Text == "list" || (cmd.SessionID == "" && cmd.Text == "") {
		r.handleTelemetryList()
		return
	}
	id := cmd.SessionID
	if id == "" {
		id = cmd.Text
	}
	r.handleTelemetryGet(id)
}

func (r *Router) handleTelemetryGet(id string) {
	body, err := r.commGet(fmt.Sprintf("/api/sessions/%s/telemetry", id), nil)
	if err != nil {
		r.send(fmt.Sprintf("telemetry error: %v", err))
		return
	}
	var tel struct {
		CurrentTask       string  `json:"current_task"`
		Tool              string  `json:"tool"`
		Progress          float64 `json:"progress"`
		Tasks             []struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			Status     string `json:"status"`
			DurationMs int64  `json:"duration_ms"`
		} `json:"tasks"`
		GuardrailVerdicts []struct {
			Guardrail string `json:"guardrail"`
			Outcome   string `json:"outcome"`
		} `json:"guardrail_verdicts"`
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.Unmarshal([]byte(body), &tel); err != nil {
		r.send(body)
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Telemetry: %s\n", id)
	if tel.CurrentTask != "" {
		fmt.Fprintf(&sb, "  Task: %s\n", tel.CurrentTask)
	}
	if tel.Progress > 0 {
		fmt.Fprintf(&sb, "  Progress: %.0f%%\n", tel.Progress)
	}
	done, total := 0, len(tel.Tasks)
	for _, t := range tel.Tasks {
		if t.Status == "completed" {
			done++
		}
	}
	if total > 0 {
		fmt.Fprintf(&sb, "  Tasks: %d/%d done\n", done, total)
		for _, t := range tel.Tasks {
			icon := "·"
			switch t.Status {
			case "completed":
				icon = "✓"
			case "failed":
				icon = "✗"
			case "in_progress":
				icon = "▶"
			}
			line := fmt.Sprintf("  %s [%s] %s", icon, t.ID, t.Title)
			if t.DurationMs > 0 {
				line += fmt.Sprintf(" (%dms)", t.DurationMs)
			}
			sb.WriteString(line + "\n")
		}
	}
	for _, v := range tel.GuardrailVerdicts {
		icon := "✓"
		switch v.Outcome {
		case "warn":
			icon = "⚠"
		case "block":
			icon = "✗"
		}
		fmt.Fprintf(&sb, "  %s %s: %s\n", icon, v.Guardrail, v.Outcome)
	}
	fmt.Fprintf(&sb, "  Updated: %s", tel.UpdatedAt)
	r.send(sb.String())
}

func (r *Router) handleTelemetryList() {
	sessions := r.manager.ListSessions()
	if len(sessions) == 0 {
		r.send("No active sessions.")
		return
	}
	var sb strings.Builder
	sb.WriteString("Telemetry summary:\n")
	for _, sess := range sessions {
		body, err := r.commGet(fmt.Sprintf("/api/sessions/%s/telemetry", sess.ID), nil)
		if err != nil {
			fmt.Fprintf(&sb, "  %s: error\n", sess.ID)
			continue
		}
		var tel struct {
			CurrentTask string  `json:"current_task"`
			Progress    float64 `json:"progress"`
			Tasks       []struct {
				Status string `json:"status"`
			} `json:"tasks"`
		}
		done, total := 0, 0
		if json.Unmarshal([]byte(body), &tel) == nil {
			total = len(tel.Tasks)
			for _, t := range tel.Tasks {
				if t.Status == "completed" {
					done++
				}
			}
		}
		line := fmt.Sprintf("  %s", sess.ID)
		if total > 0 {
			line += fmt.Sprintf(" tasks:%d/%d", done, total)
		}
		if tel.Progress > 0 {
			line += fmt.Sprintf(" %.0f%%", tel.Progress)
		}
		if tel.CurrentTask != "" {
			task := tel.CurrentTask
			if len(task) > 30 {
				task = task[:27] + "..."
			}
			line += " · " + task
		}
		sb.WriteString(line + "\n")
	}
	r.send(strings.TrimRight(sb.String(), "\n"))
}
