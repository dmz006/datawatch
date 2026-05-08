// BL289 (v6.22.2 audit-honesty backfill) — fallback test: Council Mode
// runs cleanly when no LLM backend is wired. The Orchestrator's LLMFn
// hook is nil-guarded (council.go:425); when nil, respond() emits a
// deterministic stub instead of crashing or hanging.
//
// This locks in the no-GPU constraint operator filed (BL289): "no
// feature may REQUIRE Ollama to function" — Council ships in stub
// mode and the daemon stays responsive.

package council

import (
	"strings"
	"testing"
)

func TestRespond_NoLLMFn_ReturnsStub(t *testing.T) {
	o := &Orchestrator{} // LLMFn left nil
	persona := Persona{
		Name:         "test-persona",
		SystemPrompt: "You are a test persona. Respond pragmatically.",
	}
	resp := o.respond(persona, "should we cache the API?", nil)

	// Stub MUST contain the persona name + a recognizable stub marker so
	// upstream consumers (synthesize, PWA renderer) can detect stub mode.
	if !strings.Contains(resp, "test-persona") {
		t.Errorf("stub missing persona name: %q", resp)
	}
	if !strings.Contains(resp, "STUB") {
		t.Errorf("stub missing STUB marker: %q", resp)
	}
}

func TestSynthesize_NoLLMFn_AggregatesStubResponses(t *testing.T) {
	o := &Orchestrator{}
	run := &Run{
		Rounds: []Round{
			{
				Index: 1,
				Responses: map[string]string{
					"persona-a": o.respond(Persona{Name: "persona-a", SystemPrompt: "A."}, "hi", nil),
					"persona-b": o.respond(Persona{Name: "persona-b", SystemPrompt: "B."}, "hi", nil),
				},
			},
		},
	}
	consensus, dissent := synthesize(run)

	if !strings.Contains(consensus, "stub mode") {
		t.Errorf("synthesize must mark stub mode in consensus: %q", consensus)
	}
	if !strings.Contains(consensus, "persona-a") || !strings.Contains(consensus, "persona-b") {
		t.Errorf("synthesize must aggregate every persona's response: %q", consensus)
	}
	if dissent != "" {
		t.Errorf("dissent should be empty in stub mode: %q", dissent)
	}
}
