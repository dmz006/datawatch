// Deploy status text: one renderer the CLI and the chat verb share, so both
// show the same lines. It reads the GET /api/autonomous/deploy body.

package deploy

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// StatusView is the GET /api/autonomous/deploy body.
type StatusView struct {
	Settings map[string]any `json:"settings"`
	Phase    string         `json:"phase,omitempty"`
	Outcome  string         `json:"outcome,omitempty"`
	Deferred *struct {
		Reason string `json:"reason"`
		At     string `json:"at"`
	} `json:"deferred,omitempty"`
	Snapshot *struct {
		Time   string `json:"time"`
		SHA256 string `json:"sha256"`
	} `json:"snapshot,omitempty"`
	Running bool `json:"running"`
}

// ParseStatusView decodes a GET body.
func ParseStatusView(body []byte) (StatusView, error) {
	var v StatusView
	if err := json.Unmarshal(body, &v); err != nil {
		return v, fmt.Errorf("the daemon's reply could not be read: %w", err)
	}
	return v, nil
}

// RenderStatusText renders the settings and the last record.
func RenderStatusText(v StatusView) string {
	var b strings.Builder
	b.WriteString("Deploy settings:\n")
	keys := make([]string, 0, len(v.Settings))
	for k := range v.Settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		val := strings.TrimSpace(fmt.Sprint(v.Settings[k]))
		if val == "" {
			val = "(not set)"
		}
		fmt.Fprintf(&b, "  %s: %s\n", k, val)
	}
	if v.Running {
		b.WriteString("A merge or deploy is running now.\n")
	}
	if v.Phase == "" && v.Outcome == "" {
		b.WriteString("Last deploy: none recorded.\n")
	} else {
		b.WriteString("Last deploy:\n")
		if v.Phase != "" {
			fmt.Fprintf(&b, "  phase: %s\n", v.Phase)
		}
		if v.Outcome != "" {
			fmt.Fprintf(&b, "  outcome: %s\n", v.Outcome)
		}
	}
	if v.Deferred != nil {
		fmt.Fprintf(&b, "  deferred: %s (%s)\n", v.Deferred.Reason, v.Deferred.At)
	}
	if v.Snapshot != nil {
		fmt.Fprintf(&b, "  snapshot: %s sha256 %s\n", v.Snapshot.Time, v.Snapshot.SHA256)
	}
	return b.String()
}

// RenderRunText renders the POST /api/autonomous/deploy/run body.
func RenderRunText(body []byte) (string, error) {
	var r struct {
		Status string `json:"status"`
		Note   string `json:"note"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("the daemon's reply could not be read: %w", err)
	}
	out := "Deploy " + r.Status
	if r.Note != "" {
		out += ": " + r.Note
	}
	if r.Error != "" {
		out += "\nerror: " + r.Error
	}
	return out + "\n", nil
}
