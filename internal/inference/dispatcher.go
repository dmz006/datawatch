// v7.0.0 S2 — Dispatcher: routes LLM-call requests through ordered
// ComputeNode failover with operator-controlled placement.
//
// Per BL295 design Q3 (no round-robin) + Q11 (per-LLM ordered failover
// list of ComputeNodes; failover one-retry on next host on transient
// failure):
//
//	dispatcher.Call(ctx, llmName, req) →
//	  registry.Get(llmName) → LLM definition with ordered ComputeNodes
//	  for each ComputeNode in order:
//	    skip if InMaintenance, !AllowsConsumer, or kind-mismatch
//	    adapter := adapters.For(llm.Kind)
//	    response, err := adapter.Infer(ctx, node, llm, req)
//	    if err is transient → next node
//	    if err is final → return err
//	  if all nodes exhausted → return ErrNoBackend
//
// Local kinds (ollama, openwebui, opencode) require a real reachable
// ComputeNode. Cloud kinds (claude) IGNORE the ComputeNode list at
// the protocol level (Anthropic API endpoint is fixed) but still walk
// the list for RBAC/permissions accounting; an empty list = "any
// allowed consumer can call" with hard-coded api.anthropic.com.

package inference

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

// Request is the inference call shape (kind-agnostic).
type Request struct {
	// Prompt is the user-facing prompt for chat / completion.
	Prompt string
	// SystemPrompt is optional preamble (kind-specific encoding —
	// ollama puts it in `system`, openwebui prepends a `system`
	// message, claude uses `system` field).
	SystemPrompt string
	// ModelOverride forces a specific model name. When empty, use
	// llm.Model.
	ModelOverride string
	// Consumer names the calling subsystem (council|ask|agent_spawn|
	// session_spawn|wizard) for ComputeNode RBAC enforcement.
	Consumer string
}

// Response from one inference call.
type Response struct {
	Text       string
	UsedNode   string // ComputeNode name that served the call (empty for cloud kinds)
	UsedModel  string
	Backend    Kind
	DurationMs int64
}

// Adapter is the kind-specific protocol contract.
type Adapter interface {
	Kind() Kind
	// Infer issues one inference call. node may be nil for cloud
	// kinds. Should return ErrTransient (wrapped) for failover-eligible
	// errors; any other error is final.
	Infer(ctx context.Context, node *compute.Node, llm *LLM, req Request) (Response, error)
}

// ErrTransient marks an error as failover-eligible.
type ErrTransient struct{ Err error }

func (e *ErrTransient) Error() string { return e.Err.Error() }
func (e *ErrTransient) Unwrap() error { return e.Err }

// IsTransient is the failover detector.
func IsTransient(err error) bool {
	var te *ErrTransient
	return errors.As(err, &te)
}

// Dispatcher routes Request → Adapter via the registry + ComputeNode
// resolver.
type Dispatcher struct {
	registry  *Registry
	computeFn func(name string) (*compute.Node, error)
	adapters  map[Kind]Adapter
}

// NewDispatcher wires the LLM registry + a ComputeNode lookup
// function. Adapters are registered via RegisterAdapter.
func NewDispatcher(reg *Registry, computeFn func(name string) (*compute.Node, error)) *Dispatcher {
	return &Dispatcher{
		registry:  reg,
		computeFn: computeFn,
		adapters:  map[Kind]Adapter{},
	}
}

// RegisterAdapter binds an adapter implementation to its Kind.
// Adapters are the unit of pluggability — new kinds (gemini, vllm,
// etc.) land as new adapter packages without touching dispatcher.
func (d *Dispatcher) RegisterAdapter(a Adapter) { d.adapters[a.Kind()] = a }

