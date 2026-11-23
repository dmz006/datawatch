package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

func TestNodeInPooledEntry(t *testing.T) {
	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha", "beta"}}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(&inference.LLM{Name: "solo", Kind: "ollama", ComputeNodes: []string{"gamma"}}); err != nil {
		t.Fatal(err)
	}
	for node, want := range map[string]bool{"alpha": true, "beta": true, "gamma": false, "unknown": false, "": false} {
		if got := nodeInPooledEntry(reg, node); got != want {
			t.Errorf("nodeInPooledEntry(%q) = %v, want %v", node, got, want)
		}
	}
	if nodeInPooledEntry(nil, "alpha") {
		t.Error("nil registry reported a pooled node")
	}
}
