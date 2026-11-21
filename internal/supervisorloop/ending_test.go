package supervisorloop

import (
	"strings"
	"testing"
)

// A supervisor pass that is not told the completion marker never ends: the daemon
// ends a one-shot session only when it sees a DATAWATCH_COMPLETE line. Every way a
// pass is started carries the instruction.
func TestEveryPassPromptNamesTheCompletionMarker(t *testing.T) {
	for _, tick := range AllTicks {
		if p := SupervisorTickPrompt(tick); !strings.Contains(p, "DATAWATCH_COMPLETE:") {
			t.Errorf("tick %q prompt does not name the completion marker: %q", tick, p)
		}
	}
	for _, trig := range AllTriggers {
		if p := SupervisorTriggerPrompt(trig); !strings.Contains(p, "DATAWATCH_COMPLETE:") {
			t.Errorf("trigger %q prompt does not name the completion marker: %q", trig, p)
		}
	}
}

func TestLargeHandoverBootstrapNamesTheCompletionMarker(t *testing.T) {
	p, err := DeliverablePrompt(t.TempDir(), "watch", strings.Repeat("x", MaxDeliveredPromptChars+10))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, "DATAWATCH_COMPLETE:") {
		t.Errorf("bootstrap prompt does not name the completion marker: %q", p)
	}
	if len(p) > MaxDeliveredPromptChars {
		t.Errorf("bootstrap prompt is %d characters, want at most %d", len(p), MaxDeliveredPromptChars)
	}
}

func TestRenderedHandoverEndsWithTheInstructionWithinTheBound(t *testing.T) {
	h := Handover{Project: "p"}
	h.Rules.DefaultPack = strings.Repeat("rule line\n", 5000)
	h.Rules.ProjectPack = strings.Repeat("project rule line\n", 5000)
	out := h.Render()
	if len(out) > MaxHandoverChars {
		t.Fatalf("handover is %d characters, want at most %d", len(out), MaxHandoverChars)
	}
	if !strings.Contains(out, "## Ending the pass") || !strings.HasSuffix(strings.TrimSpace(out), EndingInstruction) {
		t.Errorf("handover does not end with the completion instruction, tail: %q", out[len(out)-200:])
	}
}
