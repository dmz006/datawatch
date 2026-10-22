package inference

import (
	"strings"
	"testing"
)

const gpu1Host = "http://gpu1:11434"

func gpu1Nodes() []NodeAddress {
	return []NodeAddress{{Name: "gpu1", Address: gpu1Host}}
}

// Two different models that both run on the local ollama node produce one
// warning that names both models and the node.
func TestDetectModelNodeConflicts_DifferentModelsOnOneNodeWarnOnce(t *testing.T) {
	in := ModelNodeInputs{
		Nodes: gpu1Nodes(),
		References: []ModelReference{
			{Model: "qwen3.8:27b", Backend: "ollama", Host: gpu1Host},
			{Model: "ollama/qwen3.8-64k:27b", Backend: "claude-code", Host: gpu1Host},
		},
	}
	got := DetectModelNodeConflicts(in)
	if len(got) != 1 {
		t.Fatalf("want exactly 1 warning, got %d: %+v", len(got), got)
	}
	w := got[0]
	if w.Node != "gpu1" {
		t.Errorf("Node = %q, want gpu1 (compute node name for the host)", w.Node)
	}
	if len(w.Models) != 2 || w.Models[0] != "qwen3.8:27b" || w.Models[1] != "qwen3.8-64k:27b" {
		t.Errorf("Models = %v, want [qwen3.8:27b qwen3.8-64k:27b]", w.Models)
	}
	for _, want := range []string{"qwen3.8:27b", "qwen3.8-64k:27b", "gpu1"} {
		if !strings.Contains(w.Message, want) {
			t.Errorf("Message %q does not name %q", w.Message, want)
		}
	}
}

// The same model name repeated, with or without the ollama/ prefix, is not a conflict.
func TestDetectModelNodeConflicts_SameModelRepeatedDoesNotWarn(t *testing.T) {
	in := ModelNodeInputs{
		Nodes: gpu1Nodes(),
		References: []ModelReference{
			{Model: "qwen3.8:27b", Backend: "ollama", Host: gpu1Host},
			{Model: "ollama/qwen3.8:27b", Backend: "claude-code", Host: gpu1Host},
			{Model: "qwen3.8:27b", Backend: "pi", Host: gpu1Host},
		},
	}
	if got := DetectModelNodeConflicts(in); len(got) != 0 {
		t.Fatalf("same model repeated: want no warning, got %+v", got)
	}
}

// Models on different nodes do not conflict, and cloud models that never
// touch an ollama node do not either.
func TestDetectModelNodeConflicts_DifferentNodesDoNotWarn(t *testing.T) {
	in := ModelNodeInputs{
		Nodes: []NodeAddress{
			{Name: "gpu1", Address: gpu1Host},
			{Name: "gpu2", Address: "http://gpu2:11434"},
		},
		References: []ModelReference{
			{Model: "qwen3.8:27b", Backend: "ollama", Host: gpu1Host},
			{Model: "llama3:8b", Backend: "ollama", Host: "http://gpu2:11434"},
			{Model: "claude-opus-5-5", Backend: "claude-code"},
			{Model: "claude-sonnet-5-5", Backend: "claude-code"},
		},
	}
	if got := DetectModelNodeConflicts(in); len(got) != 0 {
		t.Fatalf("different nodes: want no warning, got %+v", got)
	}
}

// Empty and zero values produce no warnings, and a nil-equivalent input is safe.
func TestDetectModelNodeConflicts_EmptyValuesDoNotWarn(t *testing.T) {
	if got := DetectModelNodeConflicts(ModelNodeInputs{}); len(got) != 0 {
		t.Fatalf("zero inputs: want no warning, got %+v", got)
	}
	in := ModelNodeInputs{
		References: []ModelReference{
			{Model: "", Backend: "ollama"},
			{Model: "   ", Backend: "ollama"},
			{Model: "ollama/", Backend: "claude-code"},
			{Model: "qwen3.8:27b", Backend: ""},
			{Model: "qwen3.8:27b", Backend: "claude-code"},
		},
	}
	if got := DetectModelNodeConflicts(in); len(got) != 0 {
		t.Fatalf("empty values: want no warning, got %+v", got)
	}
}

// A bare name on a backend that is not ollama is not counted, even when a
// different ollama model shares the default host.
func TestDetectModelNodeConflicts_BareNameOnNonOllamaBackendIgnored(t *testing.T) {
	in := ModelNodeInputs{
		References: []ModelReference{
			{Model: "qwen3.8:27b", Backend: "ollama"},
			{Model: "claude-opus-5-5", Backend: "claude-code"},
		},
	}
	if got := DetectModelNodeConflicts(in); len(got) != 0 {
		t.Fatalf("bare non-ollama name: want no warning, got %+v", got)
	}
}

// Three different models on one node are listed together in one warning.
func TestDetectModelNodeConflicts_ThreeModelsOneWarning(t *testing.T) {
	in := ModelNodeInputs{
		References: []ModelReference{
			{Model: "a:27b", Backend: "ollama"},
			{Model: "b:27b", Backend: "ollama"},
			{Model: "c:8b", Backend: "ollama"},
		},
	}
	got := DetectModelNodeConflicts(in)
	if len(got) != 1 {
		t.Fatalf("want 1 warning, got %+v", got)
	}
	if got[0].Node != defaultOllamaHost {
		t.Errorf("Node = %q, want default host %q when no host is set", got[0].Node, defaultOllamaHost)
	}
	if want := "models a:27b, b:27b and c:8b target node"; !strings.Contains(got[0].Message, want) {
		t.Errorf("Message %q does not contain %q", got[0].Message, want)
	}
}
