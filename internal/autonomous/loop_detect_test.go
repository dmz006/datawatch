package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/pilog"
)

func announceTurn(text string) pilog.Turn {
	return pilog.Turn{Text: text}
}

func toolTurn(name, args string) pilog.Turn {
	return pilog.Turn{Text: "proceeding", ToolCalls: []pilog.ToolCall{{Name: name, Args: args}}}
}

func TestDetectLoop_FiresOnAnnounceOnlyTurns(t *testing.T) {
	turns := []pilog.Turn{
		announceTurn("The store looks right. Now I will create the test file."),
		announceTurn("Let me write the implementation."),
		announceTurn("I will now finish the remaining tests."),
	}
	f, ok := DetectLoop(turns, 3)
	if !ok {
		t.Fatal("DetectLoop = false, want a finding for three announce-only turns")
	}
	if f.Kind != KindAnnounce {
		t.Errorf("Kind = %q, want %q", f.Kind, KindAnnounce)
	}
	if f.Action == "" {
		t.Error("Action is empty, want the announced action phrase")
	}
	if f.Hint == "" {
		t.Error("Hint is empty, want a hint about calling the write tool")
	}
	if !strings.Contains(f.Hint, "write") {
		t.Errorf("Hint = %q, want a mention of the write tool", f.Hint)
	}
}

func TestDetectLoop_FiresOnRepeatedToolCalls(t *testing.T) {
	turns := []pilog.Turn{
		toolTurn("read", `{"path":"internal/autonomous/models.go"}`),
		toolTurn("read", `{"path":"internal/autonomous/models.go"}`),
		toolTurn("read", `{"path":"internal/autonomous/models.go"}`),
	}
	f, ok := DetectLoop(turns, 3)
	if !ok {
		t.Fatal("DetectLoop = false, want a finding for three identical tool calls")
	}
	if f.Kind != KindRepeat {
		t.Errorf("Kind = %q, want %q", f.Kind, KindRepeat)
	}
	if f.Action != "read" {
		t.Errorf("Action = %q, want %q", f.Action, "read")
	}
	if f.Hint == "" {
		t.Error("Hint is empty, want a hint about calling the write tool")
	}
}

func TestDetectLoop_NoFireOnNormalProgress(t *testing.T) {
	// Varied tool calls across the last three turns.
	varied := []pilog.Turn{
		toolTurn("read", `{"path":"a.go"}`),
		toolTurn("edit", `{"path":"a.go"}`),
		toolTurn("bash", `{"command":"go build ./..."}`),
	}
	if f, ok := DetectLoop(varied, 3); ok {
		t.Errorf("DetectLoop fired (%+v) on varied tool calls, want no finding", f)
	}

	// An announcement followed by the actual tool call is progress.
	announcedThenActed := []pilog.Turn{
		announceTurn("Next, I'll create the helper."),
		toolTurn("write", `{"path":"helper.go"}`),
		toolTurn("bash", `{"command":"rtk go test ./..."}`),
	}
	if f, ok := DetectLoop(announcedThenActed, 3); ok {
		t.Errorf("DetectLoop fired (%+v) on announcement followed by a tool call, want no finding", f)
	}
}

func TestDetectLoop_NoFireWhenDisabledOrNotEnoughTurns(t *testing.T) {
	turns := []pilog.Turn{
		announceTurn("Now I will create the file."),
		announceTurn("Now I will create the file."),
		announceTurn("Now I will create the file."),
	}
	if f, ok := DetectLoop(turns, 0); ok {
		t.Errorf("DetectLoop fired (%+v) with n = 0, want no finding", f)
	}
	if f, ok := DetectLoop(turns, -1); ok {
		t.Errorf("DetectLoop fired (%+v) with n = -1, want no finding", f)
	}
	if f, ok := DetectLoop(turns, 4); ok {
		t.Errorf("DetectLoop fired (%+v) with fewer than n turns, want no finding", f)
	}
	if f, ok := DetectLoop(nil, 3); ok {
		t.Errorf("DetectLoop fired (%+v) with no turns, want no finding", f)
	}
}
