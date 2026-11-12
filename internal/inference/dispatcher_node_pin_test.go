package inference

import (
	"context"
	"errors"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

// TestDispatcher_PinnedNodeRunsOnlyThere: a call that names one of the LLM's
// compute nodes is served by that node alone, with no walk over the others.
func TestDispatcher_PinnedNodeRunsOnlyThere(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "llama", Kind: KindOllama, Model: "llama3:8b", ComputeNodes: []string{"gpu-1", "gpu-2"}})
	nodes := map[string]*compute.Node{
		"gpu-1": {Name: "gpu-1", Kind: compute.KindOllama, Address: "http://gpu-1:11434"},
		"gpu-2": {Name: "gpu-2", Kind: compute.KindOllama, Address: "http://gpu-2:11434"},
	}
	d := NewDispatcher(reg, nodeFn(nodes))
	mock := &mockAdapter{kind: KindOllama, respText: "pinned"}
	d.RegisterAdapter(mock)

	resp, err := d.Call(context.Background(), "llama", Request{Prompt: "ping", Node: "gpu-2"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(mock.calls) != 1 || mock.calls[0].NodeName != "gpu-2" {
		t.Fatalf("calls = %+v, want one call on gpu-2", mock.calls)
	}
	if resp.UsedNode != "gpu-2" {
		t.Fatalf("used node = %q, want gpu-2", resp.UsedNode)
	}
}

// TestDispatcher_PinnedNodeOutsideEntryRefused: a node the LLM does not list
// is refused before any adapter runs.
func TestDispatcher_PinnedNodeOutsideEntryRefused(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "llama", Kind: KindOllama, ComputeNodes: []string{"gpu-1"}})
	nodes := map[string]*compute.Node{
		"gpu-1": {Name: "gpu-1", Kind: compute.KindOllama, Address: "http://gpu-1:11434"},
		"gpu-9": {Name: "gpu-9", Kind: compute.KindOllama, Address: "http://gpu-9:11434"},
	}
	d := NewDispatcher(reg, nodeFn(nodes))
	mock := &mockAdapter{kind: KindOllama, respText: "never"}
	d.RegisterAdapter(mock)

	_, err := d.Call(context.Background(), "llama", Request{Prompt: "ping", Node: "gpu-9"})
	if !errors.Is(err, ErrNoBackend) {
		t.Fatalf("err = %v, want ErrNoBackend", err)
	}
	if len(mock.calls) != 0 {
		t.Fatalf("adapter ran %d times for an unlisted node", len(mock.calls))
	}
}
