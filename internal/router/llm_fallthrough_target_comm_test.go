// Comm-channel surface for the per-LLM fall-through mark: `llm add|update
// <name> fallthrough_target=true|false` becomes a JSON boolean in the body.

package router

import (
	"strings"
	"testing"
)

func TestParseLLMKVPairs_FallthroughTarget(t *testing.T) {
	for in, want := range map[string]bool{
		"fallthrough_target=true":  true,
		"fallthrough_target=TRUE":  true,
		"fallthrough_target=1":     true,
		"fallthrough_target=false": false,
		"fallthrough_target=no":    false,
	} {
		body := parseLLMKVPairs("kind=ollama " + in)
		got, ok := body["fallthrough_target"].(bool)
		if !ok || got != want {
			t.Errorf("%s: body[fallthrough_target] = %v (%T), want bool %v", in, body["fallthrough_target"], body["fallthrough_target"], want)
		}
	}
	if _, ok := parseLLMKVPairs("kind=ollama")["fallthrough_target"]; ok {
		t.Error("an absent key must stay absent so an update keeps the stored value")
	}
}

func TestLLMUsage_NamesFallthroughTarget(t *testing.T) {
	if !strings.Contains(llmUsage, "fallthrough_target=") {
		t.Error("llm help does not document fallthrough_target")
	}
}
