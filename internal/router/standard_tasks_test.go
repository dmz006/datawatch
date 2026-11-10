package router

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// standardTasksFixturePath is the shared expected list every surface reads.
const standardTasksFixturePath = "../../testdata/standard-tasks/expected.json"

// loadStandardTaskFixture returns the shared expected rows (id, title,
// enabled, mode and runner).
func loadStandardTaskFixture(t *testing.T) []struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`
	Runner  string `json:"runner"`
} {
	t.Helper()
	raw, err := os.ReadFile(standardTasksFixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f struct {
		Tasks []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Enabled bool   `json:"enabled"`
			Mode    string `json:"mode"`
			Runner  string `json:"runner"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return f.Tasks
}

// standardTaskLine returns the reply line that starts with id, or "" when
// the reply has none.
func standardTaskLine(reply, id string) string {
	for _, line := range strings.Split(reply, "\n") {
		if strings.HasPrefix(line, id+" | ") {
			return line
		}
	}
	return ""
}

// TestStandardTaskRouterReply checks the standard task reply lines against the
// shared fixture: id, title, on/off, mode and runner, one line each, and no "PRD".
func TestStandardTaskRouterReply(t *testing.T) {
	want := loadStandardTaskFixture(t)
	on := config.DefaultConfig()
	on.Autonomous.TaskTemplates.Enabled = true
	reply := buildStandardTasksReply(on, "")

	if strings.Contains(reply, "PRD") {
		t.Fatalf("reply says PRD:\n%s", reply)
	}
	for _, w := range want {
		line := standardTaskLine(reply, w.ID)
		if line == "" {
			t.Fatalf("no reply line for built-in id %q:\n%s", w.ID, reply)
		}
		parts := strings.Split(line, " | ")
		if len(parts) != 5 {
			t.Fatalf("line %q: want 5 fields (id, title, on/off, mode, runner), got %d", line, len(parts))
		}
		if parts[1] != w.Title {
			t.Errorf("line %q: title = %q, want %q", line, parts[1], w.Title)
		}
		wantState := "off"
		if w.Enabled {
			wantState = "on"
		}
		if parts[2] != wantState {
			t.Errorf("line %q: want %s, got %q", line, wantState, parts[2])
		}
		if parts[3] != w.Mode {
			t.Errorf("line %q: want mode %q, got %q", line, w.Mode, parts[3])
		}
		if parts[4] != w.Runner {
			t.Errorf("line %q: want runner %q, got %q", line, w.Runner, parts[4])
		}
	}
	if got := len(strings.Split(reply, "\n")); got != len(want) {
		t.Errorf("want one line per built-in task (%d), got %d:\n%s", len(want), got, reply)
	}

	off := config.DefaultConfig()
	off.Autonomous.TaskTemplates.Enabled = false
	offReply := buildStandardTasksReply(off, "")
	for _, w := range want {
		if line := standardTaskLine(offReply, w.ID); !strings.Contains(line, " | off | ") {
			t.Errorf("disabled config: want off for %q, got %q", w.ID, line)
		}
	}
}

// TestStandardTaskRouterCommand checks that the chat word routes to the
// reply: "standard tasks" parses to the autonomous standard-tasks verb and
// the router answers with the listing.
func TestStandardTaskRouterCommand(t *testing.T) {
	want := loadStandardTaskFixture(t)
	cmd := Parse("standard tasks")
	if cmd.Type != CmdAutonomous || cmd.Text != "standard-tasks" {
		t.Fatalf("Parse(standard tasks) = {%v %q}, want {autonomous standard-tasks}", cmd.Type, cmd.Text)
	}

	r := NewRouter("h", "g", nil, nil, 0, nil)
	r.SetConfig(config.DefaultConfig())
	responses := r.HandleTestMessage("standard tasks")
	joined := strings.Join(responses, "\n")
	if !strings.HasPrefix(joined, "[h] standard tasks\n") {
		t.Fatalf("unexpected reply header:\n%s", joined)
	}
	for _, w := range want {
		if standardTaskLine(joined, w.ID) == "" {
			t.Errorf("chat reply has no line for %q:\n%s", w.ID, joined)
		}
	}
	if strings.Contains(joined, "PRD") {
		t.Errorf("chat reply says PRD:\n%s", joined)
	}
}
