// Model-pin tests for the dispatcher and the host-based pin lookup.

package inference

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

// pinModelAdapter records the model each call would run. When failFirst is
// set, the first call fails transiently so the dispatcher fails over.
type pinModelAdapter struct {
	models    []string
	failFirst bool
}

func (m *pinModelAdapter) Kind() Kind { return KindOllama }

func (m *pinModelAdapter) Infer(_ context.Context, _ *compute.Node, llm *LLM, req Request) (Response, error) {
	model := ResolveModel(llm, req)
	m.models = append(m.models, model)
	if m.failFirst {
		m.failFirst = false
		return Response{}, &ErrTransient{Err: errors.New("node down")}
	}
	return Response{Text: "ok", UsedModel: model}, nil
}

func pinTestNode(name, addr, pin string, swap bool) *compute.Node {
	return &compute.Node{
		Name:            name,
		Kind:            compute.KindOllama,
		Address:         addr,
		NodePinnedModel: pin,
		AllowModelSwap:  swap,
	}
}

func TestDispatcher_PinnedNodeRunsPinnedModelForDefault(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "llama", Kind: KindOllama, Model: "llama3:8b", ComputeNodes: []string{"gpu-1"}})
	nodes := map[string]*compute.Node{"gpu-1": pinTestNode("gpu-1", "http://gpu-1:11434", "qwen3:32b", false)}
	d := NewDispatcher(reg, nodeFn(nodes))
	ad := &pinModelAdapter{}
	d.RegisterAdapter(ad)

	if _, err := d.Call(context.Background(), "llama", Request{Prompt: "ping"}); err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(ad.models) != 1 || ad.models[0] != "qwen3:32b" {
		t.Fatalf("models sent = %v, want [qwen3:32b]", ad.models)
	}
}

func TestDispatcher_PinnedNodeRefusesExplicitOtherModel(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "llama", Kind: KindOllama, Model: "llama3:8b", ComputeNodes: []string{"gpu-1"}})
	nodes := map[string]*compute.Node{"gpu-1": pinTestNode("gpu-1", "http://gpu-1:11434", "qwen3:32b", false)}
	d := NewDispatcher(reg, nodeFn(nodes))
	ad := &pinModelAdapter{}
	d.RegisterAdapter(ad)

	_, err := d.Call(context.Background(), "llama", Request{Prompt: "ping", ModelOverride: "llama3:8b"})
	if err == nil {
		t.Fatal("explicit swap on a pinned node must be refused")
	}
	msg, refused := PinRefusalMessage(err)
	if !refused {
		t.Fatalf("error is not a pin refusal: %v", err)
	}
	for _, want := range []string{"gpu-1", "qwen3:32b", "llama3:8b", "allow_model_swap"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal %q does not name %q", msg, want)
		}
	}
	if len(ad.models) != 0 {
		t.Fatalf("refused call reached the adapter with models %v", ad.models)
	}
}

func TestDispatcher_PinnedNodeAllowsSwapWhenConfigured(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "llama", Kind: KindOllama, Model: "llama3:8b", ComputeNodes: []string{"gpu-1"}})
	nodes := map[string]*compute.Node{"gpu-1": pinTestNode("gpu-1", "http://gpu-1:11434", "qwen3:32b", true)}
	d := NewDispatcher(reg, nodeFn(nodes))
	ad := &pinModelAdapter{}
	d.RegisterAdapter(ad)

	if _, err := d.Call(context.Background(), "llama", Request{Prompt: "ping", ModelOverride: "llama3:8b"}); err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(ad.models) != 1 || ad.models[0] != "llama3:8b" {
		t.Fatalf("models sent = %v, want [llama3:8b]", ad.models)
	}
}

func TestDispatcher_RefusalIsNotFailedOverToAnotherNode(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "llama", Kind: KindOllama, Model: "llama3:8b", ComputeNodes: []string{"gpu-1", "gpu-2"}})
	nodes := map[string]*compute.Node{
		"gpu-1": pinTestNode("gpu-1", "http://gpu-1:11434", "qwen3:32b", false),
		"gpu-2": pinTestNode("gpu-2", "http://gpu-2:11434", "", false),
	}
	d := NewDispatcher(reg, nodeFn(nodes))
	ad := &pinModelAdapter{}
	d.RegisterAdapter(ad)

	_, err := d.Call(context.Background(), "llama", Request{Prompt: "ping", ModelOverride: "llama3:8b"})
	if _, refused := PinRefusalMessage(err); !refused {
		t.Fatalf("want pin refusal, got %v", err)
	}
	if len(ad.models) != 0 {
		t.Fatalf("refusal must not fail over; adapter saw %v", ad.models)
	}
}

