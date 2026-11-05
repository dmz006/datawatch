package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

func TestResolveVerificationLLMModeAbsentIsGenerate(t *testing.T) {
	cfg := Config{VerificationBackend: "ollama", VerificationModel: "m"}
	if got := ResolveVerificationLLM(nil, nil, nil, cfg).Mode; got != "generate" {
		t.Fatalf("mode = %q, want generate", got)
	}
	empty := func(string, string) inference.VerificationMode { return "" }
	if got := ResolveVerificationLLMWithMode(nil, nil, nil, cfg, empty).Mode; got != "generate" {
		t.Fatalf("mode with empty lookup = %q, want generate", got)
	}
}

func TestResolveVerificationLLMModeFromEntry(t *testing.T) {
	cfg := Config{VerificationBackend: "ollama", VerificationModel: "m"}
	lookup := func(b, m string) inference.VerificationMode {
		if b == "ollama" && m == "m" {
			return inference.VerificationModeClassify
		}
		return ""
	}
	r := ResolveVerificationLLMWithMode(nil, nil, nil, cfg, lookup)
	if r.Mode != "classify" || r.Backend != "ollama" || r.Source != VerifierSourceGlobal {
		t.Fatalf("got %+v", r)
	}
	// override chain unchanged: task backend wins, mode follows the chosen entry.
	task := &Task{VerificationBackend: "other"}
	if r := ResolveVerificationLLMWithMode(nil, task, nil, cfg, lookup); r.Mode != "generate" || r.Source != VerifierSourceTask {
		t.Fatalf("got %+v", r)
	}
}
