package session

import (
	"encoding/json"
	"testing"
)

func TestStartOptionsCarryTaskTitle(t *testing.T) {
	saved := startWithEffortOpts(t, &StartOptions{TaskTitle: "Add retry to fetcher"})
	if saved.TaskTitle != "Add retry to fetcher" {
		t.Errorf("Session.TaskTitle = %q, want %q", saved.TaskTitle, "Add retry to fetcher")
	}

	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}
	if fields["task_title"] != "Add retry to fetcher" {
		t.Errorf("serialized task_title = %v, want %q", fields["task_title"], "Add retry to fetcher")
	}

	var round Session
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if round.TaskTitle != "Add retry to fetcher" {
		t.Errorf("round-trip TaskTitle = %q, want %q", round.TaskTitle, "Add retry to fetcher")
	}
}