func TestDispatcher_FailoverResolvesPinPerNode(t *testing.T) {
	// gpu-1 is pinned and fails transiently. gpu-2 has no pin, so it must get
	// the LLM default, not the model gpu-1 was pinned to.
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "llama", Kind: KindOllama, Model: "llama3:8b", ComputeNodes: []string{"gpu-1", "gpu-2"}})
	nodes := map[string]*compute.Node{
		"gpu-1": pinTestNode("gpu-1", "http://gpu-1:11434", "qwen3:32b", false),
		"gpu-2": pinTestNode("gpu-2", "http://gpu-2:11434", "", false),
	}
	d := NewDispatcher(reg, nodeFn(nodes))
	ad := &pinModelAdapter{failFirst: true}
	d.RegisterAdapter(ad)

	if _, err := d.Call(context.Background(), "llama", Request{Prompt: "ping"}); err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(ad.models) != 2 || ad.models[0] != "qwen3:32b" || ad.models[1] != "llama3:8b" {
		t.Fatalf("models sent = %v, want [qwen3:32b llama3:8b]", ad.models)
	}
}

func TestDispatcher_UnpinnedNodePassesRequestThrough(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(&LLM{Name: "llama", Kind: KindOllama, Model: "llama3:8b", ComputeNodes: []string{"gpu-1"}})
	nodes := map[string]*compute.Node{"gpu-1": pinTestNode("gpu-1", "http://gpu-1:11434", "", false)}
	d := NewDispatcher(reg, nodeFn(nodes))
	ad := &pinModelAdapter{}
	d.RegisterAdapter(ad)

	if _, err := d.Call(context.Background(), "llama", Request{Prompt: "ping", ModelOverride: "mistral:7b"}); err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(ad.models) != 1 || ad.models[0] != "mistral:7b" {
		t.Fatalf("models sent = %v, want [mistral:7b]", ad.models)
	}
}

func newPinComputeRegistry(t *testing.T, nodes ...*compute.Node) *compute.Registry {
	t.Helper()
	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	for _, n := range nodes {
		if err := reg.Add(n); err != nil {
			t.Fatalf("add %s: %v", n.Name, err)
		}
	}
	return reg
}

func TestHostPins_MatchesNodeByAddressIgnoringCaseAndSlash(t *testing.T) {
	reg := newPinComputeRegistry(t, pinTestNode("gpu-1", "http://gpu-1:11434/", "qwen3:32b", false))
	pins := NewHostPins(reg)

	got, err := pins.ResolveModel("HTTP://GPU-1:11434", "llama3:8b", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "qwen3:32b" {
		t.Fatalf("got %q, want the pinned qwen3:32b", got)
	}
}

func TestHostPins_UnmatchedHostPassesThrough(t *testing.T) {
	reg := newPinComputeRegistry(t, pinTestNode("gpu-1", "http://gpu-1:11434", "qwen3:32b", false))
	got, err := NewHostPins(reg).ResolveModel("http://other:11434", "llama3:8b", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "llama3:8b" {
		t.Fatalf("got %q, want the requested llama3:8b", got)
	}
}

func TestHostPins_EmptyHostUsesLocalDefault(t *testing.T) {
	reg := newPinComputeRegistry(t, pinTestNode("local", "http://localhost:11434", "qwen3:32b", false))
	got, err := NewHostPins(reg).ResolveModel("", "llama3:8b", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "qwen3:32b" {
		t.Fatalf("got %q, want the pinned qwen3:32b", got)
	}
}

func TestHostPins_NilRegistryAndNilPinsPassThrough(t *testing.T) {
	for name, pins := range map[string]*HostPins{"nil registry": NewHostPins(nil), "nil pins": nil} {
		got, err := pins.ResolveModel("http://gpu-1:11434", "llama3:8b", "")
		if err != nil {
			t.Fatalf("%s: resolve: %v", name, err)
		}
		if got != "llama3:8b" {
			t.Fatalf("%s: got %q, want llama3:8b", name, got)
		}
	}
}

func TestHostPins_ExplicitOverrideRefusedWithoutSwap(t *testing.T) {
	reg := newPinComputeRegistry(t, pinTestNode("gpu-1", "http://gpu-1:11434", "qwen3:32b", false))
	_, err := NewHostPins(reg).ResolveModel("http://gpu-1:11434", "llama3:8b", "mistral:7b")
	msg, refused := PinRefusalMessage(fmt.Errorf("wrapped: %w", err))
	if !refused {
		t.Fatalf("want refusal, got %v", err)
	}
	if !strings.Contains(msg, "mistral:7b") {
		t.Fatalf("refusal %q does not name the refused model", msg)
	}
}

func TestPinRefusalMessage_IgnoresOtherErrors(t *testing.T) {
	if _, ok := PinRefusalMessage(errors.New("connection refused")); ok {
		t.Fatal("a transport error must not read as a pin refusal")
	}
	if _, ok := PinRefusalMessage(nil); ok {
		t.Fatal("nil must not read as a pin refusal")
	}
}
