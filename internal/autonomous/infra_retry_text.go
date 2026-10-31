package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// This file reads the infrastructure retry status body and formats it as text.
// The CLI and the comm channel both render through it, so the two surfaces
// show the same lines.

// ParseInfraRetryStatus decodes the body GET /api/autonomous/infra-retries
// returns.
func ParseInfraRetryStatus(body []byte) (InfraRetryStatus, error) {
	var st InfraRetryStatus
	if err := json.Unmarshal(body, &st); err != nil {
		return InfraRetryStatus{}, fmt.Errorf("reading the infrastructure retry status: %w", err)
	}
	return st, nil
}

// RenderInfraRetryText formats the status: the settings in effect on the first
// line, then one block per task. limit caps how many tasks are shown; 0 or
// less shows all of them.
func RenderInfraRetryText(st InfraRetryStatus, limit int) string {
	var b strings.Builder
	b.WriteString(infraRetrySettingsText(st.InfraRetrySettings))
	b.WriteString("\n")
	tasks := st.Tasks
	if limit > 0 && len(tasks) > limit {
		tasks = tasks[:limit]
	}
	if len(tasks) == 0 {
		b.WriteString("No infrastructure retries yet.\n")
		return b.String()
	}
	for _, t := range tasks {
		b.WriteString(infraRetryTaskText(t))
	}
	return b.String()
}

// infraRetrySettingsText is the first line: whether retry is on, the number
// of tries, the delays and what happens when the tries are used up.
func infraRetrySettingsText(s InfraRetrySettings) string {
	state := "on"
	if !s.Enabled {
		state = "off"
	}
	tries := fmt.Sprintf("%d tries", s.MaxTries)
	if s.MaxTries == 1 {
		tries = "1 try"
	}
	backoff := "no back-off"
	if len(s.BackoffSeconds) > 0 {
		parts := make([]string, len(s.BackoffSeconds))
		for i, secs := range s.BackoffSeconds {
			parts[i] = fmt.Sprintf("%ds", secs)
		}
		backoff = "back-off " + strings.Join(parts, "/")
	}
	exhausted := "hold when used up"
	if s.ExhaustedAction == config.ExhaustedActionCount {
		exhausted = "count as a failure when used up"
	}
	return fmt.Sprintf("Infrastructure retry: %s, %s, %s, %s", state, tries, backoff, exhausted)
}

// infraRetryTaskText is one task's block: where it is and how far its retries
// have got, each retry, and for a held task why it is held.
func infraRetryTaskText(t InfraRetryTask) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Automaton %s %q, task %s %q: %s, retry %d of %d\n",
		t.AutomatonID, t.AutomatonTitle, t.TaskID, t.TaskTitle, t.Status, t.RetryCount, t.MaxTries)
	for _, r := range t.Retries {
		fmt.Fprintf(&b, "  retry %d: %s", r.Number, r.Class)
		if r.Reason != "" {
			b.WriteString(": " + r.Reason)
		}
		fmt.Fprintf(&b, " (%s)\n", r.At.Format("2006-01-02 15:04:05"))
	}
	if t.Held {
		fmt.Fprintf(&b, "  held after infrastructure retries: %s\n", t.HeldCause)
		if t.QuestionID != "" {
			fmt.Fprintf(&b, "  waiting on question %s\n", t.QuestionID)
		}
	}
	return b.String()
}