// Call resolves the LLM, walks ordered ComputeNodes, and dispatches
// to the kind adapter. ErrNoBackend when all Nodes refuse / fail.
func (d *Dispatcher) Call(ctx context.Context, llmName string, req Request) (Response, error) {
	llm, err := d.registry.Get(llmName)
	if err != nil {
		return Response{}, fmt.Errorf("llm %q: %w", llmName, err)
	}
	// v7.0.0-alpha.16 #247 — operator-disabled LLMs bypass dispatcher.
	if llm.Disabled {
		return Response{}, fmt.Errorf("llm %q: disabled by operator (toggle on in Compute → LLMs to re-enable)", llmName)
	}
	adapter, ok := d.adapters[llm.Kind]
	if !ok {
		return Response{}, fmt.Errorf("no adapter for kind %q (registered: %v)", llm.Kind, d.adapterKinds())
	}
	// Cloud kinds: dispatch directly without ComputeNode resolution.
	if isCloudKind(llm.Kind) {
		return d.callOnce(ctx, adapter, nil, llm, req)
	}
	// Local kinds: walk ordered ComputeNodes.
	if len(llm.ComputeNodes) == 0 {
		return Response{}, fmt.Errorf("%w: llm %q has no ComputeNodes configured", ErrNoBackend, llmName)
	}
	var lastErr error
	for _, nodeName := range llm.ComputeNodes {
		node, err := d.computeFn(nodeName)
		if err != nil {
			lastErr = fmt.Errorf("compute node %q: %w", nodeName, err)
			continue
		}
		// v7.0.0-alpha.23 (Q2 fail-loud) — refuse deprecated-Kind nodes.
		// Operator must migrate via PWA banner / PUT /api/migration/compute-kinds.
		if node.Kind.IsDeprecated() {
			lastErr = fmt.Errorf("compute node %q has deprecated Kind %q — migrate via Settings → Compute → Migration banner", nodeName, node.Kind)
			continue
		}
		// v7.0.0-alpha.23 (Q6) — operator-disabled nodes bypass dispatch.
		// PWA renders the switch as OFF with no badge (operator-set state).
		if node.Disabled {
			lastErr = fmt.Errorf("compute node %q: disabled by operator", nodeName)
			continue
		}
		if node.InMaintenance(time.Now().UTC()) {
			lastErr = fmt.Errorf("compute node %q: in maintenance", nodeName)
			continue
		}
		if req.Consumer != "" && !node.AllowsConsumer(req.Consumer) {
			lastErr = fmt.Errorf("compute node %q: consumer %q denied", nodeName, req.Consumer)
			continue
		}
		resp, err := d.callOnce(ctx, adapter, node, llm, req)
		if err == nil {
			return resp, nil
		}
		if !IsTransient(err) {
			return Response{}, fmt.Errorf("compute node %q: %w", nodeName, err)
		}
		lastErr = fmt.Errorf("compute node %q (transient): %w", nodeName, err)
		// Loop continues to next node.
	}
	if lastErr != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrNoBackend, lastErr)
	}
	return Response{}, ErrNoBackend
}

func (d *Dispatcher) callOnce(ctx context.Context, adapter Adapter, node *compute.Node, llm *LLM, req Request) (Response, error) {
	start := time.Now()
	resp, err := adapter.Infer(ctx, node, llm, req)
	resp.DurationMs = time.Since(start).Milliseconds()
	resp.Backend = llm.Kind
	if node != nil && resp.UsedNode == "" {
		resp.UsedNode = node.Name
	}
	if resp.UsedModel == "" {
		if req.ModelOverride != "" {
			resp.UsedModel = req.ModelOverride
		} else {
			resp.UsedModel = llm.Model
		}
	}
	return resp, err
}

func (d *Dispatcher) adapterKinds() []string {
	out := make([]string, 0, len(d.adapters))
	for k := range d.adapters {
		out = append(out, string(k))
	}
	return out
}

// isCloudKind returns true for kinds whose protocol endpoint is
// hard-coded (no ComputeNode resolution needed at the network layer).
func isCloudKind(k Kind) bool {
	return k == KindClaude
}

// ResolveModel picks the effective model name for a call.
func ResolveModel(llm *LLM, req Request) string {
	if req.ModelOverride != "" {
		return req.ModelOverride
	}
	return llm.Model
}

// ResolveTimeout picks the effective per-call timeout. Defaults: 300s
// for local kinds (cold-model latency observation per v5.26.9), 60s
// for cloud kinds.
func ResolveTimeout(llm *LLM) time.Duration {
	if llm.TimeoutSeconds > 0 {
		return time.Duration(llm.TimeoutSeconds) * time.Second
	}
	if isCloudKind(llm.Kind) {
		return 60 * time.Second
	}
	return 300 * time.Second
}

// FormatChatPrompt is a tiny helper — adapters that take a single
// "prompt" string fold the system prompt in front. Used by the ollama
// adapter today.
func FormatChatPrompt(req Request) string {
	if req.SystemPrompt == "" {
		return req.Prompt
	}
	return strings.TrimSpace(req.SystemPrompt) + "\n\n" + req.Prompt
}
