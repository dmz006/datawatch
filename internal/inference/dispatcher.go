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

// ServerStoreIface resolves a registered server by name.
// The multiserver.Store implements this interface.
type ServerStoreIface interface {
	// GetByName returns the URL and bearer token for the named server entry.
	// Returns ("", "", false) when the name is not found.
	GetByName(name string) (url, token string, ok bool)
}

// Dispatcher routes Request → Adapter via the registry + ComputeNode
// resolver.
type Dispatcher struct {
	registry    *Registry
	computeFn   func(name string) (*compute.Node, error)
	adapters    map[Kind]Adapter
	serverStore ServerStoreIface
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

// SetServerStore wires the multiserver store so the dispatcher can resolve
// peer URLs for datawatch-proxy routing (BL320).
func (d *Dispatcher) SetServerStore(s ServerStoreIface) { d.serverStore = s }

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
		// v8.0 BL319 — docker-network routing: ensure container is running
		// and rewrite Address to the container's IP before dispatch.
		if node.Routing == compute.RoutingDockerNetwork {
			lifecycle := &compute.DockerLifecycle{}
			ip, port, lcErr := lifecycle.EnsureRunning(ctx, node)
			if lcErr != nil {
				lastErr = fmt.Errorf("compute node %q (docker-network): %w", nodeName, &ErrTransient{Err: lcErr})
				continue
			}
			node = node.WithAddress(fmt.Sprintf("http://%s:%d", ip, port))
		}

		// v8.0 BL320 — datawatch-proxy routing: delegate inference to peer.
		if node.Routing == compute.RoutingDatawatchProxy {
			if d.serverStore == nil {
				return Response{}, fmt.Errorf("datawatch-proxy: server store not wired")
			}
			cfg := node.RoutingDatawatchProxy
			peerURL, peerToken, ok := d.serverStore.GetByName(cfg.Peer)
			if !ok {
				return Response{}, fmt.Errorf("datawatch-proxy: peer %q not registered", cfg.Peer)
			}
			router := &ProxyRouter{PeerURL: peerURL, PeerToken: peerToken}
			proxyResp, proxyErr := router.Infer(ctx, cfg.RemoteLLMName, req)
			if proxyErr != nil {
				if IsTransient(proxyErr) {
					lastErr = fmt.Errorf("compute node %q (datawatch-proxy, transient): %w", nodeName, proxyErr)
					continue
				}
				return Response{}, fmt.Errorf("compute node %q (datawatch-proxy): %w", nodeName, proxyErr)
			}
			proxyResp.UsedNode = nodeName
			proxyResp.DurationMs = time.Since(time.Now()).Milliseconds() // best-effort
			return proxyResp, nil
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

// IsSessionBackendKind returns true for coding-agent kinds that run in a
// tmux session and have no inference adapter (#46). Callers (e.g. the
// enable-pretest path) skip dispatcher.Call for these kinds.
func IsSessionBackendKind(k Kind) bool {
	switch k {
	case KindClaudeCode, KindOpenCodeACP, KindOpenCodePrompt,
		KindAider, KindGoose, KindGemini, KindShell:
		return true
	}
	return false
}

// ResolveModel picks the effective model name for a call.
func ResolveModel(llm *LLM, req Request) string {
	if req.ModelOverride != "" {
		return req.ModelOverride
	}
	return llm.Model
}

// ResolveTimeout picks the effective per-call timeout. Explicit
// TimeoutSeconds takes precedence, then DefaultEffort scales the base
// (quick=0.4×, normal=1×, high=3× — #48). Defaults: 300s local, 60s cloud.
func ResolveTimeout(llm *LLM) time.Duration {
	if llm.TimeoutSeconds > 0 {
		return time.Duration(llm.TimeoutSeconds) * time.Second
	}
	base := 300 * time.Second
	if isCloudKind(llm.Kind) {
		base = 60 * time.Second
	}
	switch llm.DefaultEffort {
	case "quick":
		return time.Duration(float64(base) * 0.4)
	case "high":
		return time.Duration(float64(base) * 3.0)
	default: // "normal" or ""
		return base
	}
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
