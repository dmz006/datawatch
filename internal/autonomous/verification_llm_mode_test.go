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
	r, err := ResolveVerificationLLMWithMode(nil, nil, nil, cfg, empty)
	if got := r.Mode; err != nil || got != "generate" {
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
	r, err := ResolveVerificationLLMWithMode(nil, nil, nil, cfg, lookup)
	if err != nil || r.Mode != "classify" || r.Backend != "ollama" || r.Source != VerifierSourceGlobal {
		t.Fatalf("got %+v", r)
	}
	// override chain unchanged: task backend wins, mode follows the chosen entry.
	task := &Task{VerificationBackend: "other"}
	if r, err := ResolveVerificationLLMWithMode(nil, task, nil, cfg, lookup); err != nil || r.Mode != "generate" || r.Source != VerifierSourceTask {
		t.Fatalf("got %+v", r)
	}
}

func TestResolveVerificationLLMModeInvalidRejected(t *testing.T) {
	cfg := Config{VerificationBackend: "ollama", VerificationModel: "m"}
	bad := func(string, string) inference.VerificationMode { return "bogus" }
	r, err := ResolveVerificationLLMWithMode(nil, nil, nil, cfg, bad)
	if err == nil {
		t.Fatalf("invalid mode accepted: %+v", r)
	}
	if r.Mode != "" {
		t.Fatalf("mode = %q on rejection, want empty", r.Mode)
	}
}
