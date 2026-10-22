// Per-node model pinning for local ComputeNodes.
//
// A compute.Node may pin one model (node_pinned_model). Pinning keeps
// that model resident on the node, so datawatch never swaps it
// implicitly:
//
//   - a request with no explicit model runs on the pinned model, even
//     when the caller passed the backend default;
//   - an explicit request for the pinned model runs on it;
//   - an explicit request for a different model is refused unless the
//     node sets allow_model_swap, in which case that model runs.
//
// A node with no pin passes the request through unchanged. A node name
// the registry does not know also passes through, because it carries no
// pin to enforce.

package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/compute"
)

// PinResolver chooses the model a request on a ComputeNode runs,
// honouring that node's pin and allow_model_swap settings.
type PinResolver struct {
	lookup func(name string) (*compute.Node, error)
}

// NewPinResolver builds a resolver that reads node settings through
// lookup. compute.Registry.Get matches the lookup signature directly.
func NewPinResolver(lookup func(name string) (*compute.Node, error)) *PinResolver {
	return &PinResolver{lookup: lookup}
}

// ResolveModel returns the model a request on nodeName should run.
//
// requestedModel is the model the caller would use by default and may
// be empty. explicitOverride is a model the caller asked for by name
// and is empty when the caller did not ask for one. When both are set,
// explicitOverride wins as the target.
//
// It returns an error only when the node cannot be looked up, or when an
// explicit override would replace a pinned model on a node that does not
// allow swapping. Use Generate to also send the request.
func (r *PinResolver) ResolveModel(ctx context.Context, nodeName, requestedModel, explicitOverride string) (string, error) {
	node, err := r.node(nodeName)
	if err != nil {
		return "", err
	}
	return pinnedModel(node, nodeName, requestedModel, explicitOverride)
}

// Generate resolves the model for nodeName exactly as ResolveModel does,
// then posts one non-streaming /api/generate request to the node's
// Ollama endpoint. It returns the model that ran and the response text.
// A refused model never reaches the node.
func (r *PinResolver) Generate(ctx context.Context, nodeName, requestedModel, explicitOverride, prompt string) (string, string, error) {
	node, err := r.node(nodeName)
	if err != nil {
		return "", "", err
	}
	if node == nil {
		return "", "", fmt.Errorf("compute node %q not found", nodeName)
	}
	if strings.TrimSpace(node.Address) == "" {
		return "", "", fmt.Errorf("compute node %q has no address", nodeName)
	}
	model, err := pinnedModel(node, nodeName, requestedModel, explicitOverride)
	if err != nil {
		return "", "", err
	}
	if model == "" {
		return "", "", fmt.Errorf("compute node %q: no model (pin one or pass a model)", nodeName)
	}
	body, err := json.Marshal(map[string]any{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	})
	if err != nil {
		return "", "", fmt.Errorf("ollama: encode request: %w", err)
	}
	url := strings.TrimRight(node.Address, "/") + "/api/generate"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", "", &ErrTransient{Err: fmt.Errorf("ollama: %w", err)}
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 500 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", &ErrTransient{Err: fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, string(buf))}
	}
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", fmt.Errorf("ollama decode: %w", err)
	}
	return model, strings.TrimSpace(out.Response), nil
}

// node returns the named ComputeNode, or nil with no error when the
// registry does not know the name. Any other lookup failure is returned.
func (r *PinResolver) node(nodeName string) (*compute.Node, error) {
	node, err := r.lookup(nodeName)
	if errors.Is(err, compute.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("compute node %q: %w", nodeName, err)
	}
	return node, nil
}

// pinnedModel applies the pin rules to one request. A nil node means the
// name is unconfigured, so the request passes through.
func pinnedModel(node *compute.Node, nodeName, requestedModel, explicitOverride string) (string, error) {
	target := requestedModel
	explicit := explicitOverride != ""
	if explicit {
		target = explicitOverride
	}
	if node == nil || node.NodePinnedModel == "" {
		return target, nil
	}
	pinned := node.NodePinnedModel
	// Implicit requests and explicit requests for the pinned model both
	// run on the pin. Datawatch never swaps a pinned model implicitly.
	if !explicit || target == pinned {
		return pinned, nil
	}
	if node.AllowModelSwap {
		return target, nil
	}
	return "", &PinRefusedError{Node: nodeName, Pinned: pinned, Requested: target}
}

// PinRefusedError reports a request refused because it would replace the
// pinned model of a node that does not allow swapping.
type PinRefusedError struct {
	Node      string
	Pinned    string
	Requested string
}

func (e *PinRefusedError) Error() string {
	return fmt.Sprintf("refused: node %s is pinned to %s; request for %s would evict it (set compute allow_model_swap for this node to allow)", e.Node, e.Pinned, e.Requested)
}

// PinRefusalMessage returns the plain-language refusal text when err is, or
// wraps, a PinRefusedError. ok is false for any other error.
func PinRefusalMessage(err error) (msg string, ok bool) {
	var refusal *PinRefusedError
	if errors.As(err, &refusal) {
		return refusal.Error(), true
	}
	return "", false
}

// HostPins applies node pins to ollama requests by the address they are sent
// to. The compute node whose address matches the host supplies the pin. A host
// that no node has as its address passes through unpinned, because no node
// carries a pin for it.
type HostPins struct {
	reg *compute.Registry
}

// NewHostPins builds a HostPins over the runtime compute registry. A nil
// registry passes every request through unchanged.
func NewHostPins(reg *compute.Registry) *HostPins {
	return &HostPins{reg: reg}
}

// ResolveModel returns the model an ollama request sent to host should run.
// requestedModel is the default for the caller and explicitOverride is a
// model the caller asked for by name; both may be empty. An empty host means
// the local ollama default. The error is a *PinRefusedError when the request
// would evict a pinned model.
func (p *HostPins) ResolveModel(host, requestedModel, explicitOverride string) (string, error) {
	if strings.TrimSpace(host) == "" {
		host = defaultOllamaHost
	}
	var node *compute.Node
	if p != nil && p.reg != nil {
		node = nodeForHost(p.reg.List(), host)
	}
	name := ""
	if node != nil {
		name = node.Name
	}
	return pinnedModel(node, name, requestedModel, explicitOverride)
}

// nodeForHost returns the node whose address equals host, or nil when none
// does. Addresses compare the way the model-conflict check compares them.
func nodeForHost(nodes []*compute.Node, host string) *compute.Node {
	want := normalizeNodeAddress(host)
	if want == "" {
		return nil
	}
	for _, n := range nodes {
		if normalizeNodeAddress(n.Address) == want {
			return n
		}
	}
	return nil
}
