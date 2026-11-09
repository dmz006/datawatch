package router

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// builtInStandardTaskIDs are the built-in template ids the reply must list.
var builtInStandardTaskIDs = []string{
	autonomous.TemplateChangelogEntry,
	autonomous.TemplateParityIssueDraft,
	autonomous.TemplateDocsSet,
	autonomous.TemplateBoundedLoadStep,
	autonomous.TemplateFinalGate,
	autonomous.TemplateVersionBumpLeftToMerge,
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

// TestStandardTaskRouterReply checks the standard task reply lines for the
// built-in ids: id, title, on/off and mode, one line each, and no "PRD".
func TestStandardTaskRouterReply(t *testing.T) {
	on := config.DefaultConfig()
	on.Autonomous.TaskTemplates.Enabled = true
	reply := buildStandardTasksReply(on, "")

	if strings.Contains(reply, "PRD") {
		t.Fatalf("reply says PRD:\n%s", reply)
	}
	for _, id := range builtInStandardTaskIDs {
		line := standardTaskLine(reply, id)
		if line == "" {
			t.Fatalf("no reply line for built-in id %q:\n%s", id, reply)
		}
		parts := strings.Split(line, " | ")
		if len(parts) != 4 {
			t.Fatalf("line %q: want 4 fields (id, title, on/off, mode), got %d", line, len(parts))
		}
		if parts[1] == "" {
			t.Errorf("line %q: empty title", line)
		}
		if parts[2] != "on" {
			t.Errorf("line %q: want on, got %q", line, parts[2])
		}
		if parts[3] != autonomous.StandardTaskModeModelSession {
			t.Errorf("line %q: want mode %q, got %q", line, autonomous.StandardTaskModeModelSession, parts[3])
		}
	}
	if got := len(strings.Split(reply, "\n")); got != len(builtInStandardTaskIDs) {
		t.Errorf("want one line per built-in task (%d), got %d:\n%s", len(builtInStandardTaskIDs), got, reply)
	}

	off := config.DefaultConfig()
	off.Autonomous.TaskTemplates.Enabled = false
	offReply := buildStandardTasksReply(off, "")
	for _, id := range builtInStandardTaskIDs {
		if line := standardTaskLine(offReply, id); !strings.Contains(line, " | off | ") {
			t.Errorf("disabled config: want off for %q, got %q", id, line)
		}
	}
}

// TestStandardTaskRouterCommand checks that the chat word routes to the
// reply: "standard tasks" parses to the autonomous standard-tasks verb and
// the router answers with the listing.
func TestStandardTaskRouterCommand(t *testing.T) {
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
	for _, id := range builtInStandardTaskIDs {
		if standardTaskLine(joined, id) == "" {
			t.Errorf("chat reply has no line for %q:\n%s", id, joined)
		}
	}
	if strings.Contains(joined, "PRD") {
		t.Errorf("chat reply says PRD:\n%s", joined)
	}
}
